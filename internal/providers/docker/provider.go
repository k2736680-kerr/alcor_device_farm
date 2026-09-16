package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

const (
	labelManaged        = "io.alcor.device-farm.managed"
	labelProviderRef    = "io.alcor.device-farm.provider-ref"
	labelHostID         = "io.alcor.device-farm.host-id"
	labelDeviceID       = "io.alcor.device-farm.device-id"
	labelImageID        = "io.alcor.device-farm.image-id"
	labelRuntimeImage   = "io.alcor.device-farm.runtime-image"
	labelGeneration     = "io.alcor.device-farm.generation"
	labelCapabilities   = "io.alcor.device-farm.capabilities"
	labelRuntimeProfile = "io.alcor.device-farm.runtime-profile"
)

type AppiumProbe interface {
	Healthy(context.Context, providers.ConnectionInfo) (bool, error)
}

type Config struct {
	Binary              string
	Image               string
	AdvertiseHost       string
	BindAddress         string
	KVMDevice           string
	RenderDevice        string
	ContainerADBPort    int
	ContainerAppiumPort int
	ContainerADBSerial  string
	ADBPath             string
	DataMountPath       string
	CPUs                float64
	Memory              string
	PidsLimit           int
	Environment         map[string]string
	AppiumProbe         AppiumProbe
}

type Provider struct {
	config       Config
	backend      backend
	gpuDevice    string
	renderDevice string
}

func New(ctx context.Context, config Config) (*Provider, error) {
	config = withDefaults(config)
	return newProvider(ctx, config, newCLIBackend(config.Binary, nil), systemHostProbe{})
}

func newProvider(ctx context.Context, config Config, docker backend, host hostProbe) (*Provider, error) {
	config = withDefaults(config)
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if docker == nil || host == nil {
		return nil, errors.New("docker backend and host probe are required")
	}
	if err := host.ValidateKVM(config.KVMDevice); err != nil {
		return nil, providerError(providers.OperationDiscover, "KVM_UNAVAILABLE", "Linux KVM is required", false, err)
	}
	if err := docker.Ping(ctx); err != nil {
		return nil, providerError(providers.OperationDiscover, "DOCKER_UNAVAILABLE", "Docker Engine is unavailable", true, err)
	}
	renderDevice := ""
	gpuDevice := ""
	if host.RenderDeviceAvailable(config.RenderDevice) {
		renderDevice = config.RenderDevice
		candidate := filepath.Join(filepath.Dir(config.RenderDevice), "card0")
		if host.RenderDeviceAvailable(candidate) {
			gpuDevice = candidate
		}
	}
	return &Provider{config: config, backend: docker, gpuDevice: gpuDevice, renderDevice: renderDevice}, nil
}

func (provider *Provider) Discover(ctx context.Context, hostID string) ([]providers.Snapshot, error) {
	if strings.TrimSpace(hostID) == "" {
		return nil, providerError(providers.OperationDiscover, "INVALID_ARGUMENT", "host ID is required", false, nil)
	}
	containers, err := provider.backend.ListContainers(ctx, map[string]string{labelManaged: "true", labelHostID: hostID})
	if err != nil {
		return nil, providerError(providers.OperationDiscover, "DOCKER_DISCOVER_FAILED", "cannot list managed emulator containers", true, err)
	}
	result := make([]providers.Snapshot, 0, len(containers))
	for _, value := range containers {
		snapshot, err := provider.snapshot(value)
		if err != nil {
			return nil, providerError(providers.OperationDiscover, "DOCKER_METADATA_INVALID", "managed container metadata is invalid", false, err)
		}
		if snapshot.State == providers.StateRunning {
			health, _ := provider.inspectContainerHealth(ctx, value)
			snapshot.Health = health
		}
		result = append(result, snapshot)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ProviderRef < result[right].ProviderRef })
	return result, nil
}

func (provider *Provider) VerifyImageDigest(ctx context.Context, runtimeImage, expected string) error {
	runtimeImage = strings.TrimSpace(runtimeImage)
	if !providers.ValidRuntimeImageReference(runtimeImage) {
		return providerError(providers.OperationValidateImage, "INVALID_IMAGE_REFERENCE", "runtime image must use a fixed tag or digest", false, nil)
	}
	expected = strings.ToLower(strings.TrimSpace(expected))
	if !validSHA256Digest(expected) {
		return providerError(providers.OperationValidateImage, "INVALID_IMAGE_DIGEST", "expected image digest must be sha256", false, nil)
	}
	metadata, err := provider.backend.InspectImage(ctx, runtimeImage)
	if errors.Is(err, errNotFound) {
		return providerError(providers.OperationValidateImage, "IMAGE_NOT_FOUND", "selected emulator image is not present on the host", false, err)
	}
	if err != nil {
		return providerError(providers.OperationValidateImage, "IMAGE_INSPECT_FAILED", "cannot inspect selected emulator image", true, err)
	}
	if strings.ToLower(metadata.ID) == expected {
		return nil
	}
	for _, value := range metadata.RepoDigests {
		if _, digest, ok := strings.Cut(value, "@"); ok && strings.ToLower(digest) == expected {
			return nil
		}
	}
	return providerError(providers.OperationValidateImage, "IMAGE_DIGEST_MISMATCH", "selected emulator image does not match the registered digest", false, nil)
}

