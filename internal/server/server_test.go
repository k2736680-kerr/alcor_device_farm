package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/logging"
	farmmetrics "github.com/Ad-Quanta/alcor-device-farm/internal/metrics"
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

func TestReadinessFailsClosedWithoutDatabaseAndMetricsStayPublic(t *testing.T) {
	handler := Handler(config.SecurityConfig{}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status=%d body=%s", ready.Code, ready.Body.String())
	}

	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), "device_farm_database_ready 0") {
		t.Fatalf("metrics status=%d body=%s", metricsResponse.Code, metricsResponse.Body.String())
	}
}

func TestRecoveredPanicIsCountedAsHTTP500(t *testing.T) {
	registry := farmmetrics.New(nil)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := requestLogMiddleware(logger, registry, recoverMiddleware(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("test panic")
	})))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("panic status=%d", response.Code)
	}
	metricsResponse := httptest.NewRecorder()
	registry.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metricsResponse.Body.String(), `device_farm_http_requests_total{method="GET",route="unmatched",status="500"} 1`) {
		t.Fatalf("panic metric missing:\n%s", metricsResponse.Body.String())
	}
}

func TestRequestLogMiddlewarePreservesWebSocketHijacking(t *testing.T) {
	registry := farmmetrics.New(nil)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	errors := make(chan error, 1)
	handler := requestLogMiddleware(logger, registry, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			errors <- fmt.Errorf("请求日志包装器没有实现 http.Hijacker")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			errors <- err
			return
		}
		defer connection.Close()
		_, err = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		if err == nil {
			err = buffered.Flush()
		}
		errors <- err
	}))

	testServer := httptest.NewServer(handler)
	defer testServer.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(testServer.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection, "GET /ws HTTP/1.1\r\nHost: test\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if status != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Fatalf("unexpected websocket response: %q", status)
	}
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "status=101") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(logs.String(), "status=101") {
		t.Fatalf("websocket status missing from request log: %s", logs.String())
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
