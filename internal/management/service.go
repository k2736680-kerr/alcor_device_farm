package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
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
	image := Image{ID: id, Name: input.Name, DockerImage: strings.TrimSpace(input.DockerImage), DockerDigest: input.DockerDigest, APILevel: input.APILevel,
		ABI: input.ABI, Resolution: input.Resolution, ResourceConfig: cloneMap(input.ResourceConfig), Status: status}
	if image.DockerImage == "" {
		reason := "IMAGE_REFERENCE_REQUIRED"
		image.ValidationError = &reason
	}
	meta, err := idempotency(clientID, "create_device_image", key, "device_image", id, input, 201)
	if err != nil {
		return Image{}, err
	}
	return service.store.CreateImage(ctx, meta, image)
}

func (service *Service) ListImages(ctx context.Context, page paging.Page, status *domain.ImageStatus) (paging.Result[Image], error) {
	items, total, err := service.store.ListImages(ctx, page, status)
	if err != nil {
		return paging.Result[Image]{}, err
	}
	return paging.NewResult(items, page, total), nil
}

func (service *Service) RetireImage(ctx context.Context, id, reason string, actor audit.Actor, requestID string) (Image, error) {
	if strings.TrimSpace(id) == "" || !validReason(reason) {
		return Image{}, ErrInvalidArgument
	}
	current, err := service.store.GetImage(ctx, id)
	if err != nil {
		return Image{}, err
	}
	if current.Status == domain.ImageDisabled || current.Status == domain.ImageValidating {
		return Image{}, ErrConflict
	}
	aggregate, err := domain.RestoreImage(current.ID, current.Status)
	if err != nil {
		return Image{}, err
	}
	from := aggregate.Status()
	if err := aggregate.Transition(domain.ImageDisabled, "image retired", time.Now().UTC()); err != nil {
		return Image{}, err
	}
	event, err := service.deviceAudit(actor, requestID, "retire_device_image", strings.TrimSpace(reason))
	if err != nil {
		return Image{}, err
	}
	event.DestructiveApproved = true
	current.Status = aggregate.Status()
	return service.store.RetireImage(ctx, current, from, event)
}
func (service *Service) GetImage(ctx context.Context, id string) (Image, error) {
	return service.store.GetImage(ctx, id)
}

