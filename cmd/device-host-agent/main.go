package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	appiumadapter "github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appium"
	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appiumdevicefarm"
	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stfadb"
	"github.com/Ad-Quanta/alcor-device-farm/internal/agent"
	"github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcapacity"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imageprepare"
	"github.com/Ad-Quanta/alcor-device-farm/internal/ioshost"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossessionfence"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	providerdocker "github.com/Ad-Quanta/alcor-device-farm/internal/providers/docker"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
)

func main() {
	version := flag.Bool("version", false, "显示版本信息后退出")
	serverURL := flag.String("server-url", os.Getenv("DEVICE_FARM_AGENT_SERVER_URL"), "设备农场 Server 地址")
	hostID := flag.String("host-id", os.Getenv("DEVICE_FARM_AGENT_HOST_ID"), "已登记的设备宿主机 ID")
	token := flag.String("agent-token", os.Getenv("DEVICE_FARM_SECURITY_AGENT_TOKEN"), "宿主机代理 Bearer Token")
	concurrency := flag.Int("concurrency", envInt("DEVICE_FARM_AGENT_CONCURRENCY", 1), "可并发执行的 Provider 命令上限")
	deviceSlotLimit := flag.Int("device-slot-limit", envIntAllowZero("DEVICE_FARM_AGENT_DEVICE_SLOT_LIMIT", 0), "可选的设备数量硬上限；零表示只按 CPU、内存和磁盘计算")
	leaseSeconds := flag.Int("lease-seconds", envInt("DEVICE_FARM_AGENT_LEASE_SECONDS", 300), "宿主机命令租约秒数")
	commandTimeout := flag.Duration("command-timeout", envDuration("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", 270*time.Second), "Provider 命令执行超时时间")
	imagePrepareTimeout := flag.Duration("image-prepare-timeout", envDuration("DEVICE_FARM_IMAGE_PREPARE_TIMEOUT", 2*time.Hour), "Android 目录同步和镜像构建超时时间")
	providerType := flag.String("provider", strings.TrimSpace(os.Getenv("DEVICE_FARM_AGENT_PROVIDER")), "设备 Provider：mock、docker 或 appium_device_farm_ios；必填")
	dockerBinary := flag.String("docker-binary", envOr("DEVICE_FARM_DOCKER_BINARY", "docker"), "Docker CLI 路径")
	dockerImage := flag.String("docker-image", os.Getenv("DEVICE_FARM_DOCKER_IMAGE"), "直接测试 Provider 时可选的固定备用镜像；生产命令使用 Device Image 运行时引用")
	dockerAdvertiseHost := flag.String("docker-advertise-host", os.Getenv("DEVICE_FARM_DOCKER_ADVERTISE_HOST"), "对外发布 ADB 端口时使用的宿主机地址")
	dockerBindAddress := flag.String("docker-bind-address", envOr("DEVICE_FARM_DOCKER_BIND_ADDRESS", "127.0.0.1"), "发布 ADB 端口时绑定的 IP 地址")
	dockerKVMDevice := flag.String("docker-kvm-device", envOr("DEVICE_FARM_DOCKER_KVM_DEVICE", "/dev/kvm"), "KVM 设备路径")
	dockerADBPort := flag.Int("docker-adb-port", envInt("DEVICE_FARM_DOCKER_ADB_PORT", 5555), "模拟器容器暴露的 ADB 端口")
	dockerAppiumPort := flag.Int("docker-appium-port", envInt("DEVICE_FARM_DOCKER_APPIUM_PORT", 4723), "模拟器容器暴露的 Appium 端口")
	dockerADBSerial := flag.String("docker-adb-serial", envOr("DEVICE_FARM_DOCKER_ADB_SERIAL", "emulator-5554"), "模拟器容器内的 ADB serial")
	appiumHealthTimeout := flag.Duration("appium-health-timeout", envDuration("DEVICE_FARM_APPIUM_HEALTH_TIMEOUT", 5*time.Second), "Appium 状态请求超时时间")
	stfADBServer := flag.String("stf-adb-server", strings.TrimSpace(os.Getenv("DEVICE_FARM_AGENT_STF_ADB_SERVER")), "可选的本机回环 STF ADB Server host:port")
	adbBinary := flag.String("adb-binary", envOr("DEVICE_FARM_ADB_BINARY", "adb"), "STF Endpoint 注册使用的 ADB CLI 路径")
	dockerDataMountPath := flag.String("docker-data-mount-path", envOr("DEVICE_FARM_DOCKER_DATA_MOUNT_PATH", "/home/androidusr"), "容器内由单设备数据卷支撑的数据路径")
	dockerEmulatorDevice := flag.String("docker-emulator-device", envOr("DEVICE_FARM_DOCKER_EMULATOR_DEVICE", "Pixel 9"), "docker-android 模拟器设备规格")
	dockerCPUs := flag.Float64("docker-cpus", envFloat("DEVICE_FARM_DOCKER_CPUS", 4), "单台模拟器的 CPU 上限")
	dockerMemory := flag.String("docker-memory", envOr("DEVICE_FARM_DOCKER_MEMORY", "5g"), "单台模拟器的内存上限")
	dockerPidsLimit := flag.Int("docker-pids-limit", envInt("DEVICE_FARM_DOCKER_PIDS_LIMIT", 512), "单台模拟器的 PID 上限")
	dockerDataRoot := flag.String("docker-data-root", envOr("DEVICE_FARM_DOCKER_DATA_ROOT", "/var/lib/docker"), "容量测量使用的 Docker 数据文件系统")
	iosSimulatorDataRoot := flag.String("ios-simulator-data-root", envOr("DEVICE_FARM_IOS_SIMULATOR_DATA_ROOT", defaultIOSDataRoot()), "CoreSimulator 数据所在文件系统，用于容量探测")
	dockerRenderDevice := flag.String("docker-render-device", envOr("DEVICE_FARM_DOCKER_RENDER_DEVICE", "/dev/dri/renderD128"), "宿主机代理探测的可选 GPU 渲染节点")
	imagePrepareScript := flag.String("image-prepare-script", strings.TrimSpace(os.Getenv("DEVICE_FARM_IMAGE_PREPARE_SCRIPT")), "受信任的本地 Android 镜像准备脚本；留空时禁用 Build Agent 命令")
	iosEndpoint := flag.String("ios-appium-endpoint", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_APPIUM_ENDPOINT")), "本机 Appium Device Farm Node 地址")
	iosNodeEndpoint := flag.String("ios-appium-node-endpoint", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_APPIUM_NODE_ENDPOINT")), "本机 Appium Device Farm Node 设备清单地址")
	iosAllowUDIDs := flag.String("ios-allow-udids", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_ALLOW_UDIDS")), "允许使用的固定 iOS UDID，多个值使用逗号分隔")
	iosRuntimeIDs := flag.String("ios-runtime-ids", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_RUNTIME_IDS")), "允许动态创建的 iOS Runtime ID，多个值使用逗号分隔")
	iosDeviceTypeIDs := flag.String("ios-device-type-ids", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_DEVICE_TYPE_IDS")), "允许动态创建的 iPhone Device Type ID，多个值使用逗号分隔")
	iosManagedNamePrefix := flag.String("ios-managed-name-prefix", envOr("DEVICE_FARM_IOS_MANAGED_NAME_PREFIX", "Alcor-DF-"), "设备农场动态 Simulator 的保留名称前缀")
	iosWDAPackageJSON := flag.String("ios-wda-package-json", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_WDA_PACKAGE_JSON")), "用于校验固定 WDA 版本的 appium-webdriveragent package.json")
	iosXcrunBinary := flag.String("ios-xcrun-binary", envOr("DEVICE_FARM_IOS_XCRUN_BINARY", "xcrun"), "用于受控 Simulator 生命周期的 xcrun 可执行文件")
	nodeBinary := flag.String("node-binary", envOr("DEVICE_FARM_NODE_BINARY", "node"), "iOS 宿主机使用的固定版本 Node.js 可执行文件")
	appiumBinary := flag.String("appium-binary", envOr("DEVICE_FARM_APPIUM_BINARY", "appium"), "iOS 宿主机使用的固定版本 Appium 可执行文件")
	goIOSBinary := flag.String("go-ios-binary", envOr("DEVICE_FARM_GO_IOS_BINARY", "ios"), "iOS 宿主机使用的固定版本 go-ios 可执行文件")
	iosFenceListen := flag.String("ios-session-fence-listen", envOr("DEVICE_FARM_IOS_SESSION_FENCE_LISTEN", "127.0.0.1:4810"), "受信任的 iOS Session Fence 监听地址")
	iosFenceAdvertiseURL := flag.String("ios-session-fence-advertise-url", envOr("DEVICE_FARM_IOS_SESSION_FENCE_ADVERTISE_URL", "http://127.0.0.1:4810"), "只向受信任 Session Grant 客户端返回的 iOS Session Fence 地址")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "用法：%s [选项]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *version {
		fmt.Fprintln(os.Stdout, buildinfo.String("device-host-agent"))
		return
	}

	client, err := agent.NewHTTPClient(*serverURL, *token, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "宿主机代理客户端配置错误：%v\n", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	appiumProbe, err := appiumadapter.NewProbe(*appiumHealthTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Appium 探测配置错误：%v\n", err)
		os.Exit(1)
	}
	var iosAdapter *appiumdevicefarm.Client
	var environmentProbe agent.EnvironmentProbe
	var sessionFence *iossessionfence.Server
	agentEnvironment := map[string]any{}
	if strings.EqualFold(strings.TrimSpace(*providerType), "appium_device_farm_ios") {
		iosCommandRunner := appiumdevicefarm.NewSerializedCommandRunner(nil)
		if strings.TrimSpace(*iosNodeEndpoint) == "" {
			*iosNodeEndpoint = *iosEndpoint
		}
		iosAdapter, err = appiumdevicefarm.New(appiumdevicefarm.Config{Endpoint: *iosEndpoint, Timeout: *appiumHealthTimeout,
			RegistrationEndpoints: []string{*iosEndpoint, *iosNodeEndpoint},
			AllowUDIDs:            splitCSV(*iosAllowUDIDs), AllowedRuntimeIDs: splitCSV(*iosRuntimeIDs),
			AllowedDeviceTypeIDs: splitCSV(*iosDeviceTypeIDs), ManagedNamePrefix: *iosManagedNamePrefix, XcrunBinary: *iosXcrunBinary,
			CommandRunner: iosCommandRunner})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Appium Device Farm Adapter 配置错误：%v\n", err)
			os.Exit(1)
		}
		environmentProbe, err = ioshost.New(ioshost.Config{NodeBinary: *nodeBinary, AppiumBinary: *appiumBinary,
			GoIOSBinary: *goIOSBinary, XcrunBinary: *iosXcrunBinary, WDAPackageJSON: *iosWDAPackageJSON,
			NodeHealth: iosAdapter, SimulatorCatalog: iosAdapter, Runner: iosCommandRunner})
		if err != nil {
			fmt.Fprintf(os.Stderr, "iOS 宿主机就绪探测配置错误：%v\n", err)
			os.Exit(1)
		}
		sessionFence, err = iossessionfence.New(iossessionfence.Config{
			ListenAddress: *iosFenceListen, AdvertiseURL: *iosFenceAdvertiseURL,
			ControlServerURL: *serverURL, AgentToken: *token, HostID: *hostID,
			UpstreamEndpoint: *iosEndpoint, Timeout: *commandTimeout, ShutdownTimeout: 30 * time.Second, Logger: logger,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "iOS Session Fence 配置错误：%v\n", err)
			os.Exit(1)
		}
		agentEnvironment["session_fence_endpoint"] = sessionFence.AdvertiseURL()
	}
	deviceProvider, err := buildProvider(*providerType, providerdocker.Config{
		Binary: *dockerBinary, Image: *dockerImage, AdvertiseHost: *dockerAdvertiseHost,
		BindAddress: *dockerBindAddress, KVMDevice: *dockerKVMDevice, RenderDevice: *dockerRenderDevice,
		ContainerADBPort: *dockerADBPort, ContainerAppiumPort: *dockerAppiumPort, ContainerADBSerial: *dockerADBSerial,
		DataMountPath: *dockerDataMountPath,
		CPUs:          *dockerCPUs, Memory: *dockerMemory, PidsLimit: *dockerPidsLimit,
		Environment: dockerEnvironment(*dockerEmulatorDevice), AppiumProbe: appiumProbe,
	}, iosAdapter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "宿主机代理 Provider 配置错误：%v\n", err)
		os.Exit(1)
	}
	var stfRegistrar agent.EndpointRegistrar
	if strings.TrimSpace(*stfADBServer) != "" {
		stfRegistrar, err = stfadb.New(stfadb.Config{Binary: *adbBinary, ServerAddress: *stfADBServer})
		if err != nil {
			fmt.Fprintf(os.Stderr, "STF ADB 注册器配置错误：%v\n", err)
			os.Exit(1)
		}
	}
	var capacityProbe agent.CapacityProbe
	if strings.EqualFold(strings.TrimSpace(*providerType), "docker") {
		capacityProbe = hostcapacity.NewSystem(*dockerDataRoot, *dockerRenderDevice, *deviceSlotLimit)
	} else if strings.EqualFold(strings.TrimSpace(*providerType), "appium_device_farm_ios") {
		capacityProbe = hostcapacity.NewIOSSystem(*iosSimulatorDataRoot, *deviceSlotLimit)
	}
	var imagePreparer imageprepare.Preparer
	if strings.TrimSpace(*imagePrepareScript) != "" {
		imagePreparer, err = imageprepare.New(*imagePrepareScript)
		if err != nil {
			fmt.Fprintf(os.Stderr, "镜像准备器配置错误：%v\n", err)
			os.Exit(1)
		}
	}
	runtime, err := agent.New(agent.Config{
		HostID: *hostID, ProviderType: strings.ToLower(strings.TrimSpace(*providerType)),
		HeartbeatInterval: 5 * time.Second, LeaseSeconds: *leaseSeconds,
		WaitSeconds: 5, Concurrency: *concurrency, CommandTimeout: *commandTimeout,
		ImagePrepareTimeout: *imagePrepareTimeout,
		ShutdownTimeout:     30 * time.Second,
		Capacity:            map[string]any{"device_slots": *concurrency}, Environment: agentEnvironment,
		CapacityProbe: capacityProbe, STFADBRegistrar: stfRegistrar,
		ImagePreparer: imagePreparer, EnvironmentProbe: environmentProbe,
	}, client, deviceProvider, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "宿主机代理配置错误：%v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runComponents(ctx, runtime, sessionFence); err != nil {
		logger.Error("宿主机代理异常停止", "error", err)
		os.Exit(1)
	}
}

func defaultIOSDataRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "/Users/Shared"
	}
	return home
}

type componentRunner interface{ Run(context.Context) error }

func runComponents(ctx context.Context, runtime componentRunner, fence *iossessionfence.Server) error {
	if fence == nil {
		return runtime.Run(ctx)
	}
	componentContext, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- runtime.Run(componentContext) }()
	go func() { errorsChannel <- fence.Run(componentContext) }()
	first := <-errorsChannel
	cancel()
	second := <-errorsChannel
	return errors.Join(first, second)
}

func buildProvider(providerType string, dockerConfig providerdocker.Config, iosProvider providers.Provider) (providers.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "":
		return nil, errors.New("必须配置设备 Provider；请设置 DEVICE_FARM_AGENT_PROVIDER 或 --provider")
	case "mock":
		return providermock.New(providermock.Config{}), nil
	case "docker":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return providerdocker.New(ctx, dockerConfig)
	case "appium_device_farm_ios":
		if iosProvider == nil {
			return nil, errors.New("必须配置 Appium Device Farm iOS Provider")
		}
		return iosProvider, nil
	default:
		return nil, fmt.Errorf("不支持的 Provider：%q", providerType)
	}
}

func splitCSV(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value == 0 {
		return fallback
	}
	return value
}

func envIntAllowZero(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	if err != nil || value == 0 {
		return fallback
	}
	return value
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func dockerEnvironment(emulatorDevice string) map[string]string {
	values := map[string]string{
		"WEB_VNC":                 "false",
		"WEB_LOG":                 "false",
		"APPIUM":                  "true",
		"USER_BEHAVIOR_ANALYTICS": "false",
	}
	if emulatorDevice = strings.TrimSpace(emulatorDevice); emulatorDevice != "" {
		values["EMULATOR_DEVICE"] = emulatorDevice
	}
	return values
}
