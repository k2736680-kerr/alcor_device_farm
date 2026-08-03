package agent_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/agent"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
)

type fakeClient struct {
	mu          sync.Mutex
	commands    []hostcommand.Command
	completions []hostcommand.CompletionInput
	heartbeats  int
}

func (client *fakeClient) Heartbeat(context.Context, string, hostcommand.HeartbeatInput) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.heartbeats++
	return nil
}
func (client *fakeClient) Claim(ctx context.Context, _ string, input hostcommand.ClaimInput) ([]hostcommand.Command, error) {
	client.mu.Lock()
	if len(client.commands) > 0 {
		count := input.MaxCommands
		if count > len(client.commands) {
			count = len(client.commands)
		}
		values := append([]hostcommand.Command(nil), client.commands[:count]...)
		client.commands = client.commands[count:]
		client.mu.Unlock()
		return values, nil
	}
	client.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}
func (client *fakeClient) Complete(_ context.Context, _ string, input hostcommand.CompletionInput) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.completions = append(client.completions, input)
	return nil
}

type trackingSleeper struct {
	mu      sync.Mutex
	active  int
	maximum int
	started chan struct{}
	release chan struct{}
}

func (sleeper *trackingSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	sleeper.mu.Lock()
	sleeper.active++
	if sleeper.active > sleeper.maximum {
		sleeper.maximum = sleeper.active
	}
	sleeper.mu.Unlock()
	sleeper.started <- struct{}{}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sleeper.release:
	}
	sleeper.mu.Lock()
	sleeper.active--
	sleeper.mu.Unlock()
	return nil
}

func TestAgentStopsClaimingAndFinishesInflightCommands(t *testing.T) {
	sleeper := &trackingSleeper{started: make(chan struct{}, 3), release: make(chan struct{})}
	provider := providermock.New(providermock.Config{Sleeper: sleeper, Scenario: providermock.Scenario{
		Delays: map[providers.Operation]time.Duration{providers.OperationRestart: time.Second},
	}})
	commands := make([]hostcommand.Command, 0, 3)
	for index := 0; index < 3; index++ {
		ref := fmt.Sprintf("container-%d", index)
		if _, err := provider.Create(context.Background(), providers.CreateRequest{
			DeviceID: fmt.Sprintf("device_%019d", index), HostID: "host_000000000000001",
			ImageID: "image_00000000000001", ProviderRef: ref,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Start(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
		token := fmt.Sprintf("lease_token_%016d", index)
		commands = append(commands, hostcommand.Command{ID: fmt.Sprintf("command_%016d", index),
			CommandType: "restart", Payload: map[string]any{"provider_ref": ref}, LeaseToken: &token, Attempt: 1})
	}
	client := &fakeClient{commands: commands}
	runtime, err := agent.New(agent.Config{
		HostID: "host_000000000000001", HeartbeatInterval: 10 * time.Millisecond,
		LeaseSeconds: 30, WaitSeconds: 1, Concurrency: 2,
		CommandTimeout: time.Second, ShutdownTimeout: time.Second, Capacity: map[string]any{"device_slots": 2},
	}, client, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	<-sleeper.started
	<-sleeper.started
	cancel()
	close(sleeper.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.completions) != 2 || len(client.commands) != 1 || client.heartbeats < 1 {
		t.Fatalf("completions=%d unclaimed=%d heartbeats=%d", len(client.completions), len(client.commands), client.heartbeats)
	}
	sleeper.mu.Lock()
	defer sleeper.mu.Unlock()
	if sleeper.maximum != 2 {
		t.Fatalf("maximum provider concurrency=%d", sleeper.maximum)
	}
}