func (service *Service) UpdateImage(ctx context.Context, id string, input ImageInput) (Image, error) {
	if err := validateImageInput(input); err != nil {
		return Image{}, err
	}
	if input.Enabled != nil && !*input.Enabled {
		return Image{}, ErrInvalidArgument
	}
	current, err := service.store.GetImage(ctx, id)
	if err != nil {
		return Image{}, err
	}
	from, target := current.Status, current.Status
	dockerImage := strings.TrimSpace(input.DockerImage)
	if dockerImage == "" {
		dockerImage = current.DockerImage
	}
	definitionChanged := dockerImage != current.DockerImage || input.DockerDigest != current.DockerDigest ||
		input.APILevel != current.APILevel || input.ABI != current.ABI || input.Resolution != current.Resolution ||
		!reflect.DeepEqual(input.ResourceConfig, current.ResourceConfig)
	if definitionChanged && current.Status != domain.ImageDraft && current.Status != domain.ImageDisabled {
		target = domain.ImageDraft
	}
	if input.Enabled != nil {
		if *input.Enabled && current.Status == domain.ImageDisabled {
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
	current.Name, current.DockerImage, current.DockerDigest, current.APILevel = input.Name, dockerImage, input.DockerDigest, input.APILevel
	current.ABI, current.Resolution, current.ResourceConfig, current.Status = input.ABI, input.Resolution, cloneMap(input.ResourceConfig), target
	if current.DockerImage == "" {
		reason := "IMAGE_REFERENCE_REQUIRED"
		current.ValidationError = &reason
	} else if definitionChanged {
		reason := "IMAGE_REVALIDATION_REQUIRED"
		current.ValidationError = &reason
	} else if current.ValidationError != nil && *current.ValidationError == "IMAGE_REFERENCE_REQUIRED" {
		current.ValidationError = nil
	}
	return service.store.UpdateImage(ctx, current, from)
}

func (service *Service) StartImageValidation(ctx context.Context, id string) (Image, error) {
	current, err := service.store.GetImage(ctx, id)
	if err != nil {
		return Image{}, err
	}
	if !providers.ValidRuntimeImageReference(current.DockerImage) {
		return Image{}, ErrImageUnavailable
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
	input.HostOS = normalizedHostOS(input.HostOS)
	input.HostArch = normalizedHostArch(input.HostArch)
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HostType) == "" ||
		!validHostType(input.HostType) || input.HostOS == "" || input.HostArch == "" ||
		(input.HostType == "appium_device_farm_ios" && input.HostOS != "macos") ||
		sensitive.ContainsMap(input.Capabilities) || sensitive.ContainsMap(input.Capacity) {
		return Host{}, ErrInvalidArgument
	}
	id, err := service.newID()
	if err != nil {
		return Host{}, err
	}
	host := Host{ID: id, Name: input.Name, HostType: input.HostType, HostOS: input.HostOS, HostArch: input.HostArch, Address: input.Address,
		Capabilities: cloneMap(input.Capabilities), Capacity: cloneMap(input.Capacity), UsedCapacity: map[string]any{}, Status: domain.HostOffline}
	meta, err := idempotency(clientID, "create_device_host", key, "device_host", id, input, 201)
	if err != nil {
		return Host{}, err
	}
	return service.store.CreateHost(ctx, meta, host)
}

func (service *Service) ListHosts(ctx context.Context, page paging.Page) (paging.Result[Host], error) {
	items, total, err := service.store.ListHosts(ctx, page)
	if err != nil {
		return paging.Result[Host]{}, err
	}
	return paging.NewResult(items, page, total), nil
}
func (service *Service) GetHost(ctx context.Context, id string) (Host, error) {
	return service.store.GetHost(ctx, id)
}

func (service *Service) UpdateHost(ctx context.Context, id string, input HostInput) (Host, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HostType) == "" ||
		!validHostType(input.HostType) ||
		sensitive.ContainsMap(input.Capabilities) || sensitive.ContainsMap(input.Capacity) {
		return Host{}, ErrInvalidArgument
	}
	current, err := service.store.GetHost(ctx, id)
	if err != nil {
		return Host{}, err
	}
	if strings.TrimSpace(input.HostOS) == "" {
		input.HostOS = current.HostOS
	} else {
		input.HostOS = normalizedHostOS(input.HostOS)
	}
	if strings.TrimSpace(input.HostArch) == "" {
		input.HostArch = current.HostArch
	} else {
		input.HostArch = normalizedHostArch(input.HostArch)
	}
	if input.HostOS == "" || input.HostArch == "" ||
		(input.HostType == "appium_device_farm_ios" && input.HostOS != "macos") {
		return Host{}, ErrInvalidArgument
	}
	current.Name, current.HostType, current.HostOS, current.HostArch, current.Address = input.Name, input.HostType, input.HostOS, input.HostArch, input.Address
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
	if strings.TrimSpace(input.Platform) == "" {
		input.Platform = "android"
	} else {
		input.Platform = normalizedPlatform(input.Platform)
	}
	totalTarget, minReady := input.MaxConcurrency, input.MaxConcurrency
	if input.TotalTarget != nil {
		totalTarget = *input.TotalTarget
	}
	if input.MinReady != nil {
		minReady = *input.MinReady
	}
	if err := validatePoolInput(input, totalTarget, minReady); err != nil {
		return Pool{}, err
	}
	var defaultImageID *string
	if input.DefaultImageID != nil && strings.TrimSpace(*input.DefaultImageID) != "" {
		if input.Platform != "android" {
			return Pool{}, ErrInvalidArgument
		}
		imageID := strings.TrimSpace(*input.DefaultImageID)
		image, err := service.store.GetImage(ctx, imageID)
		if err != nil || image.Status != domain.ImageReady {
			return Pool{}, ErrInvalidArgument
		}
		defaultImageID = &imageID
	}
	id, err := service.newID()
	if err != nil {
		return Pool{}, err
	}
	status := domain.PoolActive
	if input.Enabled != nil && !*input.Enabled {
		status = domain.PoolDisabled
	}
	pool := Pool{ID: id, Name: input.Name, Platform: input.Platform, DefaultLeaseSeconds: input.DefaultLeaseSeconds,
		MaxLeaseSeconds: input.MaxLeaseSeconds, MaxConcurrency: input.MaxConcurrency,
		TotalTarget: totalTarget, MinReady: minReady, DefaultImageID: defaultImageID, Status: status}
	meta, err := idempotency(clientID, "create_device_pool", key, "device_pool", id, input, 201)
	if err != nil {
		return Pool{}, err
	}
	return service.store.CreatePool(ctx, meta, pool)
}

func (service *Service) ListPools(ctx context.Context, page paging.Page) (paging.Result[Pool], error) {
	items, total, err := service.store.ListPools(ctx, page)
	if err != nil {
		return paging.Result[Pool]{}, err
	}
	return paging.NewResult(items, page, total), nil
}
func (service *Service) GetPool(ctx context.Context, id string) (Pool, error) {
	return service.store.GetPool(ctx, id)
}

func (service *Service) UpdatePool(ctx context.Context, id string, input PoolInput, actor audit.Actor, requestID string) (Pool, error) {
	current, err := service.store.GetPool(ctx, id)
	if err != nil {
		return Pool{}, err
	}
	oldTotalTarget := current.TotalTarget
	if strings.TrimSpace(input.Platform) == "" {
		input.Platform = current.Platform
	} else {
		input.Platform = normalizedPlatform(input.Platform)
	}
	totalTarget, minReady := current.TotalTarget, current.MinReady
	if input.TotalTarget != nil {
		totalTarget = *input.TotalTarget
	}
	if input.MinReady != nil {
		minReady = *input.MinReady
	}
	if err := validatePoolInput(input, totalTarget, minReady); err != nil {
		return Pool{}, err
	}
	if totalTarget < current.TotalTarget && !validReason(input.Reason) {
		return Pool{}, ErrInvalidArgument
	}
	defaultImageID := current.DefaultImageID
	if input.DefaultImageID != nil {
		if input.Platform != "android" {
			return Pool{}, ErrInvalidArgument
		}
		imageID := strings.TrimSpace(*input.DefaultImageID)
		if imageID == "" {
			return Pool{}, ErrInvalidArgument
		}
		image, imageErr := service.store.GetImage(ctx, imageID)
		if imageErr != nil || image.Status != domain.ImageReady {
			return Pool{}, ErrInvalidArgument
		}
		membership, membershipErr := service.store.GetPoolImage(ctx, id, imageID)
		if membershipErr != nil || !membership.Enabled {
			return Pool{}, ErrInvalidArgument
		}
		defaultImageID = &imageID
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
	current.Platform = input.Platform
	current.MaxLeaseSeconds, current.MaxConcurrency = input.MaxLeaseSeconds, input.MaxConcurrency
	current.TotalTarget, current.MinReady, current.DefaultImageID = totalTarget, minReady, defaultImageID
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "pool capacity configuration updated"
	}
	event, err := service.deviceAudit(actor, requestID, "update_device_pool_capacity", reason)
	if err != nil {
		return Pool{}, err
	}
	event.DestructiveApproved = totalTarget >= oldTotalTarget || validReason(input.Reason)
	return service.store.UpdatePool(ctx, current, from, event)
}

func (service *Service) ListPoolImages(ctx context.Context, poolID string, page paging.Page) (paging.Result[PoolImage], error) {
	if _, err := service.store.GetPool(ctx, poolID); err != nil {
		return paging.Result[PoolImage]{}, err
	}
	items, total, err := service.store.ListPoolImages(ctx, poolID, page)
	if err != nil {
		return paging.Result[PoolImage]{}, err
	}
	return paging.NewResult(items, page, total), nil
}

func (service *Service) SetPoolImage(
	ctx context.Context,
	poolID, imageID string,
	input PoolImageInput,
	actor audit.Actor,
	requestID string,
) (PoolImage, error) {
	if strings.TrimSpace(poolID) == "" || strings.TrimSpace(imageID) == "" || input.Enabled == nil || input.MinReady < 0 ||
		input.MaxInstances < 1 || input.MinReady != input.MaxInstances {
		return PoolImage{}, ErrInvalidArgument
	}
	if _, err := service.store.GetPool(ctx, poolID); err != nil {
		return PoolImage{}, err
	}
	if _, err := service.store.GetImage(ctx, imageID); err != nil {
		return PoolImage{}, err
	}
	current, err := service.store.GetPoolImage(ctx, poolID, imageID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return PoolImage{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if err == nil && input.MaxInstances < current.MaxInstances && !validReason(reason) {
		return PoolImage{}, ErrInvalidArgument
	}
	destructiveApproved := validReason(reason)
	if reason == "" {
		reason = "fixed emulator target updated"
	}
	event, err := service.deviceAudit(actor, requestID, "set_device_pool_target", reason)
	if err != nil {
		return PoolImage{}, err
	}
	event.DestructiveApproved = destructiveApproved
	return service.store.SetPoolImage(ctx, PoolImage{PoolID: poolID, ImageID: imageID,
		MinReady: input.MinReady, MaxInstances: input.MaxInstances, Enabled: *input.Enabled}, event)
}

func (service *Service) DisablePoolImage(ctx context.Context, poolID, imageID string) (PoolImage, error) {
	if strings.TrimSpace(poolID) == "" || strings.TrimSpace(imageID) == "" {
		return PoolImage{}, ErrInvalidArgument
	}
	pool, err := service.store.GetPool(ctx, poolID)
	if err != nil {
		return PoolImage{}, err
	}
	if pool.DefaultImageID != nil && *pool.DefaultImageID == imageID {
		return PoolImage{}, ErrConflict
	}
	return service.store.DisablePoolImage(ctx, poolID, imageID)
}

func (service *Service) SelectPoolDefaultImage(
	ctx context.Context,
	poolID, imageID, reason string,
	actor audit.Actor,
	requestID string,
) (Pool, error) {
	if strings.TrimSpace(poolID) == "" || strings.TrimSpace(imageID) == "" || !validReason(reason) {
		return Pool{}, ErrInvalidArgument
	}
	image, err := service.store.GetImage(ctx, imageID)
	if err != nil {
		return Pool{}, err
	}
	if image.Status != domain.ImageReady || !providers.ValidRuntimeImageReference(image.DockerImage) {
		return Pool{}, ErrImageUnavailable
	}
	if _, err := service.store.GetPool(ctx, poolID); err != nil {
		return Pool{}, err
	}
	event, err := service.deviceAudit(actor, requestID, "select_pool_default_image", strings.TrimSpace(reason))
	if err != nil {
		return Pool{}, err
	}
	return service.store.SelectPoolDefaultImage(ctx, poolID, imageID, event)
}

// SetPoolBaseDevice selects the long-lived device whose effective image,
// hardware profile and runtime profile are copied for future scale-out.
func (service *Service) SetPoolBaseDevice(ctx context.Context, poolID, deviceID, reason string, actor audit.Actor, requestID string) (Pool, error) {
	if !validReason(reason) || strings.TrimSpace(deviceID) == "" {
		return Pool{}, ErrInvalidArgument
	}
	event, err := service.deviceAudit(actor, requestID, "set_pool_base_device", reason)
	if err != nil {
		return Pool{}, err
	}
	return service.store.SetPoolBaseDevice(ctx, poolID, strings.TrimSpace(deviceID), event)
}

func (service *Service) AddDeviceToPool(ctx context.Context, poolID, deviceID string) error {
	pool, err := service.store.GetPool(ctx, poolID)
	if err != nil {
		return err
	}
	if pool.Status != domain.PoolActive {
		return ErrConflict
	}
	device, err := service.store.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if pool.Platform != device.Platform {
		return ErrInvalidArgument
	}
	return service.store.AddDeviceToPool(ctx, poolID, deviceID)
}
func (service *Service) RemoveDeviceFromPool(ctx context.Context, poolID, deviceID string) error {
	return service.store.RemoveDeviceFromPool(ctx, poolID, deviceID)
}
func (service *Service) ListDevices(ctx context.Context, page paging.Page, filter DeviceFilter) (paging.Result[Device], error) {
	items, total, err := service.store.ListDevices(ctx, page, filter)
	if err != nil {
		return paging.Result[Device]{}, err
	}
	return paging.NewResult(items, page, total), nil
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
	if strings.TrimSpace(input.Platform) == "" {
		input.Platform = "android"
	} else {
		input.Platform = normalizedPlatform(input.Platform)
	}
	if input.Platform == "" {
		return Device{}, ErrInvalidArgument
	}
	if input.DeviceKind == "" {
		input.DeviceKind = "emulator"
		if input.Platform == "ios" {
			input.DeviceKind = "simulator"
		}
	}
	var image *Image
	if input.Platform == "android" {
		value, imageErr := service.store.GetImage(ctx, input.ImageID)
		if imageErr != nil {
			return Device{}, imageErr
		}
		if value.Status != domain.ImageReady {
			return Device{}, ErrImageUnavailable
		}
		image = &value
	} else if input.ImageID != "" || (input.DeviceKind != "simulator" && input.DeviceKind != "physical") {
		return Device{}, ErrInvalidArgument
	}
	capabilities := cloneMap(input.Capabilities)
	capabilities["platformName"] = canonicalPlatformName(input.Platform)
	request := providers.CreateRequest{
		DeviceID: input.ID, HostID: input.HostID, ImageID: input.ImageID, Platform: providers.Platform(input.Platform), DeviceKind: input.DeviceKind,
		ProviderRef: input.ProviderRef, Capabilities: capabilities,
	}
	if image != nil {
		request.RuntimeImage = image.DockerImage
		request.RuntimeProfile = mustRuntimeProfile(image.ResourceConfig)
	}
	snapshot, err := service.provider.Create(ctx, providers.CreateRequest{
		DeviceID: request.DeviceID, HostID: request.HostID, ImageID: request.ImageID, Platform: request.Platform, DeviceKind: request.DeviceKind,
		RuntimeImage: request.RuntimeImage, ProviderRef: request.ProviderRef, Capabilities: request.Capabilities, RuntimeProfile: request.RuntimeProfile,
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
	var imageID *string
	if input.ImageID != "" {
		value := input.ImageID
		imageID = &value
	}
	device := Device{ID: input.ID, HostID: input.HostID, Platform: input.Platform, ImageID: imageID, DeviceKind: input.DeviceKind,
		ProviderType: "mock", ProviderRef: input.ProviderRef, LifecycleMode: "rebuild", Serial: snapshot.Connection.Serial,
		ADBEndpoint: stringPointer(snapshot.Connection.ADBEndpoint), AppiumEndpoint: stringPointer(snapshot.Connection.AppiumEndpoint), Capabilities: capabilities,
		LifecycleStatus: domain.DeviceReady, HealthStatus: domain.HealthHealthy}
	created, err := service.store.CreateDevice(ctx, device)
	if err != nil {
		_ = service.provider.Delete(context.Background(), input.ProviderRef)
	}
	return created, err
}

func (service *Service) QuarantineDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "quarantine_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.transitionDevice(ctx, id, domain.DeviceQuarantined, domain.HealthUnhealthy, reason, event)
}

func (service *Service) UnquarantineDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "unquarantine_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.transitionDevice(ctx, id, domain.DeviceProvisioning, domain.HealthUnknown, reason, event)
}

func (service *Service) RestartDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID, idempotencyKey string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "restart_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.restartDevice(ctx, id, reason, idempotencyKey, event)
}

func (service *Service) StartDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID, idempotencyKey string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "start_ios_simulator", reason)
	if err != nil {
		return Device{}, err
	}
	return service.iosSimulatorLifecycle(ctx, id, reason, idempotencyKey, "start", event)
}

