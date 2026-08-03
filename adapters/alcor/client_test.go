package alcor_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/adapters/alcor"
	"github.com/Ad-Quanta/alcor-device-farm/adapters/alcor/mockserver"
)

const mockToken = "mock-service-token"

var runContext = alcor.RunContext{
	RunID: "run_000000000000001", RunAttemptID: "attempt_000000000001",
	Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
}

func TestClientCompletesRunAttemptReservationLifecycleAgainstMock(t *testing.T) {
	server := httptest.NewServer(mockserver.New(mockserver.Config{Token: mockToken, PendingPolls: 2}))
	defer server.Close()
	client := newClient(t, server.URL)

	created, err := client.Reserve(context.Background(), alcor.ReserveRequest{
		PoolID: "pool_000000000000001", Run: runContext, LeaseSeconds: 600,
		RequestedCapabilities: map[string]any{"platformName": "Android", "apiLevel": 34},
		IdempotencyKey:        "reserve-attempt-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "pending" || created.OwnerType != alcor.OwnerTypeRunAttempt || created.OwnerID != runContext.RunAttemptID {
		t.Fatalf("created reservation=%#v", created)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lease, err := client.WaitActive(ctx, created.ID, runContext)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Reservation.Status != "active" || lease.AppiumUDID != "emulator-5554" ||
		lease.AppiumEndpoint != "http://127.0.0.1:4723" || lease.ADBEndpoint != "127.0.0.1:5555" {
		t.Fatalf("lease=%#v", lease)
	}

	extended, err := client.Extend(context.Background(), created.ID, 300, "extend-attempt-0001", runContext)
	if err != nil {
		t.Fatal(err)
	}
	if extended.ExpiresAt == nil || lease.Reservation.ExpiresAt == nil ||
		extended.ExpiresAt.Sub(*lease.Reservation.ExpiresAt) != 300*time.Second {
		t.Fatalf("extended reservation=%#v", extended)
	}
	replayed, err := client.Extend(context.Background(), created.ID, 300, "extend-attempt-0001", runContext)
	if err != nil || !replayed.ExpiresAt.Equal(*extended.ExpiresAt) {
		t.Fatalf("idempotent extension=%#v err=%v", replayed, err)
	}
	if _, err := client.Extend(context.Background(), created.ID, 600, "extend-attempt-0001", runContext); errorCode(err) != "CONFLICT" {
		t.Fatalf("extension idempotency conflict error=%v", err)
	}

	released, err := client.Release(context.Background(), created.ID, "RunAttempt finished", "release-attempt-0001", runContext)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != "released" || released.ReleasedAt == nil {
		t.Fatalf("released reservation=%#v", released)
	}
	replayedRelease, err := client.Release(context.Background(), created.ID, "RunAttempt finished", "release-attempt-0001", runContext)
	if err != nil || replayedRelease.Status != "released" || replayedRelease.ReleasedAt == nil || !replayedRelease.ReleasedAt.Equal(*released.ReleasedAt) {
		t.Fatalf("idempotent release=%#v err=%v", replayedRelease, err)
	}
	if _, err := client.Release(context.Background(), created.ID, "different release reason", "release-attempt-0001", runContext); errorCode(err) != "CONFLICT" {
		t.Fatalf("release idempotency conflict error=%v", err)
	}
}

func TestClientMapsCapacityAndInfrastructureFailures(t *testing.T) {
	tests := []struct {
		name        string
		scenario    string
		disposition alcor.AttemptDisposition
		code        string
	}{
		{name: "capacity", scenario: mockserver.ScenarioCapacityUnavailable, disposition: alcor.DispositionRetryCapacity, code: alcor.CodeDeviceCapacityUnavailable},
		{name: "infrastructure", scenario: mockserver.ScenarioInfrastructureFail, disposition: alcor.DispositionInfraFailed, code: "KVM_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(mockserver.New(mockserver.Config{Token: mockToken, Scenario: test.scenario}))
			defer server.Close()
			client := newClient(t, server.URL)
			_, err := client.Reserve(context.Background(), alcor.ReserveRequest{
				PoolID: "pool_000000000000001", Run: runContext, LeaseSeconds: 600, IdempotencyKey: "reserve-attempt-0002",
			})
			var apiError *alcor.APIError
			if !errors.As(err, &apiError) || apiError.Code != test.code {
				t.Fatalf("error=%#v", err)
			}
			decision := alcor.MapError(err)
			if decision.Disposition != test.disposition || decision.Code != test.code {
				t.Fatalf("decision=%#v", decision)
			}
		})
	}
}

func TestWaitDeadlineBecomesRetryableCapacityDecision(t *testing.T) {
	server := httptest.NewServer(mockserver.New(mockserver.Config{Token: mockToken, PendingPolls: 1000}))
	defer server.Close()
	client := newClient(t, server.URL)
	created, err := client.Reserve(context.Background(), alcor.ReserveRequest{
		PoolID: "pool_000000000000001", Run: runContext, LeaseSeconds: 600, IdempotencyKey: "reserve-attempt-0003",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.WaitActive(ctx, created.ID, runContext)
	decision := alcor.MapError(err)
	if decision.Disposition != alcor.DispositionRetryCapacity || !decision.Retryable {
		t.Fatalf("error=%v decision=%#v", err, decision)
	}
	canceled, err := client.Release(context.Background(), created.ID, "RunAttempt wait timed out", "release-attempt-0003", runContext)
	if err != nil || canceled.Status != "failed" || canceled.FailureCode == nil || *canceled.FailureCode != "RESERVATION_CANCELED" {
		t.Fatalf("canceled pending reservation=%#v err=%v", canceled, err)
	}
	replayed, err := client.Release(context.Background(), created.ID, "RunAttempt wait timed out", "release-attempt-0003", runContext)
	if err != nil || replayed.Status != "failed" {
		t.Fatalf("replayed pending cancellation=%#v err=%v", replayed, err)
	}
}

func newClient(t *testing.T, baseURL string) *alcor.Client {
	t.Helper()
	client, err := alcor.New(alcor.Config{BaseURL: baseURL, Token: mockToken, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func errorCode(err error) string {
	var apiError *alcor.APIError
	if errors.As(err, &apiError) {
		return apiError.Code
	}
	return ""
}
