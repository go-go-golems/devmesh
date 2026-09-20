package runtime

import (
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wesen/devmesh/internal/proxy"
	"github.com/wesen/devmesh/internal/registry"
)

// ServiceRuntime owns exactly one bound frontend listener and the current
// backend pointer for a logical service. Existing connections keep the backend
// they dialed; only new accepts read the new pointer.
type ServiceRuntime struct {
	Name     string
	Frontend registry.Frontend

	listener    net.Listener
	backend     atomic.Pointer[registry.Backend]
	logger      *slog.Logger
	dialTimeout time.Duration
	sem         chan struct{}
	closed      atomic.Bool
	closeOnce   sync.Once
	done        chan struct{}
	lastActive  atomic.Int64
}

// NewServiceRuntime builds a runtime with a bound listener. It does not start
// accepting until Start is called.
func NewServiceRuntime(name string, frontend registry.Frontend, ln net.Listener, logger *slog.Logger, dialTimeout time.Duration) *ServiceRuntime {
	rt := &ServiceRuntime{
		Name:        name,
		Frontend:    frontend,
		listener:    ln,
		logger:      logger.With("service", name),
		dialTimeout: dialTimeout,
		sem:         make(chan struct{}, 4096),
		done:        make(chan struct{}),
	}
	rt.Touch()
	return rt
}

// Start launches the accept loop.
func (rt *ServiceRuntime) Start() {
	go rt.acceptLoop()
}

func (rt *ServiceRuntime) acceptLoop() {
	for {
		client, err := rt.listener.Accept()
		if err != nil {
			if rt.closed.Load() {
				return
			}
			select {
			case <-rt.done:
				return
			default:
			}
			rt.logger.Warn("accept_failed", "error", err)
			continue
		}
		backend := rt.CurrentBackend()
		if backend == nil {
			_ = client.Close()
			continue
		}
		select {
		case rt.sem <- struct{}{}:
		default:
			_ = client.Close()
			continue
		}
		go func(c net.Conn, b registry.Backend) {
			defer func() { <-rt.sem }()
			proxy.TCP(c, b, rt.dialTimeout, rt.logger)
		}(client, *backend)
	}
}

// SetBackend atomically replaces the backend pointer.
func (rt *ServiceRuntime) SetBackend(b registry.Backend) {
	rt.backend.Store(&b)
	rt.Touch()
}

// ClearBackend removes the backend while keeping the listener bound.
func (rt *ServiceRuntime) ClearBackend() {
	rt.backend.Store(nil)
	rt.Touch()
}

// CurrentBackend returns a copy of the current backend, or nil.
func (rt *ServiceRuntime) CurrentBackend() *registry.Backend {
	if b := rt.backend.Load(); b != nil {
		c := *b
		return &c
	}
	return nil
}

// Endpoint returns the stable frontend host:port.
func (rt *ServiceRuntime) Endpoint() string { return rt.Frontend.Addr() }

// Listener exposes the bound listener.
func (rt *ServiceRuntime) Listener() net.Listener { return rt.listener }

// Touch records activity time.
func (rt *ServiceRuntime) Touch() { rt.lastActive.Store(time.Now().UnixNano()) }

// LastActive returns the last activity time.
func (rt *ServiceRuntime) LastActive() time.Time {
	return time.Unix(0, rt.lastActive.Load())
}

// Idle reports whether the runtime has been inactive longer than ttl.
func (rt *ServiceRuntime) Idle(ttl time.Duration) bool {
	return time.Since(rt.LastActive()) > ttl
}

// Close stops the accept loop and closes the listener once.
func (rt *ServiceRuntime) Close() error {
	var err error
	rt.closeOnce.Do(func() {
		rt.closed.Store(true)
		close(rt.done)
		err = rt.listener.Close()
	})
	return err
}
