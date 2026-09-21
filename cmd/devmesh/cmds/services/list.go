// Package services implements the `devmesh services` command group.
package services

import (
	"context"
	"net"
	"strconv"
	"strings"

	"github.com/go-go-golems/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/go-go-golems/devmesh/internal/api"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
)

// ListSettings are the `services list` flags.
type ListSettings struct {
	NameFilter string `glazed:"name"`
	Status     string `glazed:"status"`
}

// ListCommand lists services known to devmeshd.
type ListCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = (*ListCommand)(nil)

// NewListCommand builds the list command.
func NewListCommand() (*ListCommand, error) {
	section, err := daemonconn.NewSection()
	if err != nil {
		return nil, err
	}
	return &ListCommand{CommandDescription: cmds.NewCommandDescription(
		"list",
		cmds.WithParents("services"),
		cmds.WithShort("List services known to devmeshd"),
		cmds.WithFlags(
			fields.New("name", fields.TypeString,
				fields.WithHelp("Only show services whose name contains this substring")),
			fields.New("status", fields.TypeString,
				fields.WithHelp("Only show services with this status (ready|unavailable)")),
		),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor emits one row per service.
func (c *ListCommand) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	settings := &ListSettings{}
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
	var resp api.ListResponse
	if err := client.Do(ctx, "GET", "/v1/services", nil, &resp); err != nil {
		return err
	}
	for _, svc := range resp.Services {
		if settings.NameFilter != "" && !strings.Contains(svc.Name, settings.NameFilter) {
			continue
		}
		if settings.Status != "" && svc.Status != settings.Status {
			continue
		}
		if err := gp.AddRow(ctx, types.NewRow(
			types.MRP("name", svc.Name),
			types.MRP("kind", svc.Kind),
			types.MRP("app_protocol", svc.AppProtocol),
			types.MRP("status", svc.Status),
			types.MRP("endpoint", EndpointOf(svc.Frontend)),
		)); err != nil {
			return err
		}
	}
	return nil
}

// Endpoint formats a frontend as host:port, or "" when unset.
func Endpoint(host string, port int) string {
	if port == 0 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// EndpointOf is the one endpoint formatter used by every command: it prefers
// the complete URL (HTTP services, including nondefault ports) and falls back
// to host:port for TCP frontends.
func EndpointOf(f api.FrontendDTO) string {
	if f.URL != "" {
		return f.URL
	}
	return Endpoint(f.Host, f.Port)
}
