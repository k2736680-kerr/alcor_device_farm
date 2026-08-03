package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
)

func TestAgentHeartbeatClaimAndCompletionAPI(t *testing.T) {
	environment := newManagementEnvironment(t)
	if _, err := environment.db.Pool().Exec(context.Background(), `INSERT INTO device_hosts
        (id,name,host_type,status) VALUES ('host_000000000000001','agent-api-host','docker_emulator','offline')`); err != nil {
		t.Fatal(err)
	}
	heartbeatPath := "/internal/v1/device-hosts/host_000000000000001/heartbeats"
	heartbeat := map[string]any{
		"agent_time": time.Now().UTC(), "capacity": map[string]any{"cpu": 8, "device_slots": 2},
		"environment": map[string]any{"docker": "mock"}, "devices": []any{},
	}
	assertStatus(t, environment.request(t, http.MethodPost, heartbeatPath, heartbeat, "", ""), http.StatusUnauthorized)
	assertStatus(t, environment.request(t, http.MethodPost, heartbeatPath, heartbeat, serviceToken, ""), http.StatusForbidden)
	assertStatus(t, environment.request(t, http.MethodPost, heartbeatPath, heartbeat, agentToken, ""), http.StatusOK)

	created, err := environment.hostCommands.Create(context.Background(), "host_000000000000001", "inspect",
		map[string]any{"provider_ref": "container-1"}, 3, "agent-api-command-key")
	if err != nil {
		t.Fatal(err)
	}
	claimResponse := environment.request(t, http.MethodPost,
		"/internal/v1/device-hosts/host_000000000000001/commands/claims",
		map[string]any{"lease_seconds": 30, "max_commands": 1, "wait_seconds": 0}, agentToken, "")
	assertStatus(t, claimResponse, http.StatusOK)
	var claimed struct {
		Items []hostcommand.Command `json:"items"`
	}
	decodeData(t, claimResponse, &claimed)
	if len(claimed.Items) != 1 || claimed.Items[0].ID != created.ID || claimed.Items[0].LeaseToken == nil {
		t.Fatalf("claimed=%+v", claimed.Items)
	}
	completionResponse := environment.request(t, http.MethodPost,
		"/internal/v1/device-host-commands/"+created.ID+"/completions",
		map[string]any{"lease_token": *claimed.Items[0].LeaseToken, "attempt": claimed.Items[0].Attempt,
			"status": "succeeded", "result": map[string]any{"healthy": true, "authorization": "Bearer command-result-secret",
				"message": "provider password=command-password"}}, agentToken, "")
	assertStatus(t, completionResponse, http.StatusOK)
	var completed hostcommand.Command
	decodeData(t, completionResponse, &completed)
	if completed.Status != "succeeded" || completed.CompletedAt == nil {
		t.Fatalf("completed=%+v", completed)
	}
	var leaked int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_host_commands
		WHERE id=$1 AND result::text LIKE '%command-result-secret%' OR id=$1 AND result::text LIKE '%command-password%'`, created.ID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("command result secret rows=%d", leaked)
	}
	assertStatus(t, environment.request(t, http.MethodPost,
		"/internal/v1/device-host-commands/"+created.ID+"/completions",
		map[string]any{"lease_token": *claimed.Items[0].LeaseToken, "attempt": claimed.Items[0].Attempt, "status": "succeeded"}, agentToken, ""), http.StatusConflict)
}
