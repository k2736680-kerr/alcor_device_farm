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

// 不传 pool_id 时，请求里就不能出现 pool_id 字段，由农场自己挑池。
//
// 这条是在保证「Alcor 只报能力、农场自己决定池」这条路真的走得通：
// 只要客户端还偷偷塞一个空 pool_id 或随机池名，农场侧的选择逻辑就被绕过了。
func TestClientReserveWithoutPoolIDDelegatesSelectionToFarm(t *testing.T) {
	server := httptest.NewServer(mockserver.New(mockserver.Config{Token: mockToken}))
	defer server.Close()
	client := newClient(t, server.URL)

	created, err := client.Reserve(context.Background(), alcor.ReserveRequest{
		Run: runContext, LeaseSeconds: 600,
		RequestedCapabilities: map[string]any{"platformName": "Android", "apiLevel": 34},
		IdempotencyKey:        "reserve-auto-pool-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.PoolID != mockserver.AutoSelectedPoolID {
		t.Fatalf("未指定池时应当由农场挑一个池，实际 pool_id=%q", created.PoolID)
	}
	if created.OwnerID != runContext.RunAttemptID || created.RequestedCapabilities["apiLevel"] != float64(34) {
		t.Fatalf("created reservation=%#v", created)
	}
}

// 指定了 pool_id 时行为必须完全不变：池名原样发给农场，不被自动选择覆盖。
func TestClientReserveWithPoolIDKeepsCallerChoice(t *testing.T) {
	server := httptest.NewServer(mockserver.New(mockserver.Config{Token: mockToken}))
	defer server.Close()
	client := newClient(t, server.URL)

	created, err := client.Reserve(context.Background(), alcor.ReserveRequest{
		PoolID: "pool_000000000000001", Run: runContext, LeaseSeconds: 600,
		IdempotencyKey: "reserve-explicit-pool-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.PoolID != "pool_000000000000001" {
		t.Fatalf("显式指定的池不能被改写，实际 %q", created.PoolID)
	}
}

// pool_id 明确写错（不合法标识）时仍然要报错，不能当成「没指定」而静默自动选池。
func TestClientReserveRejectsMalformedPoolID(t *testing.T) {
	server := httptest.NewServer(mockserver.New(mockserver.Config{Token: mockToken}))
	defer server.Close()
	client := newClient(t, server.URL)

	if _, err := client.Reserve(context.Background(), alcor.ReserveRequest{
		PoolID: "no", Run: runContext, LeaseSeconds: 600, IdempotencyKey: "reserve-bad-pool-0001",
	}); err == nil {
		t.Fatal("不合法 pool_id 应当被拒绝")
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

// DEVICE_POOL_UNAVAILABLE 的两种性质必须被分开，不能被错误码一刀切。
//
// 背景：Alcor 侧原先的失败处理是把任何 Reserve 错误都当成运行的永久失败
// （infra_failed）。而「此刻没有池能服务」是容量类瞬态问题，容量释放后会自行好转，
// 必须让 Alcor 能够识别并重试；反之「数据库未配置」「池不是 active」重试无意义。
//
// 农场因此在产生点用 IsRetryable() 声明性质，API 层透传到错误信封，
// MapError 再据此分诊。这三层任缺一层，瞬态问题都会被判成永久失败。
func TestMapErrorSeparatesTransientAndPermanentPoolUnavailability(t *testing.T) {
	tests := []struct {
		name        string
		retryable   bool
		disposition alcor.AttemptDisposition
	}{
		{
			// 此刻没有池能服务（池满 / 设备 booting / 宿主临时离线）
			name:        "transient pool unavailability retries as capacity",
			retryable:   true,
			disposition: alcor.DispositionRetryCapacity,
		},
		{
			// 配置/部署问题（数据库未配置、池非 active）
			name:        "permanent pool unavailability fails",
			retryable:   false,
			disposition: alcor.DispositionFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &alcor.APIError{
				Code:      alcor.CodeDevicePoolUnavailable,
				Message:   "当前没有可服务该请求的设备池",
				Retryable: test.retryable,
			}
			decision := alcor.MapError(err)
			if decision.Disposition != test.disposition {
				t.Fatalf("retryable=%v decision=%#v（期望 disposition=%s）",
					test.retryable, decision, test.disposition)
			}
			if decision.Retryable != test.retryable {
				t.Fatalf("retryable=%v decision.Retryable=%v", test.retryable, decision.Retryable)
			}
		})
	}
}
