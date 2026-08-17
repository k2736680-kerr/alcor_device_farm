package ioshost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appiumdevicefarm"
)

type fakeRunner map[string]struct {
	output string
	err    error
}

type hangingDoctorRunner struct{ fakeRunner }

func (runner hangingDoctorRunner) Run(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
	if binary+" "+strings.Join(arguments, " ") == "appium driver doctor xcuitest" {
		<-ctx.Done()
		return []byte("HOME is set to: /Users/test\nXcode is installed at /Applications/Xcode.app\nXcode Command Line Tools are installed and work properly\n"), ctx.Err()
	}
	return runner.fakeRunner.Run(ctx, binary, arguments...)
}

func (runner fakeRunner) Run(_ context.Context, binary string, arguments ...string) ([]byte, error) {
	value, exists := runner[binary+" "+strings.Join(arguments, " ")]
	if !exists {
		return nil, errors.New("unexpected command")
	}
	return []byte(value.output), value.err
}

type fakeHealth struct {
	value appiumdevicefarm.NodeHealth
	err   error
}

func (health fakeHealth) Health(context.Context) (appiumdevicefarm.NodeHealth, error) {
	return health.value, health.err
}

func TestProbeReportsPinnedReadyToolchainWithoutCommandOutput(t *testing.T) {
	runner := fakeRunner{
		"sw -productVersion": {output: "26.5.1\n"}, "sw -buildVersion": {output: "25F80\n"}, "uname -m": {output: "arm64\n"},
		"xcode -version": {output: "Xcode 26.3\nBuild version 17C529\n"}, "xcode -checkFirstLaunchStatus": {},
		"xcrun simctl list runtimes --json": {output: `{"runtimes":[{"name":"iOS 26.3","version":"26.3","isAvailable":true},{"name":"tvOS 26.3","version":"26.3","isAvailable":true}]}`},
		"node --version":                    {output: "v22.23.2\n"}, "appium --version": {output: "3.6.0\n"},
		"appium plugin list --installed --json": {output: `{"device-farm":{"version":"12.0.1"}}`},
		"appium driver list --installed --json": {output: `{"xcuitest":{"version":"12.4.0"}}`},
		"ios version":                           {output: "go-ios 1.3.2\n"}, "appium driver doctor xcuitest": {output: "all checks passed"},
	}
	probe, err := New(Config{SWVersBinary: "sw", UnameBinary: "uname", XcodebuildBinary: "xcode", XcrunBinary: "xcrun", NodeBinary: "node", AppiumBinary: "appium", GoIOSBinary: "ios",
		WDAPackageJSON: "wda.json", CacheTTL: time.Minute, Runner: runner, ReadFile: func(string) ([]byte, error) { return []byte(`{"version":"16.2.0","secret":"must-not-leak"}`), nil },
		NodeHealth: fakeHealth{value: appiumdevicefarm.NodeHealth{AppiumReady: true, PluginReady: true, PluginVersion: "12.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := probe.Snapshot(context.Background())
	readiness := snapshot["host_readiness"].(map[string]any)
	if err != nil || readiness["ready"] != true || snapshot["host_os"] != "macos" || snapshot["host_arch"] != "arm64" || strings.Contains(strings.ToLower(strings.TrimSpace(toJSON(snapshot))), "must-not-leak") {
		t.Fatalf("snapshot=%+v error=%v", snapshot, err)
	}
}

func TestProbeFailsClosedOnVersionDriftAndDoctorFailure(t *testing.T) {
	runner := fakeRunner{
		"sw -productVersion": {output: "26.5.1"}, "sw -buildVersion": {output: "25F80"}, "uname -m": {output: "arm64"},
		"xcode -version": {output: "Xcode 26.3\nBuild version 17C529"}, "xcode -checkFirstLaunchStatus": {},
		"xcrun simctl list runtimes --json": {output: `{"runtimes":[{"name":"iOS 26.3","version":"26.3","isAvailable":true}]}`},
		"node --version":                    {output: "v26.7.0"}, "appium --version": {output: "3.6.0"},
		"appium plugin list --installed --json": {output: `{"device-farm":{"version":"12.0.1"}}`},
		"appium driver list --installed --json": {output: `{"xcuitest":{"version":"12.4.0"}}`},
		"ios version":                           {output: "1.3.2"}, "appium driver doctor xcuitest": {err: errors.New("doctor failed")},
	}
	probe, _ := New(Config{SWVersBinary: "sw", UnameBinary: "uname", XcodebuildBinary: "xcode", XcrunBinary: "xcrun", NodeBinary: "node", AppiumBinary: "appium", GoIOSBinary: "ios", WDAPackageJSON: "wda.json", Runner: runner,
		ReadFile: func(string) ([]byte, error) { return []byte(`{"version":"16.2.0"}`), nil }, NodeHealth: fakeHealth{value: appiumdevicefarm.NodeHealth{AppiumReady: true, PluginReady: true, PluginVersion: "12.0.1"}}})
	snapshot, _ := probe.Snapshot(context.Background())
	if snapshot["host_readiness"].(map[string]any)["ready"] != false {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestProbeBoundsDoctorOptionalCheckAfterRequiredChecksPass(t *testing.T) {
	runner := fakeRunner{
		"sw -productVersion": {output: "26.5.1"}, "sw -buildVersion": {output: "25F80"}, "uname -m": {output: "arm64"},
		"xcode -version": {output: "Xcode 26.3\nBuild version 17C529"}, "xcode -checkFirstLaunchStatus": {},
		"xcrun simctl list runtimes --json": {output: `{"runtimes":[{"name":"iOS 26.3","version":"26.3","isAvailable":true}]}`},
		"node --version":                    {output: "v22.23.2"}, "appium --version": {output: "3.6.0"},
		"appium plugin list --installed --json": {output: `{"device-farm":{"version":"12.0.1"}}`},
		"appium driver list --installed --json": {output: `{"xcuitest":{"version":"12.4.0"}}`},
		"ios version":                           {output: "1.3.2"},
	}
	probe, err := New(Config{SWVersBinary: "sw", UnameBinary: "uname", XcodebuildBinary: "xcode", XcrunBinary: "xcrun", NodeBinary: "node", AppiumBinary: "appium", GoIOSBinary: "ios",
		WDAPackageJSON: "wda.json", DoctorTimeout: 10 * time.Millisecond, Runner: hangingDoctorRunner{fakeRunner: runner},
		ReadFile: func(string) ([]byte, error) { return []byte(`{"version":"16.2.0"}`), nil }, NodeHealth: fakeHealth{value: appiumdevicefarm.NodeHealth{AppiumReady: true, PluginReady: true, PluginVersion: "12.0.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := probe.Snapshot(context.Background())
	if err != nil || snapshot["host_readiness"].(map[string]any)["ready"] != true {
		t.Fatalf("snapshot=%+v error=%v", snapshot, err)
	}
}

func toJSON(value any) string { content, _ := json.Marshal(value); return string(content) }
