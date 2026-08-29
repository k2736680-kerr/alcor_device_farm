package domain

import "time"

type PoolStatus string

const (
	PoolActive   PoolStatus = "active"
	PoolDisabled PoolStatus = "disabled"
)

var poolTransitions = map[PoolStatus]map[PoolStatus]struct{}{
	PoolActive:   allowed(PoolDisabled),
	PoolDisabled: allowed(PoolActive),
}

type Pool struct{ state stateMachine[PoolStatus] }

func NewPool(id string) (*Pool, error) { return RestorePool(id, PoolActive) }

func RestorePool(id string, status PoolStatus) (*Pool, error) {
	state, err := newStateMachine("device_pool", id, "status", status, validPoolStatus, poolTransitions)
	if err != nil {
		return nil, err
	}
	return &Pool{state: state}, nil
}

func (pool *Pool) Status() PoolStatus { return pool.state.statusValue() }
func (pool *Pool) Transition(to PoolStatus, reason string, at time.Time) error {
	return pool.state.transition(to, reason, at)
}
func (pool *Pool) Events() []TransitionEvent { return pool.state.eventsCopy() }
func (pool *Pool) ClearEvents()              { pool.state.clearEvents() }

func validPoolStatus(status PoolStatus) bool {
	_, ok := poolTransitions[status]
	return ok
}
