package domain

import "time"

type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandLeased    CommandStatus = "leased"
	CommandSucceeded CommandStatus = "succeeded"
	CommandFailed    CommandStatus = "failed"
	CommandTimedOut  CommandStatus = "timed_out"
	CommandCanceled  CommandStatus = "canceled"
)

var commandTransitions = map[CommandStatus]map[CommandStatus]struct{}{
	CommandPending:   allowed(CommandLeased, CommandCanceled),
	CommandLeased:    allowed(CommandPending, CommandSucceeded, CommandFailed, CommandTimedOut, CommandCanceled),
	CommandSucceeded: allowed[CommandStatus](),
	CommandFailed:    allowed[CommandStatus](),
	CommandTimedOut:  allowed[CommandStatus](),
	CommandCanceled:  allowed[CommandStatus](),
}

type Command struct{ state stateMachine[CommandStatus] }

func RestoreCommand(id string, status CommandStatus) (*Command, error) {
	state, err := newStateMachine("device_host_command", id, "status", status, validCommandStatus, commandTransitions)
	if err != nil {
		return nil, err
	}
	return &Command{state: state}, nil
}

func (command *Command) Status() CommandStatus { return command.state.statusValue() }
func (command *Command) Transition(to CommandStatus, reason string, at time.Time) error {
	return command.state.transition(to, reason, at)
}
func (command *Command) Events() []TransitionEvent { return command.state.eventsCopy() }

func validCommandStatus(status CommandStatus) bool {
	_, ok := commandTransitions[status]
	return ok
}
