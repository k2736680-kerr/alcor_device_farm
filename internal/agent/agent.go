package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/capacity"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imageprepare"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

type Client interface {
	Heartbeat(context.Context, string, hostcommand.HeartbeatInput) error
	Claim(context.Context, string, hostcommand.ClaimInput) ([]hostcommand.Command, error)
	Extend(context.Context, string, hostcommand.LeaseExtensionInput) error
	Complete(context.Context, string, hostcommand.CompletionInput) error
}

type EndpointRegistrar interface {
	Register(context.Context, string) error
}

type CapacityProbe interface {
	Snapshot(context.Context) (map[string]any, map[string]any, error)
}

type EnvironmentProbe interface {
	Snapshot(context.Context) (map[string]any, error)
}

type Config struct {
	HostID                    string
	ProviderType              string
	HeartbeatInterval         time.Duration
	LeaseSeconds              int
	WaitSeconds               int
	Concurrency               int
	CommandTimeout            time.Duration
	ImagePrepareTimeout       time.Duration
	ShutdownTimeout           time.Duration
	EnvironmentProbeInterval  time.Duration
	EnvironmentProbeTimeout   time.Duration
	EnvironmentSnapshotMaxAge time.Duration
	Capacity                  map[string]any
	Environment               map[string]any
	CapacityProbe             CapacityProbe
	EnvironmentProbe          EnvironmentProbe
	STFADBRegistrar           EndpointRegistrar
	ImagePreparer             imageprepare.Preparer
}

type Agent struct {
	config     Config
	client     Client
	provider   providers.Provider
	registrar  EndpointRegistrar
	preparer   imageprepare.Preparer
	logger     *slog.Logger
	resourceMu sync.Mutex

	environmentMu         sync.RWMutex
	environmentSnapshot   map[string]any
	environmentSnapshotAt time.Time
	environmentProbeErr   error
}

const (
	defaultEnvironmentProbeTimeout   = 90 * time.Second
	defaultEnvironmentSnapshotMaxAge = 120 * time.Second
)

