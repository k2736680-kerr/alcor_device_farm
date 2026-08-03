package appium

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func TestProbeReadsAppiumReadyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/status" || request.Header.Get("Accept") != "application/json" {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"value":{"ready":true,"message":"ready"}}`))
	}))
	defer server.Close()
	probe, err := NewProbe(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := probe.Healthy(context.Background(), providers.ConnectionInfo{AppiumEndpoint: server.URL})
	if err != nil || !healthy {
		t.Fatalf("healthy=%v error=%v", healthy, err)
	}
}

func TestProbePreservesConfiguredBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/wd/hub/status" {
			http.Error(writer, "unexpected path", http.StatusBadRequest)
			return
		}
		_, _ = writer.Write([]byte(`{"value":{"ready":false}}`))
	}))
	defer server.Close()
	probe, _ := NewProbe(time.Second)
	healthy, err := probe.Healthy(context.Background(), providers.ConnectionInfo{AppiumEndpoint: server.URL + "/wd/hub/"})
	if err != nil || healthy {
		t.Fatalf("healthy=%v error=%v", healthy, err)
	}
}

func TestProbeRejectsInvalidOrUnhealthyResponses(t *testing.T) {
	probe, _ := NewProbe(time.Second)
	for _, endpoint := range []string{"", "ftp://127.0.0.1:4723", "http://user:secret@127.0.0.1:4723", "http://127.0.0.1:4723?token=secret"} {
		if _, err := probe.Healthy(context.Background(), providers.ConnectionInfo{AppiumEndpoint: endpoint}); err == nil {
			t.Fatalf("endpoint %q must fail", endpoint)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	if _, err := probe.Healthy(context.Background(), providers.ConnectionInfo{AppiumEndpoint: server.URL}); err == nil {
		t.Fatal("non-success status must fail")
	}
}
