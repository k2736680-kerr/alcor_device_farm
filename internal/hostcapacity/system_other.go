//go:build !linux && !darwin

package hostcapacity

import "errors"

func systemMemoryMB() (int64, int64, error) {
	return 0, 0, errors.New("host capacity probe requires Linux")
}
func systemDiskMB(string) (int64, int64, error) {
	return 0, 0, errors.New("host capacity probe requires Linux")
}