func New(config Config, client Client, provider providers.Provider, logger *slog.Logger) (*Agent, error) {
	config.ProviderType = strings.ToLower(strings.TrimSpace(config.ProviderType))
	if config.ImagePrepareTimeout <= 0 {
		config.ImagePrepareTimeout = config.CommandTimeout
	}
	if config.EnvironmentProbe != nil {
		if config.EnvironmentProbeInterval <= 0 {
			config.EnvironmentProbeInterval = config.HeartbeatInterval
		}
		if config.EnvironmentProbeTimeout <= 0 {
			config.EnvironmentProbeTimeout = defaultEnvironmentProbeTimeout
		}
		if config.EnvironmentSnapshotMaxAge <= 0 {
			config.EnvironmentSnapshotMaxAge = defaultEnvironmentSnapshotMaxAge
		}
	}
	if len(config.HostID) < 16 || client == nil || provider == nil || config.HeartbeatInterval <= 0 ||
		config.LeaseSeconds < 5 || config.LeaseSeconds > 300 || config.Concurrency < 1 ||
		config.CommandTimeout <= 0 || config.ImagePrepareTimeout <= 0 || config.ShutdownTimeout <= 0 || config.ProviderType == "" ||
		(config.EnvironmentProbe != nil && (config.EnvironmentProbeInterval <= 0 || config.EnvironmentProbeTimeout <= 0 || config.EnvironmentSnapshotMaxAge <= 0)) {
		return nil, errors.New("宿主机代理配置无效")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{config: config, client: client, provider: provider, registrar: config.STFADBRegistrar,
		preparer: config.ImagePreparer, logger: logger}, nil
}

func (agent *Agent) Run(ctx context.Context) error {
	backgroundCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	if agent.config.EnvironmentProbe != nil {
		go agent.environmentProbeLoop(backgroundCtx)
	}
	if err := agent.sendHeartbeat(ctx); err != nil && ctx.Err() == nil {
		agent.logger.Warn("宿主机代理首次心跳失败", "error", err)
	}
	heartbeatErrors := make(chan error, 1)
	go agent.heartbeatLoop(backgroundCtx, heartbeatErrors)

	semaphore := make(chan struct{}, agent.config.Concurrency)
	var workers sync.WaitGroup
	for {
		if ctx.Err() != nil {
			stopBackground()
			return waitWorkers(&workers, agent.config.ShutdownTimeout)
		}
		select {
		case err := <-heartbeatErrors:
			if err != nil {
				agent.logger.Warn("宿主机代理心跳失败", "error", err)
			}
		default:
		}
		if len(semaphore) >= cap(semaphore) {
			select {
			case <-ctx.Done():
				continue
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		commands, err := agent.client.Claim(ctx, agent.config.HostID, hostcommand.ClaimInput{
			LeaseSeconds: agent.config.LeaseSeconds, MaxCommands: cap(semaphore) - len(semaphore), WaitSeconds: agent.config.WaitSeconds,
		})
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			agent.logger.Warn("宿主机代理领取命令失败", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		for _, command := range commands {
			command := command
			semaphore <- struct{}{}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-semaphore }()
				agent.execute(ctx, command)
			}()
		}
	}
}

func (agent *Agent) environmentProbeLoop(ctx context.Context) {
	ticker := time.NewTicker(agent.config.EnvironmentProbeInterval)
	defer ticker.Stop()
	for {
		agent.refreshEnvironmentSnapshot(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (agent *Agent) refreshEnvironmentSnapshot(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, agent.config.EnvironmentProbeTimeout)
	values, err := agent.config.EnvironmentProbe.Snapshot(probeCtx)
	cancel()
	if err == nil {
		readiness, ok := values["host_readiness"].(map[string]any)
		_, readyOK := readiness["ready"].(bool)
		if !ok || !readyOK {
			err = errors.New("宿主机环境探测结果缺少就绪状态")
		}
	}
	agent.environmentMu.Lock()
	if err == nil {
		agent.environmentSnapshot = maps.Clone(values)
		agent.environmentSnapshotAt = time.Now()
		agent.environmentProbeErr = nil
	} else {
		agent.environmentProbeErr = err
	}
	agent.environmentMu.Unlock()
	if err != nil && ctx.Err() == nil {
		agent.logger.Warn("宿主机环境就绪探测失败", "error", err)
		return
	}
	if readiness, ok := values["host_readiness"].(map[string]any); ok && readiness["ready"] == false {
		agent.logger.Warn("宿主机环境未就绪", "reasons", readiness["reasons"])
	}
}

func (agent *Agent) currentEnvironmentSnapshot(now time.Time) map[string]any {
	agent.environmentMu.RLock()
	values := maps.Clone(agent.environmentSnapshot)
	completedAt := agent.environmentSnapshotAt
	probeErr := agent.environmentProbeErr
	agent.environmentMu.RUnlock()

	if values != nil && now.Sub(completedAt) <= agent.config.EnvironmentSnapshotMaxAge {
		return values
	}
	if values == nil {
		values = map[string]any{}
	}
	reason := "environment_probe_pending"
	if probeErr != nil {
		reason = "environment_probe_failed"
	} else if !completedAt.IsZero() {
		reason = "environment_probe_stale"
	}
	values["host_readiness"] = map[string]any{"ready": false, "reasons": []any{reason}}
	return values
}

func (agent *Agent) heartbeatLoop(ctx context.Context, errorsChannel chan<- error) {
	ticker := time.NewTicker(agent.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		err := agent.sendHeartbeat(ctx)
		select {
		case errorsChannel <- err:
		default:
		}
	}
}

func (agent *Agent) sendHeartbeat(ctx context.Context) error {
	snapshots, err := agent.provider.Discover(ctx, agent.config.HostID)
	if err != nil && agent.config.EnvironmentProbe == nil {
		return err
	}
	devices := make([]hostcommand.DiscoveredDevice, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Platform == providers.PlatformAndroid && snapshot.Ready() && agent.registrar != nil {
			if err := agent.registrar.Register(ctx, snapshot.Connection.ADBEndpoint); err != nil {
				agent.logger.Warn("STF ADB Endpoint 注册失败", "provider_ref", snapshot.ProviderRef, "error", err)
			}
		}
		connection := map[string]any{"appium_endpoint": snapshot.Connection.AppiumEndpoint,
			"appium_udid": snapshot.Connection.AppiumUDID}
		if snapshot.Connection.ADBEndpoint != "" {
			connection["adb_endpoint"] = snapshot.Connection.ADBEndpoint
		}
		var runtimeProfile map[string]any
		if snapshot.RuntimeProfile != (runtimeprofile.Profile{}) {
			runtimeProfile = snapshot.RuntimeProfile.Map()
		}
		components := make(map[string]string, len(snapshot.Health.Components))
		for name, status := range snapshot.Health.Components {
			components[name] = string(status)
		}
		devices = append(devices, hostcommand.DiscoveredDevice{
			ProviderRef: snapshot.ProviderRef, Serial: snapshot.Connection.Serial,
			Platform: string(snapshot.Platform), DeviceKind: snapshot.DeviceKind, ProviderType: heartbeatProviderType(agent.config.ProviderType),
			LifecycleStatus: providerLifecycle(snapshot), HealthStatus: providerHealth(snapshot),
			Connection: connection, Capabilities: snapshot.Capabilities, Components: components,
			RuntimeProfile: runtimeProfile,
		})
	}
	hostOS := runtime.GOOS
	if hostOS == "darwin" {
		hostOS = "macos"
	}
	capacity, environment := agent.config.Capacity, map[string]any{
		"provider": agent.config.ProviderType, "host_os": hostOS, "host_arch": runtime.GOARCH,
		"provider_inventory_complete": err == nil,
	}
	for key, value := range agent.config.Environment {
		environment[key] = value
	}
	if agent.preparer != nil {
		environment["image_build_agent"] = true
	}
	if agent.config.CapacityProbe != nil {
		measured, capabilities, probeErr := agent.config.CapacityProbe.Snapshot(ctx)
		if probeErr != nil {
			return probeErr
		}
		capacity = measured
		for key, value := range capabilities {
			environment[key] = value
		}
	}
	if agent.config.EnvironmentProbe != nil {
		for key, value := range agent.currentEnvironmentSnapshot(time.Now()) {
			environment[key] = value
		}
	}
	if err != nil {
		agent.logger.Warn("Provider 设备清单获取失败，宿主机将上报为未就绪", "error", err)
		environment["host_readiness"] = map[string]any{"ready": false, "reasons": []any{"provider_inventory_failed"}}
	}
	return agent.client.Heartbeat(ctx, agent.config.HostID, hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: capacity,
		Environment: environment, Devices: devices,
	})
}

func (agent *Agent) execute(parent context.Context, command hostcommand.Command) {
	timeout := agent.config.CommandTimeout
	if command.CommandType == "sync_android_catalog" || command.CommandType == "prepare_android_image" {
		timeout = agent.config.ImagePrepareTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	stopRenewal := agent.startLeaseRenewal(parent, cancel, command)
	var err error
	result := map[string]any(nil)
	providerRef, _ := command.Payload["provider_ref"].(string)
	switch command.CommandType {
	case "create":
		var snapshot providers.Snapshot
		created := false
		ready := false
		platform := providers.Platform(strings.ToLower(stringValue(command.Payload, "platform")))
		if platform == "" {
			platform = providers.PlatformAndroid
		}
		deviceKind := stringValue(command.Payload, "device_kind")
		if deviceKind == "" && platform == providers.PlatformAndroid {
			deviceKind = "emulator"
		}
		var profile runtimeprofile.Profile
		if platform == providers.PlatformAndroid {
			profile, err = profileFromPayload(command.Payload)
			if err == nil {
				err = agent.verifyRuntimeImage(ctx, command.Payload)
			}
		} else if platform != providers.PlatformIOS || deviceKind != "simulator" {
			err = &providers.Error{Operation: providers.OperationCreate, Code: "INVALID_ARGUMENT", Message: "只支持受控的 Android Emulator 或 iOS Simulator 创建", Retryable: false}
		}
		if err == nil {
			agent.resourceMu.Lock()
			if platform == providers.PlatformAndroid {
				err = agent.preflightCreate(ctx, profile, stringValue(command.Payload, "image_id"))
			}
			if err == nil {
				snapshot, err = agent.provider.Create(ctx, providers.CreateRequest{
					DeviceID: stringValue(command.Payload, "device_id"), HostID: agent.config.HostID,
					ImageID: stringValue(command.Payload, "image_id"), Platform: platform, DeviceKind: deviceKind,
					RuntimeImage: stringValue(command.Payload, "docker_image"), ProviderRef: providerRef,
					Serial: stringValue(command.Payload, "serial"), Capabilities: mapValue(command.Payload, "capabilities"), RuntimeProfile: profile,
				})
				if err == nil && snapshot.ProviderRef != "" {
					providerRef = snapshot.ProviderRef
				}
			}
			agent.resourceMu.Unlock()
			created = err == nil
		}
		if err == nil {
			snapshot, err = agent.provider.Start(ctx, providerRef)
		}
		if err == nil {
			snapshot, err = agent.waitReady(ctx, snapshot)
			ready = err == nil
		}
		if err == nil {
			err = agent.registerSTF(ctx, snapshot)
		}
		if err == nil {
			result = snapshotResult(snapshot)
		} else if created && !ready {
			if cleanupErr := agent.cleanupProvider(providerRef); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("创建失败后的模拟器清理也失败：%w", cleanupErr))
			}
		}
	case "start":
		var snapshot providers.Snapshot
		snapshot, err = agent.provider.Start(ctx, providerRef)
		if err == nil {
			result = snapshotResult(snapshot)
		}
	case "stop":
		var snapshot providers.Snapshot
		snapshot, err = agent.provider.Stop(ctx, providerRef)
		if err == nil {
			result = snapshotResult(snapshot)
		}
	case "restart":
		var snapshot providers.Snapshot
		if stringValue(command.Payload, "operation_kind") == "runtime_profile_update" {
			result, err = agent.updateRuntimeProfile(ctx, command.Payload)
		} else if rawProfile := mapValue(command.Payload, "runtime_profile"); len(rawProfile) > 0 {
			if profile, parseErr := runtimeprofile.Parse(rawProfile); parseErr != nil {
				err = parseErr
			} else if restartable, ok := agent.provider.(interface {
				RestartWithProfile(context.Context, string, runtimeprofile.Profile) (providers.Snapshot, error)
			}); ok {
				snapshot, err = restartable.RestartWithProfile(ctx, providerRef, profile)
			} else {
				snapshot, err = agent.provider.Restart(ctx, providerRef)
			}
		} else {
			snapshot, err = agent.provider.Restart(ctx, providerRef)
		}
		if err == nil && result == nil {
			snapshot, err = agent.waitReady(ctx, snapshot)
		}
		if err == nil && result == nil {
			err = agent.registerSTF(ctx, snapshot)
		}
		if err == nil && result == nil {
			result = snapshotResult(snapshot)
		}
	case "rebuild":
		var snapshot providers.Snapshot
		if stringValue(command.Payload, "operation_kind") == "reimage" {
			result, err = agent.reimage(ctx, command.Payload)
		} else if stringValue(command.Payload, "device_id") != "" && stringValue(command.Payload, "image_id") != "" {
			snapshot, err = agent.recreate(ctx, command.Payload)
		} else {
			snapshot, err = agent.provider.Rebuild(ctx, providerRef)
			if err == nil {
				snapshot, err = agent.waitReady(ctx, snapshot)
			}
		}
		if err == nil && result == nil {
			err = agent.registerSTF(ctx, snapshot)
		}
		if err == nil && result == nil {
			result = snapshotResult(snapshot)
		}
	case "delete":
		err = agent.provider.Delete(ctx, providerRef)
		if err == nil {
			result = map[string]any{"provider_ref": providerRef, "deleted": true}
		}
	case "inspect":
		var health providers.Health
		health, err = agent.provider.InspectHealth(ctx, providerRef)
		if err == nil {
			result = healthResult(health)
			result["provider_ref"] = providerRef
		}
	case "validate_image":
		result, err = agent.validateImage(ctx, command.Payload)
	case "sync_android_catalog":
		if agent.preparer == nil {
			err = errors.New("当前宿主机代理未启用镜像准备功能")
		} else {
			result, err = agent.preparer.SyncCatalog(ctx)
		}
	case "prepare_android_image":
		if agent.preparer == nil {
			err = errors.New("当前宿主机代理未启用镜像准备功能")
		} else {
			result, err = agent.preparer.Prepare(ctx, stringValue(command.Payload, "package_name"), stringValue(command.Payload, "revision"))
		}
	default:
		err = fmt.Errorf("不支持的宿主机命令类型：%s", command.CommandType)
	}
	if renewalErr := stopRenewal(); err == nil && renewalErr != nil {
		err = renewalErr
	}
	cancel()
	completion := hostcommand.CompletionInput{Attempt: command.Attempt, Status: "succeeded", Result: result}
	if command.LeaseToken != nil {
		completion.LeaseToken = *command.LeaseToken
	}
	if err != nil {
		completion.Status = "failed"
		completion.Error = &hostcommand.CompletionError{
			Code: providerErrorCode(err), Message: err.Error(), Retryable: providerErrorRetryable(err),
		}
	}
	completionContext, completionCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer completionCancel()
	if completeErr := agent.client.Complete(completionContext, command.ID, completion); completeErr != nil {
		agent.logger.Error("宿主机代理回报命令完成状态失败", "command_id", command.ID, "error", completeErr)
	}
}

func (agent *Agent) startLeaseRenewal(ctx context.Context, cancelOperation context.CancelFunc, command hostcommand.Command) func() error {
	if command.LeaseToken == nil || *command.LeaseToken == "" {
		return func() error { return nil }
	}
	renewContext, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	interval := time.Duration(agent.config.LeaseSeconds) * time.Second / 3
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-renewContext.Done():
				done <- nil
				return
			case <-ticker.C:
				err := agent.client.Extend(renewContext, command.ID, hostcommand.LeaseExtensionInput{LeaseToken: *command.LeaseToken, Attempt: command.Attempt, LeaseSeconds: agent.config.LeaseSeconds})
				if err != nil {
					cancelOperation()
					done <- fmt.Errorf("续订宿主机命令租约失败：%w", err)
					return
				}
			}
		}
	}()
	return func() error { stop(); return <-done }
}

