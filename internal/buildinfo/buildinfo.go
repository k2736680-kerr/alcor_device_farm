package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

type Info struct {
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
	OS        string
	Arch      string
}

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// String returns one stable, single-line representation for command output and
// deployment diagnostics. Values are normally injected with linker flags.
func String(component string) string {
	info := Values()
	return fmt.Sprintf(
		"%s version=%s commit=%s build_date=%s go=%s os=%s arch=%s",
		normalize(component, "unknown"),
		info.Version,
		info.Commit,
		info.BuildDate,
		info.GoVersion,
		info.OS,
		info.Arch,
	)
}

func Values() Info {
	return Info{
		Version: normalize(version, "dev"), BuildDate: normalize(buildDate, "unknown"),
		Commit: normalize(commit, "unknown"), GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
	}
}

func normalize(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