func (provider *Provider) Create(ctx context.Context, request providers.CreateRequest) (providers.Snapshot, error) {
	return provider.create(ctx, request, 1)
}

func (provider *Provider) create(ctx context.Context, request providers.CreateRequest, generation int) (providers.Snapshot, error) {
	return provider.createWithOptions(ctx, request, generation, false)
}

func (provider *Provider) createWithOptions(ctx context.Context, request providers.CreateRequest, generation int, preserveResources bool) (providers.Snapshot, error) {
	if request.RuntimeProfile == (runtimeprofile.Profile{}) {
		request.RuntimeProfile = runtimeprofile.Default()
	}
	runtimeImage := strings.TrimSpace(request.RuntimeImage)
	if runtimeImage == "" {
		runtimeImage = strings.TrimSpace(provider.config.Image)
	}
	request.RuntimeImage = runtimeImage
	if err := validateCreateRequest(request); err != nil {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", err.Error(), false, nil)
	}
	name, networkName, volumeName := resourceNames(request.ProviderRef)
	existing, err := provider.backend.InspectContainer(ctx, name)
	if err == nil {
		if existing.Labels[labelProviderRef] != request.ProviderRef || existing.Labels[labelDeviceID] != request.DeviceID ||
			existing.Labels[labelHostID] != request.HostID || existing.Labels[labelImageID] != request.ImageID ||
			existing.Labels[labelRuntimeImage] != runtimeImage {
			return providers.Snapshot{}, providerError(providers.OperationCreate, "PROVIDER_REF_CONFLICT", "container name belongs to another device", false, nil)
		}
		return provider.snapshot(existing)
	}
	if !errors.Is(err, errNotFound) {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "cannot inspect existing emulator", true, err)
	}
	if !preserveResources {
		if err := provider.delete(ctx, request.ProviderRef); err != nil {
			return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "cannot clean stale emulator resources", true, err)
		}
	}

	capabilities, err := json.Marshal(request.Capabilities)
	if err != nil {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", "capabilities cannot be encoded", false, err)
	}
	encodedProfile, err := json.Marshal(request.RuntimeProfile)
	if err != nil {
		return providers.Snapshot{}, providerError(providers.OperationCreate, "INVALID_ARGUMENT", "runtime profile cannot be encoded", false, err)
	}
	labels := map[string]string{
		labelManaged: "true", labelProviderRef: request.ProviderRef, labelHostID: request.HostID,
		labelDeviceID: request.DeviceID, labelImageID: request.ImageID, labelRuntimeImage: runtimeImage,
		labelGeneration: strconv.Itoa(generation), labelCapabilities: string(capabilities), labelRuntimeProfile: string(encodedProfile),
	}
	resourceLabels := map[string]string{labelManaged: "true", labelProviderRef: request.ProviderRef, labelHostID: request.HostID}
	if !preserveResources {
		if err := provider.backend.CreateNetwork(ctx, networkName, resourceLabels); err != nil {
			return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "cannot create emulator network", true, err)
		}
		if err := provider.backend.CreateVolume(ctx, volumeName, resourceLabels); err != nil {
			provider.cleanup(request.ProviderRef)
			return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "cannot create emulator data volume", true, err)
		}
	}
	environment := cloneStringMap(provider.config.Environment)
	graphics, renderDevice, err := provider.resolveGraphics(request.RuntimeProfile.Graphics)
	if err != nil {
		provider.cleanupCreateFailure(ctx, request.ProviderRef, preserveResources)
		return providers.Snapshot{}, err
	}
	applyRuntimeEnvironment(environment, request.RuntimeProfile, graphics)
	if avdDevice, ok := request.Capabilities["avd_device"].(string); ok && strings.TrimSpace(avdDevice) != "" {
		environment["EMULATOR_DEVICE"] = strings.TrimSpace(avdDevice)
	}
	err = provider.backend.CreateContainer(ctx, containerSpec{
		Name: name, Hostname: name, Image: runtimeImage, Network: networkName, Volume: volumeName,
		DataMountPath: provider.config.DataMountPath, KVMDevice: provider.config.KVMDevice, GPUDevice: provider.gpuDevice, RenderDevice: renderDevice,
		BindAddress: provider.config.BindAddress, ContainerADBPort: provider.config.ContainerADBPort,
		ContainerAppiumPort: provider.config.ContainerAppiumPort,
		CPUs:                request.RuntimeProfile.ContainerCPUCores, Memory: fmt.Sprintf("%dm", request.RuntimeProfile.ContainerMemoryMB), PidsLimit: provider.config.PidsLimit,
		Labels: labels, Environment: environment,
	})
	if err != nil {
		provider.cleanupCreateFailure(ctx, request.ProviderRef, preserveResources)
		return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "cannot create emulator container", true, err)
	}
	created, err := provider.backend.InspectContainer(ctx, name)
	if err != nil {
		provider.cleanupCreateFailure(ctx, request.ProviderRef, preserveResources)
		return providers.Snapshot{}, providerError(providers.OperationCreate, "EMULATOR_CREATE_FAILED", "cannot inspect created emulator", true, err)
	}
	return provider.snapshot(created)
}

