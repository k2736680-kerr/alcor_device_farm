package reservation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// 这组测试锁住「pool_id 缺席 -> 农场自己选池」和「pool_id 传空串 -> 报错」的区别。
//
// 为什么必须区分：调用方（Alcor）的自动选池路径靠省略字段来表达。如果农场把
// 空串也当成「没指定」，那么一个把 device_pool_id 拼错成 pool_id=\"\" 的请求会
// 被静默改写成「随便挑个池」，运行失败的原因就变得很难查。

func decodeCreateInput(t *testing.T, body string) (CreateInput, error) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	var input CreateInput
	err := decoder.Decode(&input)
	return input, err
}

func TestCreateInputTracksWhetherPoolIDWasSent(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantPresent bool
	}{
		{
			name:        "省略 pool_id：委托农场选池",
			body:        `{"owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600}`,
			wantPresent: false,
		},
		{
			name:        "pool_id 是空串：调用方写错",
			body:        `{"pool_id":"","owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600}`,
			wantPresent: true,
		},
		{
			name:        "pool_id 有值：走指定的池",
			body:        `{"pool_id":"pool_000000000000001","owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600}`,
			wantPresent: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, err := decodeCreateInput(t, test.body)
			if err != nil {
				t.Fatal(err)
			}
			if input.PoolIDProvided() != test.wantPresent {
				t.Fatalf("PoolIDProvided()=%v want=%v", input.PoolIDProvided(), test.wantPresent)
			}
		})
	}
}

// 显式空串必须在选池之前就被拒绝 —— 即使用数据库里没有任何可用池，
// 报的也应该是「参数不合法」，而不是「没有可用池」。
func TestServiceRejectsExplicitlyEmptyPoolID(t *testing.T) {
	db := openSelectorDatabase(t)
	input, err := decodeCreateInput(t, `{"pool_id":"","owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600}`)
	if err != nil {
		t.Fatal(err)
	}
	if !input.PoolIDProvided() {
		t.Fatal("空串应当被记为「已提供」")
	}
	service := newSelectorService(db)
	_, createErr := service.create(
		context.Background(), testActor(), "idem-key-000000000001", input, "")
	if createErr == nil {
		t.Fatal("显式空 pool_id 应当报错")
	}
	// 关键：语义必须是「参数不合法」，不能落到「没有可用池」上。
	if errors.Is(createErr, ErrPoolUnavailable) {
		t.Fatalf("空串是参数错误，不该被当成没有可用池：%v", createErr)
	}
	if !errors.Is(createErr, ErrInvalidArgument) {
		t.Fatalf("应当返回 ErrInvalidArgument，实际 %v", createErr)
	}
}

// 省略 pool_id 且没有任何可用池时，错误应当是「没有可用池」这种可重试语义，
// 而不是参数错误 —— 这样上层才知道该稍后重试而不是报配置错。
func TestServiceReportsPoolUnavailableWhenNothingIsSelectable(t *testing.T) {
	db := openSelectorDatabase(t)
	input, err := decodeCreateInput(t, `{"owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600,"requested_capabilities":{"platformName":"Android","apiLevel":34}}`)
	if err != nil {
		t.Fatal(err)
	}
	if input.PoolIDProvided() {
		t.Fatal("省略 pool_id 不该被记为「已提供」")
	}
	service := newSelectorService(db)
	_, createErr := service.create(
		context.Background(), testActor(), "idem-key-000000000002", input, "")
	if createErr == nil {
		t.Fatal("没有任何可用池时应当报错")
	}
	if !errors.Is(createErr, ErrPoolUnavailable) {
		t.Fatalf("应当返回 ErrPoolUnavailable，实际 %v", createErr)
	}
	// 关键：必须**显式**声明可重试。上层（api 层 isRetryable -> 错误信封
	// Retryable 字段 -> Alcor 的失败处理）全靠这个声明来区分
	// 「稍后会好转」和「配置有问题」。缺了它，一次瞬时的「此刻没有池」
	// 会被 Alcor 当成运行的永久失败。
	retryable, ok := createErr.(interface{ IsRetryable() bool })
	if !ok {
		t.Fatalf("错误应当实现 IsRetryable()，实际类型 %T", createErr)
	}
	if !retryable.IsRetryable() {
		t.Fatalf("「此刻没有池能服务」是瞬态，必须可重试：%v", createErr)
	}
}

// 省略 pool_id 且存在可用池时，必须真的选中它。
//
// 这里直接断言选池结果，而不是跑完整个 create：create 后半段要写预约、生成 ID、
// 发审计，属于持久化路径，由 reservation 的其它用例覆盖。本用例要证明的只有一件事
// ——「省略 pool_id 会被翻译成一次成功的自动选池」。
func TestServiceAutoSelectsPoolWhenPoolIDOmitted(t *testing.T) {
	db := openSelectorDatabase(t)
	seedSelectorHost(t, db, "host_0000000000001", "online")
	seedSelectorAndroidImage(t, db, 34)
	seedSelectorPool(t, db, "pool_0000000000001", "android", "", 2)
	seedSelectorDevice(t, db, "device_00000000001", "pool_0000000000001", "host_0000000000001",
		"android", "ready", "healthy", `{"platformName":"Android","apiLevel":34}`)

	input, err := decodeCreateInput(t, `{"owner_type":"run_attempt","owner_id":"attempt_000000000001","lease_seconds":600,"requested_capabilities":{"platformName":"Android","apiLevel":34}}`)
	if err != nil {
		t.Fatal(err)
	}
	if input.PoolIDProvided() {
		t.Fatal("省略 pool_id 不该被记为「已提供」")
	}
	service := newSelectorService(db)
	selectedPoolID, selectErr := service.selectPoolForRequest(context.Background(), testActor(), input.RequestedCapabilities)
	if selectErr != nil {
		t.Fatalf("省略 pool_id 时应当能自动选池：%v", selectErr)
	}
	if selectedPoolID != "pool_0000000000001" {
		t.Fatalf("选中的池应当是唯一可用池，实际 %q", selectedPoolID)
	}
}
