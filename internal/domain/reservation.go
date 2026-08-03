package domain

import "time"

type ReservationStatus string

const (
	ReservationPending       ReservationStatus = "pending"
	ReservationActive        ReservationStatus = "active"
	ReservationReleased      ReservationStatus = "released"
	ReservationFailed        ReservationStatus = "failed"
	ReservationExpired       ReservationStatus = "expired"
	ReservationForceReleased ReservationStatus = "force_released"
)

var reservationTransitions = map[ReservationStatus]map[ReservationStatus]struct{}{
	ReservationPending:       allowed(ReservationActive, ReservationFailed),
	ReservationActive:        allowed(ReservationReleased, ReservationExpired, ReservationForceReleased),
	ReservationReleased:      allowed[ReservationStatus](),
	ReservationFailed:        allowed[ReservationStatus](),
	ReservationExpired:       allowed[ReservationStatus](),
	ReservationForceReleased: allowed[ReservationStatus](),
}

type Reservation struct {
	state stateMachine[ReservationStatus]
}

func NewReservation(id string) (*Reservation, error) {
	return RestoreReservation(id, ReservationPending)
}

func RestoreReservation(id string, status ReservationStatus) (*Reservation, error) {
	state, err := newStateMachine("device_reservation", id, "status", status, validReservationStatus, reservationTransitions)
	if err != nil {
		return nil, err
	}
	return &Reservation{state: state}, nil
}

func (reservation *Reservation) ID() string                { return reservation.state.idValue() }
func (reservation *Reservation) Status() ReservationStatus { return reservation.state.statusValue() }
func (reservation *Reservation) Transition(to ReservationStatus, reason string, at time.Time) error {
	return reservation.state.transition(to, reason, at)
}
func (reservation *Reservation) Events() []TransitionEvent { return reservation.state.eventsCopy() }
func (reservation *Reservation) ClearEvents()              { reservation.state.clearEvents() }

func validReservationStatus(status ReservationStatus) bool {
	_, ok := reservationTransitions[status]
	return ok
}
