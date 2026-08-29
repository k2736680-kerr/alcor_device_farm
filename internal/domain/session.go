package domain

import "time"

type SessionStatus string

const (
	SessionStarting SessionStatus = "starting"
	SessionActive   SessionStatus = "active"
	SessionClosing  SessionStatus = "closing"
	SessionClosed   SessionStatus = "closed"
	SessionFailed   SessionStatus = "failed"
)

var sessionTransitions = map[SessionStatus]map[SessionStatus]struct{}{
	SessionStarting: allowed(SessionActive, SessionFailed),
	SessionActive:   allowed(SessionClosing, SessionFailed),
	SessionClosing:  allowed(SessionClosed, SessionFailed),
	SessionClosed:   allowed[SessionStatus](),
	SessionFailed:   allowed[SessionStatus](),
}

type Session struct{ state stateMachine[SessionStatus] }

func NewSession(id string) (*Session, error) { return RestoreSession(id, SessionStarting) }

func RestoreSession(id string, status SessionStatus) (*Session, error) {
	state, err := newStateMachine("device_session", id, "status", status, validSessionStatus, sessionTransitions)
	if err != nil {
		return nil, err
	}
	return &Session{state: state}, nil
}

func (session *Session) Status() SessionStatus { return session.state.statusValue() }
func (session *Session) Transition(to SessionStatus, reason string, at time.Time) error {
	return session.state.transition(to, reason, at)
}
func (session *Session) Events() []TransitionEvent { return session.state.eventsCopy() }

func validSessionStatus(status SessionStatus) bool {
	_, ok := sessionTransitions[status]
	return ok
}
