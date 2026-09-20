// Package transport provides HTTP-over-Unix-socket plumbing for the devmesh
// administrative API: socket path resolution, safe stale-socket handling, and
// a matching HTTP client.
package transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// DefaultSocketPath resolves the preferred socket path from the user's home
// directory. Environment overrides are handled by Glazed's env middleware at
// the command layer (DEVMESH_SOCKET for the socket field), so this package does
// not read the environment itself.
func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "devmesh.sock")
	}
	return filepath.Join(home, ".devmesh", "run", "devmesh.sock")
}

// Listen binds an HTTP-over-Unix listener at socketPath. If a socket already
// exists it is probed first: a successful connection means another daemon is
// alive (error), a failed connection means the socket is stale and is removed.
func Listen(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	if _, err := os.Stat(socketPath); err == nil {
		conn, derr := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("another devmeshd is already listening on %s", socketPath)
		}
		if rerr := os.Remove(socketPath); rerr != nil {
			return nil, fmt.Errorf("remove stale socket: %w", rerr)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return ln, nil
}

// RemoveSocket removes the socket file, ignoring absence.
func RemoveSocket(socketPath string) {
	_ = os.Remove(socketPath)
}