func (service *Service) StopDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID, idempotencyKey string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "stop_ios_simulator", reason)
	if err != nil {
		return Device{}, err
	}
	return service.iosSimulatorLifecycle(ctx, id, reason, idempotencyKey, "stop", event)
}

func (service *Service) iosSimulatorLifecycle(ctx context.Context, id, reason, idempotencyKey, operation string, event DeviceAudit) (Device, error) {
	if strings.TrimSpace(reason) == "" || len(strings.TrimSpace(idempotencyKey)) < 8 || (operation != "start" && operation != "stop") {
		return Device{}, ErrInvalidArgument
	}
	current, err := service.store.GetDevice(ctx, id)
	if err != nil {
		return Device{}, err
	}
	if current.Platform != "ios" || current.DeviceKind != "simulator" || current.ProviderType != "appium_device_farm_ios" {
		return Device{}, ErrInvalidArgument
	}
	allowlisted, _ := current.Capabilities["allowlisted"].(bool)
	if !allowlisted {
		return Device{}, ErrInvalidArgument
	}
	wantFrom, target := domain.DeviceStopped, domain.DeviceBooting
	if operation == "stop" {
		wantFrom, target = domain.DeviceReady, domain.DeviceStopped
	}
	commandKey := operationCommandKey(operation, event.ActorID, idempotencyKey)
	requestHash := operationRequestHash(operation, current.ID, reason)
	if replayed, found, replayErr := service.store.ReplayDeviceOperation(ctx, current.ID, current.HostID, commandKey, operation, requestHash); replayErr != nil {
		return Device{}, replayErr
	} else if found {
		return replayed, nil
	}
	if current.LifecycleStatus != wantFrom {
		return Device{}, &domain.TransitionError{Resource: "device", ID: id, Field: "lifecycle_status", From: string(current.LifecycleStatus), To: string(target)}
	}
	oldLifecycle, oldHealth := current.LifecycleStatus, current.HealthStatus
	aggregate, err := domain.RestoreDevice(current.ID, current.LifecycleStatus, current.HealthStatus)
	if err != nil {
		return Device{}, err
	}
	now := time.Now().UTC()
	if aggregate.Health() != domain.HealthUnknown {
		if err := aggregate.UpdateHealth(domain.HealthUnknown, reason, now); err != nil {
			return Device{}, err
		}
	}
	if err := aggregate.Transition(target, reason, now); err != nil {
		return Device{}, err
	}
	current.LifecycleStatus, current.HealthStatus = aggregate.Lifecycle(), aggregate.Health()
	current.HealthReason = stringPointer(reason)
	commandID, err := service.newID()
	if err != nil {
		return Device{}, err
	}
	return service.store.QueueDeviceOperation(ctx, DeviceOperation{
		CommandID: commandID, CommandType: operation, IdempotencyKey: commandKey, MaxAttempts: 3,
		Payload: map[string]any{"operation_source": "management", "operation_state": current.LifecycleStatus,
			"request_hash": requestHash, "device_id": current.ID, "host_id": current.HostID, "provider_ref": current.ProviderRef},
		Device: current, ExpectedLifecycle: oldLifecycle, ExpectedHealth: oldHealth, Audit: event,
		RequireNoActiveReservation: true, RequireNoActiveCommand: true,
	})
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

func (service *Service) RebuildDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID, idempotencyKey string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "rebuild_device", reason)
	if err != nil {
		return Device{}, err
	}
	return service.rebuildDevice(ctx, id, reason, idempotencyKey, event)
}

