package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
)

type Operation string

const (
	OperationDiscover       Operation = "discover"
	OperationCreate         Operation = "create"
	OperationStart          Operation = "start"
	OperationStop           Operation = "stop"
	OperationRestart        Operation = "restart"
	OperationRebuild        Operation = "rebuild"
	OperationDelete         Operation = "delete"
	OperationInspectHealth  Operation = "inspect_health"
	OperationConnectionInfo Operation = "get_connection_info"
	OperationValidateImage  Operation = "validate_image"
)

type State string

const (
	StateCreated State = "created"
	StateRunning State = "running"
	StateStopped State = "stopped"
)

type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
)

type ProbeStatus string

const (
	ProbePassed      ProbeStatus = "passed"
	ProbeFailed      ProbeStatus = "failed"
	ProbeUnknown     ProbeStatus = "unknown"
	ProbeUnsupported ProbeStatus = "unsupported"
)

const (
	ProbeTransport     = "transport"
	ProbeOSReady       = "os_ready"
	ProbeAutomation    = "automation"
	ProbeRouter        = "router"
	ProbeRemoteControl = "remote_control"
)

type CreateRequest struct {
	DeviceID       string
	HostID         string
	ImageID        string
	Platform       Platform
	DeviceKind     string
	RuntimeImage   string
	ProviderRef    string
	Serial         string
	Capabilities   map[string]any
	RuntimeProfile runtimeprofile.Profile
}

type Health struct {
	Platform      Platform
	Components    map[string]ProbeStatus
	Online        bool
	ADBOnline     bool
	BootCompleted bool
	AppiumHealthy bool
}

func (health Health) Ready() bool {
	if len(health.Components) > 0 {
		for _, component := range []string{ProbeTransport, ProbeOSReady, ProbeAutomation, ProbeRouter} {
			if health.Components[component] != ProbePassed {
				return false
			}
		}
		return true
	}
	return health.Online && health.ADBOnline && health.BootCompleted && health.AppiumHealthy
}

type ConnectionInfo struct {
	Platform       Platform
	Serial         string
	DeviceUDID     string
	ProviderID     string
	ADBEndpoint    string
	AppiumEndpoint string
	AppiumUDID     string
}

type Snapshot struct {
	DeviceID       string
	HostID         string
	ImageID        string
	Platform       Platform
	ProviderRef    string
	State          State
	Generation     int
	Capabilities   map[string]any
	RuntimeProfile runtimeprofile.Profile
	Health         Health
	Connection     ConnectionInfo
}

func (snapshot Snapshot) Ready() bool {
	return snapshot.State == StateRunning && snapshot.Health.Ready()
}

type Provider interface {
	Discover(context.Context, string) ([]Snapshot, error)
	Create(context.Context, CreateRequest) (Snapshot, error)
	Start(context.Context, string) (Snapshot, error)
	Stop(context.Context, string) (Snapshot, error)
	Restart(context.Context, string) (Snapshot, error)
	Rebuild(context.Context, string) (Snapshot, error)
	Delete(context.Context, string) error
	InspectHealth(context.Context, string) (Health, error)
	GetConnectionInfo(context.Context, string) (ConnectionInfo, error)
}

// ImageDigestVerifier is deliberately separate from Provider so USB and other
// physical-device providers do not need to implement Docker image behavior.
type ImageDigestVerifier interface {
	VerifyImageDigest(context.Context, string, string) error
}

// ValidRuntimeImageReference accepts immutable digest references or explicit
// non-latest tags. Digest verification remains a separate mandatory step for
// Docker commands issued by the production Host Agent.
func ValidRuntimeImageReference(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") {
		return false
	}
	if separator := strings.LastIndex(value, "@sha256:"); separator >= 0 {
		if separator == 0 || strings.Count(value, "@") != 1 || len(value[separator+len("@sha256:"):]) != 64 {
			return false
		}
		for _, character := range value[separator+len("@sha256:"):] {
			if !strings.ContainsRune("0123456789abcdef", character) {
				return false
			}
		}
		return true
	}
	if strings.Contains(value, "@") {
		return false
	}
	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	if lastColon <= lastSlash {
		return false
	}
	tag := strings.TrimSpace(value[lastColon+1:])
	return tag != "" && !strings.EqualFold(tag, "latest")
}

type Error struct {
	Operation Operation
	Code      string
	Message   string
	Retryable bool
	Cause     error
}

func (err *Error) Error() string {
	if err.Cause != nil {
		return fmt.Sprintf("provider %s: %s: %v", err.Operation, err.Message, err.Cause)
	}
	return fmt.Sprintf("provider %s: %s", err.Operation, err.Message)
}

func (err *Error) Unwrap() error { return err.Cause }

func ErrorCode(err error) string {
	var providerError *Error
	if errors.As(err, &providerError) {
		return providerError.Code
	}
	return ""
}
