package correlation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewarePreservesSafeCorrelationValues(t *testing.T) {
	var got Values
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		got = FromContext(request.Context())
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(HeaderRequestID, "req_test")
	request.Header.Set(HeaderRunID, "run_test")
	request.Header.Set(HeaderAttemptID, "attempt_test")
	request.Header.Set(HeaderTrace, "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got.RequestID != "req_test" || got.RunID != "run_test" || got.AttemptID != "attempt_test" {
		t.Fatalf("correlation values = %+v", got)
	}
	if response.Header().Get(HeaderRequestID) != "req_test" {
		t.Fatalf("response request ID = %q", response.Header().Get(HeaderRequestID))
	}
}

func TestMiddlewareReplacesUnsafeRequestID(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		if values := FromContext(request.Context()); !strings.HasPrefix(values.RequestID, "req_") {
			t.Fatalf("generated request ID = %q", values.RequestID)
		}
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(HeaderRequestID, strings.Repeat("x", maxHeaderLength+1))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
}
