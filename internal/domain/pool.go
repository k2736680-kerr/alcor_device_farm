package domain

import "time"

type PoolStatus string

// ImageStateTTL 是一次「这台宿主上没有某个镜像」观测的有效期，也是镜像新鲜度的
// 唯一口径：调度侧和管理接口都读它。观测过期后宿主重新参与调度，所以宿主后来
// 装上镜像不需要人工清理记录。
const ImageStateTTL = 30 * time.Minute

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
