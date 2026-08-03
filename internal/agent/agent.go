package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

type Client interface {
	Heartbeat(context.Context, string, hostcommand.HeartbeatInput) error
	Claim(context.Context, string, hostcommand.ClaimInput) ([]hostcommand.Command, error)
	Complete(context.Context, string, hostcommand.CompletionInput) error
}

type Config struct {
	HostID            string
	ProviderType      string
	HeartbeatInterval time.Duration
	LeaseSeconds      int
	WaitSeconds       int
	Concurrency       int
	CommandTimeout    time.Duration
	ShutdownTimeout   time.Duration
	Capacity          map[string]any
}

type Agent struct {
	config   Config
	client   Client
	provider providers.Provider
	logger   *slog.Logger
}

func New(config Config, client Client, provider providers.Provider, logger *slog.Logger) (*Agent, error) {
	config.ProviderType = strings.ToLower(strings.TrimSpace(config.ProviderType))
	if len(config.HostID) < 16 || client == nil || provider == nil || config.HeartbeatInterval <= 0 ||
		config.LeaseSeconds < 5 || config.LeaseSeconds > 300 || config.Concurrency < 1 ||
		config.CommandTimeout <= 0 || config.ShutdownTimeout <= 0 || config.ProviderType == "" {
		return nil, errors.New("invalid agent configuration")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{config: config, client: client, provider: provider, logger: logger}, nil
}

func (agent *Agent) Run(ctx context.Context) error {
	if err := agent.sendHeartbeat(ctx); err != nil && ctx.Err() == nil {
		agent.logger.Warn("agent initial heartbeat failed", "error", err)
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	defer stopHeartbeat()
	heartbeatErrors := make(chan error, 1)
	go agent.heartbeatLoop(heartbeatCtx, heartbeatErrors)

	semaphore := make(chan struct{}, agent.config.Concurrency)
	var workers sync.WaitGroup
	for {
		if ctx.Err() != nil {
			stopHeartbeat()
			return waitWorkers(&workers, agent.config.ShutdownTimeout)
		}
		select {
		case err := <-heartbeatErrors:
			if err != nil {
				agent.logger.Warn("agent heartbeat failed", "error", err)
			}
		default:
		}
		if len(semaphore) >= cap(semaphore) {
			select {
			case <-ctx.Done():
				continue
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		commands, err := agent.client.Claim(ctx, agent.config.HostID, hostcommand.ClaimInput{
			LeaseSeconds: agent.config.LeaseSeconds, MaxCommands: cap(semaphore) - len(semaphore), WaitSeconds: agent.config.WaitSeconds,
		})
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			agent.logger.Warn("agent command claim failed", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		for _, command := range commands {
			command := command
			semaphore <- struct{}{}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-semaphore }()
				agent.execute(command)
			}()
		}
	}
}

func (agent *Agent) heartbeatLoop(ctx context.Context, errorsChannel chan<- error) {
	ticker := time.NewTicker(agent.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		err := agent.sendHeartbeat(ctx)
		select {
		case errorsChannel <- err:
		default:
		}
	}
}

func (agent *Agent) sendHeartbeat(ctx context.Context) error {
	snapshots, err := agent.provider.Discover(ctx, agent.config.HostID)
	if err != nil {
		return err
	}
	devices := make([]hostcommand.DiscoveredDevice, 0, len(snapshots))
	for _, snapshot := range snapshots {
		devices = append(devices, hostcommand.DiscoveredDevice{
			ProviderRef: snapshot.ProviderRef, Serial: snapshot.Connection.Serial,
			LifecycleStatus: providerLifecycle(snapshot), HealthStatus: providerHealth(snapshot),
			Connection: map[string]any{"adb_endpoint": snapshot.Connection.ADBEndpoint, "appium_endpoint": snapshot.Connection.AppiumEndpoint,
				"appium_udid": snapshot.Connection.AppiumUDID},
		})
	}
	return agent.client.Heartbeat(ctx, agent.config.HostID, hostcommand.HeartbeatInput{
		AgentTime: time.Now().UTC(), Capacity: agent.config.Capacity,
		Environment: map[string]any{"provider": agent.config.ProviderType}, Devices: devices,
	})
}

func (agent *Agent) execute(command hostcommand.Command) {
	ctx, cancel := context.WithTimeout(context.Background(), agent.config.CommandTimeout)
	defer cancel()
	var err error
	result := map[string]any(nil)
	providerRef, _ := command.Payload["provider_ref"].(string)
	switch command.CommandType {
	case "create":
		var snapshot providers.Snapshot
		created := false
		if digest := stringValue(command.Payload, "docker_digest"); digest != "" {
			verifier, supported := agent.provider.(providers.ImageDigestVerifier)
			if !supported {
				err = &providers.Error{Operation: providers.OperationValidateImage, Code: "IMAGE_VALIDATION_UNSUPPORTED",
					Message: "provider does not support image digest validation", Retryable: false}
			} else {
				err = verifier.VerifyImageDigest(ctx, digest)
			}
		}
		if err == nil {
			snapshot, err = agent.provider.Create(ctx, providers.CreateRequest{
				DeviceID: stringValue(command.Payload, "device_id"), HostID: agent.config.HostID,
				ImageID: stringValue(command.Payload, "image_id"), ProviderRef: providerRef,
				Serial: stringValue(command.Payload, "serial"), Capabilities: mapValue(command.Payload, "capabilities"),
			})
			created = err == nil
		}
		if err == nil {
			snapshot, err = agent.provider.Start(ctx, providerRef)
		}
		if err == nil {
			snapshot, err = agent.waitReady(ctx, snapshot)
		}
		if err == nil {
			result = snapshotResult(snapshot)
		} else if created {
			if cleanupErr := agent.cleanupProvider(providerRef); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("cleanup failed emulator create: %w", cleanupErr))
			}
		}
	case "start":
		var snapshot providers.Snapshot
		snapshot, err = agent.provider.Start(ctx, providerRef)
		if err == nil {
			result = snapshotResult(snapshot)
		}
	case "stop":
		var snapshot providers.Snapshot
		snapshot, err = agent.provider.Stop(ctx, providerRef)
		if err == nil {
			result = snapshotResult(snapshot)
		}
	case "restart":
		var snapshot providers.Snapshot
		snapshot, err = agent.provider.Restart(ctx, providerRef)
		if err == nil {
			snapshot, err = agent.waitReady(ctx, snapshot)
		}
		if err == nil {
			result = snapshotResult(snapshot)
		}
	case "rebuild":
		var snapshot providers.Snapshot
		if stringValue(command.Payload, "device_id") != "" && stringValue(command.Payload, "image_id") != "" {
			snapshot, err = agent.recreate(ctx, command.Payload)
		} else {
			snapshot, err = agent.provider.Rebuild(ctx, providerRef)
			if err == nil {
				snapshot, err = agent.waitReady(ctx, snapshot)
			}
		}
		if err == nil {
			result = snapshotResult(snapshot)
		}
	case "delete":
		err = agent.provider.Delete(ctx, providerRef)
		if err == nil {
			result = map[string]any{"provider_ref": providerRef, "deleted": true}
		}
	case "inspect":
		var health providers.Health
		health, err = agent.provider.InspectHealth(ctx, providerRef)
		if err == nil {
			result = healthResult(health)
			result["provider_ref"] = providerRef
		}
	case "validate_image":
		result, err = agent.validateImage(ctx, command.Payload)
	default:
		err = fmt.Errorf("unsupported command type %s", command.CommandType)
	}
	completion := hostcommand.CompletionInput{Attempt: command.Attempt, Status: "succeeded", Result: result}
	if command.LeaseToken != nil {
		completion.LeaseToken = *command.LeaseToken
	}
	if err != nil {
		completion.Status = "failed"
		completion.Error = &hostcommand.CompletionError{
			Code: providerErrorCode(err), Message: err.Error(), Retryable: providerErrorRetryable(err),
		}
	}
	if completeErr := agent.client.Complete(context.Background(), command.ID, completion); completeErr != nil {
		agent.logger.Error("agent command completion failed", "command_id", command.ID, "error", completeErr)
	}
}

func (agent *Agent) recreate(ctx context.Context, payload map[string]any) (snapshot providers.Snapshot, returnErr error) {
	providerRef := stringValue(payload, "provider_ref")
	if err := agent.provider.Delete(ctx, providerRef); err != nil && providers.ErrorCode(err) != "PROVIDER_DEVICE_NOT_FOUND" {
		return providers.Snapshot{}, err
	}
	created := false
	defer func() {
		if returnErr != nil && created {
			if cleanupErr := agent.cleanupProvider(providerRef); cleanupErr != nil && providers.ErrorCode(cleanupErr) != "PROVIDER_DEVICE_NOT_FOUND" {
				returnErr = errors.Join(returnErr, fmt.Errorf("cleanup failed emulator rebuild: %w", cleanupErr))
			}
		}
	}()
	snapshot, returnErr = agent.provider.Create(ctx, providers.CreateRequest{
		DeviceID: stringValue(payload, "device_id"), HostID: agent.config.HostID,
		ImageID: stringValue(payload, "image_id"), ProviderRef: providerRef,
		Capabilities: mapValue(payload, "capabilities"),
	})
	if returnErr != nil {
		return providers.Snapshot{}, returnErr
	}
	created = true
	snapshot, returnErr = agent.provider.Start(ctx, providerRef)
	if returnErr != nil {
		return providers.Snapshot{}, returnErr
	}
	return agent.waitReady(ctx, snapshot)
}

func (agent *Agent) waitReady(ctx context.Context, snapshot providers.Snapshot) (providers.Snapshot, error) {
	var lastErr error
	for {
		health, err := agent.provider.InspectHealth(ctx, snapshot.ProviderRef)
		if err == nil && health.Ready() {
			connection, connectionErr := agent.provider.GetConnectionInfo(ctx, snapshot.ProviderRef)
			if connectionErr != nil {
				return providers.Snapshot{}, connectionErr
			}
			snapshot.Health, snapshot.Connection = health, connection
			return snapshot, nil
		}
		lastErr = err
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			code, message := "DEVICE_BOOT_TIMEOUT", "emulator did not become ready before command timeout"
			if providers.ErrorCode(lastErr) == "APPIUM_UNHEALTHY" {
				code, message = "APPIUM_UNHEALTHY", "Appium did not become healthy before command timeout"
			}
			return providers.Snapshot{}, &providers.Error{Operation: providers.OperationInspectHealth,
				Code: code, Message: message, Retryable: true, Cause: errors.Join(lastErr, ctx.Err())}
		case <-timer.C:
		}
	}
}

