package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
)

func TestWriteDataUsesUnifiedEnvelope(t *testing.T) {
	handler := correlation.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		WriteData(writer, request, http.StatusOK, map[string]string{"status": "ok"})
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(correlation.HeaderRequestID, "req_test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var envelope Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.RequestID != "req_test" || envelope.Error != nil || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
}

func TestWriteErrorUsesUnifiedEnvelope(t *testing.T) {
	handler := correlation.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		WriteError(writer, request, http.StatusBadRequest, APIError{Code: "INVALID_ARGUMENT", Message: "invalid", Retryable: false})
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var envelope Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Data != nil || envelope.Error == nil || envelope.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("envelope = %+v", envelope)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
