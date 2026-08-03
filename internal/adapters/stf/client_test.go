package stf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientUsesOfficialInventoryClaimReleaseAndRemoteConnectAPIs(t *testing.T) {
	const token = "stf-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("authorization=%q", request.Header.Get("Authorization"))
		}
		switch request.Method + " " + request.URL.Path {
		case "GET /base/api/v1/devices":
			_ = json.NewEncoder(writer).Encode(map[string]any{"devices": []map[string]any{{"serial": "host:32771", "present": true, "ready": true, "using": false}}})
		case "POST /base/api/v1/user/devices":
			var payload map[string]any
			_ = json.NewDecoder(request.Body).Decode(&payload)
			if payload["serial"] != "host:32771" || payload["timeout"] != float64(600000) {
				t.Fatalf("claim payload=%#v", payload)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"success": true})
		case "DELETE /base/api/v1/user/devices/host:32771":
			_ = json.NewEncoder(writer).Encode(map[string]any{"success": true})
		case "POST /base/api/v1/user/devices/host:32771/remoteConnect":
			_ = json.NewEncoder(writer).Encode(map[string]any{"success": true, "remoteConnectUrl": "10.0.0.20:7401"})
		case "DELETE /base/api/v1/user/devices/host:32771/remoteConnect":
			_ = json.NewEncoder(writer).Encode(map[string]any{"success": true})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL + "/base", Token: token, Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	devices, err := client.Inventory(context.Background())
	if err != nil || len(devices) != 1 || devices[0].Serial != "host:32771" || !devices[0].Ready {
		t.Fatalf("inventory=%#v error=%v", devices, err)
	}
	visible, err := client.Visible(context.Background(), "host:32771")
	if err != nil || !visible {
		t.Fatalf("visible=%t error=%v", visible, err)
	}
	visible, err = client.Visible(context.Background(), "missing:5555")
	if err != nil || visible {
		t.Fatalf("missing visible=%t error=%v", visible, err)
	}
	if err := client.Claim(context.Background(), "host:32771", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	remote, err := client.RemoteConnect(context.Background(), "host:32771")
	if err != nil || remote.URL != "10.0.0.20:7401" {
		t.Fatalf("remote=%#v error=%v", remote, err)
	}
	if err := client.RemoteDisconnect(context.Background(), "host:32771"); err != nil {
		t.Fatal(err)
	}
	if err := client.Release(context.Background(), "host:32771"); err != nil {
		t.Fatal(err)
	}
}

func TestClientRetriesTransientFailureAndDoesNotLeakToken(t *testing.T) {
	const token = "never-print-this-token"
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(writer, token, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"devices": []any{}})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Token: token, Attempts: 3, RetryDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Inventory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts=%d", attempts.Load())
	}

	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, token, http.StatusBadRequest)
	})
	err = client.Claim(context.Background(), "host:32771", time.Minute)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("error=%v", err)
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Retryable || typed.Code != "STF_CLAIM_FAILED" {
		t.Fatalf("typed error=%#v", typed)
	}
}

func TestClientRejectsUnsafeConfigurationAndTreatsDeleteNotFoundAsReleased(t *testing.T) {
	for _, value := range []string{"", "ftp://stf.internal", "http://user:pass@stf.internal", "http://stf.internal?token=x"} {
		if _, err := New(Config{BaseURL: value, Token: "token"}); err == nil {
			t.Fatalf("unsafe URL %q accepted", value)
		}
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Token: "token", Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Release(context.Background(), "missing-device"); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsRemoteConnectURLContainingCredentialsOrQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"success": true, "remoteConnectUrl": "tcp://user:secret@10.0.0.20:7401?token=secret",
		})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Token: "token", Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RemoteConnect(context.Background(), "host:32771"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("RemoteConnect() error=%v", err)
	}
}
