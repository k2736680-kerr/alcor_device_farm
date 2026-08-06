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