func (provider *Provider) cleanupCreateFailure(ctx context.Context, providerRef string, preserveResources bool) {
	if !preserveResources {
		provider.cleanup(providerRef)
		return
	}
	name, _, _ := resourceNames(providerRef)
	_ = provider.backend.RemoveContainer(ctx, name)
}

func (provider *Provider) Start(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	snapshot, err := provider.changeState(ctx, providers.OperationStart, providerRef, provider.backend.StartContainer)
	if err != nil {
		return snapshot, err
	}
	name, _, _ := resourceNames(providerRef)
	provider.ensureAVDGuard(ctx, name)
	return snapshot, nil
}

func (provider *Provider) Stop(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	return provider.changeState(ctx, providers.OperationStop, providerRef, provider.backend.StopContainer)
}

func (provider *Provider) Restart(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	snapshot, err := provider.changeState(ctx, providers.OperationRestart, providerRef, provider.backend.RestartContainer)
	if err != nil {
		return snapshot, err
	}
	name, _, _ := resourceNames(providerRef)
	provider.ensureAVDGuard(ctx, name)
	return snapshot, nil
}

// RestartWithProfile replaces only the container while retaining its network
// and data volume, so a changed runtime profile can be applied non-destructively.
func (provider *Provider) RestartWithProfile(ctx context.Context, providerRef string, profile runtimeprofile.Profile) (providers.Snapshot, error) {
	value, err := provider.find(ctx, providers.OperationRestart, providerRef)
	if err != nil {
		return providers.Snapshot{}, err
	}
	request, generation, err := requestFromContainer(value)
	if err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRestart, "DOCKER_METADATA_INVALID", "cannot read emulator metadata", false, err)
	}
	if err := profile.Validate(); err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRestart, "INVALID_RUNTIME_PROFILE", err.Error(), false, err)
	}
	if value.State == "running" {
		_, flushErr := provider.backend.Exec(ctx, value.Name, provider.adbArgs("shell", "sync")...)
		if flushErr != nil && provider.adbReachable(ctx, value.Name) {
			// 运行时仍在线：冲刷失败可能只是 adb 瞬时抖动，维持原保护
			// 行为（中止重启），避免白白丢掉容器数据卷里的用户状态。
			return providers.Snapshot{}, providerError(providers.OperationRestart, "RUNTIME_PROFILE_DATA_FLUSH_FAILED", "cannot flush emulator data before replacing its container", false, flushErr)
		}
		// 冲刷成功，或运行时已僵死（ADB 不可达且不会自愈）：僵死场景下
		// 继续中止只会把设备永久卡在隔离态、必须人工登宿主机重启容器，
		// 因此放弃冲刷直接硬替换。
		if err := provider.backend.StopContainer(ctx, value.Name); err != nil {
			return providers.Snapshot{}, providerError(providers.OperationRestart, "EMULATOR_OPERATION_FAILED", "cannot stop emulator before replacing its container", true, err)
		}
	}
	request.RuntimeProfile = profile
	if err := provider.backend.RemoveContainer(ctx, value.Name); err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRestart, "EMULATOR_OPERATION_FAILED", "cannot replace emulator container", true, err)
	}
	created, err := provider.createWithOptions(ctx, request, generation+1, true)
	if err != nil {
		targetErr := err
		restored, restoreErr := provider.createWithOptions(ctx, requestFromProfile(request, value), generation+1, true)
		if restoreErr != nil {
			return providers.Snapshot{}, providerError(providers.OperationRestart, "RUNTIME_PROFILE_ROLLBACK_FAILED", "cannot restore the previous emulator container after target creation failed", false, errors.Join(targetErr, restoreErr))
		}
		name, _, _ := resourceNames(restored.ProviderRef)
		if restoreErr = provider.backend.StartContainer(ctx, name); restoreErr == nil {
			provider.ensureAVDGuard(ctx, name)
			value, restoreErr = provider.backend.InspectContainer(ctx, name)
			if restoreErr == nil {
				restored, restoreErr = provider.snapshot(value)
			}
		}
		return restored, providerError(providers.OperationRestart, "RUNTIME_PROFILE_TARGET_CREATE_FAILED_ROLLBACK_STARTED", "target container creation failed and the previous container was restored", false, errors.Join(targetErr, restoreErr))
	}
	name, _, _ := resourceNames(created.ProviderRef)
	if err := provider.backend.StartContainer(ctx, name); err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRestart, "EMULATOR_OPERATION_FAILED", "cannot start replaced emulator container", true, err)
	}
	provider.ensureAVDGuard(ctx, name)
	value, err = provider.backend.InspectContainer(ctx, name)
	if err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRestart, "EMULATOR_INSPECT_FAILED", "cannot inspect replaced emulator", true, err)
	}
	return provider.snapshot(value)
}

