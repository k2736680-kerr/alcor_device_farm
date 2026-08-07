package management

import (
	"context"
	"errors"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

var (
	ErrNotFound            = errors.New("management resource not found")
	ErrConflict            = errors.New("management resource conflict")
	ErrInvalidArgument     = errors.New("invalid management argument")
	ErrHostUnavailable     = errors.New("device host is not accepting new devices")
	ErrImageUnavailable    = errors.New("device image is not ready")
	ErrProviderUnavailable = errors.New("device provider is not configured")
)

type Idempotency struct {
	ClientID       string
	Scope          string
	Key            string
	RequestHash    string
	ResourceType   string
	ResourceID     string
	ResponseStatus int
}

type DeviceAudit struct {
	ID                  string
	ActorType           string
	ActorID             string
	Action              string
	RequestID           string
	Reason              string
	DestructiveApproved bool
}

type DeviceOperation struct {
	CommandID         string
	CommandType       string
	IdempotencyKey    string
	Payload           map[string]any
	MaxAttempts       int
	Device            Device
	ExpectedLifecycle domain.DeviceLifecycleStatus
	ExpectedHealth    domain.HealthStatus
	Audit             DeviceAudit
}

type Image struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	DockerImage     string             `json:"docker_image,omitempty"`
	DockerDigest    string             `json:"docker_digest"`
	APILevel        int                `json:"api_level"`
	ABI             string             `json:"abi"`
	Resolution      string             `json:"resolution"`
	ResourceConfig  map[string]any     `json:"resource_config"`
	Status          domain.ImageStatus `json:"status"`
	ValidationError *string            `json:"validation_error,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type Host struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	HostType        string            `json:"host_type"`
	Address         string            `json:"address,omitempty"`
	Capabilities    map[string]any    `json:"capabilities"`
	Capacity        map[string]any    `json:"capacity"`
	UsedCapacity    map[string]any    `json:"used_capacity"`
	Status          domain.HostStatus `json:"status"`
	Draining        bool              `json:"draining"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type Pool struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	DefaultLeaseSeconds int               `json:"default_lease_seconds"`
	MaxLeaseSeconds     int               `json:"max_lease_seconds"`
	MaxConcurrency      int               `json:"max_concurrency"`
	Status              domain.PoolStatus `json:"status"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type PoolImage struct {
	PoolID       string    `json:"pool_id"`
	ImageID      string    `json:"image_id"`
	MinReady     int       `json:"min_ready"`
	MaxInstances int       `json:"max_instances"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Device struct {
	ID                  string                       `json:"id"`
	HostID              string                       `json:"host_id"`
	ImageID             *string                      `json:"image_id,omitempty"`
	DeviceKind          string                       `json:"device_kind"`
	ProviderType        string                       `json:"provider_type"`
	ProviderRef         string                       `json:"provider_ref"`
	LifecycleMode       string                       `json:"lifecycle_mode"`
	Serial              string                       `json:"serial"`
	STFSerial           *string                      `json:"stf_serial,omitempty"`
	ADBEndpoint         *string                      `json:"adb_endpoint,omitempty"`
	AppiumEndpoint      *string                      `json:"appium_endpoint,omitempty"`
	Capabilities        map[string]any               `json:"capabilities"`
	LifecycleStatus     domain.DeviceLifecycleStatus `json:"lifecycle_status"`
	HealthStatus        domain.HealthStatus          `json:"health_status"`
	HealthReason        *string                      `json:"health_reason,omitempty"`
	ConsecutiveFailures int                          `json:"consecutive_failures"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

type ImageInput struct {
	Name           string         `json:"name"`
	DockerImage    string         `json:"docker_image,omitempty"`
	DockerDigest   string         `json:"docker_digest"`
	APILevel       int            `json:"api_level"`
	ABI            string         `json:"abi"`
	Resolution     string         `json:"resolution"`
	ResourceConfig map[string]any `json:"resource_config"`
	Enabled        *bool          `json:"enabled,omitempty"`
}

type HostInput struct {
	Name         string         `json:"name"`
	HostType     string         `json:"host_type"`
	Address      string         `json:"address"`
	Capabilities map[string]any `json:"capabilities"`
	Capacity     map[string]any `json:"capacity"`
}

type PoolInput struct {
	Name                string `json:"name"`
	DefaultLeaseSeconds int    `json:"default_lease_seconds"`
	MaxLeaseSeconds     int    `json:"max_lease_seconds"`
	MaxConcurrency      int    `json:"max_concurrency"`
	Enabled             *bool  `json:"enabled,omitempty"`
}

type PoolImageInput struct {
	MinReady     int    `json:"min_ready"`
	MaxInstances int    `json:"max_instances"`
	Enabled      *bool  `json:"enabled"`
	Reason       string `json:"reason,omitempty"`
}

type ProvisionMockDeviceInput struct {
	ID           string
	HostID       string
	ImageID      string
	ProviderRef  string
	Capabilities map[string]any
}

type Store interface {
	CreateImage(context.Context, Idempotency, Image) (Image, error)
	ListImages(context.Context, paging.Page) ([]Image, int, error)
	GetImage(context.Context, string) (Image, error)
	UpdateImage(context.Context, Image, domain.ImageStatus) (Image, error)

	CreateHost(context.Context, Idempotency, Host) (Host, error)
	ListHosts(context.Context, paging.Page) ([]Host, int, error)
	GetHost(context.Context, string) (Host, error)
	UpdateHost(context.Context, Host, domain.HostStatus) (Host, error)

	CreatePool(context.Context, Idempotency, Pool) (Pool, error)
	ListPools(context.Context, paging.Page) ([]Pool, int, error)
	GetPool(context.Context, string) (Pool, error)
	UpdatePool(context.Context, Pool, domain.PoolStatus) (Pool, error)
	ListPoolImages(context.Context, string, paging.Page) ([]PoolImage, int, error)
	GetPoolImage(context.Context, string, string) (PoolImage, error)
	SetPoolImage(context.Context, PoolImage, DeviceAudit) (PoolImage, error)
	DisablePoolImage(context.Context, string, string) (PoolImage, error)
	AddDeviceToPool(context.Context, string, string) error
	RemoveDeviceFromPool(context.Context, string, string) error

	CreateDevice(context.Context, Device) (Device, error)
	ListDevices(context.Context, paging.Page) ([]Device, int, error)
	ListSchedulableDevices(context.Context, string) ([]Device, error)
	GetDevice(context.Context, string) (Device, error)
	UpdateDeviceState(context.Context, Device, domain.DeviceLifecycleStatus, domain.HealthStatus, DeviceAudit) (Device, error)
	ReplayDeviceOperation(context.Context, string, string, string, string, string) (Device, bool, error)
	QueueDeviceOperation(context.Context, DeviceOperation) (Device, error)
}