func (agent *Agent) reimage(ctx context.Context, payload map[string]any) (map[string]any, error) {
	targetProfile, err := profileFromPayload(payload)
	if err != nil {
		return nil, err
	}
	agent.resourceMu.Lock()
	if err := agent.preflightReplacement(ctx, stringValue(payload, "provider_ref"), targetProfile); err != nil {
		agent.resourceMu.Unlock()
		return nil, err
	}
	target, targetErr := agent.recreate(ctx, payload)
	agent.resourceMu.Unlock()
	if targetErr == nil {
		if err := agent.registerSTF(ctx, target); err != nil {
			targetErr = err
		}
	}
	if targetErr == nil {
		result := snapshotResult(target)
		result["reimage_applied"] = true
		return result, nil
	}
	rollback, ok := mapValue(payload, "rollback"), false
	if rollback != nil {
		// The target boot can consume the entire command deadline. Rollback is
		// a safety operation and needs its own bounded window; otherwise a boot
		// timeout makes restoration fail immediately with the same canceled
		// context. Lease renewal remains active until this recovery finishes.
		rollbackContext, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), agent.config.CommandTimeout)
		defer cancelRollback()
		rollback["device_id"] = stringValue(payload, "device_id")
		rollback["host_id"] = stringValue(payload, "host_id")
		rollback["provider_ref"] = stringValue(payload, "provider_ref")
		if mapValue(rollback, "capabilities") == nil {
			// Compatibility for commands queued before rollback capabilities were
			// recorded separately from the target image capabilities.
			rollback["capabilities"] = mapValue(payload, "capabilities")
		}
		var restored providers.Snapshot
		restored, err = agent.recreate(rollbackContext, rollback)
		if err == nil {
			err = agent.registerSTF(rollbackContext, restored)
		}
		if err == nil {
			result := snapshotResult(restored)
			result["reimage_applied"] = false
			result["rollback_restored"] = true
			result["target_error_code"] = providerErrorCode(targetErr)
			return result, &providers.Error{Operation: providers.OperationRebuild, Code: "REIMAGE_TARGET_FAILED",
				Message: "目标镜像启动失败，已恢复原镜像", Retryable: false, Cause: targetErr}
		}
		ok = true
	}
	result := map[string]any{"reimage_applied": false, "rollback_restored": false,
		"target_error_code": providerErrorCode(targetErr)}
	if ok && err != nil {
		result["rollback_error_code"] = providerErrorCode(err)
	}
	return result, &providers.Error{Operation: providers.OperationRebuild, Code: "REIMAGE_ROLLBACK_FAILED",
		Message: "目标镜像启动失败，恢复原镜像也失败", Retryable: false, Cause: errors.Join(targetErr, err)}
}

