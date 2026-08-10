package runtimeprofile

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	GraphicsAuto     = "auto"
	GraphicsHost     = "host"
	GraphicsSoftware = "software"
)

// Profile is the complete resource contract for one Docker Android Emulator.
// It deliberately separates the Docker limit from the Android guest setting.
type Profile struct {
	ContainerCPUCores float64 `json:"container_cpu_cores"`
	ContainerMemoryMB int64   `json:"container_memory_mb"`
	GuestCPUCores     int     `json:"guest_cpu_cores"`
	GuestMemoryMB     int64   `json:"guest_memory_mb"`
	DataDiskMB        int64   `json:"data_disk_mb"`
	ImageDiskMB       int64   `json:"image_disk_mb,omitempty"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DensityDPI        int     `json:"density_dpi"`
	VMHeapMB          int64   `json:"vm_heap_mb"`
	Graphics          string  `json:"graphics"`
}

func Default() Profile {
	return Profile{
		ContainerCPUCores: 4, ContainerMemoryMB: 5120,
		GuestCPUCores: 4, GuestMemoryMB: 4096,
		DataDiskMB: 4096, Width: 1080, Height: 2400, DensityDPI: 420,
		VMHeapMB: 512, Graphics: GraphicsAuto,
	}
}

// Parse accepts the canonical keys and the old cpu/memory_mb keys so existing
// registered Images keep working while the API is migrated.
func Parse(values map[string]any) (Profile, error) {
	result := Default()
	if values == nil {
		return result, nil
	}
	var err error
	if result.ContainerCPUCores, err = floatValue(values, result.ContainerCPUCores, "container_cpu_cores", "cpu"); err != nil {
		return Profile{}, err
	}
	fields := []struct {
		target *int64
		keys   []string
	}{
		{&result.ContainerMemoryMB, []string{"container_memory_mb", "memory_mb"}},
		{&result.GuestMemoryMB, []string{"guest_memory_mb"}},
		{&result.DataDiskMB, []string{"data_disk_mb"}},
		{&result.ImageDiskMB, []string{"image_disk_mb"}},
		{&result.VMHeapMB, []string{"vm_heap_mb"}},
	}
	for _, field := range fields {
		if *field.target, err = int64Value(values, *field.target, field.keys...); err != nil {
			return Profile{}, err
		}
	}
	ints := []struct {
		target *int
		keys   []string
	}{
		{&result.GuestCPUCores, []string{"guest_cpu_cores"}},
		{&result.Width, []string{"width"}},
		{&result.Height, []string{"height"}},
		{&result.DensityDPI, []string{"density_dpi"}},
	}
	for _, field := range ints {
		value, parseErr := int64Value(values, int64(*field.target), field.keys...)
		if parseErr != nil || value > math.MaxInt {
			if parseErr == nil {
				parseErr = errors.New("integer exceeds platform range")
			}
			return Profile{}, parseErr
		}
		*field.target = int(value)
	}
	if !hasAny(values, "guest_cpu_cores") {
		result.GuestCPUCores = maxInt(1, minInt(result.GuestCPUCores, int(math.Floor(result.ContainerCPUCores))))
	}
	if !hasAny(values, "guest_memory_mb") {
		result.GuestMemoryMB = maxInt64(1536, minInt64(result.GuestMemoryMB, result.ContainerMemoryMB-1024))
		if result.GuestMemoryMB > result.ContainerMemoryMB-512 {
			result.GuestMemoryMB = result.ContainerMemoryMB - 512
		}
	}
	if value, ok := values["graphics"]; ok {
		text, valid := value.(string)
		if !valid {
			return Profile{}, errors.New("graphics must be a string")
		}
		result.Graphics = strings.ToLower(strings.TrimSpace(text))
	}
	if err := result.Validate(); err != nil {
		return Profile{}, err
	}
	return result, nil
}

func (profile Profile) Validate() error {
	if profile.ContainerCPUCores < 1 || profile.ContainerCPUCores > 64 ||
		profile.ContainerMemoryMB < 2048 || profile.ContainerMemoryMB > 262144 ||
		profile.GuestCPUCores < 1 || profile.GuestCPUCores > 32 ||
		profile.GuestMemoryMB < 1536 || profile.GuestMemoryMB > profile.ContainerMemoryMB-512 ||
		profile.DataDiskMB < 2048 || profile.DataDiskMB > 1048576 || profile.ImageDiskMB < 0 ||
		profile.Width < 320 || profile.Width > 4320 || profile.Height < 480 || profile.Height > 7680 ||
		profile.DensityDPI < 120 || profile.DensityDPI > 960 || profile.VMHeapMB < 128 || profile.VMHeapMB > profile.GuestMemoryMB/2 {
		return errors.New("emulator runtime profile is outside supported limits")
	}
	switch profile.Graphics {
	case GraphicsAuto, GraphicsHost, GraphicsSoftware:
	default:
		return errors.New("graphics must be auto, host or software")
	}
	return nil
}

func (profile Profile) Map() map[string]any {
	return map[string]any{
		"container_cpu_cores": profile.ContainerCPUCores, "container_memory_mb": profile.ContainerMemoryMB,
		"guest_cpu_cores": profile.GuestCPUCores, "guest_memory_mb": profile.GuestMemoryMB,
		"data_disk_mb": profile.DataDiskMB, "image_disk_mb": profile.ImageDiskMB,
		"width": profile.Width, "height": profile.Height, "density_dpi": profile.DensityDPI,
		"vm_heap_mb": profile.VMHeapMB, "graphics": profile.Graphics,
	}
}

func floatValue(values map[string]any, fallback float64, keys ...string) (float64, error) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed, nil
		case float32:
			return float64(typed), nil
		case int:
			return float64(typed), nil
		case int64:
			return float64(typed), nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err == nil {
				return parsed, nil
			}
		}
		return 0, fmt.Errorf("%s must be numeric", key)
	}
	return fallback, nil
}

func int64Value(values map[string]any, fallback int64, keys ...string) (int64, error) {
	value, err := floatValue(values, float64(fallback), keys...)
	if err != nil || math.Trunc(value) != value || value > math.MaxInt64 || value < math.MinInt64 {
		if err == nil {
			err = fmt.Errorf("%s must be an integer", keys[0])
		}
		return 0, err
	}
	return int64(value), nil
}

func hasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