func requestFromProfile(request providers.CreateRequest, previous container) providers.CreateRequest {
	previousRequest, _, err := requestFromContainer(previous)
	if err == nil {
		return previousRequest
	}
	return request
}

func (provider *Provider) Rebuild(ctx context.Context, providerRef string) (providers.Snapshot, error) {
	value, err := provider.find(ctx, providers.OperationRebuild, providerRef)
	if err != nil {
		return providers.Snapshot{}, err
	}
	request, generation, err := requestFromContainer(value)
	if err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRebuild, "DOCKER_METADATA_INVALID", "cannot rebuild from container metadata", false, err)
	}
	if err := provider.delete(ctx, providerRef); err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRebuild, "EMULATOR_REBUILD_FAILED", "cannot remove previous emulator", true, err)
	}
	created, err := provider.create(ctx, request, generation+1)
	if err != nil {
		return providers.Snapshot{}, err
	}
	started, err := provider.Start(ctx, created.ProviderRef)
	if err != nil {
		return providers.Snapshot{}, providerError(providers.OperationRebuild, "EMULATOR_REBUILD_FAILED", "cannot start rebuilt emulator", true, err)
	}
	return started, nil
}

func (provider *Provider) Delete(ctx context.Context, providerRef string) error {
	if strings.TrimSpace(providerRef) == "" {
		return providerError(providers.OperationDelete, "INVALID_ARGUMENT", "provider ref is required", false, nil)
	}
	if err := provider.delete(ctx, providerRef); err != nil {
		return providerError(providers.OperationDelete, "EMULATOR_DELETE_FAILED", "cannot delete emulator resources", true, err)
	}
	return nil
}

func (provider *Provider) delete(ctx context.Context, providerRef string) error {
	name, _, _ := resourceNames(providerRef)
	if err := provider.backend.RemoveContainer(ctx, name); err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	labels := map[string]string{labelManaged: "true", labelProviderRef: providerRef}
	if err := provider.backend.RemoveNetworks(ctx, labels); err != nil {
		return err
	}
	return provider.backend.RemoveVolumes(ctx, labels)
}

func (provider *Provider) InspectHealth(ctx context.Context, providerRef string) (providers.Health, error) {
	value, err := provider.find(ctx, providers.OperationInspectHealth, providerRef)
	if err != nil {
		return providers.Health{}, err
	}
	return provider.inspectContainerHealth(ctx, value)
}

func (provider *Provider) GetConnectionInfo(ctx context.Context, providerRef string) (providers.ConnectionInfo, error) {
	value, err := provider.find(ctx, providers.OperationConnectionInfo, providerRef)
	if err != nil {
		return providers.ConnectionInfo{}, err
	}
	connection, err := provider.connection(value)
	if err != nil {
		return connection, providerError(providers.OperationConnectionInfo, "DEVICE_ENDPOINT_UNAVAILABLE", "emulator ADB or Appium port is not published", true, err)
	}
	return connection, nil
}

func (provider *Provider) changeState(
	ctx context.Context,
	operation providers.Operation,
	providerRef string,
	change func(context.Context, string) error,
) (providers.Snapshot, error) {
	if strings.TrimSpace(providerRef) == "" {
		return providers.Snapshot{}, providerError(operation, "INVALID_ARGUMENT", "provider ref is required", false, nil)
	}
	name, _, _ := resourceNames(providerRef)
	if err := change(ctx, name); err != nil {
		code := "EMULATOR_OPERATION_FAILED"
		if errors.Is(err, errNotFound) {
			code = "PROVIDER_DEVICE_NOT_FOUND"
		}
		return providers.Snapshot{}, providerError(operation, code, "Docker container state change failed", true, err)
	}
	value, err := provider.backend.InspectContainer(ctx, name)
	if err != nil {
		return providers.Snapshot{}, providerError(operation, "EMULATOR_INSPECT_FAILED", "cannot inspect emulator after state change", true, err)
	}
	return provider.snapshot(value)
}

