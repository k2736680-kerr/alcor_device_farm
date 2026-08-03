package docker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

func TestDockerProviderLifecycleUsesUniquePortsAndCleansResources(t *testing.T) {
	engine := newFakeBackend()
	provider, err := newProvider(context.Background(), testConfig(), engine, staticHostProbe{})
	if err != nil {
		t.Fatal(err)
	}

	first, err := provider.Create(context.Background(), dockerCreateRequest("device_0000000000001", "emulator-one"))
	if err != nil {
		t.Fatal(err)
	}
	if first.State != providers.StateCreated || first.Generation != 1 || first.Connection.ADBEndpoint != "" {
		t.Fatalf("created snapshot=%#v", first)
	}
	duplicate, err := provider.Create(context.Background(), dockerCreateRequest("device_0000000000001", "emulator-one"))
	if err != nil || duplicate.ProviderRef != first.ProviderRef || len(engine.containers) != 1 {
		t.Fatalf("idempotent create=%#v error=%v containers=%d", duplicate, err, len(engine.containers))
	}

	first, err = provider.Start(context.Background(), "emulator-one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Create(context.Background(), dockerCreateRequest("device_0000000000002", "emulator-two"))
	if err != nil {
		t.Fatal(err)
	}
	second, err = provider.Start(context.Background(), second.ProviderRef)
	if err != nil {
		t.Fatal(err)
	}
	if first.Connection.Serial == second.Connection.Serial || first.Connection.ADBEndpoint == second.Connection.ADBEndpoint {
		t.Fatalf("duplicate connections: %#v / %#v", first.Connection, second.Connection)
	}
	if first.Connection.Serial != "10.20.30.40:31000" || second.Connection.Serial != "10.20.30.40:31001" ||
		first.Connection.AppiumEndpoint != "http://10.20.30.40:32000" || second.Connection.AppiumEndpoint != "http://10.20.30.40:32001" ||
		first.Connection.AppiumUDID != "emulator-5554" || second.Connection.AppiumUDID != "emulator-5554" {
		t.Fatalf("unexpected serials: %s / %s", first.Connection.Serial, second.Connection.Serial)
	}
	health, err := provider.InspectHealth(context.Background(), "emulator-one")
	if err != nil || !health.Online || !health.ADBOnline || !health.BootCompleted || health.AppiumHealthy {
		t.Fatalf("health=%#v error=%v", health, err)
	}

	discovered, err := provider.Discover(context.Background(), "host_000000000000001")
	if err != nil || len(discovered) != 2 {
		t.Fatalf("discover=%#v error=%v", discovered, err)
	}
	_, _, firstVolume := resourceNames("emulator-one")
	engine.volumes[firstVolume]["test.previous-run-data"] = "installed-app-cache-and-files"
	rebuilt, err := provider.Rebuild(context.Background(), "emulator-one")
	if err != nil || rebuilt.Generation != 2 || rebuilt.State != providers.StateRunning {
		t.Fatalf("rebuilt=%#v error=%v", rebuilt, err)
	}
	if engine.volumes[firstVolume]["test.previous-run-data"] != "" {
		t.Fatal("rebuilt emulator reused the previous run data volume")
	}

	if err := provider.Delete(context.Background(), "emulator-one"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Delete(context.Background(), "emulator-one"); err != nil {
		t.Fatalf("idempotent delete failed: %v", err)
	}
	if engine.hasResources("emulator-one") {
		t.Fatal("container, network or volume remained after delete")
	}
}

func TestDockerProviderVerifiesConfiguredImageDigest(t *testing.T) {
	provider, err := newProvider(context.Background(), testConfig(), newFakeBackend(), staticHostProbe{})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.VerifyImageDigest(context.Background(), "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	err = provider.VerifyImageDigest(context.Background(), "sha256:"+strings.Repeat("b", 64))
	if providers.ErrorCode(err) != "IMAGE_DIGEST_MISMATCH" {
		t.Fatalf("digest mismatch error=%v", err)
	}
}

func TestDockerProviderRequiresIndependentHealthyAppiumEndpoints(t *testing.T) {
	engine := newFakeBackend()
	probe := &recordingAppiumProbe{}
	config := testConfig()
	config.AppiumProbe = probe
	provider, err := newProvider(context.Background(), config, engine, staticHostProbe{})
	if err != nil {
		t.Fatal(err)
	}
	for index, ref := range []string{"emulator-one", "emulator-two"} {
		if _, err := provider.Create(context.Background(), dockerCreateRequest(fmt.Sprintf("device_000000000000%d", index+1), ref)); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Start(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
		health, err := provider.InspectHealth(context.Background(), ref)
		if err != nil || !health.Ready() {
			t.Fatalf("ref=%s health=%#v error=%v", ref, health, err)
		}
	}
	if !reflect.DeepEqual(probe.endpoints, []string{"http://10.20.30.40:32000", "http://10.20.30.40:32001"}) {
		t.Fatalf("Appium endpoints=%v", probe.endpoints)
	}
}

func TestDockerProviderDoesNotSilentlyRunWithoutKVMOrFixedImage(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		host     hostProbe
		wantCode string
	}{
		{name: "floating latest image", config: func() Config { value := testConfig(); value.Image = "example/android:latest"; return value }(), host: staticHostProbe{}},
		{name: "missing KVM", config: testConfig(), host: staticHostProbe{err: errors.New("/dev/kvm missing")}, wantCode: "KVM_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newProvider(context.Background(), test.config, newFakeBackend(), test.host)
			if err == nil {
				t.Fatal("expected configuration failure")
			}
			if test.wantCode != "" && providers.ErrorCode(err) != test.wantCode {
				t.Fatalf("error=%v code=%s", err, providers.ErrorCode(err))
			}
		})
	}
}

func TestDockerResourceNamesAreStableAndBounded(t *testing.T) {
	first, network, volume := resourceNames("Device_REF.with:many_symbols-1234567890-abcdefghijklmnopqrstuvwxyz")
	second, _, _ := resourceNames("Device_REF.with:many_symbols-1234567890-abcdefghijklmnopqrstuvwxyz")
	if first != second || len(first) > 50 || !strings.HasSuffix(network, "-net") || !strings.HasSuffix(volume, "-data") {
		t.Fatalf("names=%q %q %q", first, network, volume)
	}
	other, _, _ := resourceNames("device_ref.with:many_symbols-1234567890-abcdefghijklmnopqrstuvwxyz")
	if first == other {
		t.Fatal("case-distinct provider refs must not collide")
	}
}

func testConfig() Config {
	return Config{
		Image: "registry.example/android-emulator:2026.08", AdvertiseHost: "10.20.30.40",
		BindAddress: "127.0.0.1", Environment: map[string]string{"EMULATOR_DEVICE": "Pixel 7"},
	}
}

func dockerCreateRequest(deviceID, providerRef string) providers.CreateRequest {
	return providers.CreateRequest{
		DeviceID: deviceID, HostID: "host_000000000000001", ImageID: "image_00000000000001",
		ProviderRef: providerRef, Capabilities: map[string]any{"apiLevel": 34, "abi": "x86_64"},
	}
}

type staticHostProbe struct{ err error }

func (probe staticHostProbe) ValidateKVM(string) error { return probe.err }

type recordingAppiumProbe struct{ endpoints []string }

func (probe *recordingAppiumProbe) Healthy(_ context.Context, connection providers.ConnectionInfo) (bool, error) {
	probe.endpoints = append(probe.endpoints, connection.AppiumEndpoint)
	return true, nil
}

type fakeBackend struct {
	containers     map[string]container
	specs          map[string]containerSpec
	networks       map[string]map[string]string
	volumes        map[string]map[string]string
	nextADBPort    int
	nextAppiumPort int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		containers: map[string]container{}, specs: map[string]containerSpec{},
		networks: map[string]map[string]string{}, volumes: map[string]map[string]string{},
		nextADBPort: 31000, nextAppiumPort: 32000,
	}
}

func (*fakeBackend) Ping(context.Context) error { return nil }

func (*fakeBackend) InspectImage(context.Context, string) (imageMetadata, error) {
	return imageMetadata{ID: "sha256:" + strings.Repeat("a", 64)}, nil
}

func (engine *fakeBackend) CreateNetwork(_ context.Context, name string, labels map[string]string) error {
	if _, exists := engine.networks[name]; exists {
		return errors.New("network exists")
	}
	engine.networks[name] = cloneStringMap(labels)
	return nil
}

func (engine *fakeBackend) CreateVolume(_ context.Context, name string, labels map[string]string) error {
	if _, exists := engine.volumes[name]; exists {
		return errors.New("volume exists")
	}
	engine.volumes[name] = cloneStringMap(labels)
	return nil
}

func (engine *fakeBackend) CreateContainer(_ context.Context, spec containerSpec) error {
	if _, exists := engine.containers[spec.Name]; exists {
		return errors.New("container exists")
	}
	engine.specs[spec.Name] = spec
	engine.containers[spec.Name] = container{
		ID: "id-" + spec.Name, Name: spec.Name, Image: spec.Image, State: "created",
		Labels: cloneStringMap(spec.Labels), Ports: map[int]int{},
	}
	return nil
}

func (engine *fakeBackend) InspectContainer(_ context.Context, name string) (container, error) {
	value, exists := engine.containers[name]
	if !exists {
		return container{}, errNotFound
	}
	return cloneContainer(value), nil
}

func (engine *fakeBackend) ListContainers(_ context.Context, labels map[string]string) ([]container, error) {
	result := make([]container, 0)
	for _, value := range engine.containers {
		if labelsMatch(value.Labels, labels) {
			result = append(result, cloneContainer(value))
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

func (engine *fakeBackend) StartContainer(_ context.Context, name string) error {
	value, exists := engine.containers[name]
	if !exists {
		return errNotFound
	}
	value.State = "running"
	if value.Ports[engine.specs[name].ContainerADBPort] == 0 {
		value.Ports[engine.specs[name].ContainerADBPort] = engine.nextADBPort
		engine.nextADBPort++
	}
	if value.Ports[engine.specs[name].ContainerAppiumPort] == 0 {
		value.Ports[engine.specs[name].ContainerAppiumPort] = engine.nextAppiumPort
		engine.nextAppiumPort++
	}
	engine.containers[name] = value
	return nil
}

func (engine *fakeBackend) StopContainer(_ context.Context, name string) error {
	value, exists := engine.containers[name]
	if !exists {
		return errNotFound
	}
	value.State = "exited"
	engine.containers[name] = value
	return nil
}

func (engine *fakeBackend) RestartContainer(ctx context.Context, name string) error {
	return engine.StartContainer(ctx, name)
}

func (engine *fakeBackend) RemoveContainer(_ context.Context, name string) error {
	if _, exists := engine.containers[name]; !exists {
		return errNotFound
	}
	delete(engine.containers, name)
	delete(engine.specs, name)
	return nil
}

func (engine *fakeBackend) RemoveNetworks(_ context.Context, labels map[string]string) error {
	removeLabeled(engine.networks, labels)
	return nil
}

func (engine *fakeBackend) RemoveVolumes(_ context.Context, labels map[string]string) error {
	removeLabeled(engine.volumes, labels)
	return nil
}

func (*fakeBackend) Exec(_ context.Context, _ string, args ...string) (string, error) {
	last := args[len(args)-1]
	if last == "get-state" {
		return "device", nil
	}
	if last == "sys.boot_completed" {
		return "1", nil
	}
	return "", fmt.Errorf("unexpected exec args %v", args)
}

func (engine *fakeBackend) hasResources(providerRef string) bool {
	for _, value := range engine.containers {
		if value.Labels[labelProviderRef] == providerRef {
			return true
		}
	}
	for _, resources := range []map[string]map[string]string{engine.networks, engine.volumes} {
		for _, labels := range resources {
			if labels[labelProviderRef] == providerRef {
				return true
			}
		}
	}
	return false
}

func cloneContainer(source container) container {
	result := source
	result.Labels = cloneStringMap(source.Labels)
	result.Ports = make(map[int]int, len(source.Ports))
	for key, value := range source.Ports {
		result.Ports[key] = value
	}
	return result
}

func labelsMatch(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func removeLabeled(resources map[string]map[string]string, labels map[string]string) {
	for name, actual := range resources {
		if labelsMatch(actual, labels) {
			delete(resources, name)
		}
	}
}

func TestFakeBackendCloneSanity(t *testing.T) {
	value := container{Labels: map[string]string{"a": "b"}, Ports: map[int]int{5555: 30000}}
	cloned := cloneContainer(value)
	if !reflect.DeepEqual(value, cloned) {
		t.Fatal("clone changed container")
	}
}
