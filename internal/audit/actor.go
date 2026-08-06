// Package audit defines the authenticated identity that device-domain writes
// record in device_audit_events. It exists so the API layer resolves the actor
// exactly once and every service persists the same identity.
package audit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ActorService = "service"
	ActorAgent   = "agent"
	ActorConsole = "console"
	ActorSystem  = "system"
)

// Actor carries the audited identity of a single request.
//
// Type and ID are persisted as device_audit_events.actor_type and actor_id.
// ClientID is the idempotency and ownership scope; it stays separate because a
// service integration may act on behalf of several actor IDs while sharing one
// idempotency namespace.
type Actor struct {
	Type     string
	ID       string
	ClientID string
}

// Service builds the identity of a northbound integration authenticated with
// the service token. actorID may be supplied by the caller through the actor
// header, so any value that would not survive Valid is discarded rather than
// persisted; the identity then degrades to the anonymous service actor instead
// of failing the request.
func Service(actorID string) Actor {
	actorID = strings.TrimSpace(actorID)
	if !validField(actorID, 128) {
		actorID = ActorService
	}
	return Actor{Type: ActorService, ID: actorID, ClientID: ActorService}
}

// Console builds the identity of a browser session. The subject ID always wins
// so a console user cannot forge another actor through request headers.
func Console(subjectID string) Actor {
	subjectID = strings.TrimSpace(subjectID)
	return Actor{Type: ActorConsole, ID: subjectID, ClientID: ActorConsole + ":" + subjectID}
}

// System builds the identity used by background controllers.
func System() Actor {
	return Actor{Type: ActorSystem, ID: ActorSystem, ClientID: ActorSystem}
}

// Valid reports whether the actor can be persisted. It rejects unknown types,
// empty identities, control characters and values longer than the audit column.
func (actor Actor) Valid() bool {
	switch actor.Type {
	case ActorService, ActorAgent, ActorConsole, ActorSystem:
	default:
		return false
	}
	return validField(actor.ID, 128) && validField(actor.ClientID, 128)
}

func validField(value string, limit int) bool {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
