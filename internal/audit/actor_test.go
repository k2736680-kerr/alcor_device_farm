package audit_test

import (
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
)

func TestServiceKeepsCallerSuppliedActorID(t *testing.T) {
	actor := audit.Service("release-pipeline")
	if actor.Type != audit.ActorService {
		t.Fatalf("actor type = %q, want %q", actor.Type, audit.ActorService)
	}
	if actor.ID != "release-pipeline" {
		t.Fatalf("actor id = %q, want %q", actor.ID, "release-pipeline")
	}
	if actor.ClientID != audit.ActorService {
		t.Fatalf("client id = %q, want %q", actor.ClientID, audit.ActorService)
	}
	if !actor.Valid() {
		t.Fatal("actor must be valid")
	}
}

func TestServiceRejectsUnusableActorHeaderValues(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"whitespace":        "   ",
		"control character": "pipeline\nactor_type=console",
		"too long":          strings.Repeat("a", 129),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			actor := audit.Service(value)
			if actor.ID != audit.ActorService {
				t.Fatalf("actor id = %q, want fallback %q", actor.ID, audit.ActorService)
			}
			if !actor.Valid() {
				t.Fatal("fallback actor must stay valid")
			}
		})
	}
}

func TestConsoleAlwaysReportsItsOwnSubject(t *testing.T) {
	actor := audit.Console("alice")
	if actor.Type != audit.ActorConsole {
		t.Fatalf("actor type = %q, want %q", actor.Type, audit.ActorConsole)
	}
	if actor.ID != "alice" {
		t.Fatalf("actor id = %q, want %q", actor.ID, "alice")
	}
	if actor.ClientID != "console:alice" {
		t.Fatalf("client id = %q, want %q", actor.ClientID, "console:alice")
	}
	if !actor.Valid() {
		t.Fatal("actor must be valid")
	}
}

func TestConsoleWithoutSubjectIsNotPersistable(t *testing.T) {
	if audit.Console("").Valid() {
		t.Fatal("a console actor without a subject must not be persistable")
	}
}

func TestValidRejectsUnknownActorTypes(t *testing.T) {
	actor := audit.Actor{Type: "operator", ID: "alice", ClientID: "alice"}
	if actor.Valid() {
		t.Fatal("unknown actor type must be rejected")
	}
}

func TestSystemActorIsValid(t *testing.T) {
	actor := audit.System()
	if actor.Type != audit.ActorSystem || !actor.Valid() {
		t.Fatalf("system actor = %+v, want a valid system identity", actor)
	}
}
