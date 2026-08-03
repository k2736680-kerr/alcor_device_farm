package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/sensitive"
)

type IDGenerator func() (string, error)

type Service struct {
	store    Store
	provider providers.Provider
	newID    IDGenerator
}

func NewService(store Store, provider providers.Provider, generator IDGenerator) *Service {
	if generator == nil {
		generator = identifier.New
	}
	return &Service{store: store, provider: provider, newID: generator}
}

func (service *Service) CreateImage(ctx context.Context, clientID, key string, input ImageInput) (Image, error) {
	if err := validateImageInput(input); err != nil {
		return Image{}, err
	}
	id, err := service.newID()
	if err != nil {
		return Image{}, err
	}
	status := domain.ImageDraft
	if input.Enabled != nil && !*input.Enabled {
		status = domain.ImageDisabled
	}
	image := Image{ID: id, Name: input.Name, DockerDigest: input.DockerDigest, APILevel: input.APILevel,
		ABI: input.ABI, Resolution: input.Resolution, ResourceConfig: cloneMap(input.ResourceConfig), Status: status}
	meta, err := idempotency(clientID, "create_device_image", key, "device_image", id, input, 201)
	if err != nil {
		return Image{}, err
	}
	return service.store.CreateImage(ctx, meta, image)
}

func (service *Service) ListImages(ctx context.Context) ([]Image, error) {
	return service.store.ListImages(ctx)
}
func (service *Service) GetImage(ctx context.Context, id string) (Image, error) {
	return service.store.GetImage(ctx, id)
}

func (service *Service) UpdateImage(ctx context.Context, id string, input ImageInput) (Image, error) {
	if err := validateImageInput(input); err != nil {
		return Image{}, err
	}
	current, err := service.store.GetImage(ctx, id)
	if err != nil {
		return Image{}, err
	}
	from, target := current.Status, current.Status
	if input.Enabled != nil {
		if !*input.Enabled && current.Status != domain.ImageDisabled {
			target = domain.ImageDisabled
		} else if *input.Enabled && current.Status == domain.ImageDisabled {
			target = domain.ImageDraft
		}
	}
	if target != current.Status {
		aggregate, err := domain.RestoreImage(current.ID, current.Status)
		if err != nil {
			return Image{}, err
		}
		if err := aggregate.Transition(target, "management update", time.Now().UTC()); err != nil {
			return Image{}, err
		}
	}
	current.Name, current.DockerDigest, current.APILevel = input.Name, input.DockerDigest, input.APILevel
	current.ABI, current.Resolution, current.ResourceConfig, current.Status = input.ABI, input.Resolution, cloneMap(input.ResourceConfig), target
	return service.store.UpdateImage(ctx, current, from)
}

func (service *Service) StartImageValidation(ctx context.Context, id string) (Image, error) {
	current, err := service.store.GetImage(ctx, id)
	if err != nil {
		return Image{}, err
	}
	aggregate, err := domain.RestoreImage(current.ID, current.Status)
	if err != nil {
		return Image{}, err
	}
	from := aggregate.Status()
	if err := aggregate.Transition(domain.ImageValidating, "validation requested", time.Now().UTC()); err != nil {
		return Image{}, err
	}
	current.Status = aggregate.Status()
	current.ValidationError = nil
	return service.store.UpdateImage(ctx, current, from)
}

func (service *Service) CreateHost(ctx context.Context, clientID, key string, input HostInput) (Host, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HostType) == "" ||
		sensitive.ContainsMap(input.Capabilities) || sensitive.ContainsMap(input.Capacity) {
		return Host{}, ErrInvalidArgument
	}
	id, err := service.newID()
	if err != nil {
		return Host{}, err
	}
	host := Host{ID: id, Name: input.Name, HostType: input.HostType, Address: input.Address,
		Capabilities: cloneMap(input.Capabilities), Capacity: cloneMap(input.Capacity), UsedCapacity: map[string]any{}, Status: domain.HostOffline}
	meta, err := idempotency(clientID, "create_device_host", key, "device_host", id, input, 201)
	if err != nil {
		return Host{}, err
	}
	return service.store.CreateHost(ctx, meta, host)
}

func (service *Service) ListHosts(ctx context.Context) ([]Host, error) {
	return service.store.ListHosts(ctx)
}
func (service *Service) GetHost(ctx context.Context, id string) (Host, error) {
	return service.store.GetHost(ctx, id)
}

