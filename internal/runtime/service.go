package runtime

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-go-golems/devmesh/internal/proxy"
	"github.com/go-go-golems/devmesh/internal/registry"
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

	connMu      sync.Mutex
	connections map[net.Conn]struct{}
	proxies     sync.WaitGroup
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
		connections:     map[net.Conn]struct{}{},
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
		if !rt.beginProxy(client) {
			<-rt.sem
			_ = client.Close()
			continue
		}
		go func(c net.Conn, b registry.Backend) {
			defer func() {
				rt.endProxy(c)
				<-rt.sem
			}()
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

func (rt *ServiceRuntime) beginProxy(conn net.Conn) bool {
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	if rt.closed.Load() {
		return false
	}
	rt.connections[conn] = struct{}{}
	rt.proxies.Add(1)
	return true
}

func (rt *ServiceRuntime) endProxy(conn net.Conn) {
	rt.connMu.Lock()
	delete(rt.connections, conn)
	rt.connMu.Unlock()
	rt.proxies.Done()
}

// Close stops the accept loop and closes the listener once. It prevents any
// new proxy worker from being admitted before Shutdown waits on the worker
// group, avoiding Add/Wait races.
func (rt *ServiceRuntime) Close() error {
	var err error
	rt.closeOnce.Do(func() {
		rt.connMu.Lock()
		rt.closed.Store(true)
		rt.connMu.Unlock()
		close(rt.done)
		err = rt.listener.Close()
	})
	return err
}

// CloseActiveConnections promptly closes accepted client sockets. proxy.TCP
// then closes its upstream connection through its existing defer path.
func (rt *ServiceRuntime) CloseActiveConnections() {
	rt.connMu.Lock()
	connections := make([]net.Conn, 0, len(rt.connections))
	for conn := range rt.connections {
		connections = append(connections, conn)
	}
	rt.connMu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

// WaitForProxies waits for accepted proxy workers to finish, or returns false
// when the caller's overall shutdown context expires.
func (rt *ServiceRuntime) WaitForProxies(ctx context.Context) bool {
	done := make(chan struct{})
	go func() {
		rt.proxies.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
