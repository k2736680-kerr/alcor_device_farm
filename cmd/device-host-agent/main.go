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
	version := flag.Bool("version", false, "print version information and exit")
	serverURL := flag.String("server-url", os.Getenv("DEVICE_FARM_AGENT_SERVER_URL"), "device farm server URL")
	hostID := flag.String("host-id", os.Getenv("DEVICE_FARM_AGENT_HOST_ID"), "registered device host ID")
	token := flag.String("agent-token", os.Getenv("DEVICE_FARM_SECURITY_AGENT_TOKEN"), "agent bearer token")
	concurrency := flag.Int("concurrency", envInt("DEVICE_FARM_AGENT_CONCURRENCY", 1), "maximum concurrent provider commands")
	deviceSlotLimit := flag.Int("device-slot-limit", envIntAllowZero("DEVICE_FARM_AGENT_DEVICE_SLOT_LIMIT", 0), "optional hard device count safety limit; zero uses only CPU, memory and disk")
	leaseSeconds := flag.Int("lease-seconds", envInt("DEVICE_FARM_AGENT_LEASE_SECONDS", 300), "host command lease duration in seconds")
	commandTimeout := flag.Duration("command-timeout", envDuration("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", 270*time.Second), "provider command execution timeout")
	imagePrepareTimeout := flag.Duration("image-prepare-timeout", envDuration("DEVICE_FARM_IMAGE_PREPARE_TIMEOUT", 2*time.Hour), "Android catalogue synchronization and image build timeout")
	providerType := flag.String("provider", strings.TrimSpace(os.Getenv("DEVICE_FARM_AGENT_PROVIDER")), "device provider: mock, docker, or appium_device_farm_ios; required")
	dockerBinary := flag.String("docker-binary", envOr("DEVICE_FARM_DOCKER_BINARY", "docker"), "Docker CLI path")
	dockerImage := flag.String("docker-image", os.Getenv("DEVICE_FARM_DOCKER_IMAGE"), "optional fixed fallback image for direct provider tests; production commands select the Device Image runtime reference")
	dockerAdvertiseHost := flag.String("docker-advertise-host", os.Getenv("DEVICE_FARM_DOCKER_ADVERTISE_HOST"), "host advertised for published ADB ports")
	dockerBindAddress := flag.String("docker-bind-address", envOr("DEVICE_FARM_DOCKER_BIND_ADDRESS", "127.0.0.1"), "IP used to bind published ADB ports")
	dockerKVMDevice := flag.String("docker-kvm-device", envOr("DEVICE_FARM_DOCKER_KVM_DEVICE", "/dev/kvm"), "KVM device path")
	dockerADBPort := flag.Int("docker-adb-port", envInt("DEVICE_FARM_DOCKER_ADB_PORT", 5555), "ADB port exposed by the emulator container")
	dockerAppiumPort := flag.Int("docker-appium-port", envInt("DEVICE_FARM_DOCKER_APPIUM_PORT", 4723), "Appium port exposed by the emulator container")
	dockerADBSerial := flag.String("docker-adb-serial", envOr("DEVICE_FARM_DOCKER_ADB_SERIAL", "emulator-5554"), "ADB serial inside the emulator container")
	appiumHealthTimeout := flag.Duration("appium-health-timeout", envDuration("DEVICE_FARM_APPIUM_HEALTH_TIMEOUT", 5*time.Second), "Appium status request timeout")
	stfADBServer := flag.String("stf-adb-server", strings.TrimSpace(os.Getenv("DEVICE_FARM_AGENT_STF_ADB_SERVER")), "optional loopback STF ADB server host:port")
	adbBinary := flag.String("adb-binary", envOr("DEVICE_FARM_ADB_BINARY", "adb"), "ADB CLI path used for STF endpoint registration")
	dockerDataMountPath := flag.String("docker-data-mount-path", envOr("DEVICE_FARM_DOCKER_DATA_MOUNT_PATH", "/home/androidusr"), "container path backed by the per-device data volume")
	dockerEmulatorDevice := flag.String("docker-emulator-device", envOr("DEVICE_FARM_DOCKER_EMULATOR_DEVICE", "Pixel 9"), "docker-android emulator device profile")
	dockerCPUs := flag.Float64("docker-cpus", envFloat("DEVICE_FARM_DOCKER_CPUS", 4), "CPU limit per emulator")
	dockerMemory := flag.String("docker-memory", envOr("DEVICE_FARM_DOCKER_MEMORY", "5g"), "memory limit per emulator")
	dockerPidsLimit := flag.Int("docker-pids-limit", envInt("DEVICE_FARM_DOCKER_PIDS_LIMIT", 512), "PID limit per emulator")
	dockerDataRoot := flag.String("docker-data-root", envOr("DEVICE_FARM_DOCKER_DATA_ROOT", "/var/lib/docker"), "Docker data filesystem used for capacity measurement")
	dockerRenderDevice := flag.String("docker-render-device", envOr("DEVICE_FARM_DOCKER_RENDER_DEVICE", "/dev/dri/renderD128"), "optional GPU render node detected by the agent")
	imagePrepareScript := flag.String("image-prepare-script", strings.TrimSpace(os.Getenv("DEVICE_FARM_IMAGE_PREPARE_SCRIPT")), "trusted local Android image preparation script; empty disables Build Agent commands")
	iosEndpoint := flag.String("ios-appium-endpoint", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_APPIUM_ENDPOINT")), "local Appium Device Farm Node endpoint")
	iosAllowUDIDs := flag.String("ios-allow-udids", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_ALLOW_UDIDS")), "comma-separated fixed iOS UDID allowlist")
	iosWDAPackageJSON := flag.String("ios-wda-package-json", strings.TrimSpace(os.Getenv("DEVICE_FARM_IOS_WDA_PACKAGE_JSON")), "appium-webdriveragent package.json used for pinned WDA readiness")
	nodeBinary := flag.String("node-binary", envOr("DEVICE_FARM_NODE_BINARY", "node"), "pinned Node.js binary used by the iOS Host")
	appiumBinary := flag.String("appium-binary", envOr("DEVICE_FARM_APPIUM_BINARY", "appium"), "pinned Appium binary used by the iOS Host")
	goIOSBinary := flag.String("go-ios-binary", envOr("DEVICE_FARM_GO_IOS_BINARY", "ios"), "pinned go-ios binary used by the iOS Host")
	iosFenceListen := flag.String("ios-session-fence-listen", envOr("DEVICE_FARM_IOS_SESSION_FENCE_LISTEN", "127.0.0.1:4810"), "trusted iOS Session Fence listen address")
	iosFenceAdvertiseURL := flag.String("ios-session-fence-advertise-url", envOr("DEVICE_FARM_IOS_SESSION_FENCE_ADVERTISE_URL", "http://127.0.0.1:4810"), "iOS Session Fence URL returned only to trusted Session Grant clients")
	flag.Parse()

	if *version {
		fmt.Fprintln(os.Stdout, buildinfo.String("device-host-agent"))
		return
	}

	client, err := agent.NewHTTPClient(*serverURL, *token, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent client configuration error: %v\n", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	appiumProbe, err := appiumadapter.NewProbe(*appiumHealthTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Appium probe configuration error: %v\n", err)
		os.Exit(1)
	}
	var iosAdapter *appiumdevicefarm.Client
	var environmentProbe agent.EnvironmentProbe
	var sessionFence *iossessionfence.Server
	agentEnvironment := map[string]any{}
	if strings.EqualFold(strings.TrimSpace(*providerType), "appium_device_farm_ios") {
		iosAdapter, err = appiumdevicefarm.New(appiumdevicefarm.Config{Endpoint: *iosEndpoint, Timeout: *appiumHealthTimeout,
			AllowUDIDs: splitCSV(*iosAllowUDIDs)})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Appium Device Farm adapter configuration error: %v\n", err)
			os.Exit(1)
		}
		environmentProbe, err = ioshost.New(ioshost.Config{NodeBinary: *nodeBinary, AppiumBinary: *appiumBinary,
			GoIOSBinary: *goIOSBinary, WDAPackageJSON: *iosWDAPackageJSON, NodeHealth: iosAdapter})
		if err != nil {
			fmt.Fprintf(os.Stderr, "iOS Host readiness configuration error: %v\n", err)
			os.Exit(1)
		}
		sessionFence, err = iossessionfence.New(iossessionfence.Config{
			ListenAddress: *iosFenceListen, AdvertiseURL: *iosFenceAdvertiseURL,
			ControlServerURL: *serverURL, AgentToken: *token, HostID: *hostID,
			UpstreamEndpoint: *iosEndpoint, Timeout: *commandTimeout, ShutdownTimeout: 30 * time.Second, Logger: logger,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "iOS Session Fence configuration error: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "agent provider configuration error: %v\n", err)
		os.Exit(1)
	}
	var stfRegistrar agent.EndpointRegistrar
	if strings.TrimSpace(*stfADBServer) != "" {
		stfRegistrar, err = stfadb.New(stfadb.Config{Binary: *adbBinary, ServerAddress: *stfADBServer})
		if err != nil {
			fmt.Fprintf(os.Stderr, "STF ADB registrar configuration error: %v\n", err)
			os.Exit(1)
		}
	}
	var capacityProbe agent.CapacityProbe
	if strings.EqualFold(strings.TrimSpace(*providerType), "docker") {
		capacityProbe = hostcapacity.NewSystem(*dockerDataRoot, *dockerRenderDevice, *deviceSlotLimit)
	}
	var imagePreparer imageprepare.Preparer
	if strings.TrimSpace(*imagePrepareScript) != "" {
		imagePreparer, err = imageprepare.New(*imagePrepareScript)
		if err != nil {
			fmt.Fprintf(os.Stderr, "image preparer configuration error: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "agent configuration error: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runComponents(ctx, runtime, sessionFence); err != nil {
		logger.Error("device host agent stopped with error", "error", err)
		os.Exit(1)
	}
}

type componentRunner interface{ Run(context.Context) error }

func runComponents(ctx context.Context, runtime componentRunner, fence componentRunner) error {
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
		return nil, errors.New("device provider is required; set DEVICE_FARM_AGENT_PROVIDER or --provider")
	case "mock":
		return providermock.New(providermock.Config{}), nil
	case "docker":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return providerdocker.New(ctx, dockerConfig)
	case "appium_device_farm_ios":
		if iosProvider == nil {
			return nil, errors.New("Appium Device Farm iOS provider is required")
		}
		return iosProvider, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", providerType)
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
