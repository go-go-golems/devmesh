package devmesh

import (
	"context"
	"net"
)

// ListenerHandle is an application listener plus its devmesh registration.
type ListenerHandle struct {
	net.Listener
	Registration Handle
}

// Close closes the application listener first, then unregisters.
func (l *ListenerHandle) Close() error {
	err := l.Listener.Close()
	if l.Registration != nil {
		_ = l.Registration.Close()
	}
	return err
}

// ListenTCP atomically binds an ephemeral loopback backend port and registers
// it under name. The application serves on the returned listener; consumers
// connect to Registration.Endpoint().
func ListenTCP(ctx context.Context, name string, preferredPort int) (*ListenerHandle, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	h, err := Register(ctx, RegistrationOptions{
		Name:          name,
		Kind:          KindTCP,
		Backend:       ln.Addr().String(),
		PreferredPort: preferredPort,
	})
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &ListenerHandle{Listener: ln, Registration: h}, nil
}
