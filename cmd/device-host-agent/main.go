package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

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
	concurrency := flag.Int("concurrency", 2, "maximum concurrent provider commands")
	providerType := flag.String("provider", envOr("DEVICE_FARM_AGENT_PROVIDER", "mock"), "device provider: mock or docker")
	dockerBinary := flag.String("docker-binary", envOr("DEVICE_FARM_DOCKER_BINARY", "docker"), "Docker CLI path")
	dockerImage := flag.String("docker-image", os.Getenv("DEVICE_FARM_DOCKER_IMAGE"), "fixed Android Emulator container image")
	dockerAdvertiseHost := flag.String("docker-advertise-host", os.Getenv("DEVICE_FARM_DOCKER_ADVERTISE_HOST"), "host advertised for published ADB ports")
	dockerBindAddress := flag.String("docker-bind-address", envOr("DEVICE_FARM_DOCKER_BIND_ADDRESS", "127.0.0.1"), "IP used to bind published ADB ports")
	dockerKVMDevice := flag.String("docker-kvm-device", envOr("DEVICE_FARM_DOCKER_KVM_DEVICE", "/dev/kvm"), "KVM device path")
	dockerCPUs := flag.Float64("docker-cpus", envFloat("DEVICE_FARM_DOCKER_CPUS", 2), "CPU limit per emulator")
	dockerMemory := flag.String("docker-memory", envOr("DEVICE_FARM_DOCKER_MEMORY", "4g"), "memory limit per emulator")
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
	deviceProvider, err := buildProvider(*providerType, providerdocker.Config{
		Binary: *dockerBinary, Image: *dockerImage, AdvertiseHost: *dockerAdvertiseHost,
		BindAddress: *dockerBindAddress, KVMDevice: *dockerKVMDevice,
		CPUs: *dockerCPUs, Memory: *dockerMemory, PidsLimit: *dockerPidsLimit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent provider configuration error: %v\n", err)
		os.Exit(1)
	}
	runtime, err := agent.New(agent.Config{
		HostID: *hostID, ProviderType: strings.ToLower(strings.TrimSpace(*providerType)),
		HeartbeatInterval: 5 * time.Second, LeaseSeconds: 60,
		WaitSeconds: 5, Concurrency: *concurrency, CommandTimeout: 5 * time.Minute,
		ShutdownTimeout: 30 * time.Second,
		Capacity:        map[string]any{"device_slots": *concurrency},
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
