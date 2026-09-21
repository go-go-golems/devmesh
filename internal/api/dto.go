package api

import (
	"github.com/wesen/devmesh/internal/daemon"
	"github.com/wesen/devmesh/internal/registry"
)

// BackendDTO is the wire backend representation.
type BackendDTO struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// FrontendDTO is the wire frontend representation.
type FrontendDTO struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"`
}

// ServiceDTO is a consumer-facing service projection (no backend internals).
type ServiceDTO struct {
	Name        string      `json:"name"`
	Kind        string      `json:"kind"`
	AppProtocol string      `json:"app_protocol,omitempty"`
	Status      string      `json:"status"`
	Frontend    FrontendDTO `json:"frontend"`
}

// InspectDTO adds backend, source, and owner details for debugging.
type InspectDTO struct {
	Name              string      `json:"name"`
	Kind              string      `json:"kind"`
	AppProtocol       string      `json:"app_protocol,omitempty"`
	Status            string      `json:"status"`
	Frontend          FrontendDTO `json:"frontend"`
	Backend           *BackendDTO `json:"backend,omitempty"`
	Source            string      `json:"source,omitempty"`
	OwnerKey          string      `json:"owner_key,omitempty"`
	ProducerID        string      `json:"producer_id,omitempty"`
	DockerContainerID string      `json:"docker_container_id,omitempty"`
	Hostname          string      `json:"hostname,omitempty"`
}

// ListResponse wraps a service list.
type ListResponse struct {
	Services []ServiceDTO `json:"services"`
}

// HealthResponse is the /v1/health payload.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Docker  string `json:"docker"`
}

// RegisterRequest is the POST /v1/registrations body. Producer identity
// (owner key, registration ID, container ID) is daemon-issued and must not be
// supplied by callers; unknown fields are rejected by the JSON decoder.
type RegisterRequest struct {
	Name          string     `json:"name"`
	Kind          string     `json:"kind,omitempty"`
	AppProtocol   string     `json:"app_protocol,omitempty"`
	Source        string     `json:"source,omitempty"`
	Backend       BackendDTO `json:"backend"`
	PreferredPort int        `json:"preferred_port,omitempty"`
	TTLSeconds    int        `json:"ttl_seconds,omitempty"`
	HTTPHost      string     `json:"http_host,omitempty"`
}

// RegisterResponse is the POST /v1/registrations response.
type RegisterResponse struct {
	RegistrationID string      `json:"registration_id"`
	LeaseToken     string      `json:"lease_token,omitempty"`
	Name           string      `json:"name"`
	Frontend       FrontendDTO `json:"frontend"`
	ExpiresAt      string      `json:"expires_at,omitempty"`
	TTLSeconds     int         `json:"ttl_seconds,omitempty"`
}

// HeartbeatResponse is the heartbeat response.
type HeartbeatResponse struct {
	ExpiresAt string `json:"expires_at"`
}

func frontendDTO(f registry.Frontend) FrontendDTO {
	return FrontendDTO{Host: f.Host, Port: f.Port, URL: f.URL}
}

func serviceDTO(info daemon.ServiceInfo) ServiceDTO {
	return ServiceDTO{
		Name:        info.Name,
		Kind:        string(info.Kind),
		AppProtocol: info.AppProtocol,
		Status:      string(info.Status),
		Frontend:    frontendDTO(info.Frontend),
	}
}

func inspectDTO(info daemon.ServiceInfo) InspectDTO {
	dto := InspectDTO{
		Name:              info.Name,
		Kind:              string(info.Kind),
		AppProtocol:       info.AppProtocol,
		Status:            string(info.Status),
		Frontend:          frontendDTO(info.Frontend),
		Source:            string(info.Source),
		OwnerKey:          info.OwnerKey,
		ProducerID:        info.ProducerID,
		DockerContainerID: info.DockerContainerID,
		Hostname:          info.Hostname,
	}
	if info.Backend != nil {
		dto.Backend = &BackendDTO{Host: info.Backend.Host, Port: info.Backend.Port}
	}
	return dto
}
