//go:build darwin

package hostcapacity

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func systemMemoryMB() (int64, int64, error) {
	totalOutput, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, err
	}
	totalBytes, err := strconv.ParseInt(strings.TrimSpace(string(totalOutput)), 10, 64)
	if err != nil || totalBytes <= 0 {
		return 0, 0, fmt.Errorf("macOS 内存总量响应无效")
	}
	vmOutput, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, 0, err
	}
	pageSize := int64(4096)
	availablePages := int64(0)
	for index, line := range strings.Split(string(vmOutput), "\n") {
		if index == 0 {
			if marker := strings.Index(line, "page size of "); marker >= 0 {
				fields := strings.Fields(line[marker+len("page size of "):])
				if len(fields) > 0 {
					if value, parseErr := strconv.ParseInt(fields[0], 10, 64); parseErr == nil && value > 0 {
						pageSize = value
					}
				}
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name != "Pages free" && name != "Pages inactive" && name != "Pages speculative" && name != "Pages purgeable" {
			continue
		}
		value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "."))
		pages, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr == nil && pages > 0 {
			availablePages += pages
		}
	}
	return totalBytes / (1024 * 1024), availablePages * pageSize / (1024 * 1024), nil
}

func systemDiskMB(path string) (int64, int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	return int64(stat.Blocks) * int64(stat.Bsize) / (1024 * 1024),
		int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024), nil
}
