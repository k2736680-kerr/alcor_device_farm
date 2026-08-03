package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/agent"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
)

func TestHTTPClientClaimUsesAgentTokenAndDecodesEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("method=%s", request.Method)
		}
		if request.URL.Path != "/internal/v1/device-hosts/host_000000000000001/commands/claims" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer agent-secret" {
			t.Fatalf("authorization=%q", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type=%q", got)
		}
		var input hostcommand.ClaimInput
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.LeaseSeconds != 60 || input.MaxCommands != 2 || input.WaitSeconds != 5 {
			t.Fatalf("claim input=%+v", input)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"request_id": "request_0000000000001",
			"data": map[string]any{"items": []map[string]any{{
				"id": "command_0000000000001", "host_id": "host_000000000000001",
				"command_type": "inspect", "payload": map[string]any{"provider_ref": "emulator-1"},
				"status": domain.CommandLeased, "attempt": 1, "max_attempts": 3,
			}}},
			"error": nil,
		})
	}))
	defer server.Close()

	client, err := agent.NewHTTPClient(server.URL+"/", "agent-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	commands, err := client.Claim(context.Background(), "host_000000000000001", hostcommand.ClaimInput{
		LeaseSeconds: 60, MaxCommands: 2, WaitSeconds: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].ID != "command_0000000000001" || commands[0].Payload["provider_ref"] != "emulator-1" {
		t.Fatalf("commands=%+v", commands)
	}
}

func TestHTTPClientReturnsAPIErrorForNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"request_id": "request_0000000000002",
			"data":       nil,
			"error": map[string]any{
				"code": "UNAUTHORIZED", "message": "agent token is invalid", "retryable": false,
			},
		})
	}))
	defer server.Close()

	client, err := agent.NewHTTPClient(server.URL, "wrong-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = client.Heartbeat(context.Background(), "host_000000000000001", hostcommand.HeartbeatInput{})
	if err == nil || !strings.Contains(err.Error(), "UNAUTHORIZED: agent token is invalid") {
		t.Fatalf("error=%v", err)
	}
}
