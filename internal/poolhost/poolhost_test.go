package poolhost_test

import (
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/poolhost"
)

// 池名里的地址必须按**整段 IPv4** 解析。
//
// 严格性是刻意的：`android-16-api36` 里的 16 不是地址，把版本号误判成地址
// 会让调度收窄到一个永远匹配不到的归属上，池被饿死。
func TestAddressFromPoolName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"android-10.0.30.54", "10.0.30.54"},
		{"android-10.0.30.171", "10.0.30.171"},
		{"iOS - 10.0.33.68", "10.0.33.68"},
		{"android-10.0.30.54-extra", "10.0.30.54"},
		{"pool_10.0.30.54", "10.0.30.54"},
		{"pool/10.0.30.54", "10.0.30.54"},
		// 抽不到：无地址或不是合法 IPv4
		{"default-android", ""},
		{"android-16-api36", ""},
		{"warm-host", ""},
		{"pool-1.2.3.4.5", ""},
		{"pool-999.1.1.1", ""},
		{"android-10.0.30.540", ""},
		{"android-10.0.30.5.4", ""},
		{"abc123.4.5.6", ""},
		{"", ""},
		{"android-10.0.30", ""},
		{"0.0.0.0", "0.0.0.0"},
		{"255.255.255.255", "255.255.255.255"},
	}
	for _, testCase := range cases {
		if got := poolhost.AddressFromPoolName(testCase.name); got != testCase.want {
			t.Errorf("AddressFromPoolName(%q)=%q，期望 %q", testCase.name, got, testCase.want)
		}
	}
}

// 归属是否生效取决于「该地址在农场里是否真的存在」：
//   - 没声明地址      -> 不收窄（回退全农场任选）
//   - 声明了但不存在  -> 不收窄（否则池会被永久饿死）
//   - 声明了且存在    -> 收窄
func TestResolveScopeAppliesOnlyWhenAddressIsKnown(t *testing.T) {
	known := func(address string) bool { return address == "10.0.30.54" }

	cases := []struct {
		poolName      string
		wantApplies   bool
		wantAddress   string
	}{
		{"android-10.0.30.54", true, "10.0.30.54"},
		{"android-10.0.30.55", false, "10.0.30.55"}, // 推导出地址但农场里没有
		{"default-android", false, ""},
		{"android-16-api36", false, ""},
	}
	for _, testCase := range cases {
		scope := poolhost.ResolveScope(testCase.poolName, known)
		if scope.Applies() != testCase.wantApplies {
			t.Errorf("%q: Applies()=%v，期望 %v", testCase.poolName, scope.Applies(), testCase.wantApplies)
		}
		if scope.Address != testCase.wantAddress {
			t.Errorf("%q: Address=%q，期望 %q", testCase.poolName, scope.Address, testCase.wantAddress)
		}
	}
}

// exists 为 nil 时必须保守处理：不能因为回调缺失就误判成「地址存在」而去收窄。
func TestResolveScopeWithoutExistenceProbe(t *testing.T) {
	scope := poolhost.ResolveScope("android-10.0.30.54", nil)
	if scope.Applies() {
		t.Fatal("没有存在性探针时不该收窄（会饿死池）")
	}
}