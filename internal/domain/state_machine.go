package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidID         = errors.New("资源 ID 无效")
	ErrInvalidStatus     = errors.New("状态值无效")
	ErrInvalidTransition = errors.New("不允许当前状态转换")
	ErrReasonRequired    = errors.New("必须填写状态转换原因")
	ErrTimeRequired      = errors.New("必须提供状态转换时间")
	ErrDeviceNotHealthy  = errors.New("设备健康后才能进入就绪状态")
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
	return fmt.Sprintf("%s %s 的%s不能从“%s”转换为“%s”", resourceName(err.Resource), err.ID,
		fieldName(err.Field), statusName(err.From), statusName(err.To))
}

func statusName(value string) string {
	names := map[string]string{
		"draft": "草稿", "validating": "验证中", "ready": "可用", "disabled": "已停用",
		"online": "在线", "offline": "离线", "draining": "排空中", "maintenance": "维护中",
		"active": "活动中", "pending": "等待中", "released": "已释放", "expired": "已过期",
		"force_released": "已强制释放", "starting": "启动中", "closing": "关闭中", "closed": "已关闭",
		"leased": "已领取", "succeeded": "已成功", "failed": "已失败", "timed_out": "已超时", "canceled": "已取消",
		"provisioning": "准备中", "booting": "启动中", "reserved": "已预约", "busy": "使用中",
		"recycling": "回收中", "stopped": "已停止", "quarantined": "已隔离", "deleted": "已删除",
		"unknown": "未知", "healthy": "健康", "degraded": "降级", "unhealthy": "不健康",
	}
	if name, ok := names[value]; ok {
		return name
	}
	return value
}

func resourceName(value string) string {
	switch value {
	case "device":
		return "设备"
	case "reservation":
		return "预约"
	case "session":
		return "会话"
	case "host_command":
		return "宿主机命令"
	default:
		return value
	}
}

func fieldName(value string) string {
	switch value {
	case "lifecycle_status":
		return "生命周期状态"
	case "health_status":
		return "健康状态"
	case "status":
		return "状态"
	default:
		return value
	}
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
