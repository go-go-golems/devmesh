package runtime

import (
	"log/slog"
	"sync"
	"time"

	"github.com/wesen/devmesh/internal/registry"
)

// Manager bridges registry metadata and live listeners. Runtime creation and
// removal is serialized by a single mutex, which is acceptable at local-dev
// scale and keeps per-name listener ownership unambiguous.
type Manager struct {
	mu              sync.Mutex
	runtimes        map[string]*ServiceRuntime
	alloc           *Allocator
	backendProvider func(name string) *registry.Backend
	logger          *slog.Logger
	dialTimeout     time.Duration
}

// NewManager builds a runtime manager.
func NewManager(alloc *Allocator, backendProvider func(name string) *registry.Backend, logger *slog.Logger, dialTimeout time.Duration) *Manager {
	return &Manager{
		runtimes:        map[string]*ServiceRuntime{},
		alloc:           alloc,
		backendProvider: backendProvider,
		logger:          logger,
		dialTimeout:     dialTimeout,
	}
}

// EnsureTCPRuntime returns the existing runtime for name or creates one,
// binding the frontend exactly once. It is idempotent.
func (m *Manager) EnsureTCPRuntime(name string, preferred int) (*ServiceRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.runtimes[name]; ok {
		return rt, nil
	}
	alloc, err := m.alloc.Allocate(name, preferred)
	if err != nil {
		return nil, err
	}
	rt := NewServiceRuntime(name, registry.Frontend{Host: m.alloc.host, Port: alloc.Port}, alloc.Listener, func() *registry.Backend {
		if m.backendProvider == nil {
			return nil
		}
		return m.backendProvider(name)
	}, m.logger, m.dialTimeout)
	rt.Start()
	m.runtimes[name] = rt
	return rt, nil
}

// Get returns the runtime for name.
func (m *Manager) Get(name string) (*ServiceRuntime, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.runtimes[name]
	return rt, ok
}

// RemoveRuntime closes and removes the runtime for name.
func (m *Manager) RemoveRuntime(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.runtimes[name]
	if ok {
		delete(m.runtimes, name)
	}
	if !ok {
		return false
	}
	_ = rt.Close()
	return true
}

// CloseAll closes every runtime.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	rts := make([]*ServiceRuntime, 0, len(m.runtimes))
	for name, rt := range m.runtimes {
		rts = append(rts, rt)
		delete(m.runtimes, name)
	}
	m.mu.Unlock()
	for _, rt := range rts {
		_ = rt.Close()
	}
}

// Names returns the current runtime names.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.runtimes))
	for name := range m.runtimes {
		out = append(out, name)
	}
	return out
}