func (provider *Provider) find(ctx context.Context, operation providers.Operation, providerRef string) (container, error) {
	if strings.TrimSpace(providerRef) == "" {
		return container{}, providerError(operation, "INVALID_ARGUMENT", "provider ref is required", false, nil)
	}
	name, _, _ := resourceNames(providerRef)
	value, err := provider.backend.InspectContainer(ctx, name)
	if errors.Is(err, errNotFound) {
		return container{}, providerError(operation, "PROVIDER_DEVICE_NOT_FOUND", "emulator container does not exist", false, err)
	}
	if err != nil {
		return container{}, providerError(operation, "EMULATOR_INSPECT_FAILED", "cannot inspect emulator container", true, err)
	}
	if value.Labels[labelManaged] != "true" || value.Labels[labelProviderRef] != providerRef {
		return container{}, providerError(operation, "PROVIDER_REF_CONFLICT", "container is not managed by this provider", false, nil)
	}
	return value, nil
}

func (provider *Provider) inspectContainerHealth(ctx context.Context, value container) (providers.Health, error) {
	health := providers.Health{Platform: providers.PlatformAndroid, Online: value.State == "running"}
	if value.OOMKilled {
		return health, providerError(providers.OperationInspectHealth, "EMULATOR_OOM_KILLED", "emulator container was OOM-killed", true, nil)
	}
	if !health.Online {
		return health, providerError(providers.OperationInspectHealth, "DEVICE_NOT_RUNNING", "emulator container is not running", true, nil)
	}
	adbArgs := provider.adbArgs("get-state")
	state, err := provider.backend.Exec(ctx, value.Name, adbArgs...)
	if err != nil || strings.TrimSpace(state) != "device" {
		return health, providerError(providers.OperationInspectHealth, "ADB_OFFLINE", "emulator ADB is not online", true, err)
	}
	health.ADBOnline = true
	bootArgs := provider.adbArgs("shell", "getprop", "sys.boot_completed")
	boot, err := provider.backend.Exec(ctx, value.Name, bootArgs...)
	if err != nil || strings.TrimSpace(boot) != "1" {
		return health, providerError(providers.OperationInspectHealth, "DEVICE_BOOTING", "Android boot has not completed", true, err)
	}
	health.BootCompleted = true
	if provider.config.AppiumProbe != nil {
		connection, connectionErr := provider.connection(value)
		if connectionErr != nil {
			return health, providerError(providers.OperationInspectHealth, "DEVICE_ENDPOINT_UNAVAILABLE", "emulator connection is unavailable", true, connectionErr)
		}
		health.AppiumHealthy, err = provider.config.AppiumProbe.Healthy(ctx, connection)
		if err != nil {
			return health, providerError(providers.OperationInspectHealth, "APPIUM_UNHEALTHY", "Appium health probe failed", true, err)
		}
	}
	return health, nil
}

func (provider *Provider) adbArgs(args ...string) []string {
	result := []string{provider.config.ADBPath}
	if provider.config.ContainerADBSerial != "" {
		result = append(result, "-s", provider.config.ContainerADBSerial)
	}
	return append(result, args...)
}

// adbReachable 判断容器内安卓运行时是否仍然可达。运行时僵死（qemu 崩溃、
// adbd 无响应）时返回 false，此时冲刷/健康检查都不可能成功。
func (provider *Provider) adbReachable(ctx context.Context, name string) bool {
	state, err := provider.backend.Exec(ctx, name, provider.adbArgs("get-state")...)
	return err == nil && strings.TrimSpace(state) == "device"
}

// ensureAVDGuard 尽力在运行中的容器内安装 AVD 守护补丁（内容见 avdGuardScript）。
// 刻意不把失败向上传播：补丁装在设备持久卷上，一次安装覆盖之后所有启动，
// 错过本次启动的下一个启动路径（自愈重启、下次 Start）会再次尝试；设备异常
// 由健康检查与隔离机制兜底暴露。
func (provider *Provider) ensureAVDGuard(ctx context.Context, name string) {
	_, _ = provider.backend.Exec(ctx, name, "sh", "-c", avdGuardScript)
}

