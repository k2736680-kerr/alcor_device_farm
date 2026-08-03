package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerExposesServiceAndHTTPMetricsWithoutDatabase(t *testing.T) {
	registry := New(nil)
	registry.ObserveHTTP(http.MethodGet, "GET /healthz", http.StatusOK, 25*time.Millisecond)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"device_farm_up 1", `device_farm_http_requests_total{method="GET",route="/healthz",status="200"} 1`,
		"device_farm_database_ready 0", "device_farm_build_info{",
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("metrics do not contain %q:\n%s", want, response.Body.String())
		}
	}
}

func TestReadinessRequiresDatabase(t *testing.T) {
	if err := New(nil).Ready(context.Background()); err == nil {
		t.Fatal("readiness unexpectedly succeeded without database")
	}
}