func (agent *Agent) updateRuntimeProfile(ctx context.Context, payload map[string]any) (map[string]any, error) {
	targetProfile, err := profileFromPayload(payload)
	if err != nil {
		return nil, err
	}
	rollbackProfile, err := runtimeprofile.Parse(mapValue(mapValue(payload, "rollback"), "runtime_profile"))
	if err != nil {
		return nil, err
	}
	restartable, ok := agent.provider.(interface {
		RestartWithProfile(context.Context, string, runtimeprofile.Profile) (providers.Snapshot, error)
	})
	if !ok {
		return nil, &providers.Error{Operation: providers.OperationRestart, Code: "RUNTIME_PROFILE_UPDATE_UNSUPPORTED", Message: "当前设备 Provider 不支持保留数据调整运行规格", Retryable: false}
	}
	providerRef := stringValue(payload, "provider_ref")
	agent.resourceMu.Lock()
	if err := agent.preflightReplacement(ctx, providerRef, targetProfile); err != nil {
		agent.resourceMu.Unlock()
		return nil, err
	}
	target, targetErr := restartable.RestartWithProfile(ctx, providerRef, targetProfile)
	agent.resourceMu.Unlock()
	providerRollbackStarted := providers.ErrorCode(targetErr) == "RUNTIME_PROFILE_TARGET_CREATE_FAILED_ROLLBACK_STARTED"
	if targetErr == nil {
		target, targetErr = agent.waitReady(ctx, target)
	}
	if targetErr == nil {
		targetErr = agent.registerSTF(ctx, target)
	}
	if targetErr == nil {
		result := snapshotResult(target)
		result["runtime_profile_update_applied"] = true
		return result, nil
	}

	rollbackContext, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), agent.config.CommandTimeout)
	defer cancelRollback()
	var restored providers.Snapshot
	var rollbackErr error
	if providerRollbackStarted {
		restored = target
	} else {
		agent.resourceMu.Lock()
		restored, rollbackErr = restartable.RestartWithProfile(rollbackContext, providerRef, rollbackProfile)
		agent.resourceMu.Unlock()
	}
	if rollbackErr == nil {
		restored, rollbackErr = agent.waitReady(rollbackContext, restored)
	}
	if rollbackErr == nil {
		rollbackErr = agent.registerSTF(rollbackContext, restored)
	}
	if rollbackErr == nil {
		result := snapshotResult(restored)
		result["runtime_profile_update_applied"] = false
		result["rollback_restored"] = true
		result["target_error_code"] = providerErrorCode(targetErr)
		return result, &providers.Error{Operation: providers.OperationRestart, Code: "RUNTIME_PROFILE_TARGET_FAILED",
			Message: "目标运行规格启动失败，已恢复原规格并保留设备数据", Retryable: false, Cause: targetErr}
	}
	result := map[string]any{"runtime_profile_update_applied": false, "rollback_restored": false,
		"target_error_code": providerErrorCode(targetErr), "rollback_error_code": providerErrorCode(rollbackErr)}
	return result, &providers.Error{Operation: providers.OperationRestart, Code: "RUNTIME_PROFILE_ROLLBACK_FAILED",
		Message: "目标运行规格启动失败，恢复原规格也失败", Retryable: false, Cause: errors.Join(targetErr, rollbackErr)}
}

