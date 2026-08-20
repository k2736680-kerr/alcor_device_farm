package remotecontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

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
	findErr         error
}

func (fake *fakeReservations) CreateForDevice(_ context.Context, _ audit.Actor, _ string, deviceID string, _ int) (reservation.View, error) {
	fake.createdDeviceID = deviceID
	return fake.current, nil
}
func (fake *fakeReservations) FindOpenForDevice(context.Context, string, string) (reservation.View, error) {
	return fake.current, fake.findErr
}

func TestEndIsIdempotentWhenTheTargetDeviceHasAlreadyDisappeared(t *testing.T) {
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{findErr: reservation.ErrNotFound}
	service := newTestService(t, reservations, time.Now().UTC())

	view, err := service.End(context.Background(), audit.Console("admin"), "remote-end-key", "request-1", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "ended" || view.DeviceID != deviceID || reservations.releaseCalls != 0 {
		t.Fatalf("view=%#v release=%d", view, reservations.releaseCalls)
	}
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

type fakeIOSRemote struct {
	bootedUDID string
	entry      string
	bootErr    error
}

func (fake *fakeIOSRemote) EnsureBooted(_ context.Context, udid string) error {
	fake.bootedUDID = udid
	return fake.bootErr
}

func (fake *fakeIOSRemote) EntryURL(_, _, _, _ string) (string, error) {
	return fake.entry, nil
}

func TestStartTargetsSelectedDeviceAndSignsShortSTFWebEntry(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	service := newTestService(t, reservations, now)

	view, err := service.Start(context.Background(), audit.Console("admin"), "remote-start-key", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if reservations.createdDeviceID != deviceID || view.Status != "connected" {
		t.Fatalf("target=%q view=%#v", reservations.createdDeviceID, view)
	}
	if view.Transport != TransportSTF {
		t.Fatalf("transport=%q", view.Transport)
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

func TestHeartbeatRenewsReservationWithoutInferringAnSTFDisconnectIsAHangup(t *testing.T) {
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	service := newTestService(t, reservations, time.Now().UTC())
	view, err := service.Heartbeat(context.Background(), audit.Console("admin"), "heartbeat-key", "request-1", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "connected" || view.URL != "" || reservations.releaseCalls != 0 || reservations.keepAliveCalls != 1 {
		t.Fatalf("view=%#v release=%d keepalive=%d", view, reservations.releaseCalls, reservations.keepAliveCalls)
	}
}

func newTestService(t *testing.T, reservations *fakeReservations, now time.Time) *Service {
	t.Helper()
	service, err := New(reservations, fakeDevices{device: management.Device{
		ID: "device_00000000000001", Platform: "android", DeviceKind: "emulator",
		ProviderType: "docker_emulator", Serial: "emulator-5554",
	}}, Config{
		STFWebURL: "http://stf.example.test", STFWebAuthSecret: "test-stf-auth-secret-at-least-32-bytes",
		STFWebUserName: "Device Farm Admin", STFWebUserEmail: "admin@example.test",
		STFWebTokenTTL: 30 * time.Second,
		Lease:          time.Minute, Heartbeat: 15 * time.Second,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestStartIOSUsesBaguetteTargetSimulatorEntryWithoutInternalEndpoint(t *testing.T) {
	now := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	iosRemote := &fakeIOSRemote{entry: "http://farm.example.test:18081/entry/signed-ticket"}
	service, err := New(reservations, fakeDevices{device: management.Device{
		ID: deviceID, Platform: "ios", DeviceKind: "simulator", ProviderType: "appium_device_farm_ios",
		HostID: "host_000000000000001", ProviderRef: "00000000-0000-0000-0000-000000000001",
		Serial: "00000000-0000-0000-0000-000000000001",
	}}, Config{
		IOSEnabled: true, Lease: time.Minute, Heartbeat: 15 * time.Second,
		Now: func() time.Time { return now },
	}, iosRemote)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Start(context.Background(), audit.Console("admin"), "remote-start-key", deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Transport != TransportBaguette || view.URL != iosRemote.entry || iosRemote.bootedUDID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("view=%#v", view)
	}
	for _, forbidden := range []string{"127.0.0.1", "4810", "appium-session", "password", "passwd", "00000000-0000"} {
		if strings.Contains(strings.ToLower(view.URL), strings.ToLower(forbidden)) {
			t.Fatalf("iOS entry leaked %q: %s", forbidden, view.URL)
		}
	}
}

func TestStartRejectsPhysicalIOSBeforeCreatingReservation(t *testing.T) {
	deviceID := "device_00000000000001"
	reservations := &fakeReservations{current: activeReservation(deviceID)}
	service, err := New(reservations, fakeDevices{device: management.Device{
		ID: deviceID, Platform: "ios", DeviceKind: "physical", ProviderType: "appium_device_farm_ios",
	}}, Config{
		IOSEnabled: true, Lease: time.Minute, Heartbeat: 15 * time.Second,
	}, &fakeIOSRemote{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Start(context.Background(), audit.Console("admin"), "remote-start-key", deviceID)
	if !errors.Is(err, ErrUnavailable) || reservations.createdDeviceID != "" {
		t.Fatalf("err=%v created=%q", err, reservations.createdDeviceID)
	}
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