func (service *Service) UpdateHost(ctx context.Context, id string, input HostInput) (Host, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HostType) == "" ||
		sensitive.ContainsMap(input.Capabilities) || sensitive.ContainsMap(input.Capacity) {
		return Host{}, ErrInvalidArgument
	}
	current, err := service.store.GetHost(ctx, id)
	if err != nil {
		return Host{}, err
	}
	current.Name, current.HostType, current.Address = input.Name, input.HostType, input.Address
	current.Capabilities, current.Capacity = cloneMap(input.Capabilities), cloneMap(input.Capacity)
	return service.store.UpdateHost(ctx, current, current.Status)
}

func (service *Service) SetHostDraining(ctx context.Context, id string, draining bool, reason string) (Host, error) {
	if strings.TrimSpace(reason) == "" {
		return Host{}, ErrInvalidArgument
	}
	current, err := service.store.GetHost(ctx, id)
	if err != nil {
		return Host{}, err
	}
	aggregate, err := domain.RestoreHost(current.ID, current.Status)
	if err != nil {
		return Host{}, err
	}
	from := aggregate.Status()
	target := domain.HostDraining
	if !draining {
		target = domain.HostOnline
	}
	if err := aggregate.Transition(target, reason, time.Now().UTC()); err != nil {
		return Host{}, err
	}
	current.Status, current.Draining = aggregate.Status(), draining
	return service.store.UpdateHost(ctx, current, from)
}

func (service *Service) CreatePool(ctx context.Context, clientID, key string, input PoolInput) (Pool, error) {
	if err := validatePoolInput(input); err != nil {
		return Pool{}, err
	}
	id, err := service.newID()
	if err != nil {
		return Pool{}, err
	}
	status := domain.PoolActive
	if input.Enabled != nil && !*input.Enabled {
		status = domain.PoolDisabled
	}
	pool := Pool{ID: id, Name: input.Name, DefaultLeaseSeconds: input.DefaultLeaseSeconds,
		MaxLeaseSeconds: input.MaxLeaseSeconds, MaxConcurrency: input.MaxConcurrency, Status: status}
	meta, err := idempotency(clientID, "create_device_pool", key, "device_pool", id, input, 201)
	if err != nil {
		return Pool{}, err
	}
	return service.store.CreatePool(ctx, meta, pool)
}

func (service *Service) ListPools(ctx context.Context) ([]Pool, error) {
	return service.store.ListPools(ctx)
}
func (service *Service) GetPool(ctx context.Context, id string) (Pool, error) {
	return service.store.GetPool(ctx, id)
}

func (service *Service) UpdatePool(ctx context.Context, id string, input PoolInput) (Pool, error) {
	if err := validatePoolInput(input); err != nil {
		return Pool{}, err
	}
	current, err := service.store.GetPool(ctx, id)
	if err != nil {
		return Pool{}, err
	}
	from := current.Status
	if input.Enabled != nil {
		target := domain.PoolDisabled
		if *input.Enabled {
			target = domain.PoolActive
		}
		if target != current.Status {
			aggregate, err := domain.RestorePool(current.ID, current.Status)
			if err != nil {
				return Pool{}, err
			}
			if err := aggregate.Transition(target, "management update", time.Now().UTC()); err != nil {
				return Pool{}, err
			}
			current.Status = aggregate.Status()
		}
	}
	current.Name, current.DefaultLeaseSeconds = input.Name, input.DefaultLeaseSeconds
	current.MaxLeaseSeconds, current.MaxConcurrency = input.MaxLeaseSeconds, input.MaxConcurrency
	return service.store.UpdatePool(ctx, current, from)
}

func (service *Service) ListPoolImages(ctx context.Context, poolID string) ([]PoolImage, error) {
	if _, err := service.store.GetPool(ctx, poolID); err != nil {
		return nil, err
	}
	return service.store.ListPoolImages(ctx, poolID)
}

func (service *Service) SetPoolImage(ctx context.Context, poolID, imageID string, input PoolImageInput) (PoolImage, error) {
	if strings.TrimSpace(poolID) == "" || strings.TrimSpace(imageID) == "" || input.Enabled == nil || input.MinReady < 0 ||
		input.MaxInstances < 1 || input.MinReady > input.MaxInstances {
		return PoolImage{}, ErrInvalidArgument
	}
	if _, err := service.store.GetPool(ctx, poolID); err != nil {
		return PoolImage{}, err
	}
	if _, err := service.store.GetImage(ctx, imageID); err != nil {
		return PoolImage{}, err
	}
	return service.store.SetPoolImage(ctx, PoolImage{PoolID: poolID, ImageID: imageID,
		MinReady: input.MinReady, MaxInstances: input.MaxInstances, Enabled: *input.Enabled})
}

