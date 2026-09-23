package domain

import (
	"errors"
	"testing"
	"time"
)

var transitionTime = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

type restoredMachine[S comparable] struct {
	transition func(S) error
	status     func() S
	events     func() []TransitionEvent
}

func TestAllDefinedStateMachineTransitions(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		verifyTransitions(t, imageTransitions, func(from ImageStatus) (restoredMachine[ImageStatus], error) {
			value, err := RestoreImage("image_00000000000001", from)
			return restoredMachine[ImageStatus]{
				transition: func(to ImageStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Status, events: value.Events,
			}, err
		})
	})
	t.Run("host", func(t *testing.T) {
		verifyTransitions(t, hostTransitions, func(from HostStatus) (restoredMachine[HostStatus], error) {
			value, err := RestoreHost("host_000000000000001", from)
			return restoredMachine[HostStatus]{
				transition: func(to HostStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Status, events: value.Events,
			}, err
		})
	})
	t.Run("pool", func(t *testing.T) {
		verifyTransitions(t, poolTransitions, func(from PoolStatus) (restoredMachine[PoolStatus], error) {
			value, err := RestorePool("pool_000000000000001", from)
			return restoredMachine[PoolStatus]{
				transition: func(to PoolStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Status, events: value.Events,
			}, err
		})
	})
	t.Run("reservation", func(t *testing.T) {
		verifyTransitions(t, reservationTransitions, func(from ReservationStatus) (restoredMachine[ReservationStatus], error) {
			value, err := RestoreReservation("reservation_0000000001", from)
			return restoredMachine[ReservationStatus]{
				transition: func(to ReservationStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Status, events: value.Events,
			}, err
		})
	})
	t.Run("session", func(t *testing.T) {
		verifyTransitions(t, sessionTransitions, func(from SessionStatus) (restoredMachine[SessionStatus], error) {
			value, err := RestoreSession("session_000000000001", from)
			return restoredMachine[SessionStatus]{
				transition: func(to SessionStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Status, events: value.Events,
			}, err
		})
	})
	t.Run("command", func(t *testing.T) {
		verifyTransitions(t, commandTransitions, func(from CommandStatus) (restoredMachine[CommandStatus], error) {
			value, err := RestoreCommand("command_000000000001", from)
			return restoredMachine[CommandStatus]{
				transition: func(to CommandStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Status, events: value.Events,
			}, err
		})
	})
	t.Run("device lifecycle", func(t *testing.T) {
		verifyTransitions(t, deviceTransitions, func(from DeviceLifecycleStatus) (restoredMachine[DeviceLifecycleStatus], error) {
			value, err := RestoreDevice("device_0000000000001", from, HealthHealthy)
			return restoredMachine[DeviceLifecycleStatus]{
				transition: func(to DeviceLifecycleStatus) error { return value.Transition(to, "test", transitionTime) },
				status:     value.Lifecycle, events: value.Events,
			}, err
		})
	})
	t.Run("device health", func(t *testing.T) {
		verifyTransitions(t, healthTransitions, func(from HealthStatus) (restoredMachine[HealthStatus], error) {
			value, err := RestoreDevice("device_0000000000001", DeviceBooting, from)
			return restoredMachine[HealthStatus]{
				transition: func(to HealthStatus) error { return value.UpdateHealth(to, "test", transitionTime) },
				status:     value.Health, events: value.Events,
			}, err
		})
	})
}

func verifyTransitions[S ~string](
	t *testing.T,
	transitions map[S]map[S]struct{},
	restore func(S) (restoredMachine[S], error),
) {
	t.Helper()
	for from := range transitions {
		for to := range transitions {
			from, to := from, to
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				machine, err := restore(from)
				if err != nil {
					t.Fatalf("restore: %v", err)
				}
				_, allowed := transitions[from][to]
				err = machine.transition(to)
				if allowed {
					if err != nil {
						t.Fatalf("legal transition failed: %v", err)
					}
					if machine.status() != to {
						t.Fatalf("status = %s, want %s", machine.status(), to)
					}
					events := machine.events()
					if len(events) != 1 || events[0].From != string(from) || events[0].To != string(to) {
						t.Fatalf("events = %#v", events)
					}
					return
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("illegal transition error = %v", err)
				}
				if machine.status() != from {
					t.Fatalf("illegal transition changed status to %s", machine.status())
				}
				events := machine.events()
				if len(events) != 1 || events[0].Accepted || events[0].ErrorCode != "INVALID_STATE_TRANSITION" {
					t.Fatalf("rejected transition event = %#v", events)
				}
			})
		}
	}
}

func TestDeviceReadinessAndHealthAreSeparated(t *testing.T) {
	device, err := RestoreDevice("device_0000000000001", DeviceBooting, HealthUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := device.Transition(DeviceReady, "boot completed", transitionTime); !errors.Is(err, ErrDeviceNotHealthy) {
		t.Fatalf("unhealthy ready transition error = %v", err)
	}
	if device.Lifecycle() != DeviceBooting || device.Health() != HealthUnknown {
		t.Fatal("failed readiness transition changed device state")
	}
	if err := device.UpdateHealth(HealthHealthy, "adb appium and stf healthy", transitionTime); err != nil {
		t.Fatal(err)
	}
	if device.Lifecycle() != DeviceBooting {
		t.Fatal("health transition changed lifecycle")
	}
	if err := device.Transition(DeviceReady, "all readiness checks passed", transitionTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !device.IsSchedulable() {
		t.Fatal("ready healthy device is not schedulable")
	}
	if err := device.UpdateHealth(HealthDegraded, "appium latency", transitionTime.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if device.IsSchedulable() {
		t.Fatal("degraded device is schedulable")
	}
	if device.Lifecycle() != DeviceReady {
		t.Fatal("health degradation changed lifecycle")
	}
	events := device.Events()
	if len(events) != 4 || events[0].Accepted || events[0].ErrorCode != "DEVICE_NOT_HEALTHY" {
		t.Fatalf("events = %#v", events)
	}
}

func TestQuarantinedDeviceCannotBecomeReady(t *testing.T) {
	device, err := RestoreDevice("device_0000000000001", DeviceQuarantined, HealthHealthy)
	if err != nil {
		t.Fatal(err)
	}
	if err := device.Transition(DeviceReady, "manual bypass", transitionTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("quarantined to ready error = %v", err)
	}
	if device.Lifecycle() != DeviceQuarantined || device.IsSchedulable() {
		t.Fatal("quarantined device bypassed rebuild")
	}
	if err := device.Transition(DeviceProvisioning, "approved rebuild", transitionTime); err != nil {
		t.Fatalf("quarantined rebuild transition: %v", err)
	}
}

func TestTransitionErrorUsesChineseOperatorMessage(t *testing.T) {
	device, err := RestoreDevice("device_0000000000001", DeviceBusy, HealthHealthy)
	if err != nil {
		t.Fatal(err)
	}
	err = device.Transition(DeviceDeleted, "验证忙碌保护", transitionTime)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("transition error = %v", err)
	}
	want := "设备 device_0000000000001 的生命周期状态不能从“使用中”转换为“已删除”"
	if err.Error() != want {
		t.Fatalf("transition message = %q, want %q", err.Error(), want)
	}
}

func TestTransitionValidationLeavesAggregateUnchanged(t *testing.T) {
	image, err := NewImage("image_00000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"invalid status": image.Transition(ImageStatus("unknown"), "reason", transitionTime),
		"missing reason": image.Transition(ImageValidating, " ", transitionTime),
		"missing time":   image.Transition(ImageValidating, "reason", time.Time{}),
	} {
		if err == nil {
			t.Fatalf("%s error = nil", name)
		}
		if image.Status() != ImageDraft || len(image.Events()) != 0 {
			t.Fatalf("%s changed aggregate", name)
		}
	}
	if _, err := NewImage("1"); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid ID error = %v", err)
	}
}

func TestEventsAreReturnedAsCopyAndCanBeCleared(t *testing.T) {
	pool, err := NewPool("pool_000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Transition(PoolDisabled, "maintenance", transitionTime); err != nil {
		t.Fatal(err)
	}
	events := pool.Events()
	events[0].Reason = "changed outside"
	if pool.Events()[0].Reason != "maintenance" {
		t.Fatal("caller mutated internal events")
	}
	pool.ClearEvents()
	if len(pool.Events()) != 0 || pool.Status() != PoolDisabled {
		t.Fatal("ClearEvents changed state or did not clear events")
	}
}
