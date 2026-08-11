package docker

import (
	"context"
	"errors"
)

var errNotFound = errors.New("docker resource not found")

type containerSpec struct {
	Name                string
	Hostname            string
	Image               string
	Network             string
	Volume              string
	DataMountPath       string
	KVMDevice           string
	GPUDevice           string
	RenderDevice        string
	BindAddress         string
	ContainerADBPort    int
	ContainerAppiumPort int
	CPUs                float64
	Memory              string
	PidsLimit           int
	Labels              map[string]string
	Environment         map[string]string
}

type container struct {
	ID     string
	Name   string
	Image  string
	State  string
	Labels map[string]string
	Ports  map[int]int
}

type imageMetadata struct {
	ID          string
	RepoDigests []string
}

type backend interface {
	Ping(context.Context) error
	InspectImage(context.Context, string) (imageMetadata, error)
	CreateNetwork(context.Context, string, map[string]string) error
	CreateVolume(context.Context, string, map[string]string) error
	CreateContainer(context.Context, containerSpec) error
	InspectContainer(context.Context, string) (container, error)
	ListContainers(context.Context, map[string]string) ([]container, error)
	StartContainer(context.Context, string) error
	StopContainer(context.Context, string) error
	RestartContainer(context.Context, string) error
	RemoveContainer(context.Context, string) error
	RemoveNetworks(context.Context, map[string]string) error
	RemoveVolumes(context.Context, map[string]string) error
	Exec(context.Context, string, ...string) (string, error)
}

type hostProbe interface {
	ValidateKVM(string) error
	RenderDeviceAvailable(string) bool
}
