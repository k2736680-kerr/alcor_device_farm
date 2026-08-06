package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
)

type stubConsoleAuthenticator struct {
	principal auth.Principal
	err       error
}

func (stub stubConsoleAuthenticator) Authenticate(*http.Request) (auth.Principal, error) {
	return stub.principal, stub.err
}

func (stubConsoleAuthenticator) ValidateCSRF(*http.Request, auth.Principal) error { return nil }

// resolveThroughMiddleware runs the real authentication middleware so the test
// observes the actor exactly as a handler would, instead of hand-building a
// principal context that production code never produces.
func resolveThroughMiddleware(t *testing.T, request *http.Request, authenticator auth.ConsoleAuthenticator) (audit.Actor, int) {
	t.Helper()
	security := config.SecurityConfig{ServiceToken: "service-secret", AgentToken: "agent-secret"}
	var resolved audit.Actor
	next := http.HandlerFunc(func(writer http.ResponseWriter, inner *http.Request) {
		resolved = requestActor(inner)
		writer.WriteHeader(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	if authenticator == nil {
		auth.RouteMiddleware(security, next).ServeHTTP(response, request)
	} else {
		auth.RouteMiddleware(security, next, authenticator).ServeHTTP(response, request)
	}
	return resolved, response.Code
}

func TestServiceCallerMayNameItsOwnActor(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	request.Header.Set("Authorization", "Bearer service-secret")
	request.Header.Set("X-Device-Farm-Actor-Id", "release-pipeline")

	actor, status := resolveThroughMiddleware(t, request, nil)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
	if actor.Type != audit.ActorService || actor.ID != "release-pipeline" {
		t.Fatalf("actor = %+v, want service/release-pipeline", actor)
	}
}

func TestServiceCallerWithoutActorHeaderFallsBackToServiceIdentity(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	request.Header.Set("Authorization", "Bearer service-secret")

	actor, _ := resolveThroughMiddleware(t, request, nil)
	if actor.Type != audit.ActorService || actor.ID != audit.ActorService {
		t.Fatalf("actor = %+v, want service/service", actor)
	}
}

func TestConsoleSessionCannotForgeAnotherActor(t *testing.T) {
	authenticator := stubConsoleAuthenticator{principal: auth.Principal{
		Role: auth.RoleConsole, SubjectID: "alice", ConsoleRole: auth.ConsoleAdmin,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	request.Header.Set("X-Device-Farm-Actor-Id", "release-pipeline")

	actor, status := resolveThroughMiddleware(t, request, authenticator)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
	if actor.Type != audit.ActorConsole {
		t.Fatalf("actor type = %q, want %q", actor.Type, audit.ActorConsole)
	}
	if actor.ID != "alice" {
		t.Fatalf("actor id = %q, want the session subject %q", actor.ID, "alice")
	}
	if actor.ClientID != "console:alice" {
		t.Fatalf("client id = %q, want console:alice", actor.ClientID)
	}
}

func TestConsoleSessionCannotForgeActorTypeThroughBearerToken(t *testing.T) {
	authenticator := stubConsoleAuthenticator{principal: auth.Principal{
		Role: auth.RoleConsole, SubjectID: "alice", ConsoleRole: auth.ConsoleAdmin,
	}}
	// A stolen service token must win over the console session, but it must not
	// let the console user keep a console identity while acting as a service.
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	request.Header.Set("Authorization", "Bearer service-secret")
	request.Header.Set("X-Device-Farm-Actor-Id", "alice")

	actor, _ := resolveThroughMiddleware(t, request, authenticator)
	if actor.Type != audit.ActorService {
		t.Fatalf("actor type = %q, want %q", actor.Type, audit.ActorService)
	}
}

func TestAgentCallerIsAuditedAsAgent(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/device-hosts/host_1/heartbeats", nil)
	request.Header.Set("Authorization", "Bearer agent-secret")
	request.Header.Set("X-Device-Farm-Actor-Id", "release-pipeline")

	actor, _ := resolveThroughMiddleware(t, request, nil)
	if actor.Type != audit.ActorAgent || actor.ID != audit.ActorAgent {
		t.Fatalf("actor = %+v, want agent/agent", actor)
	}
}

func TestEveryResolvedActorIsPersistable(t *testing.T) {
	authenticator := stubConsoleAuthenticator{principal: auth.Principal{
		Role: auth.RoleConsole, SubjectID: "alice", ConsoleRole: auth.ConsoleAdmin,
	}}
	requests := []struct {
		name          string
		path          string
		token         string
		authenticator auth.ConsoleAuthenticator
	}{
		{name: "service", path: "/api/v1/devices", token: "service-secret"},
		{name: "agent", path: "/internal/v1/device-hosts/host_1/heartbeats", token: "agent-secret"},
		{name: "console", path: "/api/v1/devices", authenticator: authenticator},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			actor, _ := resolveThroughMiddleware(t, request, test.authenticator)
			if !actor.Valid() {
				t.Fatalf("actor %+v cannot be persisted", actor)
			}
		})
	}
}

func TestUnauthenticatedRequestNeverReachesTheActorResolver(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	actor, status := resolveThroughMiddleware(t, request, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if actor != (audit.Actor{}) {
		t.Fatalf("actor = %+v, want the handler to be skipped entirely", actor)
	}
}
