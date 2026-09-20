// Package register implements `devmesh register`, a long-running manual
// registration that maintains its lease until interrupted.
package register

import (
	"context"
	"errors"
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
	"github.com/wesen/devmesh/internal/transport"
)

// Settings are the `register` flags.
type Settings struct {
	Name          string `glazed:"name"`
	Kind          string `glazed:"kind"`
	AppProtocol   string `glazed:"app-protocol"`
	Backend       string `glazed:"backend"`
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
		cmds.WithShort("Register a manual backend and keep its lease alive"),
		cmds.WithFlags(
			fields.New("name", fields.TypeString, fields.WithRequired(true), fields.WithHelp("Service name")),
			fields.New("backend", fields.TypeString, fields.WithRequired(true), fields.WithHelp("Backend host:port")),
			fields.New("kind", fields.TypeString, fields.WithDefault("tcp"), fields.WithHelp("Service kind")),
			fields.New("app-protocol", fields.TypeString, fields.WithHelp("Application protocol hint")),
			fields.New("preferred-port", fields.TypeInteger, fields.WithHelp("Desired stable frontend port")),
			fields.New("ttl", fields.TypeInteger, fields.WithDefault(15), fields.WithHelp("Lease TTL in seconds")),
			fields.New("once", fields.TypeBool, fields.WithDefault(false), fields.WithHelp("Register once and exit (no lease maintenance)")),
		),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor registers, emits a row, then heartbeats until canceled.
func (c *Command) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	settings := &Settings{}
	if err := parsed.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	ds := &daemonconn.Settings{}
	if err := parsed.DecodeSectionInto(daemonconn.Slug, ds); err != nil {
		return err
	}
	client, err := ds.Client()
	if err != nil {
		return err
	}

	host, portStr, err := net.SplitHostPort(settings.Backend)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return err
	}

	register := func() (api.RegisterResponse, error) {
		req := api.RegisterRequest{
			Name:        settings.Name,
			Kind:        settings.Kind,
			AppProtocol: settings.AppProtocol,
			Source:      "manual",
			Backend:     api.BackendDTO{Host: host, Port: port},
			TTLSeconds:  settings.TTLSeconds,
		}
		if settings.PreferredPort != 0 {
			req.PreferredPort = settings.PreferredPort
		}
		var resp api.RegisterResponse
		err := client.Do(ctx, "POST", "/v1/registrations", req, &resp)
		return resp, err
	}

	resp, err := register()
	if err != nil {
		return err
	}
	if err := gp.AddRow(ctx, types.NewRow(
		types.MRP("name", resp.Name),
		types.MRP("endpoint", net.JoinHostPort(resp.Frontend.Host, strconv.Itoa(resp.Frontend.Port))),
		types.MRP("registration_id", resp.RegistrationID),
		types.MRP("expires_at", resp.ExpiresAt),
	)); err != nil {
		return err
	}
	if settings.Once {
		return nil
	}

	ttl := time.Duration(settings.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	interval := ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	defer func() {
		delCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.DoAuth(delCtx, "DELETE", "/v1/registrations/"+resp.RegistrationID, resp.LeaseToken, nil, nil)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var hb api.HeartbeatResponse
			err := client.DoAuth(ctx, "POST", "/v1/registrations/"+resp.RegistrationID+"/heartbeat", resp.LeaseToken, nil, &hb)
			if err == nil {
				continue
			}
			// 404 -> daemon restarted and forgot us: re-register.
			var apiErr *transport.Error
			if errors.As(err, &apiErr) && apiErr.Status == 404 {
				newResp, rerr := register()
				if rerr != nil {
					continue
				}
				resp = newResp
				continue
			}
		}
	}
}
