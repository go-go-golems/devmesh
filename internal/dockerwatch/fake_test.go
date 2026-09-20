package dockerwatch

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// fakeAPI is an in-memory DockerAPI for unit tests.
type fakeAPI struct {
	containers []container.Summary
	byID       map[string]container.InspectResponse
	events     chan events.Message
	errs       chan error
	listErr    error
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		byID:   map[string]container.InspectResponse{},
		events: make(chan events.Message, 16),
		errs:   make(chan error, 1),
	}
}

func (f *fakeAPI) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.containers, nil
}

func (f *fakeAPI) ContainerInspect(_ context.Context, id string) (container.InspectResponse, error) {
	insp, ok := f.byID[id]
	if !ok {
		return container.InspectResponse{}, context.DeadlineExceeded
	}
	return insp, nil
}

func (f *fakeAPI) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}

func (f *fakeAPI) ContainerStart(context.Context, string, container.StartOptions) error { return nil }
func (f *fakeAPI) ContainerRemove(context.Context, string, container.RemoveOptions) error {
	return nil
}
func (f *fakeAPI) ImageList(context.Context, image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}
func (f *fakeAPI) Events(context.Context, events.ListOptions) (<-chan events.Message, <-chan error) {
	return f.events, f.errs
}
func (f *fakeAPI) Close() error { return nil }
