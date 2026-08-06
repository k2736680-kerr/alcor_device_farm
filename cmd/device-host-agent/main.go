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
	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stfadb"
	"github.com/Ad-Quanta/alcor-device-farm/internal/agent"
	"github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo"
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
	leaseSeconds := flag.Int("lease-seconds", envInt("DEVICE_FARM_AGENT_LEASE_SECONDS", 300), "host command lease duration in seconds")
	commandTimeout := flag.Duration("command-timeout", envDuration("DEVICE_FARM_AGENT_COMMAND_TIMEOUT", 270*time.Second), "provider command execution timeout")
	providerType := flag.String("provider", strings.TrimSpace(os.Getenv("DEVICE_FARM_AGENT_PROVIDER")), "device provider: mock or docker; required")
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
	deviceProvider, err := buildProvider(*providerType, providerdocker.Config{
		Binary: *dockerBinary, Image: *dockerImage, AdvertiseHost: *dockerAdvertiseHost,
		BindAddress: *dockerBindAddress, KVMDevice: *dockerKVMDevice,
		ContainerADBPort: *dockerADBPort, ContainerAppiumPort: *dockerAppiumPort, ContainerADBSerial: *dockerADBSerial,
		DataMountPath: *dockerDataMountPath,
		CPUs:          *dockerCPUs, Memory: *dockerMemory, PidsLimit: *dockerPidsLimit,
		Environment: dockerEnvironment(*dockerEmulatorDevice), AppiumProbe: appiumProbe,
	})
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
	runtime, err := agent.New(agent.Config{
		HostID: *hostID, ProviderType: strings.ToLower(strings.TrimSpace(*providerType)),
		HeartbeatInterval: 5 * time.Second, LeaseSeconds: *leaseSeconds,
		WaitSeconds: 5, Concurrency: *concurrency, CommandTimeout: *commandTimeout,
		ShutdownTimeout: 30 * time.Second,
		Capacity:        map[string]any{"device_slots": *concurrency}, STFADBRegistrar: stfRegistrar,
	}, client, deviceProvider, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent configuration error: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runtime.Run(ctx); err != nil {
		logger.Error("device host agent stopped with error", "error", err)
		os.Exit(1)
	}
}

func buildProvider(providerType string, dockerConfig providerdocker.Config) (providers.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "":
		return nil, errors.New("device provider is required; set DEVICE_FARM_AGENT_PROVIDER or --provider")
	case "mock":
		return providermock.New(providermock.Config{}), nil
	case "docker":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return providerdocker.New(ctx, dockerConfig)
	default:
		return nil, fmt.Errorf("unsupported provider %q", providerType)
	}
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