// avdGuardScript 在容器内执行，把守护逻辑写进持久卷上的镜像启动脚本
// （docker-android 的 run.sh，随 /home/androidusr 数据卷持久化），并为当前这
// 次启动立即生效。背景（2026-09-15 事故）：
//
//  1. docker-android 的 is_initialized() 以持久卷上 config.ini 的
//     hw.device.name 是否匹配 EMULATOR_DEVICE 为准，匹配时跳过 avdmanager
//     创建流程。但 avdmanager 实际写入的是设备模板 id（pixel_9），与
//     EMULATOR_DEVICE（Pixel 9）永不匹配 → 每次启动都重建 AVD 并附加
//     -wipe-data，用户安装的 APK 与数据全部丢失。
//  2. 手工把标记修正为 "Pixel 9" 后，AVD 根 ini（/root/.android/avd/<avd>.ini）
//     只存在于容器临时层——硬重启回退/换 profile 会重建容器，临时层随之丢失，
//     镜像又因标记已初始化而永远不再创建它 → qemu 报 "Unknown AVD name" 秒退。
//  3. qemu 崩溃（而非干净退出）会在持久卷 AVD 目录留下陈旧的多实例/PID 锁
//     文件；容器重启后 PID 空间重新编号，陈旧 PID 恰好指向新容器里的活进程，
//     下次启动报 "Running multiple emulators with the same AVD" 拒绝运行。
//  4. docker-android 的 create() 调 avdmanager create avd -p <持久卷 AVD 目录>
//     重建 AVD，而 avdmanager 在指定 -p 时不会写根 ini；全新数据卷的首次启动
//     因此必然死于 "Unknown AVD name"（重建/扩容的每条命令尝试都是全新卷）。
//     在 emulator.py 的 deploy() 里、qemu 启动前补写根 ini 兜底。
//
// 守护逻辑在每次启动的最早期（镜像启动脚本开头）修复前三点：写入指向持久卷
// AVD 目录的根 ini、把标记归一化为 EMULATOR_DEVICE、清理陈旧锁文件（本系统
// 单容器独占 AVD 数据卷，启动时刻不存在合法的第二模拟器实例）。首次启动
// （卷上尚无 config.ini）不动标记，走镜像正常创建流程，根 ini 由第 4 条补丁
// 在 qemu 启动前写入；exec 时机早于 python 导入 emulator.py，补丁对本次启动
// 即生效。
const avdGuardScript = `AVD_GUARD_BODY=/tmp/alcor-avd-guard-body.sh
cat > "$AVD_GUARD_BODY" <<'AVD_GUARD_EOF'
# alcor-avd-guard: installed by alcor-device-farm provider (internal/providers/docker/provider.go).
# 1) The AVD root ini lives in the ephemeral /root/.android/avd and disappears
#    when the container is recreated, while docker-android skips AVD creation
#    once its persistent marker claims initialization -> qemu dies with
#    "Unknown AVD name" on every boot. Rewrite the root ini on every boot.
# 2) avdmanager writes hw.device.name=<device template id> (e.g. pixel_9) but
#    docker-android matches EMULATOR_DEVICE (e.g. Pixel 9), so every boot
#    recreated the AVD with -wipe-data and destroyed user data. Normalize the
#    marker to EMULATOR_DEVICE before the emulator checks it.
# 3) A crashed qemu leaves stale multiinstance/PID lock files in the persistent
#    AVD dir; with a recycled in-container PID the next boot aborts with
#    "Running multiple emulators with the same AVD". Clear them at boot.
if [ -n "${EMULATOR_DEVICE:-}" ]; then
    DATA_HOME="${WORK_PATH:-/home/androidusr}"
    AVD_NAME="$(printf '%s' "$EMULATOR_DEVICE" | tr ' ' '_' | tr '[:upper:]' '[:lower:]')_${EMULATOR_ANDROID_VERSION:-15.0}"
    AVD_DIR="${ANDROID_AVD_HOME:-/root/.android/avd}"
    mkdir -p "$AVD_DIR"
    printf 'avd.ini.encoding=UTF-8\npath=%s/emulator\ntarget=android-35\n' "$DATA_HOME" > "$AVD_DIR/$AVD_NAME.ini"
    rm -f "$DATA_HOME/emulator/multiinstance.lock" "$DATA_HOME/emulator/hardware-qemu.ini.lock" || true
    if [ -f "$DATA_HOME/emulator/config.ini" ]; then
        sed -i "s/^hw\.device\.name.*/hw.device.name = $EMULATOR_DEVICE/" "$DATA_HOME/emulator/config.ini" || true
    fi
fi
AVD_GUARD_EOF
sh "$AVD_GUARD_BODY" || true
RUN_SH="${WORK_PATH:-/home/androidusr}/docker-android/mixins/scripts/run.sh"
if [ -f "$RUN_SH" ] && ! grep -q "alcor-avd-guard" "$RUN_SH"; then
    sed -i "1r $AVD_GUARD_BODY" "$RUN_SH" || true
fi
EMU_PY="${WORK_PATH:-/home/androidusr}/docker-android/cli/src/device/emulator.py"
if [ -f "$EMU_PY" ] && ! grep -q "alcor-avd-rootini" "$EMU_PY"; then
    python3 - "$EMU_PY" <<'AVD_ROOTINI_EOF'
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as f:
    src = f.read()
anchor = "        subprocess.Popen(start_cmd.split())"
if anchor not in src:
    raise SystemExit(0)
fix = (
    "        # alcor-avd-rootini-fix: avdmanager create with -p never writes the\n"
    "        # AVD root ini, so the first boot on a fresh volume dies with\n"
    "        # \"Unknown AVD name\" before qemu starts. Write it right before launch.\n"
    "        try:\n"
    "            _avd_home = os.environ.get(\"ANDROID_AVD_HOME\") or os.path.join(os.path.expanduser(\"~\"), \".android\", \"avd\")\n"
    "            os.makedirs(_avd_home, exist_ok=True)\n"
    "            with open(os.path.join(_avd_home, self.name + \".ini\"), \"w\") as _ini:\n"
    "                _ini.write(\"avd.ini.encoding=UTF-8\\npath=%s\\ntarget=android-%s\\n\" % (self.path_emulator, self.api_level))\n"
    "        except Exception as _e:\n"
    "            self.logger.warning(\"alcor-avd-rootini failed: %s\", _e)\n"
)
with open(path, "w", encoding="utf-8") as f:
    f.write(src.replace(anchor, fix + anchor, 1))
AVD_ROOTINI_EOF
fi
rm -f "$AVD_GUARD_BODY"
`

