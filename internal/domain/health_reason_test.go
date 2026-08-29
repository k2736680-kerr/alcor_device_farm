package domain

import "testing"

func TestIsSTFFailureReason(t *testing.T) {
	for _, test := range []struct {
		reason string
		want   bool
	}{
		{reason: "device is not visible through STF", want: true},
		{reason: "STF STF_INVENTORY_FAILED: request timed out", want: true},
		{reason: "STF readiness stabilization is in progress", want: true},
		{reason: "agent heartbeat reported unhealthy", want: false},
		{reason: "", want: false},
	} {
		if got := IsSTFFailureReason(test.reason); got != test.want {
			t.Fatalf("IsSTFFailureReason(%q)=%t want %t", test.reason, got, test.want)
		}
	}
}

func TestIsSystemRecoverableHealthReasonExcludesManualQuarantine(t *testing.T) {
	for _, reason := range []string{HostUnavailableReason, AgentReportedUnhealthyReason, "device is not visible through STF", "IOS_PROVIDER_DEVICE_MISSING: inventory drift"} {
		if !IsSystemRecoverableHealthReason(reason) {
			t.Fatalf("system reason %q is not recoverable", reason)
		}
	}
	for _, reason := range []string{"管理员手工隔离", "IOS_SESSION_CLEANUP_FAILED", "management restart failed"} {
		if IsSystemRecoverableHealthReason(reason) {
			t.Fatalf("manual or terminal reason %q is recoverable", reason)
		}
	}
}
