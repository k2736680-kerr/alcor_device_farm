package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, binary string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("docker command failed: %s: %w", message, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

type cliBackend struct {
	binary string
	runner commandRunner
}

func newCLIBackend(binary string, runner commandRunner) *cliBackend {
	if runner == nil {
		runner = execCommandRunner{}
	}
	return &cliBackend{binary: binary, runner: runner}
}

func (client *cliBackend) Ping(ctx context.Context) error {
	_, err := client.run(ctx, "version", "--format", "{{.Server.Version}}")
	return err
}

func (client *cliBackend) InspectImage(ctx context.Context, reference string) (imageMetadata, error) {
	output, err := client.run(ctx, "image", "inspect", reference)
	if err != nil {
		if isNotFoundError(err) {
			return imageMetadata{}, errNotFound
		}
		return imageMetadata{}, err
	}
	var values []struct {
		ID          string   `json:"Id"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.Unmarshal([]byte(output), &values); err != nil || len(values) != 1 {
		if err == nil {
			err = errors.New("docker image inspect returned an unexpected item count")
		}
		return imageMetadata{}, err
	}
	return imageMetadata{ID: values[0].ID, RepoDigests: values[0].RepoDigests}, nil
}

func (client *cliBackend) CreateNetwork(ctx context.Context, name string, labels map[string]string) error {
	args := []string{"network", "create", "--driver", "bridge"}
	args = appendLabelArgs(args, labels)
	_, err := client.run(ctx, append(args, name)...)
	return err
}

func (client *cliBackend) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	args := []string{"volume", "create"}
	args = appendLabelArgs(args, labels)
	_, err := client.run(ctx, append(args, name)...)
	return err
}

func (client *cliBackend) CreateContainer(ctx context.Context, spec containerSpec) error {
	args := []string{
		"create", "--name", spec.Name, "--hostname", spec.Hostname,
		"--network", spec.Network,
		"--device", spec.KVMDevice + ":" + spec.KVMDevice,
	}
	if spec.RenderDevice != "" {
		if spec.GPUDevice != "" {
			args = append(args, "--device", spec.GPUDevice+":"+spec.GPUDevice)
		}
		args = append(args, "--device", spec.RenderDevice+":"+spec.RenderDevice)
	}
	args = append(args,
		"--cpus", strconv.FormatFloat(spec.CPUs, 'f', -1, 64),
		"--memory", spec.Memory,
		"--pids-limit", strconv.Itoa(spec.PidsLimit),
		"--publish", fmt.Sprintf("%s::%d/tcp", spec.BindAddress, spec.ContainerADBPort),
		"--publish", fmt.Sprintf("%s::%d/tcp", spec.BindAddress, spec.ContainerAppiumPort),
		"--mount", fmt.Sprintf("type=volume,source=%s,target=%s", spec.Volume, spec.DataMountPath),
	)
	args = appendLabelArgs(args, spec.Labels)
	keys := sortedKeys(spec.Environment)
	for _, key := range keys {
		args = append(args, "--env", key+"="+spec.Environment[key])
	}
	args = append(args, spec.Image)
	_, err := client.run(ctx, args...)
	return err
}

func (client *cliBackend) InspectContainer(ctx context.Context, reference string) (container, error) {
	output, err := client.run(ctx, "inspect", reference)
	if err != nil {
		if isNotFoundError(err) {
			return container{}, errNotFound
		}
		return container{}, err
	}
	var values []struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Status    string `json:"Status"`
			OOMKilled bool   `json:"OOMKilled"`
		} `json:"State"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal([]byte(output), &values); err != nil || len(values) != 1 {
		if err == nil {
			err = errors.New("docker inspect returned an unexpected item count")
		}
		return container{}, err
	}
	ports := make(map[int]int)
	for containerPort, bindings := range values[0].NetworkSettings.Ports {
		parts := strings.SplitN(containerPort, "/", 2)
		port, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil || len(bindings) == 0 || bindings[0].HostPort == "" {
			continue
		}
		hostPort, parseErr := strconv.Atoi(bindings[0].HostPort)
		if parseErr == nil {
			ports[port] = hostPort
		}
	}
	return container{
		ID: values[0].ID, Name: strings.TrimPrefix(values[0].Name, "/"), Image: values[0].Config.Image,
		State: values[0].State.Status, OOMKilled: values[0].State.OOMKilled,
		Labels: values[0].Config.Labels, Ports: ports,
	}, nil
}

func (client *cliBackend) ListContainers(ctx context.Context, labels map[string]string) ([]container, error) {
	args := []string{"ps", "-aq"}
	args = appendFilterArgs(args, labels)
	output, err := client.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" {
		return []container{}, nil
	}
	identifiers := strings.Fields(output)
	result := make([]container, 0, len(identifiers))
	for _, identifier := range identifiers {
		value, err := client.InspectContainer(ctx, identifier)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (client *cliBackend) StartContainer(ctx context.Context, reference string) error {
	_, err := client.run(ctx, "start", reference)
	return translateNotFound(err)
}

func (client *cliBackend) StopContainer(ctx context.Context, reference string) error {
	_, err := client.run(ctx, "stop", "--time", "30", reference)
	return translateNotFound(err)
}

func (client *cliBackend) RestartContainer(ctx context.Context, reference string) error {
	_, err := client.run(ctx, "restart", "--time", "30", reference)
	return translateNotFound(err)
}

func (client *cliBackend) RemoveContainer(ctx context.Context, reference string) error {
	_, err := client.run(ctx, "rm", "--force", "--volumes", reference)
	return translateNotFound(err)
}

func (client *cliBackend) RemoveNetworks(ctx context.Context, labels map[string]string) error {
	return client.removeResources(ctx, "network", labels)
}

func (client *cliBackend) RemoveVolumes(ctx context.Context, labels map[string]string) error {
	return client.removeResources(ctx, "volume", labels)
}

func (client *cliBackend) Exec(ctx context.Context, reference string, args ...string) (string, error) {
	command := append([]string{"exec", reference}, args...)
	output, err := client.run(ctx, command...)
	return output, translateNotFound(err)
}

func (client *cliBackend) removeResources(ctx context.Context, kind string, labels map[string]string) error {
	args := []string{kind, "ls", "-q"}
	args = appendFilterArgs(args, labels)
	output, err := client.run(ctx, args...)
	if err != nil {
		return err
	}
	for _, identifier := range strings.Fields(output) {
		if _, err := client.run(ctx, kind, "rm", identifier); err != nil && !isNotFoundError(err) {
			return err
		}
	}
	return nil
}

func (client *cliBackend) run(ctx context.Context, args ...string) (string, error) {
	return client.runner.Run(ctx, client.binary, args...)
}

func appendLabelArgs(args []string, labels map[string]string) []string {
	for _, key := range sortedKeys(labels) {
		args = append(args, "--label", key+"="+labels[key])
	}
	return args
}

func appendFilterArgs(args []string, labels map[string]string) []string {
	for _, key := range sortedKeys(labels) {
		args = append(args, "--filter", "label="+key+"="+labels[key])
	}
	return args
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func translateNotFound(err error) error {
	if isNotFoundError(err) {
		return errNotFound
	}
	return err
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such container") || strings.Contains(message, "no such object") ||
		strings.Contains(message, "no such image")
}