func (provider *Provider) snapshot(value container) (providers.Snapshot, error) {
	generation, err := strconv.Atoi(value.Labels[labelGeneration])
	if err != nil || generation < 1 {
		return providers.Snapshot{}, errors.New("invalid generation label")
	}
	capabilities := map[string]any{}
	if raw := value.Labels[labelCapabilities]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
			return providers.Snapshot{}, err
		}
	}
	profileValues := map[string]any{}
	if raw := value.Labels[labelRuntimeProfile]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &profileValues); err != nil {
			return providers.Snapshot{}, err
		}
	}
	runtimeProfile, err := runtimeprofile.Parse(profileValues)
	if err != nil {
		return providers.Snapshot{}, err
	}
	connection, _ := provider.connection(value)
	state := providers.StateStopped
	switch value.State {
	case "created":
		state = providers.StateCreated
	case "running", "restarting":
		state = providers.StateRunning
	}
	return providers.Snapshot{
		DeviceID: value.Labels[labelDeviceID], HostID: value.Labels[labelHostID], ImageID: value.Labels[labelImageID],
		Platform:    providers.PlatformAndroid,
		ProviderRef: value.Labels[labelProviderRef], State: state, Generation: generation,
		Capabilities: capabilities, RuntimeProfile: runtimeProfile,
		Health: providers.Health{Platform: providers.PlatformAndroid, Online: state == providers.StateRunning}, Connection: connection,
	}, nil
}

func (provider *Provider) connection(value container) (providers.ConnectionInfo, error) {
	connection := providers.ConnectionInfo{Platform: providers.PlatformAndroid, ProviderID: value.Labels[labelProviderRef]}
	adbHostPort := value.Ports[provider.config.ContainerADBPort]
	if adbHostPort == 0 {
		return connection, errors.New("ADB host port is missing")
	}
	adbEndpoint := net.JoinHostPort(provider.config.AdvertiseHost, strconv.Itoa(adbHostPort))
	connection.Serial, connection.ADBEndpoint = adbEndpoint, adbEndpoint
	appiumHostPort := value.Ports[provider.config.ContainerAppiumPort]
	if appiumHostPort == 0 {
		return connection, errors.New("Appium host port is missing")
	}
	connection.AppiumEndpoint = "http://" + net.JoinHostPort(provider.config.AdvertiseHost, strconv.Itoa(appiumHostPort))
	connection.AppiumUDID = provider.config.ContainerADBSerial
	connection.DeviceUDID = connection.AppiumUDID
	return connection, nil
}

func (provider *Provider) cleanup(providerRef string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = provider.delete(ctx, providerRef)
}

func requestFromContainer(value container) (providers.CreateRequest, int, error) {
	generation, err := strconv.Atoi(value.Labels[labelGeneration])
	if err != nil || generation < 1 {
		return providers.CreateRequest{}, 0, errors.New("invalid generation label")
	}
	capabilities := map[string]any{}
	if raw := value.Labels[labelCapabilities]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
			return providers.CreateRequest{}, 0, err
		}
	}
	profileValues := map[string]any{}
	if raw := value.Labels[labelRuntimeProfile]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &profileValues); err != nil {
			return providers.CreateRequest{}, 0, err
		}
	}
	runtimeProfile, err := runtimeprofile.Parse(profileValues)
	if err != nil {
		return providers.CreateRequest{}, 0, err
	}
	request := providers.CreateRequest{
		DeviceID: value.Labels[labelDeviceID], HostID: value.Labels[labelHostID], ImageID: value.Labels[labelImageID],
		RuntimeImage: value.Labels[labelRuntimeImage], ProviderRef: value.Labels[labelProviderRef], Capabilities: capabilities,
		RuntimeProfile: runtimeProfile,
	}
	if err := validateCreateRequest(request); err != nil {
		return providers.CreateRequest{}, 0, err
	}
	return request, generation, nil
}

func resourceNames(providerRef string) (string, string, string) {
	digest := sha256.Sum256([]byte(providerRef))
	suffix := hex.EncodeToString(digest[:4])
	slug := strings.Map(func(value rune) rune {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' {
			return unicode.ToLower(value)
		}
		return '-'
	}, providerRef)
	slug = strings.Trim(slug, "-")
	if len(slug) > 32 {
		slug = slug[:32]
	}
	if slug == "" {
		slug = "device"
	}
	stem := "alcor-df-" + slug + "-" + suffix
	return stem, stem + "-net", stem + "-data"
}