func (agent *Agent) preflightReplacement(ctx context.Context, providerRef string, requested runtimeprofile.Profile) error {
	if agent.config.CapacityProbe == nil {
		return nil
	}
	values, _, err := agent.config.CapacityProbe.Snapshot(ctx)
	if err != nil {
		return err
	}
	host, dynamic := capacity.HostFromMap(values)
	if !dynamic {
		return nil
	}
	snapshots, err := agent.provider.Discover(ctx, agent.config.HostID)
	if err != nil {
		return err
	}
	existing := capacity.Allocation{}
	var current runtimeprofile.Profile
	for _, snapshot := range snapshots {
		if snapshot.ProviderRef == providerRef {
			current = snapshot.RuntimeProfile
			continue
		}
		existing.CPUCores += snapshot.RuntimeProfile.ContainerCPUCores
		existing.MemoryMB += snapshot.RuntimeProfile.ContainerMemoryMB
		existing.DiskMB += snapshot.RuntimeProfile.DataDiskMB
		existing.Slots++
	}
	if current != (runtimeprofile.Profile{}) {
		host.MemoryAvailableMB = minInt64(host.MemoryTotalMB, host.MemoryAvailableMB+current.ContainerMemoryMB)
		host.DiskAvailableMB = minInt64(host.DiskTotalMB, host.DiskAvailableMB+current.DataDiskMB)
	}
	result := capacity.Evaluate(host, existing, capacity.Allocation{}, requested, false)
	if result.Fits {
		return nil
	}
	return &providers.Error{Operation: providers.OperationRebuild, Code: "INSUFFICIENT_HOST_RESOURCES",
		Message: result.Error().Error(), Retryable: false}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func (agent *Agent) registerSTF(ctx context.Context, snapshot providers.Snapshot) error {
	if agent.registrar == nil || snapshot.Platform == providers.PlatformIOS {
		return nil
	}
	if err := agent.registrar.Register(ctx, snapshot.Connection.ADBEndpoint); err != nil {
		return &providers.Error{Operation: providers.OperationConnectionInfo, Code: "STF_ADB_CONNECT_FAILED",
			Message: "无法向 STF ADB 服务登记模拟器连接", Retryable: true, Cause: err}
	}
	return nil
}

func (agent *Agent) recreate(ctx context.Context, payload map[string]any) (snapshot providers.Snapshot, returnErr error) {
	providerRef := stringValue(payload, "provider_ref")
	if err := agent.verifyRuntimeImage(ctx, payload); err != nil {
		return providers.Snapshot{}, err
	}
	profile, err := profileFromPayload(payload)
	if err != nil {
		return providers.Snapshot{}, err
	}
	if err := agent.provider.Delete(ctx, providerRef); err != nil && providers.ErrorCode(err) != "PROVIDER_DEVICE_NOT_FOUND" {
		return providers.Snapshot{}, err
	}
	created := false
	defer func() {
		if returnErr != nil && created {
			if cleanupErr := agent.cleanupProvider(providerRef); cleanupErr != nil && providers.ErrorCode(cleanupErr) != "PROVIDER_DEVICE_NOT_FOUND" {
				returnErr = errors.Join(returnErr, fmt.Errorf("重建失败后的模拟器清理也失败：%w", cleanupErr))
			}
		}
	}()
	snapshot, returnErr = agent.provider.Create(ctx, providers.CreateRequest{
		DeviceID: stringValue(payload, "device_id"), HostID: agent.config.HostID,
		ImageID: stringValue(payload, "image_id"), RuntimeImage: stringValue(payload, "docker_image"), ProviderRef: providerRef,
		Capabilities: mapValue(payload, "capabilities"), RuntimeProfile: profile,
	})
	if returnErr != nil {
		return providers.Snapshot{}, returnErr
	}
	created = true
	snapshot, returnErr = agent.provider.Start(ctx, providerRef)
	if returnErr != nil {
		return providers.Snapshot{}, returnErr
	}
	return agent.waitReady(ctx, snapshot)
}

func (agent *Agent) waitReady(ctx context.Context, snapshot providers.Snapshot) (providers.Snapshot, error) {
	var lastErr error
	for {
		health, err := agent.provider.InspectHealth(ctx, snapshot.ProviderRef)
		if err == nil && health.Ready() {
			connection, connectionErr := agent.provider.GetConnectionInfo(ctx, snapshot.ProviderRef)
			if connectionErr != nil {
				return providers.Snapshot{}, connectionErr
			}
			snapshot.Health, snapshot.Connection = health, connection
			return snapshot, nil
		}
		lastErr = err
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			code, message := "DEVICE_BOOT_TIMEOUT", "emulator did not become ready before command timeout"
			if providers.ErrorCode(lastErr) == "APPIUM_UNHEALTHY" {
				code, message = "APPIUM_UNHEALTHY", "Appium did not become healthy before command timeout"
			}
			return providers.Snapshot{}, &providers.Error{Operation: providers.OperationInspectHealth,
				Code: code, Message: message, Retryable: true, Cause: errors.Join(lastErr, ctx.Err())}
		case <-timer.C:
		}
	}
}

