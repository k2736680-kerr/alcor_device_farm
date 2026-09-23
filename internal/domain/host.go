package domain

import "time"

type HostStatus string

const (
	HostOnline      HostStatus = "online"
	HostOffline     HostStatus = "offline"
	HostDraining    HostStatus = "draining"
	HostMaintenance HostStatus = "maintenance"
)

var hostTransitions = map[HostStatus]map[HostStatus]struct{}{
	HostOnline:      allowed(HostOffline, HostDraining, HostMaintenance),
	HostOffline:     allowed(HostOnline, HostMaintenance),
	HostDraining:    allowed(HostOnline, HostOffline, HostMaintenance),
	HostMaintenance: allowed(HostOnline, HostOffline),
}

type Host struct{ state stateMachine[HostStatus] }

func RestoreHost(id string, status HostStatus) (*Host, error) {
	state, err := newStateMachine("device_host", id, "status", status, validHostStatus, hostTransitions)
	if err != nil {
		return nil, err
	}
	return &Host{state: state}, nil
}

func (host *Host) Status() HostStatus { return host.state.statusValue() }
func (host *Host) Transition(to HostStatus, reason string, at time.Time) error {
	return host.state.transition(to, reason, at)
}
func (host *Host) Events() []TransitionEvent { return host.state.eventsCopy() }

func validHostStatus(status HostStatus) bool {
	_, ok := hostTransitions[status]
	return ok
}
