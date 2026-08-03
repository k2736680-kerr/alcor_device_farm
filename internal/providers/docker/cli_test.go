package docker

import (
	"context"
	"reflect"
	"testing"
)

func TestCLIBackendCreatesContainerWithKVMResourceLimitsAndRandomADBPort(t *testing.T) {
	runner := &recordingRunner{}
	client := newCLIBackend("docker", runner)
	err := client.CreateContainer(context.Background(), containerSpec{
		Name: "alcor-df-device", Hostname: "alcor-df-device", Image: "android:2026.08",
		Network: "device-net", Volume: "device-data", DataMountPath: "/data",
		KVMDevice: "/dev/kvm", BindAddress: "127.0.0.1", ContainerADBPort: 5555,
		CPUs: 2, Memory: "4g", PidsLimit: 512,
		Labels:      map[string]string{labelManaged: "true", labelProviderRef: "device-1"},
		Environment: map[string]string{"EMULATOR_DEVICE": "Pixel 7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantParts := [][]string{
		{"--device", "/dev/kvm:/dev/kvm"}, {"--cpus", "2"}, {"--memory", "4g"},
		{"--pids-limit", "512"}, {"--publish", "127.0.0.1::5555/tcp"},
		{"--mount", "type=volume,source=device-data,target=/data"},
		{"--env", "EMULATOR_DEVICE=Pixel 7"},
	}
	for _, part := range wantParts {
		if !containsAdjacent(runner.args, part) {
			t.Fatalf("args %v do not contain %v", runner.args, part)
		}
	}
}

func TestCLIBackendInspectDecodesPublishedPortAndLabels(t *testing.T) {
	runner := &recordingRunner{output: `[{"Id":"abc","Name":"/alcor-df-device","Config":{"Image":"android:2026.08","Labels":{"io.alcor.device-farm.managed":"true"}},"State":{"Status":"running"},"NetworkSettings":{"Ports":{"5555/tcp":[{"HostPort":"32771"}]}}}]`}
	client := newCLIBackend("docker", runner)
	value, err := client.InspectContainer(context.Background(), "alcor-df-device")
	if err != nil {
		t.Fatal(err)
	}
	if value.Name != "alcor-df-device" || value.State != "running" || value.Ports[5555] != 32771 || value.Labels[labelManaged] != "true" {
		t.Fatalf("container=%#v", value)
	}
}

type recordingRunner struct {
	binary string
	args   []string
	output string
	err    error
}

func (runner *recordingRunner) Run(_ context.Context, binary string, args ...string) (string, error) {
	runner.binary = binary
	runner.args = append([]string(nil), args...)
	return runner.output, runner.err
}

func containsAdjacent(values, part []string) bool {
	for index := 0; index+len(part) <= len(values); index++ {
		if reflect.DeepEqual(values[index:index+len(part)], part) {
			return true
		}
	}
	return false
}
