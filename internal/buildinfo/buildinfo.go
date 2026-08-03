package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// String returns one stable, single-line representation for command output and
// deployment diagnostics. Values are normally injected with linker flags.
func String(component string) string {
	return fmt.Sprintf(
		"%s version=%s commit=%s build_date=%s go=%s os=%s arch=%s",
		normalize(component, "unknown"),
		normalize(version, "dev"),
		normalize(commit, "unknown"),
		normalize(buildDate, "unknown"),
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}

func normalize(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
