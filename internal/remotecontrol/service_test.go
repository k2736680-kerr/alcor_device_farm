package remotecontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stf"
	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

type fakeReservations struct {
	current         reservation.View
	createdDeviceID string
	keepAliveCalls  int
	releaseCalls    int
}

func (fake *fakeReservations) CreateForDevice(_ context.Context, _ audit.Actor, _ string, deviceID string, _ int) (reservation.View, error) {
	fake.createdDeviceID = deviceID
	return fake.current, nil
}
func (fake *fakeReservations) FindOpenForDevice(context.Context, string, string) (reservation.View, error) {
	return fake.current, nil
}
func (fake *fakeReservations) KeepAliveForDevice(context.Context, string, string, string, time.Duration) (reservation.View, error) {
	fake.keepAliveCalls++
	return fake.current, nil
}
func (fake *fakeReservations) Release(context.Context, audit.Actor, string, string, string, reservation.ReleaseInput) (reservation.View, error) {
	fake.releaseCalls++
	closed := fake.current
	closed.Status = domain.ReservationReleased
	return closed, nil
}

type fakeDevices struct{ device management.Device }

func (fake fakeDevices) GetDevice(context.Context, string) (management.Device, error) {
	return fake.device, nil
}

type fakeInventory struct{ devices []stf.Device }

func (fake fakeInventory) Inventory(context.Context) ([]stf.Device, error) { return fake.devices, nil }

func TestStartTargetsSelectedDeviceAndSignsShortSTFWebEntry(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	service := newTestService(t, reservations, true, now)

	view, err := service.Start(context.Background(), audit.Console("admin"), "remote-start-key", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if reservations.createdDeviceID != deviceID || view.Status != "connected" {
		t.Fatalf("target=%q view=%#v", reservations.createdDeviceID, view)
	}
	entry, err := url.Parse(view.URL)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Fragment != "!/control/emulator-5554" {
		t.Fatalf("fragment=%q", entry.Fragment)
	}
	token := entry.Query().Get("jwt")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts=%d", len(parts))
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte("test-stf-auth-secret-at-least-32-bytes"))
	_, _ = mac.Write([]byte(unsigned))
	if !hmac.Equal(mustDecode(t, parts[2]), mac.Sum(nil)) {
		t.Fatal("JWT signature mismatch")
	}
	var header struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(mustDecode(t, parts[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Exp != now.Add(30*time.Second).UnixMilli() {
		t.Fatalf("exp=%d", header.Exp)
	}
	var payload map[string]string
	if err := json.Unmarshal(mustDecode(t, parts[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["email"] != "admin@example.test" || payload["name"] != "Device Farm Admin" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestHeartbeatEndsReservationWhenSTFReleasedDevice(t *testing.T) {
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	service := newTestService(t, reservations, false, time.Now().UTC())
	view, err := service.Heartbeat(context.Background(), audit.Console("admin"), "heartbeat-key", "request-1", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "ended" || reservations.releaseCalls != 1 || reservations.keepAliveCalls != 0 {
		t.Fatalf("view=%#v release=%d keepalive=%d", view, reservations.releaseCalls, reservations.keepAliveCalls)
	}
}

func TestHeartbeatRenewsReservationWhileSTFClaimIsActive(t *testing.T) {
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	service := newTestService(t, reservations, true, time.Now().UTC())
	view, err := service.Heartbeat(context.Background(), audit.Console("admin"), "heartbeat-key", "request-1", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "connected" || view.URL != "" || reservations.keepAliveCalls != 1 || reservations.releaseCalls != 0 {
		t.Fatalf("view=%#v release=%d keepalive=%d", view, reservations.releaseCalls, reservations.keepAliveCalls)
	}
}

func newTestService(t *testing.T, reservations *fakeReservations, using bool, now time.Time) *Service {
	t.Helper()
	service, err := New(reservations, fakeDevices{device: management.Device{
		ID: "device_00000000000001", Serial: "emulator-5554",
	}}, fakeInventory{devices: []stf.Device{{Serial: "emulator-5554", Present: true, Ready: true, Using: using}}}, Config{
		WebURL: "http://stf.example.test", WebAuthSecret: "test-stf-auth-secret-at-least-32-bytes",
		WebUserName: "Device Farm Admin", WebUserEmail: "admin@example.test",
		WebTokenTTL: 30 * time.Second, Lease: time.Minute, Heartbeat: 15 * time.Second,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func activeReservation(deviceID string) reservation.View {
	expires := time.Now().Add(time.Minute)
	return reservation.View{
		ID: "reservation_000000000001", DeviceID: &deviceID, OwnerType: "manual", OwnerID: "admin",
		Status: domain.ReservationActive, ExpiresAt: &expires,
	}
}

func mustDecode(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
