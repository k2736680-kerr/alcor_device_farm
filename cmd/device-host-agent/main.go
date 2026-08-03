package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/agent"
	"github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
)

func main() {
	version := flag.Bool("version", false, "print version information and exit")
	serverURL := flag.String("server-url", os.Getenv("DEVICE_FARM_AGENT_SERVER_URL"), "device farm server URL")
	hostID := flag.String("host-id", os.Getenv("DEVICE_FARM_AGENT_HOST_ID"), "registered device host ID")
	token := flag.String("agent-token", os.Getenv("DEVICE_FARM_SECURITY_AGENT_TOKEN"), "agent bearer token")
	concurrency := flag.Int("concurrency", 2, "maximum concurrent provider commands")
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
	runtime, err := agent.New(agent.Config{
		HostID: *hostID, HeartbeatInterval: 5 * time.Second, LeaseSeconds: 60,
		WaitSeconds: 5, Concurrency: *concurrency, CommandTimeout: 5 * time.Minute,
		ShutdownTimeout: 30 * time.Second,
		Capacity:        map[string]any{"device_slots": *concurrency},
	}, client, providermock.New(providermock.Config{}), logger)
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