func withDefaults(config Config) Config {
	if config.Binary == "" {
		config.Binary = "docker"
	}
	if config.BindAddress == "" {
		config.BindAddress = "127.0.0.1"
	}
	if config.KVMDevice == "" {
		config.KVMDevice = "/dev/kvm"
	}
	if config.RenderDevice == "" {
		config.RenderDevice = "/dev/dri/renderD128"
	}
	if config.ContainerADBPort == 0 {
		config.ContainerADBPort = 5555
	}
	if config.ContainerAppiumPort == 0 {
		config.ContainerAppiumPort = 4723
	}
	if config.ContainerADBSerial == "" {
		config.ContainerADBSerial = "emulator-5554"
	}
	if config.ADBPath == "" {
		config.ADBPath = "adb"
	}
	if config.DataMountPath == "" {
		config.DataMountPath = "/home/androidusr"
	}
	if config.CPUs == 0 {
		config.CPUs = 4
	}
	if config.Memory == "" {
		config.Memory = "5g"
	}
	if config.PidsLimit == 0 {
		config.PidsLimit = 512
	}
	return config
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Image) != "" && !providers.ValidRuntimeImageReference(config.Image) {
		return errors.New("default Docker emulator image must use a fixed tag or digest and cannot use latest")
	}
	if strings.TrimSpace(config.AdvertiseHost) == "" {
		return errors.New("docker advertise host is required")
	}
	if net.ParseIP(config.BindAddress) == nil {
		return errors.New("docker bind address must be an IP address")
	}
	if config.ContainerADBPort < 1 || config.ContainerADBPort > 65535 || config.ContainerAppiumPort < 1 || config.ContainerAppiumPort > 65535 ||
		config.ContainerADBPort == config.ContainerAppiumPort || strings.TrimSpace(config.ContainerADBSerial) == "" ||
		strings.TrimSpace(config.ADBPath) == "" || !path.IsAbs(config.KVMDevice) || !path.IsAbs(config.RenderDevice) || config.CPUs <= 0 ||
		strings.TrimSpace(config.Memory) == "" || config.PidsLimit < 1 || !path.IsAbs(config.DataMountPath) {
		return errors.New("invalid Docker emulator resource configuration")
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validateCreateRequest(request providers.CreateRequest) error {
	if strings.TrimSpace(request.DeviceID) == "" || strings.TrimSpace(request.HostID) == "" ||
		strings.TrimSpace(request.ImageID) == "" || strings.TrimSpace(request.ProviderRef) == "" ||
		!providers.ValidRuntimeImageReference(request.RuntimeImage) {
		return errors.New("device, host, image, fixed runtime image and provider ref are required")
	}
	if len(request.ProviderRef) > 128 {
		return errors.New("provider ref is too long")
	}
	if err := request.RuntimeProfile.Validate(); err != nil {
		return err
	}
	return nil
}

func (provider *Provider) resolveGraphics(requested string) (string, string, error) {
	switch requested {
	case runtimeprofile.GraphicsHost:
		if provider.renderDevice == "" {
			return "", "", providerError(providers.OperationCreate, "GPU_RENDER_UNAVAILABLE", "host graphics requires an available render device", false, nil)
		}
		return "host", provider.renderDevice, nil
	case runtimeprofile.GraphicsSoftware:
		return "swiftshader_indirect", "", nil
	default:
		// A render node only proves that the kernel device exists. Headless
		// Emulator host rendering can still fail before ADB (driver/EGL/display
		// compatibility). Keep auto safe and deterministic; administrators can
		// explicitly select host to opt into the mapped render devices.
		return "swiftshader_indirect", "", nil
	}
}

func applyRuntimeEnvironment(environment map[string]string, profile runtimeprofile.Profile, graphics string) {
	environment["EMULATOR_DATA_PARTITION"] = fmt.Sprintf("%dM", profile.DataDiskMB)
	environment["EMULATOR_ADDITIONAL_ARGS"] = fmt.Sprintf(
		"-no-window -no-audio -no-boot-anim -cores %d -memory %d -gpu %s -skin %dx%d -dpi-device %d -prop dalvik.vm.heapsize=%dm",
		profile.GuestCPUCores, profile.GuestMemoryMB, graphics, profile.Width, profile.Height, profile.DensityDPI, profile.VMHeapMB,
	)
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func providerError(operation providers.Operation, code, message string, retryable bool, cause error) error {
	return &providers.Error{Operation: operation, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

type systemHostProbe struct{}

func (systemHostProbe) ValidateKVM(device string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("host operating system is %s, want linux", runtime.GOOS)
	}
	info, err := os.Stat(device)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("%s is not a character device", device)
	}
	file, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("%s is not readable and writable: %w", device, err)
	}
	return file.Close()
}

func (systemHostProbe) RenderDeviceAvailable(device string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	info, err := os.Stat(device)
	return err == nil && info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice != 0
}

var _ providers.Provider = (*Provider)(nil)
