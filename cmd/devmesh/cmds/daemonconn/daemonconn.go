// Package daemonconn holds the shared Glazed section and HTTP client used by
// every devmesh CLI command that talks to devmeshd.
package daemonconn

import (
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/wesen/devmesh/internal/transport"
)

// Slug is the Glazed section slug for daemon connection settings.
const Slug = "daemon"

// Settings decodes the shared daemon section.
type Settings struct {
	Socket  string `glazed:"socket"`
	Timeout string `glazed:"timeout"`
}

// NewSection builds the reusable daemon-connection section.
func NewSection() (*schema.SectionImpl, error) {
	return schema.NewSection(
		Slug,
		"Daemon Connection",
		schema.WithFields(
			fields.New("socket", fields.TypeString,
				fields.WithHelp("Path to the devmeshd Unix socket (env DEVMESH_SOCKET; default ~/.devmesh/run/devmesh.sock)")),
			fields.New("timeout", fields.TypeString,
				fields.WithDefault("5s"),
				fields.WithHelp("Per-request HTTP timeout")),
		),
	)
}

// Client builds an HTTP-over-Unix-socket client from the settings.
func (s Settings) Client() (*transport.Client, error) {
	timeout := 5 * time.Second
	if s.Timeout != "" {
		if d, err := time.ParseDuration(s.Timeout); err == nil {
			timeout = d
		}
	}
	socket := s.Socket
	if socket == "" {
		socket = transport.DefaultSocketPath()
	}
	return transport.NewClient(socket, timeout), nil
}
