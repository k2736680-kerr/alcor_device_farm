package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type DeviceLifecycleStatus string
type HealthStatus string

const (
	DeviceProvisioning DeviceLifecycleStatus = "provisioning"
	DeviceBooting      DeviceLifecycleStatus = "booting"
	DeviceReady        DeviceLifecycleStatus = "ready"
	DeviceReserved     DeviceLifecycleStatus = "reserved"
	DeviceBusy         DeviceLifecycleStatus = "busy"
	DeviceRecycling    DeviceLifecycleStatus = "recycling"
	DeviceStopped      DeviceLifecycleStatus = "stopped"
	DeviceQuarantined  DeviceLifecycleStatus = "quarantined"
	DeviceDeleted      DeviceLifecycleStatus = "deleted"

	HealthUnknown   HealthStatus = "unknown"
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
)

var deviceTransitions = map[DeviceLifecycleStatus]map[DeviceLifecycleStatus]struct{}{
	DeviceProvisioning: allowed(DeviceBooting, DeviceQuarantined, DeviceDeleted),
	DeviceBooting:      allowed(DeviceReady, DeviceStopped, DeviceQuarantined),
	DeviceReady:        allowed(DeviceReserved, DeviceStopped, DeviceQuarantined, DeviceDeleted),
	DeviceReserved:     allowed(DeviceBusy, DeviceRecycling, DeviceQuarantined),
	DeviceBusy:         allowed(DeviceRecycling, DeviceQuarantined),
	DeviceRecycling:    allowed(DeviceReady, DeviceStopped, DeviceQuarantined),
	DeviceStopped:      allowed(DeviceBooting, DeviceQuarantined, DeviceDeleted),
	DeviceQuarantined:  allowed(DeviceProvisioning, DeviceDeleted),
	DeviceDeleted:      allowed[DeviceLifecycleStatus](),
}

var healthTransitions = map[HealthStatus]map[HealthStatus]struct{}{
	HealthUnknown:   allowed(HealthHealthy, HealthDegraded, HealthUnhealthy),
	HealthHealthy:   allowed(HealthUnknown, HealthDegraded, HealthUnhealthy),
	HealthDegraded:  allowed(HealthUnknown, HealthHealthy, HealthUnhealthy),
	HealthUnhealthy: allowed(HealthUnknown, HealthHealthy, HealthDegraded),
}

type Device struct {
	lifecycle stateMachine[DeviceLifecycleStatus]
	health    HealthStatus
	events    []TransitionEvent
}

func NewDevice(id string) (*Device, error) {
	return RestoreDevice(id, DeviceProvisioning, HealthUnknown)
}

func RestoreDevice(id string, lifecycle DeviceLifecycleStatus, health HealthStatus) (*Device, error) {
	state, err := newStateMachine("device", id, "lifecycle_status", lifecycle, validDeviceLifecycleStatus, deviceTransitions)
	if err != nil {
		return nil, err
	}
	if !validHealthStatus(health) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, health)
	}
	return &Device{lifecycle: state, health: health}, nil
}

func (device *Device) ID() string                       { return device.lifecycle.idValue() }
func (device *Device) Lifecycle() DeviceLifecycleStatus { return device.lifecycle.statusValue() }
func (device *Device) Health() HealthStatus             { return device.health }

func (device *Device) Transition(to DeviceLifecycleStatus, reason string, at time.Time) error {
	if to == DeviceReady && device.health != HealthHealthy {
		if strings.TrimSpace(reason) != "" && !at.IsZero() {
			device.lifecycle.recordRejected(to, reason, at, "DEVICE_NOT_HEALTHY")
		}
		return ErrDeviceNotHealthy
	}
	return device.lifecycle.transition(to, reason, at)
}

func (device *Device) UpdateHealth(to HealthStatus, reason string, at time.Time) error {
	if !validHealthStatus(to) {
		return fmt.Errorf("%w: %s", ErrInvalidStatus, to)
	}
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	if at.IsZero() {
		return ErrTimeRequired
	}
	if _, ok := healthTransitions[device.health][to]; !ok {
		device.events = append(device.events, TransitionEvent{
			Resource: "device", ID: device.ID(), Field: "health_status",
			From: string(device.health), To: string(to), Reason: strings.TrimSpace(reason), At: at,
			Accepted: false, ErrorCode: "INVALID_STATE_TRANSITION",
		})
		return &TransitionError{
			Resource: "device", ID: device.ID(), Field: "health_status",
			From: string(device.health), To: string(to),
		}
	}
	from := device.health
	device.health = to
	device.events = append(device.events, TransitionEvent{
		Resource: "device", ID: device.ID(), Field: "health_status",
		From: string(from), To: string(to), Reason: strings.TrimSpace(reason), At: at,
		Accepted: true,
	})
	return nil
}

func (device *Device) IsSchedulable() bool {
	return device.Lifecycle() == DeviceReady && device.Health() == HealthHealthy
}

func (device *Device) Events() []TransitionEvent {
	result := device.lifecycle.eventsCopy()
	result = append(result, device.events...)
	sort.SliceStable(result, func(left, right int) bool { return result[left].At.Before(result[right].At) })
	return result
}

func (device *Device) ClearEvents() {
	device.lifecycle.clearEvents()
	device.events = nil
}

func validDeviceLifecycleStatus(status DeviceLifecycleStatus) bool {
	_, ok := deviceTransitions[status]
	return ok
}

func validHealthStatus(status HealthStatus) bool {
	_, ok := healthTransitions[status]
	return ok
}
