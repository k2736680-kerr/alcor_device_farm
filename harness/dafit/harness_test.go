package dafit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	err                error
	blockUntilCanceled bool
	spec               RunSpec
}

func (runner *fakeRunner) Run(ctx context.Context, spec RunSpec) error {
	runner.spec = spec
	if runner.blockUntilCanceled {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := os.MkdirAll(spec.ReportDirectory, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"report.html", "report.json"} {
		if err := os.WriteFile(filepath.Join(spec.ReportDirectory, name), []byte("report"), 0o600); err != nil {
			return err
		}
	}
	return runner.err
}

type farmStub struct {
	server       *httptest.Server
	mutex        sync.Mutex
	pending      bool
	releaseCalls int
}

func newFarmStub(t *testing.T, pending bool) *farmStub {
	t.Helper()
	stub := &farmStub{pending: pending}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *farmStub) handle(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	switch request.Method + " " + request.URL.Path {
	case "POST /api/v1/device-reservations":
		writeData(writer, http.StatusCreated, map[string]any{"id": "reservation_00000001", "status": "pending"})
	case "GET /api/v1/device-reservations/reservation_00000001":
		if stub.pending {
			writeData(writer, http.StatusOK, map[string]any{"id": "reservation_00000001", "status": "pending"})
			return
		}
		writeData(writer, http.StatusOK, map[string]any{"id": "reservation_00000001", "device_id": "device_0000000000001", "status": "active"})
	case "GET /api/v1/devices/device_0000000000001":
		writeData(writer, http.StatusOK, map[string]any{
			"id": "device_0000000000001", "adb_endpoint": "10.0.0.20:31000",
			"appium_endpoint": "http://10.0.0.20:4723", "capabilities": map[string]any{"appiumUdid": "emulator-5554"},
		})
	case "POST /api/v1/device-reservations/reservation_00000001/releases":
		stub.mutex.Lock()
		stub.releaseCalls++
		stub.mutex.Unlock()
		writeData(writer, http.StatusOK, map[string]any{"id": "reservation_00000001", "status": "released"})
	default:
		http.NotFound(writer, request)
	}
}

func writeData(writer http.ResponseWriter, status int, data any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"request_id": "req_test", "data": data, "error": nil})
}

func TestHarnessInjectsAssignedConnectionsCollectsReportsAndReleases(t *testing.T) {
	stub := newFarmStub(t, false)
	runner := &fakeRunner{}
	reportDir := filepath.Join(t.TempDir(), "attempt-01")
	result, err := New(stub.server.Client(), runner).Run(context.Background(), testConfig(stub.server.URL, reportDir))
	if err != nil {
		t.Fatal(err)
	}
	if result.ReservationID != "reservation_00000001" || result.DeviceID != "device_0000000000001" {
		t.Fatalf("result=%#v", result)
	}
	if runner.spec.AppiumUDID != "emulator-5554" || runner.spec.ADBEndpoint != "10.0.0.20:31000" ||
		runner.spec.AppiumEndpoint != "http://10.0.0.20:4723" || runner.spec.CaseID != "STEPS_SMOKE_001" {
		t.Fatalf("run spec=%#v", runner.spec)
	}
	if stub.releaseCalls != 1 {
		t.Fatalf("release calls=%d", stub.releaseCalls)
	}
}

func TestHarnessPreservesFailureReportsAndStillReleases(t *testing.T) {
	stub := newFarmStub(t, false)
	runner := &fakeRunner{err: errors.New("intentional assertion failure")}
	result, err := New(stub.server.Client(), runner).Run(context.Background(), testConfig(stub.server.URL, filepath.Join(t.TempDir(), "failed-attempt")))
	if err == nil || result.ExitError == "" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if _, statErr := os.Stat(result.ReportJSON); statErr != nil {
		t.Fatal(statErr)
	}
	if stub.releaseCalls != 1 {
		t.Fatalf("release calls=%d", stub.releaseCalls)
	}
}

func TestHarnessTimeoutCancelsPendingReservation(t *testing.T) {
	stub := newFarmStub(t, true)
	config := testConfig(stub.server.URL, filepath.Join(t.TempDir(), "timeout-attempt"))
	config.WaitTimeout = 20 * time.Millisecond
	config.PollInterval = time.Millisecond
	_, err := New(stub.server.Client(), &fakeRunner{}).Run(context.Background(), config)
	if err == nil {
		t.Fatal("Run() error=nil")
	}
	if stub.releaseCalls != 1 {
		t.Fatalf("release calls=%d", stub.releaseCalls)
	}
}

func TestHarnessRunTimeoutStillReleasesReservation(t *testing.T) {
	stub := newFarmStub(t, false)
	config := testConfig(stub.server.URL, filepath.Join(t.TempDir(), "run-timeout-attempt"))
	config.RunTimeout = 20 * time.Millisecond
	result, err := New(stub.server.Client(), &fakeRunner{blockUntilCanceled: true}).Run(context.Background(), config)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if result.ExitError == "" {
		t.Fatalf("result=%#v", result)
	}
	if stub.releaseCalls != 1 {
		t.Fatalf("release calls=%d", stub.releaseCalls)
	}
}

func TestDaFitChildEnvironmentDoesNotReceiveDeviceFarmSecrets(t *testing.T) {
	t.Setenv("DEVICE_FARM_SECURITY_SERVICE_TOKEN", "service-secret")
	t.Setenv("DEVICE_FARM_DATABASE_URL", "postgres://secret")
	t.Setenv("DEVICE_FARM_STF_API_TOKEN", "stf-secret")
	t.Setenv("ANDROID_UDID", "wrong-device")
	environment := childEnvironment(map[string]string{"ANDROID_UDID": "assigned-device"})
	joined := strings.Join(environment, "\n")
	for _, secret := range []string{"service-secret", "postgres://secret", "stf-secret", "ANDROID_UDID=wrong-device"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("child environment contains %q", secret)
		}
	}
	if !strings.Contains(joined, "ANDROID_UDID=assigned-device") {
		t.Fatal("assigned device was not injected")
	}
}

func testConfig(serverURL, reportDir string) Config {
	return Config{
		ServerURL: serverURL, ServiceToken: "service-token", PoolID: "pool_000000000000001",
		OwnerID: "owner_00000000000001", LeaseSeconds: 600,
		RequestedCapabilities: map[string]any{"platformName": "Android"},
		DaFitDirectory:        filepath.Dir(reportDir), ReportDirectory: reportDir,
		PythonExecutable: "python", ADBExecutable: "adb", CaseID: "STEPS_SMOKE_001",
		WaitTimeout: time.Second, PollInterval: time.Millisecond, RunTimeout: time.Second,
	}
}
