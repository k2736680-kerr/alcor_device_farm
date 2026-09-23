package domain

import "strings"

const STFReadinessStabilizationReason = "STF readiness stabilization is in progress"
const AgentReportedUnhealthyReason = "agent heartbeat reported an assigned or schedulable device is not healthy"

func IsSTFFailureReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason == "device is not visible through STF" || strings.HasPrefix(reason, "STF ")
}

// IsSystemRecoverableHealthReason distinguishes transient infrastructure
// isolation from an administrator's explicit quarantine. Only these reasons
// may restore the same long-lived device without destructive replacement.
func IsSystemRecoverableHealthReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason == HostUnavailableReason || reason == AgentReportedUnhealthyReason ||
		IsSTFFailureReason(reason) || strings.HasPrefix(reason, "IOS_PROVIDER_DEVICE_MISSING:")
}
