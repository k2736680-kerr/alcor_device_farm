package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidID         = errors.New("invalid resource id")
	ErrInvalidStatus     = errors.New("invalid status")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrReasonRequired    = errors.New("transition reason is required")
	ErrTimeRequired      = errors.New("transition time is required")
	ErrDeviceNotHealthy  = errors.New("device must be healthy before becoming ready")
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type TransitionError struct {
	Resource string
	ID       string
	Field    string
	From     string
	To       string
}

func (err *TransitionError) Error() string {
	return fmt.Sprintf("%s %s cannot transition %s from %s to %s", err.Resource, err.ID, err.Field, err.From, err.To)
}

func (err *TransitionError) Unwrap() error {
	return ErrInvalidTransition
}

type TransitionEvent struct {
	Resource  string
	ID        string
	Field     string
	From      string
	To        string
	Reason    string
	At        time.Time
	Accepted  bool
	ErrorCode string
}

type stateMachine[S ~string] struct {
	resource    string
	id          string
	field       string
	status      S
	valid       func(S) bool
	transitions map[S]map[S]struct{}
	events      []TransitionEvent
}

func newStateMachine[S ~string](resource, id, field string, status S, valid func(S) bool, transitions map[S]map[S]struct{}) (stateMachine[S], error) {
	if !identifierPattern.MatchString(id) {
		return stateMachine[S]{}, fmt.Errorf("%w: %s", ErrInvalidID, id)
	}
	if !valid(status) {
		return stateMachine[S]{}, fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}
	return stateMachine[S]{
		resource:    resource,
		id:          id,
		field:       field,
		status:      status,
		valid:       valid,
		transitions: transitions,
	}, nil
}

func (machine *stateMachine[S]) transition(to S, reason string, at time.Time) error {
	if !machine.valid(to) {
		return fmt.Errorf("%w: %s", ErrInvalidStatus, to)
	}
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	if at.IsZero() {
		return ErrTimeRequired
	}
	if _, ok := machine.transitions[machine.status][to]; !ok {
		machine.recordRejected(to, reason, at, "INVALID_STATE_TRANSITION")
		return &TransitionError{
			Resource: machine.resource,
			ID:       machine.id,
			Field:    machine.field,
			From:     string(machine.status),
			To:       string(to),
		}
	}

	from := machine.status
	machine.status = to
	machine.events = append(machine.events, TransitionEvent{
		Resource: machine.resource,
		ID:       machine.id,
		Field:    machine.field,
		From:     string(from),
		To:       string(to),
		Reason:   strings.TrimSpace(reason),
		At:       at,
		Accepted: true,
	})
	return nil
}

func (machine *stateMachine[S]) recordRejected(to S, reason string, at time.Time, errorCode string) {
	machine.events = append(machine.events, TransitionEvent{
		Resource:  machine.resource,
		ID:        machine.id,
		Field:     machine.field,
		From:      string(machine.status),
		To:        string(to),
		Reason:    strings.TrimSpace(reason),
		At:        at,
		Accepted:  false,
		ErrorCode: errorCode,
	})
}

func (machine *stateMachine[S]) statusValue() S {
	return machine.status
}

func (machine *stateMachine[S]) idValue() string {
	return machine.id
}

func (machine *stateMachine[S]) eventsCopy() []TransitionEvent {
	return append([]TransitionEvent(nil), machine.events...)
}

func (machine *stateMachine[S]) clearEvents() {
	machine.events = nil
}

func allowed[S comparable](values ...S) map[S]struct{} {
	result := make(map[S]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
