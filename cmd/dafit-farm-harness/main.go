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
	serverURL := flag.String("server-url", "http://127.0.0.1:8080", "设备农场 API 地址")
	poolID := flag.String("pool-id", "", "设备池 ID")
	ownerID := flag.String("owner-id", "", "test_run 所有者 ID；为空时自动生成")
	dafitDirectory := flag.String("dafit-dir", "", "dafit_auto_platform 绝对路径")
	reportDirectory := flag.String("report-dir", "", "本次运行的报告绝对路径")
	caseID := flag.String("case", "STEPS_SMOKE_001", "已有 DaFit 用例 ID")
	python := flag.String("python", "python", "Python 可执行文件")
	adb := flag.String("adb", "adb", "ADB 可执行文件")
	lease := flag.Int("lease-seconds", 900, "预约租期秒数，运行期间会自动续约")
	wait := flag.Duration("wait-timeout", 5*time.Minute, "等待设备容量的超时时间")
	runTimeout := flag.Duration("run-timeout", 0, "DaFit 运行超时；0 表示不设置固定上限")
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
