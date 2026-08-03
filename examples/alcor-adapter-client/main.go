package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/adapters/alcor"
)

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:18080", "Device Farm API origin")
	poolID := flag.String("pool-id", "pool_000000000000001", "device pool ID")
	runID := flag.String("run-id", "run_000000000000001", "Alcor Run ID")
	attemptID := flag.String("attempt-id", "attempt_000000000001", "Alcor RunAttempt ID")
	flag.Parse()
	token := os.Getenv("DEVICE_FARM_TOKEN")
	if token == "" {
		log.Fatal("DEVICE_FARM_TOKEN is required")
	}
	client, err := alcor.New(alcor.Config{BaseURL: *baseURL, Token: token})
	if err != nil {
		log.Fatal(err)
	}
	run := alcor.RunContext{RunID: *runID, RunAttemptID: *attemptID}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reservation, err := client.Reserve(ctx, alcor.ReserveRequest{PoolID: *poolID, Run: run, LeaseSeconds: 600,
		RequestedCapabilities: map[string]any{"platformName": "Android"}, IdempotencyKey: "example-reserve-0001"})
	if err != nil {
		log.Fatal(err)
	}
	lease, err := client.WaitActive(ctx, reservation.ID, run)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("reservation=%s device=%s appium=%s udid=%s\n", reservation.ID, lease.Device.ID, lease.AppiumEndpoint, lease.AppiumUDID)
	if _, err := client.Release(ctx, reservation.ID, "example completed", "example-release-0001", run); err != nil {
		log.Fatal(err)
	}
}
