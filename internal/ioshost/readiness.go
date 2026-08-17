package ioshost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/appiumdevicefarm"
)

const (
	ExpectedNodeVersion     = "22.23.2"
	ExpectedAppiumVersion   = "3.6.0"
	ExpectedPluginVersion   = appiumdevicefarm.PinnedVersion
	ExpectedXCUITestVersion = "12.4.0"
	ExpectedWDAVersion      = "16.2.0"
	ExpectedGoIOSVersion    = "1.3.2"
	defaultCacheTTL         = 30 * time.Second
)

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type NodeHealthProbe interface {
	Health(context.Context) (appiumdevicefarm.NodeHealth, error)
}

type Config struct {
	SWVersBinary     string
	UnameBinary      string
	XcodebuildBinary string
	XcrunBinary      string
	NodeBinary       string
	AppiumBinary     string
	GoIOSBinary      string
	WDAPackageJSON   string
	CacheTTL         time.Duration
	Runner           Runner
	ReadFile         func(string) ([]byte, error)
	NodeHealth       NodeHealthProbe
}

type Probe struct {
	config   Config
	mu       sync.Mutex
	cached   map[string]any
	cachedAt time.Time
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, binary, arguments...).CombinedOutput()
}

func New(config Config) (*Probe, error) {
	defaults := map[*string]string{
		&config.SWVersBinary: "sw_vers", &config.UnameBinary: "uname", &config.XcodebuildBinary: "xcodebuild",
		&config.XcrunBinary: "xcrun", &config.NodeBinary: "node", &config.AppiumBinary: "appium", &config.GoIOSBinary: "ios",
	}
	for target, fallback := range defaults {
		if strings.TrimSpace(*target) == "" {
			*target = fallback
		}
	}
	if strings.TrimSpace(config.WDAPackageJSON) == "" || config.NodeHealth == nil {
		return nil, errors.New("WDA package metadata and Appium Device Farm health probe are required")
	}
	if config.Runner == nil {
		config.Runner = execRunner{}
	}
	if config.ReadFile == nil {
		config.ReadFile = os.ReadFile
	}
	if config.CacheTTL <= 0 {
		config.CacheTTL = defaultCacheTTL
	}
	return &Probe{config: config}, nil
}

func (probe *Probe) Snapshot(ctx context.Context) (map[string]any, error) {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.cached != nil && time.Since(probe.cachedAt) < probe.config.CacheTTL {
		return cloneMap(probe.cached), nil
	}
	result := probe.collect(ctx)
	probe.cached, probe.cachedAt = cloneMap(result), time.Now()
	return result, nil
}

func (probe *Probe) collect(ctx context.Context) map[string]any {
	components := map[string]any{}
	reasons := make([]any, 0)
	ready := true
	record := func(name, version, expected string, err error) {
		status := "passed"
		if err != nil || (expected != "" && version != expected) {
			status, ready = "failed", false
			reasons = append(reasons, name+"_not_ready")
		}
		entry := map[string]any{"status": status}
		if version != "" {
			entry["version"] = version
		}
		if expected != "" {
			entry["expected_version"] = expected
		}
		components[name] = entry
	}

	macOSVersion, macOSErr := probe.commandText(ctx, probe.config.SWVersBinary, "-productVersion")
	macOSBuild, macOSBuildErr := probe.commandText(ctx, probe.config.SWVersBinary, "-buildVersion")
	arch, archErr := probe.commandText(ctx, probe.config.UnameBinary, "-m")
	record("macos", macOSVersion, "", errors.Join(macOSErr, macOSBuildErr, archErr))

	xcodeOutput, xcodeErr := probe.config.Runner.Run(ctx, probe.config.XcodebuildBinary, "-version")
	xcodeVersion, xcodeBuild := parseXcodeVersion(string(xcodeOutput))
	licenseErr := probe.commandOnly(ctx, probe.config.XcodebuildBinary, "-checkFirstLaunchStatus")
	record("xcode", xcodeVersion, "", errors.Join(xcodeErr, licenseErr))
	if entry, ok := components["xcode"].(map[string]any); ok && xcodeBuild != "" {
		entry["build"] = xcodeBuild
	}

	runtimes, runtimeErr := probe.iosRuntimes(ctx)
	record("ios_runtime", fmt.Sprintf("%d", len(runtimes)), "", runtimeErr)
	if len(runtimes) == 0 {
		ready = false
		reasons = append(reasons, "ios_runtime_not_ready")
		components["ios_runtime"] = map[string]any{"status": "failed", "available": []string{}}
	} else {
		components["ios_runtime"] = map[string]any{"status": "passed", "available": stringsToAny(runtimes)}
	}

	nodeVersion, nodeErr := probe.versionCommand(ctx, probe.config.NodeBinary, "--version")
	record("node", nodeVersion, ExpectedNodeVersion, nodeErr)
	appiumVersion, appiumErr := probe.versionCommand(ctx, probe.config.AppiumBinary, "--version")
	record("appium", appiumVersion, ExpectedAppiumVersion, appiumErr)

	pluginOutput, pluginErr := probe.config.Runner.Run(ctx, probe.config.AppiumBinary, "plugin", "list", "--installed", "--json")
	pluginVersion := extensionVersion(pluginOutput, "device-farm")
	if pluginVersion == "" && pluginErr == nil {
		pluginErr = errors.New("device-farm plugin is absent")
	}
	record("appium_device_farm", pluginVersion, ExpectedPluginVersion, pluginErr)

	driverOutput, driverErr := probe.config.Runner.Run(ctx, probe.config.AppiumBinary, "driver", "list", "--installed", "--json")
	driverVersion := extensionVersion(driverOutput, "xcuitest")
	if driverVersion == "" && driverErr == nil {
		driverErr = errors.New("xcuitest driver is absent")
	}
	record("xcuitest", driverVersion, ExpectedXCUITestVersion, driverErr)

	wdaVersion, wdaErr := packageVersion(probe.config.ReadFile, probe.config.WDAPackageJSON)
	record("wda", wdaVersion, ExpectedWDAVersion, wdaErr)
	goIOSVersion, goIOSErr := probe.versionCommand(ctx, probe.config.GoIOSBinary, "version")
	record("go_ios", goIOSVersion, ExpectedGoIOSVersion, goIOSErr)
	doctorErr := probe.commandOnly(ctx, probe.config.AppiumBinary, "driver", "doctor", "xcuitest")
	record("appium_doctor", "", "", doctorErr)

	nodeHealth, healthErr := probe.config.NodeHealth.Health(ctx)
	nodeVersion = nodeHealth.PluginVersion
	if healthErr == nil && !nodeHealth.Ready() {
		healthErr = errors.New("Appium or Device Farm node is not ready")
	}
	record("appium_node", nodeVersion, "", healthErr)

	return map[string]any{
		"macos_version": macOSVersion, "macos_build": macOSBuild, "xcode_version": xcodeVersion, "xcode_build": xcodeBuild,
		"ios_runtimes": stringsToAny(runtimes), "toolchain_components": components,
		"host_readiness": map[string]any{"ready": ready, "reasons": reasons}, "host_arch": arch, "host_os": "macos",
	}
}

