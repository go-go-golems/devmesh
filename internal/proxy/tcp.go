// Package proxy implements the opaque TCP forwarder used by stable frontend
// listeners. It never inspects payload bytes.
package proxy

import (
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/wesen/devmesh/internal/registry"
)

// closeWriter is implemented by *net.TCPConn and used to half-close so that
// protocols relying on write shutdown (PostgreSQL, Redis, SMTP) behave.
type closeWriter interface{ CloseWrite() error }

// TCP forwards bytes between client and backend until both directions finish.
func TCP(client net.Conn, backend registry.Backend, dialTimeout time.Duration, logger *slog.Logger) {
	defer func() { _ = client.Close() }()

	upstream, err := net.DialTimeout("tcp", backend.Addr(), dialTimeout)
	if err != nil {
		logger.Warn("tcp_dial_failed", "backend", backend.Addr(), "error", err)
		return
	}
	defer func() { _ = upstream.Close() }()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		if cw, ok := upstream.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		if cw, ok := client.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}
