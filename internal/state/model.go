// Package state persists durable devmesh preferences: remembered frontend
// port assignments. Active registrations are never persisted.
package state

// version is the schema version this binary understands.
const version = 1

// Model is the on-disk state schema.
type Model struct {
	Version  int            `json:"version"`
	TCPPorts map[string]int `json:"tcp_ports"`
}
