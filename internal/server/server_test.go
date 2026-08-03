package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/logging"
)

func TestHealthUsesUnifiedResponseAndCorrelation(t *testing.T) {
	var logs bytes.Buffer
	logger := testLogger(t, &logs)
	server := httptest.NewServer(Handler(config.SecurityConfig{}, logger))
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	request.Header.Set(correlation.HeaderRequestID, "req_test")
	request.Header.Set(correlation.HeaderRunID, "run_test")
	request.Header.Set(correlation.HeaderAttemptID, "attempt_test")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	var envelope httpx.Envelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.StatusCode != http.StatusOK || envelope.RequestID != "req_test" || envelope.Error != nil {
		t.Fatalf("status = %d, envelope = %+v", response.StatusCode, envelope)
	}
	for _, value := range []string{"req_test", "run_test", "attempt_test"} {
		if !strings.Contains(logs.String(), value) {
			t.Fatalf("request log does not contain %q: %s", value, logs.String())
		}
	}
}

func TestNotFoundAndMethodNotAllowedUseStableErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := Handler(config.SecurityConfig{}, logger)

	tests := []struct {
		name      string
		method    string
		path      string
		status    int
		errorCode string
	}{
		{name: "not found", method: http.MethodGet, path: "/missing", status: http.StatusNotFound, errorCode: "NOT_FOUND"},
		{name: "method", method: http.MethodPost, path: "/healthz", status: http.StatusMethodNotAllowed, errorCode: "METHOD_NOT_ALLOWED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			var envelope httpx.Envelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if response.Code != test.status || envelope.Error == nil || envelope.Error.Code != test.errorCode {
				t.Fatalf("status = %d, envelope = %+v", response.Code, envelope)
			}
		})
	}
}

func testLogger(t *testing.T, output *bytes.Buffer) *slog.Logger {
	t.Helper()
	logger, err := logging.New(config.LogConfig{Level: "debug", Format: "json"}, output)
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	return logger
}