func (service *Service) ReimageDeviceAudited(ctx context.Context, id string, input DeviceReimageInput, actor audit.Actor, requestID, idempotencyKey string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "reimage_device", input.Reason)
	if err != nil {
		return Device{}, err
	}
	if strings.TrimSpace(input.ImageID) == "" || len(strings.TrimSpace(idempotencyKey)) < 8 {
		return Device{}, ErrInvalidArgument
	}
	targetProfile, err := runtimeprofile.Parse(input.RuntimeProfile)
	if err != nil {
		return Device{}, ErrInvalidArgument
	}
	current, err := service.store.GetDevice(ctx, id)
	if err != nil {
		return Device{}, err
	}
	requestHash := operationStructuredRequestHash("reimage", current.ID, input)
	commandKey := operationCommandKey("reimage", event.ActorID, idempotencyKey)
	if replayed, found, replayErr := service.store.ReplayDeviceOperation(ctx, current.ID, current.HostID, commandKey, "rebuild", requestHash); replayErr != nil {
		return Device{}, replayErr
	} else if found {
		return replayed, nil
	}
	if current.DeviceKind != "emulator" || current.ProviderType != "docker_emulator" {
		return Device{}, ErrInvalidArgument
	}
	if current.LifecycleStatus != domain.DeviceReady && current.LifecycleStatus != domain.DeviceStopped && current.LifecycleStatus != domain.DeviceQuarantined {
		return Device{}, &domain.TransitionError{Resource: "device", ID: id, Field: "lifecycle_status", From: string(current.LifecycleStatus), To: string(domain.DeviceProvisioning)}
	}
	targetImage, err := service.store.GetImage(ctx, strings.TrimSpace(input.ImageID))
	if err != nil {
		return Device{}, err
	}
	if targetImage.Status != domain.ImageReady || !providers.ValidRuntimeImageReference(targetImage.DockerImage) {
		return Device{}, ErrImageUnavailable
	}
	currentProfile, err := runtimeprofile.Parse(current.EffectiveRuntimeProfile)
	if err != nil {
		return Device{}, ErrInvalidArgument
	}
	capacityResult, err := service.store.CheckDeviceReimageCapacity(ctx, current, currentProfile, targetProfile, targetImage.ID)
	if err != nil {
		return Device{}, err
	}
	if !capacityResult.Fits {
		return Device{}, &CapacityError{Result: capacityResult}
	}
	if current.ImageID == nil {
		return Device{}, ErrInvalidArgument
	}
	oldImage, err := service.store.GetImage(ctx, *current.ImageID)
	if err != nil {
		return Device{}, err
	}
	oldLifecycle, oldHealth := current.LifecycleStatus, current.HealthStatus
	aggregate, err := domain.RestoreDevice(current.ID, current.LifecycleStatus, current.HealthStatus)
	if err != nil {
		return Device{}, err
	}
	if aggregate.Health() != domain.HealthUnknown {
		if err := aggregate.UpdateHealth(domain.HealthUnknown, input.Reason, time.Now().UTC()); err != nil {
			return Device{}, err
		}
	}
	if err := aggregate.Transition(domain.DeviceProvisioning, input.Reason, time.Now().UTC()); err != nil {
		return Device{}, err
	}
	current.LifecycleStatus, current.HealthStatus = aggregate.Lifecycle(), aggregate.Health()
	commandID, err := service.newID()
	if err != nil {
		return Device{}, err
	}
	payload := map[string]any{
		"operation_source": "management", "operation_kind": "reimage", "operation_state": current.LifecycleStatus,
		"request_hash": requestHash, "device_id": current.ID, "host_id": current.HostID, "provider_ref": current.ProviderRef,
		"image_id": targetImage.ID, "docker_image": targetImage.DockerImage, "docker_digest": targetImage.DockerDigest,
		"capabilities": cloneMap(current.Capabilities), "runtime_profile": targetProfile.Map(),
		"rollback": map[string]any{"image_id": oldImage.ID, "docker_image": oldImage.DockerImage,
			"docker_digest": oldImage.DockerDigest, "runtime_profile": currentProfile.Map()},
	}
	return service.store.QueueDeviceOperation(ctx, DeviceOperation{
		CommandID: commandID, CommandType: "rebuild", IdempotencyKey: commandKey, MaxAttempts: 1,
		Payload: payload, Device: current, ExpectedLifecycle: oldLifecycle, ExpectedHealth: oldHealth, Audit: event,
		RequireNoActiveReservation: true, RequireNoActiveCommand: true, Reimage: true,
		PendingImageID: targetImage.ID, PendingRuntimeProfile: targetProfile.Map(),
	})
}

