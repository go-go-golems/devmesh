package integration

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"

	"github.com/wesen/devmesh/internal/api"
	"github.com/wesen/devmesh/internal/dockerwatch"
)

const testImage = "postgres:17"

func dockerAPI(t *testing.T) dockerwatch.DockerAPI {
	t.Helper()
	api, err := dockerwatch.NewClient()
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := api.ContainerList(ctx, container.ListOptions{}); err != nil {
		_ = api.Close()
		t.Skipf("docker engine unavailable: %v", err)
	}
	return api
}

func hasImage(t *testing.T, dapi dockerwatch.DockerAPI, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	images, err := dapi.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false
	}
	for _, im := range images {
		for _, tag := range im.RepoTags {
			if tag == name {
				return true
			}
		}
	}
	return false
}

func createPostgres(t *testing.T, dapi dockerwatch.DockerAPI, project, service, devmeshName string) string {
	t.Helper()
	labels := map[string]string{
		dockerwatch.LabelEnable:         "true",
		dockerwatch.LabelName:           devmeshName,
		dockerwatch.LabelContainerPort:  "5432",
		dockerwatch.LabelKind:           "tcp",
		dockerwatch.LabelAppProtocol:    "postgres",
		dockerwatch.ComposeProjectLabel: project,
		dockerwatch.ComposeServiceLabel: service,
	}
	cfg := &container.Config{
		Image: testImage,
		Env: []string{
			"POSTGRES_USER=dev",
			"POSTGRES_PASSWORD=dev",
			"POSTGRES_DB=app",
		},
		Labels:       labels,
		ExposedPorts: nat.PortSet{"5432/tcp": struct{}{}},
	}
	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5432/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := dapi.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		t.Fatalf("create postgres container: %v", err)
	}
	if err := dapi.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rcancel()
		_ = dapi.ContainerRemove(rctx, resp.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	})
	return resp.ID
}

func waitReady(t *testing.T, h *harness, name string, timeout time.Duration) api.ServiceDTO {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last api.ServiceDTO
	for time.Now().Before(deadline) {
		var svc api.ServiceDTO
		if err := h.client.Do(context.Background(), "GET", "/v1/services/"+name, nil, &svc); err == nil {
			last = svc
			if svc.Status == "ready" {
				return svc
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("service %s not ready within %s (last=%+v)", name, timeout, last)
	return last
}

// TestDockerDiscoveryAndRecreatePreservesFrontend is the key Docker acceptance
// test: a labeled container is discovered, its ephemeral published port is
// proxied behind a stable frontend, and recreating the container changes only
// the backend.
func TestDockerDiscoveryAndRecreatePreservesFrontend(t *testing.T) {
	dapi := dockerAPI(t)
	if !hasImage(t, dapi, testImage) {
		t.Skipf("image %s not available locally", testImage)
	}

	h := startHarnessWithDocker(t, 5*time.Second, true)
	const name = "checkout.postgres"

	firstID := createPostgres(t, dapi, "devmesh-it", "db", name)
	svc := waitReady(t, h, name, 45*time.Second)
	frontend := net.JoinHostPort(svc.Frontend.Host, fmt.Sprintf("%d", svc.Frontend.Port))

	// The frontend must accept a TCP connection (Postgres will wait for a
	// startup packet, so a successful dial proves the proxy forwarded it).
	conn, err := net.DialTimeout("tcp", frontend, 3*time.Second)
	if err != nil {
		t.Fatalf("dial frontend %s: %v", frontend, err)
	}
	_ = conn.Close()

	var inspect api.InspectDTO
	if err := h.client.Do(context.Background(), "GET", "/v1/services/"+name+"/inspect", nil, &inspect); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspect.Source != "docker" || inspect.Backend == nil {
		t.Fatalf("unexpected inspect: %+v", inspect)
	}
	firstBackend := *inspect.Backend
	if inspect.OwnerKey != "docker:devmesh-it:db:5432" {
		t.Fatalf("owner key = %q", inspect.OwnerKey)
	}

	// Start the replacement before removing the old container. This is the
	// ordering that used to fail: B registers under the same logical owner, then
	// A's delayed destroy event cleared B. Conditional container identity must
	// now keep B current.
	secondID := createPostgres(t, dapi, "devmesh-it", "db", name)
	deadline := time.Now().Add(45 * time.Second)
	var second api.InspectDTO
	for time.Now().Before(deadline) {
		if err := h.client.Do(context.Background(), "GET", "/v1/services/"+name+"/inspect", nil, &second); err == nil {
			if second.Backend != nil && second.Backend.Port != firstBackend.Port && second.DockerContainerID == secondID {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if second.Backend == nil || second.DockerContainerID != secondID {
		t.Fatalf("replacement not current: %+v", second)
	}

	rmctx, rmcancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = dapi.ContainerRemove(rmctx, firstID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	rmcancel()
	// Give the old container's terminal event/reconcile path time to run. It
	// must not clear the replacement's backend.
	time.Sleep(500 * time.Millisecond)
	var afterOldRemoval api.InspectDTO
	if err := h.client.Do(context.Background(), "GET", "/v1/services/"+name+"/inspect", nil, &afterOldRemoval); err != nil {
		t.Fatal(err)
	}
	if afterOldRemoval.Status != "ready" || afterOldRemoval.DockerContainerID != secondID || afterOldRemoval.Backend == nil {
		t.Fatalf("old removal displaced replacement: %+v", afterOldRemoval)
	}
	if afterOldRemoval.Frontend.Port != svc.Frontend.Port {
		t.Fatalf("frontend changed on recreate: %d -> %d", svc.Frontend.Port, afterOldRemoval.Frontend.Port)
	}
	conn, err = net.DialTimeout("tcp", frontend, 3*time.Second)
	if err != nil {
		t.Fatalf("dial frontend after old removal %s: %v", frontend, err)
	}
	_ = conn.Close()
}