func (service *Service) DisablePoolImage(ctx context.Context, poolID, imageID string) (PoolImage, error) {
	if strings.TrimSpace(poolID) == "" || strings.TrimSpace(imageID) == "" {
		return PoolImage{}, ErrInvalidArgument
	}
	return service.store.DisablePoolImage(ctx, poolID, imageID)
}

func (service *Service) AddDeviceToPool(ctx context.Context, poolID, deviceID string) error {
	pool, err := service.store.GetPool(ctx, poolID)
	if err != nil {
		return err
	}
	if pool.Status != domain.PoolActive {
		return ErrConflict
	}
	if _, err := service.store.GetDevice(ctx, deviceID); err != nil {
		return err
	}
	return service.store.AddDeviceToPool(ctx, poolID, deviceID)
}
func (service *Service) RemoveDeviceFromPool(ctx context.Context, poolID, deviceID string) error {
	return service.store.RemoveDeviceFromPool(ctx, poolID, deviceID)
}
func (service *Service) ListDevices(ctx context.Context) ([]Device, error) {
	return service.store.ListDevices(ctx)
}
func (service *Service) GetDevice(ctx context.Context, id string) (Device, error) {
	return service.store.GetDevice(ctx, id)
}

func (service *Service) ProvisionMockDevice(ctx context.Context, input ProvisionMockDeviceInput) (Device, error) {
	if service.provider == nil {
		return Device{}, ErrProviderUnavailable
	}
	host, err := service.store.GetHost(ctx, input.HostID)
	if err != nil {
		return Device{}, err
	}
	if host.Status != domain.HostOnline || host.Draining {
		return Device{}, ErrHostUnavailable
	}
	image, err := service.store.GetImage(ctx, input.ImageID)
	if err != nil {
		return Device{}, err
	}
	if image.Status != domain.ImageReady {
		return Device{}, ErrImageUnavailable
	}
	snapshot, err := service.provider.Create(ctx, providers.CreateRequest{
		DeviceID: input.ID, HostID: input.HostID, ImageID: input.ImageID,
		ProviderRef: input.ProviderRef, Capabilities: cloneMap(input.Capabilities),
	})
	if err != nil {
		return Device{}, err
	}
	snapshot, err = service.provider.Start(ctx, input.ProviderRef)
	if err != nil {
		_ = service.provider.Delete(context.Background(), input.ProviderRef)
		return Device{}, err
	}
	health, err := service.provider.InspectHealth(ctx, input.ProviderRef)
	if err != nil || !health.Ready() {
		_ = service.provider.Delete(context.Background(), input.ProviderRef)
		if err != nil {
			return Device{}, err
		}
		return Device{}, ErrConflict
	}
	imageID := input.ImageID
	adb, appium := snapshot.Connection.ADBEndpoint, snapshot.Connection.AppiumEndpoint
	device := Device{ID: input.ID, HostID: input.HostID, ImageID: &imageID, DeviceKind: "emulator",
		ProviderType: "mock", ProviderRef: input.ProviderRef, LifecycleMode: "rebuild", Serial: snapshot.Connection.Serial,
		ADBEndpoint: &adb, AppiumEndpoint: &appium, Capabilities: cloneMap(input.Capabilities),
		LifecycleStatus: domain.DeviceReady, HealthStatus: domain.HealthHealthy}
	created, err := service.store.CreateDevice(ctx, device)
	if err != nil {
		_ = service.provider.Delete(context.Background(), input.ProviderRef)
	}
	return created, err
}