func (service *Service) DeleteDeviceAudited(ctx context.Context, id, reason string, actor audit.Actor, requestID, idempotencyKey string) (Device, error) {
	event, err := service.deviceAudit(actor, requestID, "delete_device", reason)
	if err != nil {
		return Device{}, err
	}
	event.DestructiveApproved = true
	return service.deleteDevice(ctx, id, reason, idempotencyKey, event)
}

func (service *Service) deleteDevice(ctx context.Context, id, reason, idempotencyKey string, audit DeviceAudit) (Device, error) {
	if strings.TrimSpace(reason) == "" || len(strings.TrimSpace(idempotencyKey)) < 8 {
		return Device{}, ErrInvalidArgument
	}
	current, err := service.store.GetDevice(ctx, id)
	if err != nil {
		return Device{}, err
	}
	if base, baseErr := service.store.IsDevicePoolBase(ctx, id); baseErr != nil {
		return Device{}, baseErr
	} else if base {
		return Device{}, ErrConflict
	}
	commandKey := operationCommandKey("delete", audit.ActorID, idempotencyKey)
	requestHash := operationRequestHash("delete", current.ID, reason)
	if replayed, found, err := service.store.ReplayDeviceOperation(ctx, current.ID, current.HostID, commandKey, "delete", requestHash); err != nil {
		return Device{}, err
	} else if found {
		return replayed, nil
	}
	// A long-lived device can be removed directly whenever it is not in use.
	// Busy/reserved/recycling devices remain protected by the state check and by
	// the transaction's active-reservation check below.
	if current.LifecycleStatus != domain.DeviceReady && current.LifecycleStatus != domain.DeviceQuarantined && current.LifecycleStatus != domain.DeviceStopped {
		return Device{}, &domain.TransitionError{Resource: "device", ID: id, Field: "lifecycle_status", From: string(current.LifecycleStatus), To: string(domain.DeviceDeleted)}
	}
	oldLifecycle, oldHealth := current.LifecycleStatus, current.HealthStatus
	trimmedReason := strings.TrimSpace(reason)
	current.HealthReason = &trimmedReason
	commandID, err := service.newID()
	if err != nil {
		return Device{}, err
	}
	return service.store.QueueDeviceOperation(ctx, DeviceOperation{
		CommandID: commandID, CommandType: "delete",
		IdempotencyKey: commandKey, MaxAttempts: 3,
		Payload: map[string]any{"operation_source": "management", "operation_state": current.LifecycleStatus,
			"request_hash": requestHash,
			"device_id":    current.ID, "host_id": current.HostID, "provider_ref": current.ProviderRef},
		Device: current, ExpectedLifecycle: oldLifecycle, ExpectedHealth: oldHealth, Audit: audit,
		RequireNoActiveReservation: true, RequireNoActiveCommand: true, DisableMemberships: true, ReducePoolTargets: true,
	})
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
	if current.LifecycleStatus != domain.DeviceReady && current.LifecycleStatus != domain.DeviceStopped && current.LifecycleStatus != domain.DeviceQuarantined {
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
		image, imageErr := service.store.GetImage(ctx, *current.ImageID)
		if imageErr != nil {
			return Device{}, imageErr
		}
		if !providers.ValidRuntimeImageReference(image.DockerImage) {
			return Device{}, ErrImageUnavailable
		}
		payload["image_id"] = *current.ImageID
		payload["docker_image"] = image.DockerImage
		payload["docker_digest"] = image.DockerDigest
		profile, profileErr := runtimeprofile.Parse(current.EffectiveRuntimeProfile)
		if profileErr != nil {
			return Device{}, ErrInvalidArgument
		}
		payload["runtime_profile"] = profile.Map()
	}
	return service.store.QueueDeviceOperation(ctx, DeviceOperation{
		CommandID: commandID, CommandType: "rebuild",
		IdempotencyKey: commandKey, MaxAttempts: 3,
		Payload: payload, Device: current, ExpectedLifecycle: oldLifecycle, ExpectedHealth: oldHealth, Audit: audit,
		RequireNoActiveReservation: true, RequireNoActiveCommand: true,
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

func operationStructuredRequestHash(action, deviceID string, value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(append([]byte(action+"\x00"+strings.TrimSpace(deviceID)+"\x00"), encoded...))
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

func (service *Service) deviceAudit(actor audit.Actor, requestID, action, reason string) (DeviceAudit, error) {
	if !actor.Valid() || !validAuditIdentity(actor.ID) || !validAuditIdentity(requestID) || !validReason(reason) {
		return DeviceAudit{}, ErrInvalidArgument
	}
	id, err := service.newID()
	if err != nil {
		return DeviceAudit{}, err
	}
	return DeviceAudit{ID: id, ActorType: actor.Type, ActorID: strings.TrimSpace(actor.ID), Action: action,
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
		sensitive.ContainsMap(input.ResourceConfig) ||
		(strings.TrimSpace(input.DockerImage) != "" && !providers.ValidRuntimeImageReference(input.DockerImage)) {
		return ErrInvalidArgument
	}
	if _, err := runtimeprofile.Parse(input.ResourceConfig); err != nil {
		return ErrInvalidArgument
	}
	return nil
}

func mustRuntimeProfile(values map[string]any) runtimeprofile.Profile {
	profile, err := runtimeprofile.Parse(values)
	if err != nil {
		return runtimeprofile.Default()
	}
	return profile
}

func validatePoolInput(input PoolInput, totalTarget, minReady int) error {
	if strings.TrimSpace(input.Name) == "" || input.DefaultLeaseSeconds < 60 ||
		input.MaxLeaseSeconds < input.DefaultLeaseSeconds || input.MaxConcurrency < 1 || totalTarget < 0 ||
		minReady < 0 || minReady > totalTarget || normalizedPlatform(input.Platform) == "" {
		return ErrInvalidArgument
	}
	return nil
}

func normalizedPlatform(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "android":
		return "android"
	case "ios":
		return "ios"
	default:
		return ""
	}
}

func canonicalPlatformName(value string) string {
	if value == "ios" {
		return "iOS"
	}
	return "Android"
}

func normalizedHostOS(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "linux"
	}
	switch value {
	case "linux", "macos", "windows":
		return value
	default:
		return ""
	}
}

func normalizedHostArch(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_.-", character) {
			continue
		}
		return ""
	}
	if len(value) > 32 {
		return ""
	}
	return value
}

func validHostType(value string) bool {
	switch strings.TrimSpace(value) {
	case "docker_emulator", "usb_android", "hybrid", "appium_device_farm_ios":
		return true
	default:
		return false
	}
}

func stringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
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