func (agent *Agent) cleanupProvider(providerRef string) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if cleaner, ok := agent.provider.(interface {
		CleanupCreated(context.Context, string) error
	}); ok {
		return cleaner.CleanupCreated(cleanupContext, providerRef)
	}
	return agent.provider.Delete(cleanupContext, providerRef)
}

func (agent *Agent) validateImage(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if err := agent.verifyRuntimeImage(ctx, payload); err != nil {
		return nil, err
	}
	profile, err := profileFromPayload(payload)
	if err != nil {
		return nil, err
	}
	providerRef := stringValue(payload, "provider_ref")
	created := false
	cleanup := func() error {
		if !created {
			return nil
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return agent.provider.Delete(cleanupContext, providerRef)
	}
	agent.resourceMu.Lock()
	err = agent.preflightCreate(ctx, profile, stringValue(payload, "image_id"))
	var snapshot providers.Snapshot
	if err == nil {
		snapshot, err = agent.provider.Create(ctx, providers.CreateRequest{
			DeviceID: stringValue(payload, "device_id"), HostID: agent.config.HostID,
			ImageID: stringValue(payload, "image_id"), RuntimeImage: stringValue(payload, "docker_image"), ProviderRef: providerRef,
			Capabilities: mapValue(payload, "capabilities"), RuntimeProfile: profile,
		})
	}
	agent.resourceMu.Unlock()
	if err != nil {
		return nil, err
	}
	created = true
	if snapshot, err = agent.provider.Start(ctx, providerRef); err != nil {
		_ = cleanup()
		return nil, err
	}
	if snapshot, err = agent.waitReady(ctx, snapshot); err != nil {
		_ = cleanup()
		return nil, err
	}
	if err = agent.registerSTF(ctx, snapshot); err != nil {
		_ = cleanup()
		return nil, err
	}
	if err = cleanup(); err != nil {
		return nil, err
	}
	return map[string]any{"provider_ref": providerRef, "image_id": stringValue(payload, "image_id"),
		"digest_verified": true, "ready": true, "stf_registered": agent.registrar != nil,
		"adb_endpoint": snapshot.Connection.ADBEndpoint, "appium_endpoint": snapshot.Connection.AppiumEndpoint}, nil
}

func profileFromPayload(payload map[string]any) (runtimeprofile.Profile, error) {
	return runtimeprofile.Parse(mapValue(payload, "runtime_profile"))
}

func (agent *Agent) preflightCreate(ctx context.Context, profile runtimeprofile.Profile, imageID string) error {
	if agent.config.CapacityProbe == nil {
		return nil
	}
	values, _, err := agent.config.CapacityProbe.Snapshot(ctx)
	if err != nil {
		return err
	}
	host, dynamic := capacity.HostFromMap(values)
	if !dynamic {
		return nil
	}
	snapshots, err := agent.provider.Discover(ctx, agent.config.HostID)
	if err != nil {
		return err
	}
	existing := capacity.Allocation{Slots: len(snapshots)}
	for _, snapshot := range snapshots {
		existing.CPUCores += snapshot.RuntimeProfile.ContainerCPUCores
		existing.MemoryMB += snapshot.RuntimeProfile.ContainerMemoryMB
	}
	// verifyRuntimeImage runs immediately before this preflight. A successful
	// verification proves the immutable image is already present on this Host,
	// so its shared layers must not be charged again per Device.
	result := capacity.Evaluate(host, existing, capacity.Allocation{}, profile, imageID != "")
	if result.Fits {
		return nil
	}
	return &providers.Error{Operation: providers.OperationCreate, Code: "INSUFFICIENT_HOST_RESOURCES",
		Message: result.Error().Error(), Retryable: true}
}

func (agent *Agent) verifyRuntimeImage(ctx context.Context, payload map[string]any) error {
	runtimeImage := stringValue(payload, "docker_image")
	digest := stringValue(payload, "docker_digest")
	if agent.config.ProviderType != "docker" && runtimeImage == "" && digest == "" {
		return nil
	}
	if !providers.ValidRuntimeImageReference(runtimeImage) {
		return &providers.Error{Operation: providers.OperationValidateImage, Code: "INVALID_IMAGE_REFERENCE",
			Message: "Docker 命令必须包含固定版本的运行镜像", Retryable: false}
	}
	if digest == "" {
		return &providers.Error{Operation: providers.OperationValidateImage, Code: "INVALID_IMAGE_DIGEST",
			Message: "Docker 命令必须包含已登记的镜像摘要", Retryable: false}
	}
	verifier, ok := agent.provider.(providers.ImageDigestVerifier)
	if !ok {
		return &providers.Error{Operation: providers.OperationValidateImage, Code: "IMAGE_VALIDATION_UNSUPPORTED",
			Message: "当前 Provider 不支持镜像摘要校验", Retryable: false}
	}
	return verifier.VerifyImageDigest(ctx, runtimeImage, digest)
}

func waitWorkers(workers *sync.WaitGroup, timeout time.Duration) error {
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("宿主机代理停止超时，服务端将自动回收命令租约")
	}
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
func mapValue(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}
func providerErrorCode(err error) string {
	if code := providers.ErrorCode(err); code != "" {
		return code
	}
	return "AGENT_COMMAND_FAILED"
}
func providerErrorRetryable(err error) bool {
	var providerError *providers.Error
	if errors.As(err, &providerError) {
		return providerError.Retryable
	}
	return true
}
func heartbeatProviderType(providerType string) string {
	if strings.EqualFold(strings.TrimSpace(providerType), "docker") {
		return "docker_emulator"
	}
	return strings.ToLower(strings.TrimSpace(providerType))
}
func providerLifecycle(snapshot providers.Snapshot) string {
	if snapshot.State == providers.StateRunning {
		if snapshot.Ready() {
			return "ready"
		}
		return "booting"
	}
	return "stopped"
}
func providerHealth(snapshot providers.Snapshot) string {
	if snapshot.Ready() {
		return "healthy"
	}
	if snapshot.State == providers.StateRunning {
		return "unhealthy"
	}
	if len(snapshot.Health.Components) > 0 {
		for _, status := range snapshot.Health.Components {
			if status == providers.ProbeFailed {
				return "degraded"
			}
		}
		return "unknown"
	}
	return "unknown"
}

func snapshotResult(snapshot providers.Snapshot) map[string]any {
	return map[string]any{
		"device_id": snapshot.DeviceID, "host_id": snapshot.HostID, "image_id": snapshot.ImageID,
		"platform": string(snapshot.Platform), "device_kind": snapshot.DeviceKind,
		"provider_ref": snapshot.ProviderRef, "state": string(snapshot.State), "generation": snapshot.Generation,
		"capabilities": snapshot.Capabilities,
		"connection": map[string]any{
			"serial": snapshot.Connection.Serial, "adb_endpoint": snapshot.Connection.ADBEndpoint,
			"device_udid": snapshot.Connection.DeviceUDID, "provider_id": snapshot.Connection.ProviderID,
			"appium_endpoint": snapshot.Connection.AppiumEndpoint, "appium_udid": snapshot.Connection.AppiumUDID,
		},
		"health": healthResult(snapshot.Health),
	}
}

func healthResult(health providers.Health) map[string]any {
	result := map[string]any{
		"online": health.Online, "adb_online": health.ADBOnline,
		"boot_completed": health.BootCompleted, "appium_healthy": health.AppiumHealthy,
	}
	if len(health.Components) > 0 {
		components := make(map[string]string, len(health.Components))
		for name, status := range health.Components {
			components[name] = string(status)
		}
		result["components"] = components
	}
	return result
}
