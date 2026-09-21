// Package registry defines the core devmesh domain model: logical service
// names, kinds, backends, frontends, registrations, and the in-memory registry
// with ownership rules. It performs no network I/O.
package registry

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// Kind is the transport-level service kind.
type Kind string

const (
	KindTCP  Kind = "tcp"
	KindHTTP Kind = "http"
)

// ParseKind validates a wire kind value.
func ParseKind(s string) (Kind, error) {
	switch Kind(s) {
	case KindTCP, KindHTTP:
		return Kind(s), nil
	default:
		return "", fmt.Errorf("unsupported kind %q", s)
	}
}

// Source describes how a registration arrived.
type Source string

const (
	SourceProcess Source = "process"
	SourceDocker  Source = "docker"
	SourceManual  Source = "manual"
)

// Status is the externally visible service status.
type Status string

const (
	StatusReady       Status = "ready"
	StatusUnavailable Status = "unavailable"
)

// Backend is the current host:port a producer is listening on.
type Backend struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Addr returns the backend as a dialable host:port string.
func (b Backend) Addr() string {
	return net.JoinHostPort(b.Host, fmt.Sprintf("%d", b.Port))
}

// Frontend is the stable host:port (or URL) consumers connect to.
type Frontend struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"`
}

// Addr returns the frontend as a host:port string.
func (f Frontend) Addr() string {
	return net.JoinHostPort(f.Host, fmt.Sprintf("%d", f.Port))
}

// ServiceRecord is the registry's view of one logical service.
type ServiceRecord struct {
	Name        string
	Kind        Kind
	AppProtocol string
	OwnerKey    string
	// ProducerID identifies the current concrete publication: the lease
	// registration ID for process/manual sources, or the Docker container ID
	// for docker sources. Backend removals must match it to take effect, so a
	// stale event for a replaced producer is a no-op.
	ProducerID        string
	Source            Source
	Backend           *Backend
	Frontend          Frontend
	Hostname          string // HTTP kind only
	Status            Status
	DockerContainerID string
	UpdatedAt         time.Time
}

// Clone returns a deep copy safe to hand outside the registry lock.
func (r ServiceRecord) Clone() ServiceRecord {
	c := r
	if r.Backend != nil {
		b := *r.Backend
		c.Backend = &b
	}
	return c
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`)

// ValidateName enforces the devmesh service-name grammar and length bound.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("service name is empty")
	}
	if len(name) > 120 {
		return fmt.Errorf("service name exceeds 120 characters")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("service name %q must match %s", name, nameRe.String())
	}
	return nil
}

// ValidateBackend enforces the MVP loopback-only backend restriction.
func ValidateBackend(b Backend) error {
	if b.Port <= 0 || b.Port > 65535 {
		return fmt.Errorf("backend port %d out of range", b.Port)
	}
	if strings.TrimSpace(b.Host) == "" {
		return fmt.Errorf("backend host is empty")
	}
	if !IsLoopbackHost(b.Host) {
		return fmt.Errorf("backend host %q is not a loopback address", b.Host)
	}
	return nil
}

// IsLoopbackHost reports whether host is, or resolves exclusively to, a
// loopback address. The literal names "localhost" and "127.0.0.1"/"::1" are
// accepted; other names are resolved.
func IsLoopbackHost(host string) bool {
	h := strings.Trim(host, "[]")
	if h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	addrs, err := net.LookupHost(h)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}