func (service *Service) QuarantineDeviceAudited(ctx context.Context, id, reason, actorID, requestID string) (Device, error) {
	audit, err := service.deviceAudit(actorID, requestID, "quarantine_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.transitionDevice(ctx, id, domain.DeviceQuarantined, domain.HealthUnhealthy, reason, audit)
}

func (service *Service) UnquarantineDeviceAudited(ctx context.Context, id, reason, actorID, requestID string) (Device, error) {
	audit, err := service.deviceAudit(actorID, requestID, "unquarantine_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.transitionDevice(ctx, id, domain.DeviceProvisioning, domain.HealthUnknown, reason, audit)
}

func (service *Service) RestartDeviceAudited(ctx context.Context, id, reason, actorID, requestID, idempotencyKey string) (Device, error) {
	audit, err := service.deviceAudit(actorID, requestID, "restart_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.restartDevice(ctx, id, reason, idempotencyKey, audit)
}

func (service *Service) restartDevice(ctx context.Context, id, reason, idempotencyKey string, audit DeviceAudit) (Device, error) {
	if strings.TrimSpace(reason) == "" || len(strings.TrimSpace(idempotencyKey)) < 8 {
		return Device{}, ErrInvalidArgument
	}
	current, err := service.store.GetDevice(ctx, id)
	if err != nil {
		return Device{}, err
	}
	commandKey := operationCommandKey("restart", audit.ActorID, idempotencyKey)
	requestHash := operationRequestHash("restart", current.ID, reason)
	if replayed, found, err := service.store.ReplayDeviceOperation(ctx, current.ID, current.HostID, commandKey, "restart", requestHash); err != nil {
		return Device{}, err
	} else if found {
		return replayed, nil
	}
	if current.LifecycleStatus != domain.DeviceReady && current.LifecycleStatus != domain.DeviceStopped {
		return Device{}, &domain.TransitionError{Resource: "device", ID: id, Field: "lifecycle_status", From: string(current.LifecycleStatus), To: "restart"}
	}
	oldLifecycle, oldHealth := current.LifecycleStatus, current.HealthStatus
	aggregate, err := domain.RestoreDevice(current.ID, current.LifecycleStatus, current.HealthStatus)
	if err != nil {
		return Device{}, err
	}
	if aggregate.Health() != domain.HealthUnknown {
		if err := aggregate.UpdateHealth(domain.HealthUnknown, reason, time.Now().UTC()); err != nil {
			return Device{}, err
		}
	}
	if current.LifecycleStatus == domain.DeviceStopped {
		if err := aggregate.Transition(domain.DeviceBooting, reason, time.Now().UTC()); err != nil {
			return Device{}, err
		}
	} else if err := aggregate.Transition(domain.DeviceStopped, reason, time.Now().UTC()); err != nil {
		return Device{}, err
	}
	current.LifecycleStatus, current.HealthStatus = aggregate.Lifecycle(), aggregate.Health()
	commandID, err := service.newID()
	if err != nil {
		return Device{}, err
	}
	return service.store.QueueDeviceOperation(ctx, DeviceOperation{
		CommandID: commandID, CommandType: "restart",
		IdempotencyKey: commandKey, MaxAttempts: 3,
		Payload: map[string]any{"operation_source": "management", "operation_state": current.LifecycleStatus,
			"request_hash": requestHash,
			"device_id":    current.ID, "host_id": current.HostID, "provider_ref": current.ProviderRef},
		Device: current, ExpectedLifecycle: oldLifecycle, ExpectedHealth: oldHealth, Audit: audit,
	})
}

func (service *Service) RebuildDeviceAudited(ctx context.Context, id, reason, actorID, requestID, idempotencyKey string) (Device, error) {
	audit, err := service.deviceAudit(actorID, requestID, "rebuild_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.rebuildDevice(ctx, id, reason, idempotencyKey, audit)
}

func (service *Service) rebuildDevice(ctx context.Context, id, reason, idempotencyKey string, audit DeviceAudit) (Device, error) {
	if strings.TrimSpace(reason) == "" || len(strings.TrimSpace(idempotencyKey)) < 8 {
		return Device{}, ErrInvalidArgument
	}
	current, err := service.store.GetDevice(ctx, id)
	if err != nil {
		return Device{}, err
	}
	commandKey := operationCommandKey("rebuild", audit.ActorID, idempotencyKey)
	requestHash := operationRequestHash("rebuild", current.ID, reason)
	if replayed, found, err := service.store.ReplayDeviceOperation(ctx, current.ID, current.HostID, commandKey, "rebuild", requestHash); err != nil {
		return Device{}, err
	} else if found {
		return replayed, nil
	}
	if current.LifecycleStatus != domain.DeviceQuarantined {
		return Device{}, &domain.TransitionError{Resource: "device", ID: id, Field: "lifecycle_status", From: string(current.LifecycleStatus), To: string(domain.DeviceProvisioning)}
	}
	oldLifecycle, oldHealth := current.LifecycleStatus, current.HealthStatus
	aggregate, err := domain.RestoreDevice(current.ID, current.LifecycleStatus, current.HealthStatus)
	if err != nil {
		return Device{}, err
	}
	if aggregate.Health() != domain.HealthUnknown {
		if err := aggregate.UpdateHealth(domain.HealthUnknown, reason, time.Now().UTC()); err != nil {
			return Device{}, err
		}
	}
	if err := aggregate.Transition(domain.DeviceProvisioning, reason, time.Now().UTC()); err != nil {
		return Device{}, err
	}
	current.LifecycleStatus, current.HealthStatus = aggregate.Lifecycle(), aggregate.Health()
	commandID, err := service.newID()
	if err != nil {
		return Device{}, err
	}
	payload := map[string]any{"operation_source": "management", "operation_state": current.LifecycleStatus,
		"request_hash": requestHash,
		"device_id":    current.ID, "host_id": current.HostID, "provider_ref": current.ProviderRef,
		"capabilities": cloneMap(current.Capabilities)}
	if current.ImageID != nil {
		payload["image_id"] = *current.ImageID
	}
	return service.store.QueueDeviceOperation(ctx, DeviceOperation{
		CommandID: commandID, CommandType: "rebuild",
		IdempotencyKey: commandKey, MaxAttempts: 3,
		Payload: payload, Device: current, ExpectedLifecycle: oldLifecycle, ExpectedHealth: oldHealth, Audit: audit,
	})
}

func operationCommandKey(action, actorID, idempotencyKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(actorID) + "\x00" + action + "\x00" + strings.TrimSpace(idempotencyKey)))
	return "management-" + hex.EncodeToString(digest[:16])
}

func operationRequestHash(action, deviceID, reason string) string {
	digest := sha256.Sum256([]byte(action + "\x00" + strings.TrimSpace(deviceID) + "\x00" + strings.TrimSpace(reason)))
	return hex.EncodeToString(digest[:])
}

func (service *Service) transitionDevice(ctx context.Context, id string, target domain.DeviceLifecycleStatus, health domain.HealthStatus, reason string, audit DeviceAudit) (Device, error) {
	if strings.TrimSpace(reason) == "" {
		return Device{}, ErrInvalidArgument
	}
	current, err := service.store.GetDevice(ctx, id)
	if err != nil {
		return Device{}, err
	}
	aggregate, err := domain.RestoreDevice(current.ID, current.LifecycleStatus, current.HealthStatus)
	if err != nil {
		return Device{}, err
	}
	fromLifecycle, fromHealth := aggregate.Lifecycle(), aggregate.Health()
	if err := aggregate.Transition(target, reason, time.Now().UTC()); err != nil {
		return Device{}, err
	}
	if health != aggregate.Health() {
		if err := aggregate.UpdateHealth(health, reason, time.Now().UTC()); err != nil {
			return Device{}, err
		}
	}
	current.LifecycleStatus, current.HealthStatus = aggregate.Lifecycle(), aggregate.Health()
	return service.store.UpdateDeviceState(ctx, current, fromLifecycle, fromHealth, audit)
}

func (service *Service) deviceAudit(actorID, requestID, action, reason string) (DeviceAudit, error) {
	if !validAuditIdentity(actorID) || !validAuditIdentity(requestID) || !validReason(reason) {
		return DeviceAudit{}, ErrInvalidArgument
	}
	id, err := service.newID()
	if err != nil {
		return DeviceAudit{}, err
	}
	return DeviceAudit{ID: id, ActorType: "service", ActorID: strings.TrimSpace(actorID), Action: action,
		RequestID: strings.TrimSpace(requestID), Reason: strings.TrimSpace(reason)}, nil
}

func validAuditIdentity(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 128 || sensitive.Contains(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return len(reason) >= 3 && len(reason) <= 500 && !sensitive.Contains(reason)
}

func validateImageInput(input ImageInput) error {
	if strings.TrimSpace(input.Name) == "" || !strings.HasPrefix(input.DockerDigest, "sha256:") ||
		input.APILevel < 21 || strings.TrimSpace(input.ABI) == "" || strings.TrimSpace(input.Resolution) == "" ||
		sensitive.ContainsMap(input.ResourceConfig) {
		return ErrInvalidArgument
	}
	return nil
}

func validatePoolInput(input PoolInput) error {
	if strings.TrimSpace(input.Name) == "" || input.DefaultLeaseSeconds < 60 ||
		input.MaxLeaseSeconds < input.DefaultLeaseSeconds || input.MaxConcurrency < 1 {
		return ErrInvalidArgument
	}
	return nil
}

func idempotency(clientID, scope, key, resourceType, resourceID string, request any, status int) (Idempotency, error) {
	if strings.TrimSpace(clientID) == "" || len(strings.TrimSpace(key)) < 8 {
		return Idempotency{}, ErrInvalidArgument
	}
	content, err := json.Marshal(request)
	if err != nil {
		return Idempotency{}, err
	}
	hash := sha256.Sum256(content)
	return Idempotency{ClientID: clientID, Scope: scope, Key: key, RequestHash: hex.EncodeToString(hash[:]),
		ResourceType: resourceType, ResourceID: resourceID, ResponseStatus: status}, nil
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