func (probe *Probe) commandText(ctx context.Context, binary string, arguments ...string) (string, error) {
	output, err := probe.config.Runner.Run(ctx, binary, arguments...)
	return strings.TrimSpace(string(output)), err
}

func (probe *Probe) commandOnly(ctx context.Context, binary string, arguments ...string) error {
	_, err := probe.config.Runner.Run(ctx, binary, arguments...)
	return err
}

func (probe *Probe) versionCommand(ctx context.Context, binary string, arguments ...string) (string, error) {
	value, err := probe.commandText(ctx, binary, arguments...)
	return versionPattern.FindString(value), err
}

func (probe *Probe) iosRuntimes(ctx context.Context) ([]string, error) {
	output, err := probe.config.Runner.Run(ctx, probe.config.XcrunBinary, "simctl", "list", "runtimes", "--json")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Runtimes []struct {
			Name      string `json:"name"`
			Version   string `json:"version"`
			Available bool   `json:"isAvailable"`
		} `json:"runtimes"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, err
	}
	result := make([]string, 0)
	for _, runtime := range payload.Runtimes {
		if runtime.Available && strings.HasPrefix(strings.ToLower(runtime.Name), "ios") {
			result = append(result, strings.TrimSpace(runtime.Version))
		}
	}
	sort.Strings(result)
	return result, nil
}

func parseXcodeVersion(output string) (string, string) {
	var version, build string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.EqualFold(fields[0], "Xcode") {
			version = fields[1]
		}
		if len(fields) >= 3 && strings.EqualFold(fields[0], "Build") && strings.EqualFold(fields[1], "version") {
			build = fields[2]
		}
	}
	return version, build
}

func extensionVersion(output []byte, name string) string {
	var payload map[string]any
	if json.Unmarshal(output, &payload) != nil {
		return ""
	}
	value, exists := payload[name]
	if !exists {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return versionPattern.FindString(typed)
	case map[string]any:
		for _, key := range []string{"version", "pkgVersion", "installedVersion"} {
			if text, ok := typed[key].(string); ok {
				return versionPattern.FindString(text)
			}
		}
	}
	return ""
}

func packageVersion(readFile func(string) ([]byte, error), path string) (string, error) {
	content, err := readFile(path)
	if err != nil {
		return "", err
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", err
	}
	if payload.Version == "" {
		return "", errors.New("package version is missing")
	}
	return payload.Version, nil
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index := range values {
		result[index] = values[index]
	}
	return result
}

func cloneMap(source map[string]any) map[string]any {
	content, _ := json.Marshal(source)
	var result map[string]any
	_ = json.Unmarshal(content, &result)
	return result
}
