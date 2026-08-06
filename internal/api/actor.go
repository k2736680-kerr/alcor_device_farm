package api

import (
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
)

// requestActor resolves the audited identity of the caller exactly once so
// every device-domain service writes the same actor_type and actor_id.
//
// A console session always reports its own subject; the actor header is only
// honoured for service integrations, which keeps browser users from forging
// another actor. Unauthenticated requests never reach handlers, so the final
// fallback exists only to avoid writing an empty identity.
func requestActor(request *http.Request) audit.Actor {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		return audit.Actor{Type: audit.ActorService, ID: "unknown", ClientID: "unknown"}
	}
	switch principal.Role {
	case auth.RoleConsole:
		return audit.Console(principal.SubjectID)
	case auth.RoleAgent:
		return audit.Actor{Type: audit.ActorAgent, ID: audit.ActorAgent, ClientID: audit.ActorAgent}
	default:
		return audit.Service(request.Header.Get("X-Device-Farm-Actor-Id"))
	}
}
