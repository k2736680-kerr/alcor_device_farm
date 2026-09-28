package domain

import "time"

type PoolStatus string

// ImageStateTTL 只用于「正向」镜像证据：宿主曾经有过某个镜像，可能因为镜像重
// 构建而变旧，所以隔一段时间重新校验一次。
//
// 负向证据（这台宿主确实没有这个镜像）刻意没有有效期。曾经用同一个 TTL 让负向
// 记录也过期，结果是：一个池被正确排除 30 分钟后又被放回自动调度，注定失败的
// create 重新排队、预约再次卡到超时 —— 把「已经证明做不到」当成「忘了」只会
// 让同一个故障周期性复发。负向记录一直有效，直到出现一条更新的成功证据把它覆盖
// （见 warmpool.recordHostImageOutcomes），或运维显式触发镜像重新验证。
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
