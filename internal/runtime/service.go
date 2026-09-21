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

	listener        net.Listener
	backendProvider func() *registry.Backend
	logger          *slog.Logger
	dialTimeout     time.Duration
	sem             chan struct{}
	closed          atomic.Bool
	closeOnce       sync.Once
	done            chan struct{}
}

// NewServiceRuntime builds a runtime with a bound listener. It does not start
// accepting until Start is called.
func NewServiceRuntime(name string, frontend registry.Frontend, ln net.Listener, backendProvider func() *registry.Backend, logger *slog.Logger, dialTimeout time.Duration) *ServiceRuntime {
	rt := &ServiceRuntime{
		Name:            name,
		Frontend:        frontend,
		listener:        ln,
		backendProvider: backendProvider,
		logger:          logger.With("service", name),
		dialTimeout:     dialTimeout,
		sem:             make(chan struct{}, 4096),
		done:            make(chan struct{}),
	}
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

// CurrentBackend obtains one immutable backend snapshot from the registry.
// The registry is the single authoritative routing state; TCP runtimes own
// listeners but do not keep another mutable backend pointer.
func (rt *ServiceRuntime) CurrentBackend() *registry.Backend {
	if rt.backendProvider == nil {
		return nil
	}
	b := rt.backendProvider()
	if b == nil {
		return nil
	}
	c := *b
	return &c
}

// Endpoint returns the stable frontend host:port.
func (rt *ServiceRuntime) Endpoint() string { return rt.Frontend.Addr() }

// Listener exposes the bound listener.
func (rt *ServiceRuntime) Listener() net.Listener { return rt.listener }

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
