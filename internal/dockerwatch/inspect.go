package dockerwatch

import (
	"fmt"
	"net"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
)

// Registration is a backend discovered from a container. It is converted into
// a devmesh registration by the daemon.
type Registration struct {
	Name          string
	Kind          string
	AppProtocol   string
	BackendHost   string
	BackendPort   int
	PreferredPort int
	OwnerKey      string
	ContainerID   string
	HTTPHost      string
}

// ErrNotPublished indicates the expected container port is not published to
// the host (yet). Callers that race container startup may retry.
type ErrNotPublished struct {
	Container string
	Port      int
}

func (e *ErrNotPublished) Error() string {
	return fmt.Sprintf("container %s is labeled for devmesh but %d/tcp is not published to the host", e.Container, e.Port)
}

// ErrNonLoopback indicates the published binding is not loopback-only.
type ErrNonLoopback struct {
	Container string
	HostIP    string
	Port      int
}

func (e *ErrNonLoopback) Error() string {
	return fmt.Sprintf("container %s publishes %d/tcp on %s, which is not loopback-only", e.Container, e.Port, e.HostIP)
}

// RegistrationFromInspect converts a container inspect response into a
// registration. It enforces the loopback-only publication rule unless
// allowNonLoopback is set.
func RegistrationFromInspect(inspect container.InspectResponse, allowNonLoopback bool) (Registration, error) {
	labels := map[string]string{}
	containerName := ""
	if inspect.Name != "" {
		containerName = inspect.Name
	}
	if inspect.Config != nil {
		labels = inspect.Config.Labels
	}
	parsed, err := ParseLabels(labels)
	if err != nil {
		return Registration{}, err
	}
	if !parsed.Enabled {
		return Registration{}, fmt.Errorf("container %s is not devmesh-enabled", containerName)
	}

	if inspect.NetworkSettings == nil {
		return Registration{}, &ErrNotPublished{Container: containerName, Port: parsed.ContainerPort}
	}
	key := nat.Port(fmt.Sprintf("%d/tcp", parsed.ContainerPort))
	bindings := inspect.NetworkSettings.Ports[key]
	if len(bindings) == 0 {
		return Registration{}, &ErrNotPublished{Container: containerName, Port: parsed.ContainerPort}
	}
	// A safe selected binding is not enough: another binding for the managed
	// target can still expose the container on the LAN. Refuse any mixed
	// publication unless the user explicitly opted in to non-loopback ports.
	if !allowNonLoopback {
		for _, binding := range bindings {
			if !isLoopbackIP(binding.HostIP) {
				return Registration{}, &ErrNonLoopback{Container: containerName, HostIP: binding.HostIP, Port: parsed.ContainerPort}
			}
		}
	}

	// Prefer a loopback binding when several exist.
	var chosen *binding
	for i := range bindings {
		b := binding{HostIP: bindings[i].HostIP, HostPort: bindings[i].HostPort}
		if isLoopbackIP(b.HostIP) {
			chosen = &b
			break
		}
	}
	if chosen == nil {
		b := binding{HostIP: bindings[0].HostIP, HostPort: bindings[0].HostPort}
		chosen = &b
	}
	if !isLoopbackIP(chosen.HostIP) && !allowNonLoopback {
		return Registration{}, &ErrNonLoopback{Container: containerName, HostIP: chosen.HostIP, Port: parsed.ContainerPort}
	}

	hostPort := chosen.HostPort
	if hostPort == "" {
		return Registration{}, &ErrNotPublished{Container: containerName, Port: parsed.ContainerPort}
	}
	var port int
	if _, err := fmt.Sscanf(hostPort, "%d", &port); err != nil {
		return Registration{}, fmt.Errorf("invalid published host port %q", hostPort)
	}
	backendHost := chosen.HostIP
	if backendHost == "" || !isLoopbackIP(backendHost) {
		// A non-loopback binding was explicitly allowed; connect via loopback.
		backendHost = "127.0.0.1"
	}

	return Registration{
		Name:          parsed.Name,
		Kind:          parsed.Kind,
		AppProtocol:   parsed.AppProtocol,
		BackendHost:   backendHost,
		BackendPort:   port,
		PreferredPort: parsed.PreferredPort,
		OwnerKey:      OwnerKey(containerName, labels, parsed.ContainerPort),
		ContainerID:   inspect.ID,
		HTTPHost:      parsed.HTTPHost,
	}, nil
}

type binding struct {
	HostIP   string
	HostPort string
}

func isLoopbackIP(host string) bool {
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
