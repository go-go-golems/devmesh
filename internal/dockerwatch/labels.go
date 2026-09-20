// Package dockerwatch watches labeled Docker containers and translates their
// host-published ports into devmesh registrations. It is an adapter: the
// registry core works with no Docker installed.
package dockerwatch

import (
	"fmt"
	"strconv"
	"strings"
)

// Label keys understood by devmesh.
const (
	LabelEnable        = "io.devmesh.enable"
	LabelName          = "io.devmesh.name"
	LabelContainerPort = "io.devmesh.container-port"
	LabelKind          = "io.devmesh.kind"
	LabelAppProtocol   = "io.devmesh.app-protocol"
	LabelPreferredPort = "io.devmesh.preferred-port"
	LabelHTTPHost      = "io.devmesh.http-host"

	ComposeProjectLabel = "com.docker.compose.project"
	ComposeServiceLabel = "com.docker.compose.service"
)

// Labels is the parsed devmesh container contract.
type Labels struct {
	Enabled       bool
	Name          string
	ContainerPort int
	Kind          string
	AppProtocol   string
	PreferredPort int
	HTTPHost      string
}

// ParseLabels validates the devmesh labels for a container. It returns
// Enabled=false (no error) for containers that are not devmesh-managed.
func ParseLabels(m map[string]string) (Labels, error) {
	raw, ok := m[LabelEnable]
	if !ok || !strings.EqualFold(raw, "true") {
		return Labels{}, nil
	}
	l := Labels{Enabled: true, Kind: "tcp"}

	l.Name = m[LabelName]
	if l.Name == "" {
		return Labels{}, fmt.Errorf("%s is required when %s=true", LabelName, LabelEnable)
	}

	portStr := m[LabelContainerPort]
	if portStr == "" {
		return Labels{}, fmt.Errorf("%s is required when %s=true", LabelContainerPort, LabelEnable)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return Labels{}, fmt.Errorf("invalid %s=%q", LabelContainerPort, portStr)
	}
	l.ContainerPort = port

	if kind := m[LabelKind]; kind != "" {
		if kind != "tcp" && kind != "http" {
			return Labels{}, fmt.Errorf("unsupported %s=%q", LabelKind, kind)
		}
		l.Kind = kind
	}
	l.AppProtocol = m[LabelAppProtocol]
	l.HTTPHost = m[LabelHTTPHost]
	if pp := m[LabelPreferredPort]; pp != "" {
		p, err := strconv.Atoi(pp)
		if err != nil || p <= 0 || p > 65535 {
			return Labels{}, fmt.Errorf("invalid %s=%q", LabelPreferredPort, pp)
		}
		l.PreferredPort = p
	}
	return l, nil
}

// OwnerKey builds a stable owner key that survives container recreation. It
// uses Compose project/service when available, then falls back to the
// container name. It never uses the container ID, which changes on recreate.
func OwnerKey(containerName string, labels map[string]string, containerPort int) string {
	project := labels[ComposeProjectLabel]
	service := labels[ComposeServiceLabel]
	if project != "" && service != "" {
		return fmt.Sprintf("docker:%s:%s:%d", project, service, containerPort)
	}
	name := strings.TrimPrefix(containerName, "/")
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("docker:%s:%d", name, containerPort)
}
