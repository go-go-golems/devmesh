package dockerwatch

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
)

func inspectWith(labels map[string]string, hostIP, hostPort string) container.InspectResponse {
	ports := nat.PortMap{}
	if hostPort != "" {
		ports[nat.Port("5432/tcp")] = []nat.PortBinding{{HostIP: hostIP, HostPort: hostPort}}
	}
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:    "abc123",
			Name:  "/checkout-db-1",
			State: &container.State{Running: true},
		},
		Config:          &container.Config{Labels: labels},
		NetworkSettings: &container.NetworkSettings{NetworkSettingsBase: container.NetworkSettingsBase{Ports: ports}},
	}
}

func enabledLabels() map[string]string {
	return map[string]string{
		LabelEnable:         "true",
		LabelName:           "checkout.postgres",
		LabelContainerPort:  "5432",
		LabelKind:           "tcp",
		LabelAppProtocol:    "postgres",
		LabelPreferredPort:  "5432",
		ComposeProjectLabel: "checkout",
		ComposeServiceLabel: "db",
	}
}

func TestRegistrationFromInspectLoopback(t *testing.T) {
	reg, err := RegistrationFromInspect(inspectWith(enabledLabels(), "127.0.0.1", "49173"), false)
	if err != nil {
		t.Fatalf("loopback inspect rejected: %v", err)
	}
	if reg.BackendHost != "127.0.0.1" || reg.BackendPort != 49173 {
		t.Fatalf("wrong backend: %+v", reg)
	}
	if reg.OwnerKey != "docker:checkout:db:5432" {
		t.Fatalf("wrong owner key: %q", reg.OwnerKey)
	}
	if reg.ContainerID != "abc123" {
		t.Fatalf("wrong container id: %q", reg.ContainerID)
	}
}

func TestRegistrationFromInspectRefusesNonLoopback(t *testing.T) {
	_, err := RegistrationFromInspect(inspectWith(enabledLabels(), "0.0.0.0", "49173"), false)
	var nl *ErrNonLoopback
	if !errors.As(err, &nl) {
		t.Fatalf("got %v, want ErrNonLoopback", err)
	}

	// Explicit opt-in connects via loopback.
	reg, err := RegistrationFromInspect(inspectWith(enabledLabels(), "0.0.0.0", "49173"), true)
	if err != nil {
		t.Fatalf("allowNonLoopback rejected: %v", err)
	}
	if reg.BackendHost != "127.0.0.1" {
		t.Fatalf("expected loopback backend host, got %q", reg.BackendHost)
	}
}

func TestRegistrationFromInspectNotPublished(t *testing.T) {
	_, err := RegistrationFromInspect(inspectWith(enabledLabels(), "", ""), false)
	var np *ErrNotPublished
	if !errors.As(err, &np) {
		t.Fatalf("got %v, want ErrNotPublished", err)
	}
}
