package alcor

import (
	"errors"
	"fmt"
)

const (
	CodeDeviceCapacityUnavailable = "DEVICE_CAPACITY_UNAVAILABLE"
	CodeDevicePoolUnavailable     = "DEVICE_POOL_UNAVAILABLE"
)

type APIError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
	Retryable  bool           `json:"retryable"`
	HTTPStatus int            `json:"-"`
	RequestID  string         `json:"-"`
}

func (err *APIError) Error() string {
	if err == nil {
		return ""
	}
	if err.RequestID != "" {
		return fmt.Sprintf("device farm %s: %s (request_id=%s)", err.Code, err.Message, err.RequestID)
	}
	return fmt.Sprintf("device farm %s: %s", err.Code, err.Message)
}

type AttemptDisposition string

const (
	DispositionRetryCapacity       AttemptDisposition = "retry_capacity"
	DispositionRetryInfrastructure AttemptDisposition = "retry_infrastructure"
	DispositionInfraFailed         AttemptDisposition = "infra_failed"
	DispositionFailed              AttemptDisposition = "failed"
)

type ErrorDecision struct {
	Disposition AttemptDisposition
	Retryable   bool
	Code        string
}

func MapError(err error) ErrorDecision {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return ErrorDecision{Disposition: DispositionRetryInfrastructure, Retryable: true, Code: "TRANSPORT_ERROR"}
	}
	if apiError.Code == CodeDeviceCapacityUnavailable || apiError.Code == "CAPACITY_UNAVAILABLE" {
		return ErrorDecision{Disposition: DispositionRetryCapacity, Retryable: true, Code: CodeDeviceCapacityUnavailable}
	}
	if infrastructureCode(apiError.Code) {
		if apiError.Retryable {
			return ErrorDecision{Disposition: DispositionRetryInfrastructure, Retryable: true, Code: apiError.Code}
		}
		return ErrorDecision{Disposition: DispositionInfraFailed, Retryable: false, Code: apiError.Code}
	}
	return ErrorDecision{Disposition: DispositionFailed, Retryable: apiError.Retryable, Code: apiError.Code}
}

func infrastructureCode(code string) bool {
	switch code {
	case "SERVICE_UNAVAILABLE", "HOST_OFFLINE", "AGENT_COMMAND_TIMEOUT",
		"EMULATOR_CREATE_FAILED", "EMULATOR_START_FAILED", "EMULATOR_DELETE_FAILED",
		"KVM_UNAVAILABLE", "STF_CLAIM_FAILED", "STF_RELEASE_FAILED",
		"STF_REMOTE_CONNECT_FAILED", "STF_REMOTE_DISCONNECT_FAILED", "STF_BAD_RESPONSE",
		"APPIUM_UNHEALTHY", "DEVICE_BOOT_TIMEOUT", "DEVICE_QUARANTINED", "INTERNAL_ERROR":
		return true
	default:
		return false
	}
}
