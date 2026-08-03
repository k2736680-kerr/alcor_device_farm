package alcor

import "time"

const OwnerTypeRunAttempt = "run_attempt"

type RunContext struct {
	RunID        string
	RunAttemptID string
	Traceparent  string
}

type ReserveRequest struct {
	PoolID                string
	Run                   RunContext
	RequestedCapabilities map[string]any
	LeaseSeconds          int
	IdempotencyKey        string
}

type Reservation struct {
	ID                    string         `json:"id"`
	PoolID                string         `json:"pool_id"`
	DeviceID              *string        `json:"device_id,omitempty"`
	OwnerType             string         `json:"owner_type"`
	OwnerID               string         `json:"owner_id"`
	RequestedCapabilities map[string]any `json:"requested_capabilities"`
	LeaseSeconds          int            `json:"lease_seconds"`
	Status                string         `json:"status"`
	StartsAt              *time.Time     `json:"starts_at,omitempty"`
	ExpiresAt             *time.Time     `json:"expires_at,omitempty"`
	ReleasedAt            *time.Time     `json:"released_at,omitempty"`
	FailureCode           *string        `json:"failure_code,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type Device struct {
	ID             string         `json:"id"`
	Serial         string         `json:"serial"`
	ADBEndpoint    *string        `json:"adb_endpoint,omitempty"`
	AppiumEndpoint *string        `json:"appium_endpoint,omitempty"`
	Capabilities   map[string]any `json:"capabilities"`
}

type Lease struct {
	Reservation    Reservation
	Device         Device
	AppiumUDID     string
	ADBEndpoint    string
	AppiumEndpoint string
}

type ErrorEnvelope struct {
	RequestID string    `json:"request_id"`
	Data      any       `json:"data"`
	Error     *APIError `json:"error"`
}
