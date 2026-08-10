//go:build linux

package hostcapacity

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func systemMemoryMB() (int64, int64, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, 0, err
	}
	unit := uint64(info.Unit)
	if unit == 0 {
		unit = 1
	}
	total := int64(uint64(info.Totalram) * unit / 1024 / 1024)
	available := int64(uint64(info.Freeram+info.Bufferram) * unit / 1024 / 1024)
	if file, err := os.Open("/proc/meminfo"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "MemAvailable:" {
				if kb, parseErr := strconv.ParseInt(fields[1], 10, 64); parseErr == nil {
					available = kb / 1024
				}
				break
			}
		}
	}
	return total, available, nil
}

func systemDiskMB(path string) (int64, int64, error) {
	var info unix.Statfs_t
	if err := unix.Statfs(path, &info); err != nil {
		return 0, 0, err
	}
	return int64(info.Blocks) * int64(info.Bsize) / 1024 / 1024, int64(info.Bavail) * int64(info.Bsize) / 1024 / 1024, nil
}