func (agent *Agent) cleanupProvider(providerRef string) error {
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return agent.provider.Delete(cleanupContext, providerRef)
}

func (agent *Agent) validateImage(ctx context.Context, payload map[string]any) (map[string]any, error) {
	verifier, ok := agent.provider.(providers.ImageDigestVerifier)
	if !ok {
		return nil, &providers.Error{Operation: providers.OperationValidateImage, Code: "IMAGE_VALIDATION_UNSUPPORTED",
			Message: "provider does not support image digest validation", Retryable: false}
	}
	if err := verifier.VerifyImageDigest(ctx, stringValue(payload, "docker_digest")); err != nil {
		return nil, err
	}
	providerRef := stringValue(payload, "provider_ref")
	created := false
	cleanup := func() error {
		if !created {
			return nil
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return agent.provider.Delete(cleanupContext, providerRef)
	}
	_, err := agent.provider.Create(ctx, providers.CreateRequest{
		DeviceID: stringValue(payload, "device_id"), HostID: agent.config.HostID,
		ImageID: stringValue(payload, "image_id"), ProviderRef: providerRef,
		Capabilities: mapValue(payload, "capabilities"),
	})
	if err != nil {
		return nil, err
	}
	created = true
	if _, err = agent.provider.Start(ctx, providerRef); err != nil {
		_ = cleanup()
		return nil, err
	}
	for {
		health, healthErr := agent.provider.InspectHealth(ctx, providerRef)
		if healthErr == nil && health.Ready() {
			if err := cleanup(); err != nil {
				return nil, err
			}
			return map[string]any{"provider_ref": providerRef, "image_id": stringValue(payload, "image_id"),
				"digest_verified": true, "ready": true}, nil
		}
		select {
		case <-ctx.Done():
			_ = cleanup()
			if healthErr != nil {
				return nil, healthErr
			}
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func waitWorkers(workers *sync.WaitGroup, timeout time.Duration) error {
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("agent shutdown timed out; command leases will be recovered by server")
	}
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
func mapValue(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}
func providerErrorCode(err error) string {
	if code := providers.ErrorCode(err); code != "" {
		return code
	}
	return "AGENT_COMMAND_FAILED"
}
func providerErrorRetryable(err error) bool {
	var providerError *providers.Error
	if errors.As(err, &providerError) {
		return providerError.Retryable
	}
	return true
}
func providerLifecycle(snapshot providers.Snapshot) string {
	if snapshot.State == providers.StateRunning {
		if snapshot.Ready() {
			return "ready"
		}
		return "booting"
	}
	return "stopped"
}
func providerHealth(snapshot providers.Snapshot) string {
	if snapshot.Ready() {
		return "healthy"
	}
	if snapshot.State != providers.StateRunning || snapshot.Health.Online {
		return "unknown"
	}
	return "unhealthy"
}

func snapshotResult(snapshot providers.Snapshot) map[string]any {
	return map[string]any{
		"device_id": snapshot.DeviceID, "host_id": snapshot.HostID, "image_id": snapshot.ImageID,
		"provider_ref": snapshot.ProviderRef, "state": string(snapshot.State), "generation": snapshot.Generation,
		"capabilities": snapshot.Capabilities,
		"connection": map[string]any{
			"serial": snapshot.Connection.Serial, "adb_endpoint": snapshot.Connection.ADBEndpoint,
			"appium_endpoint": snapshot.Connection.AppiumEndpoint, "appium_udid": snapshot.Connection.AppiumUDID,
		},
		"health": healthResult(snapshot.Health),
	}
}

func healthResult(health providers.Health) map[string]any {
	return map[string]any{
		"online": health.Online, "adb_online": health.ADBOnline,
		"boot_completed": health.BootCompleted, "appium_healthy": health.AppiumHealthy,
	}
}
