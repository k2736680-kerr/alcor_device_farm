package baguette

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testUDID        = "5F07171C-20F4-457B-BD16-85F43F42EA02"
	testDevice      = "device_00000000000001"
	testReservation = "reservation_000000000001"
)

func TestClientChecksExactBootedSimulatorAndSignsShortEntry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/simulators.json" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"running":[{"udid":"` + testUDID + `","state":"Booted"}],"available":[]}`))
	}))
	defer upstream.Close()
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	client := newTestClient(t, upstream.URL, now)
	if err := client.EnsureBooted(context.Background(), testUDID); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureBooted(context.Background(), "OTHER-UDID"); !errors.Is(err, ErrNotBooted) {
		t.Fatalf("其他设备错误=%v", err)
	}
	entry, err := client.EntryURL(testDevice, testUDID, testReservation, "admin")
	if err != nil || !strings.HasPrefix(entry, "http://gateway.example.test:18081/entry/") {
		t.Fatalf("入口=%q 错误=%v", entry, err)
	}
	token := strings.TrimPrefix(entry, "http://gateway.example.test:18081/entry/")
	ticket, err := client.verify(token, true)
	if err != nil || ticket.UDID != testUDID || ticket.ExpiresAt != now.Add(30*time.Second).Unix() {
		t.Fatalf("票据=%#v 错误=%v", ticket, err)
	}
}

func newTestClient(t *testing.T, upstream string, now time.Time) *Client {
	t.Helper()
	client, err := New(Config{UpstreamURL: upstream, PublicURL: "http://gateway.example.test:18081",
		Secret: "test-baguette-gateway-secret-at-least-32-bytes", TicketTTL: 30 * time.Second,
		Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
