package dockerwatch

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
)

// ProbeResult describes a disposable loopback-port publication test.
type ProbeResult struct {
	Skipped   bool
	Supported bool
	Image     string
	HostIP    string
	HostPort  string
	Detail    string
}

// ProbeLoopbackPublication creates a tiny disposable container that publishes a
// port on an ephemeral host port bound to 127.0.0.1, verifies the binding, and
// removes the container. It uses a locally available busybox/alpine image and
// reports Skipped when none is present, so it never pulls an image silently.
func ProbeLoopbackPublication(ctx context.Context, api DockerAPI) (ProbeResult, error) {
	images, err := api.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return ProbeResult{}, err
	}
	available := map[string]bool{}
	for _, im := range images {
		for _, tag := range im.RepoTags {
			available[tag] = true
		}
	}
	imageName := ""
	for _, c := range []string{"busybox:latest", "alpine:latest", "busybox", "alpine"} {
		if available[c] {
			imageName = c
			break
		}
	}
	if imageName == "" {
		return ProbeResult{
			Skipped: true,
			Detail:  "no local busybox/alpine image available for a disposable publication test",
		}, nil
	}

	cfg := &container.Config{
		Image:        imageName,
		Cmd:          []string{"sleep", "30"},
		ExposedPorts: nat.PortSet{"80/tcp": struct{}{}},
	}
	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			"80/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}},
		},
	}
	resp, err := api.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return ProbeResult{}, fmt.Errorf("create probe container: %w", err)
	}
	id := resp.ID
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = api.ContainerRemove(rctx, id, container.RemoveOptions{Force: true})
	}()

	if err := api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return ProbeResult{}, fmt.Errorf("start probe container: %w", err)
	}

	var hostIP, hostPort string
	for i := 0; i < 20; i++ {
		if insp, err := api.ContainerInspect(ctx, id); err == nil && insp.NetworkSettings != nil {
			if b, ok := insp.NetworkSettings.Ports[nat.Port("80/tcp")]; ok && len(b) > 0 {
				hostIP, hostPort = b[0].HostIP, b[0].HostPort
				break
			}
		}
		select {
		case <-ctx.Done():
			return ProbeResult{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if hostPort == "" {
		return ProbeResult{Image: imageName, Detail: "published port did not appear before timeout"}, nil
	}
	return ProbeResult{
		Supported: isLoopbackIP(hostIP),
		Image:     imageName,
		HostIP:    hostIP,
		HostPort:  hostPort,
		Detail:    fmt.Sprintf("published 80/tcp on %s:%s", hostIP, hostPort),
	}, nil
}
