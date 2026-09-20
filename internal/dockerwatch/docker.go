package dockerwatch

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerAPI is the subset of the Docker Engine API the watcher and doctor
// probe need. Defining it as an interface keeps the watcher unit-testable
// without Docker.
type DockerAPI interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
	Close() error
}

type dockerClient struct {
	cli *client.Client
}

// NewClient builds an environment-aware Docker client with API version
// negotiation (respects Docker Desktop contexts and DOCKER_HOST).
func NewClient() (DockerAPI, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &dockerClient{cli: cli}, nil
}

func (d *dockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	return d.cli.ContainerList(ctx, options)
}

func (d *dockerClient) ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error) {
	return d.cli.ContainerInspect(ctx, id)
}

func (d *dockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	return d.cli.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, containerName)
}

func (d *dockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return d.cli.ContainerStart(ctx, containerID, options)
}

func (d *dockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return d.cli.ContainerRemove(ctx, containerID, options)
}

func (d *dockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return d.cli.ImageList(ctx, options)
}

func (d *dockerClient) Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error) {
	return d.cli.Events(ctx, options)
}

func (d *dockerClient) Close() error { return d.cli.Close() }
