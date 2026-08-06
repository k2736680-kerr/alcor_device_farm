package domain

import "strings"

const STFReadinessStabilizationReason = "STF readiness stabilization is in progress"

func IsSTFFailureReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason == "device is not visible through STF" || strings.HasPrefix(reason, "STF ")
}
