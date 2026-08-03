package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	dafit "github.com/Ad-Quanta/alcor-device-farm/harness/dafit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
)

func main() {
	serverURL := flag.String("server-url", "http://127.0.0.1:8080", "Device Farm API base URL")
	poolID := flag.String("pool-id", "", "Device Pool ID")
	ownerID := flag.String("owner-id", "", "test_run owner ID; generated when empty")
	dafitDirectory := flag.String("dafit-dir", "", "absolute dafit_auto_platform directory")
	reportDirectory := flag.String("report-dir", "", "absolute report directory for this run")
	caseID := flag.String("case", "STEPS_SMOKE_001", "existing DaFit case ID")
	python := flag.String("python", "python", "Python executable")
	adb := flag.String("adb", "adb", "ADB executable")
	lease := flag.Int("lease-seconds", 900, "reservation lease seconds")
	wait := flag.Duration("wait-timeout", 5*time.Minute, "capacity wait timeout")
	runTimeout := flag.Duration("run-timeout", 30*time.Minute, "DaFit command timeout")
	flag.Parse()

	if *ownerID == "" {
		generated, err := identifier.New()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		*ownerID = generated
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := dafit.New(nil, nil).Run(ctx, dafit.Config{
		ServerURL: *serverURL, ServiceToken: os.Getenv("DEVICE_FARM_SECURITY_SERVICE_TOKEN"),
		PoolID: *poolID, OwnerID: *ownerID, LeaseSeconds: *lease,
		RequestedCapabilities: map[string]any{"platformName": "Android"},
		DaFitDirectory:        *dafitDirectory, ReportDirectory: *reportDirectory,
		PythonExecutable: *python, ADBExecutable: *adb, CaseID: *caseID,
		WaitTimeout: *wait, PollInterval: 500 * time.Millisecond, RunTimeout: *runTimeout,
	})
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
