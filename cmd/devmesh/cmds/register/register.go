// Package register implements `devmesh register`, a long-running manual
// registration that maintains its lease until interrupted. The lease
// lifecycle is owned by the shared producer handle in pkg/devmesh so the CLI
// and the Go SDK cannot drift apart.
package register

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/wesen/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/wesen/devmesh/internal/api"
	devmesh "github.com/wesen/devmesh/pkg/devmesh"
)

// Settings are the `register` flags.
type Settings struct {
	Name          string `glazed:"name"`
	Kind          string `glazed:"kind"`
	AppProtocol   string `glazed:"app-protocol"`
	Backend       string `glazed:"backend"`
	HTTPHost      string `glazed:"http-host"`
	PreferredPort int    `glazed:"preferred-port"`
	TTLSeconds    int    `glazed:"ttl"`
	Once          bool   `glazed:"once"`
}

// Command registers a manual backend and keeps the lease alive.
type Command struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = (*Command)(nil)

// NewCommand builds the register command.
func NewCommand() (*Command, error) {
	section, err := daemonconn.NewSection()
	if err != nil {
		return nil, err
	}
	return &Command{CommandDescription: cmds.NewCommandDescription(
		"register",
		cmds.WithShort("Register a manual backend and keep its lease alive until interrupted"),
		cmds.WithLong(`Register a manual backend under a logical service name.

The backend must already be listening. The command keeps the registration's
lease alive (heartbeating and re-registering after daemon restarts) until it
is interrupted, then unregisters. Use --once for a diagnostic registration that
is not renewed and expires after its TTL. Scripts that need the stable
endpoint while this command runs should use "devmesh services resolve --raw"
instead of parsing this command's output while it is still running.`,
		),
		cmds.WithFlags(
			fields.New("name", fields.TypeString, fields.WithRequired(true), fields.WithHelp("Service name")),
			fields.New("backend", fields.TypeString, fields.WithRequired(true), fields.WithHelp("Backend host:port (must already be listening)")),
			fields.New("kind", fields.TypeString, fields.WithDefault("tcp"), fields.WithHelp("Service kind (tcp|http)")),
			fields.New("app-protocol", fields.TypeString, fields.WithHelp("Application protocol hint")),
			fields.New("http-host", fields.TypeString, fields.WithHelp("Explicit hostname for kind=http services")),
			fields.New("preferred-port", fields.TypeInteger, fields.WithHelp("Desired stable frontend port")),
			fields.New("ttl", fields.TypeInteger, fields.WithDefault(15), fields.WithHelp("Lease TTL in seconds (3-3600)")),
			fields.New("once", fields.TypeBool, fields.WithDefault(false), fields.WithHelp("Register once and exit (no lease maintenance; expires after ttl)")),
		),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor registers, emits a row, then keeps the lease alive
// until the command context is canceled.
func (c *Command) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	settings := &Settings{}
	if err := parsed.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	ds := &daemonconn.Settings{}
	if err := parsed.DecodeSectionInto(daemonconn.Slug, ds); err != nil {
		return err
	}

	kind := devmesh.Kind(settings.Kind)

	if settings.Once {
		client, err := ds.Client()
		if err != nil {
			return err
		}
		return registerOnce(ctx, client, settings, kind, gp)
	}

	handle, err := devmesh.Register(ctx, devmesh.RegistrationOptions{
		Name:          settings.Name,
		Kind:          kind,
		AppProtocol:   settings.AppProtocol,
		Backend:       settings.Backend,
		HTTPHost:      settings.HTTPHost,
		PreferredPort: settings.PreferredPort,
		TTLSeconds:    settings.TTLSeconds,
		Manual:        true,
		Socket:        ds.Socket,
	})
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	if err := emitRow(ctx, gp, handle.Info()); err != nil {
		return err
	}

	// Keep the lease alive until interrupted (the command context is canceled
	// by signal handling), then let the deferred Close unregister.
	<-ctx.Done()
	return nil
}

// registerOnce performs a single diagnostic registration without lease
// maintenance. It expires after its TTL.
func registerOnce(ctx context.Context, client interface {
	Do(ctx context.Context, method, path string, in, out any) error
}, settings *Settings, kind devmesh.Kind, gp middlewares.Processor) error {
	host, portStr, err := net.SplitHostPort(settings.Backend)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return err
	}
	req := api.RegisterRequest{
		Name:        settings.Name,
		Kind:        string(kind),
		AppProtocol: settings.AppProtocol,
		Source:      "manual",
		Backend:     api.BackendDTO{Host: host, Port: port},
		HTTPHost:    settings.HTTPHost,
		TTLSeconds:  settings.TTLSeconds,
	}
	if settings.PreferredPort != 0 {
		req.PreferredPort = settings.PreferredPort
	}
	var resp api.RegisterResponse
	if err := client.Do(ctx, "POST", "/v1/registrations", req, &resp); err != nil {
		return err
	}
	endpoint := resp.Frontend.URL
	if endpoint == "" {
		endpoint = net.JoinHostPort(resp.Frontend.Host, strconv.Itoa(resp.Frontend.Port))
	}
	return gp.AddRow(ctx, types.NewRow(
		types.MRP("name", resp.Name),
		types.MRP("endpoint", endpoint),
		types.MRP("registration_id", resp.RegistrationID),
		types.MRP("expires_at", resp.ExpiresAt),
	))
}

func emitRow(ctx context.Context, gp middlewares.Processor, info devmesh.RegistrationInfo) error {
	expires := ""
	if !info.ExpiresAt.IsZero() {
		expires = info.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return gp.AddRow(ctx, types.NewRow(
		types.MRP("name", info.Name),
		types.MRP("endpoint", info.Endpoint),
		types.MRP("registration_id", info.RegistrationID),
		types.MRP("expires_at", expires),
	))
}
