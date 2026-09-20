package services

import (
	"context"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/wesen/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/wesen/devmesh/internal/api"
)

// ResolveSettings are the `services resolve` arguments.
type ResolveSettings struct {
	Name string `glazed:"name"`
}

// ResolveCommand resolves a service name to its stable frontend.
type ResolveCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = (*ResolveCommand)(nil)

// NewResolveCommand builds the resolve command.
func NewResolveCommand() (*ResolveCommand, error) {
	section, err := daemonconn.NewSection()
	if err != nil {
		return nil, err
	}
	return &ResolveCommand{CommandDescription: cmds.NewCommandDescription(
		"resolve",
		cmds.WithParents("services"),
		cmds.WithShort("Resolve a service name to its stable frontend endpoint"),
		cmds.WithArguments(
			fields.New("name", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("Service name, e.g. checkout.postgres")),
		),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor emits the resolved service row.
func (c *ResolveCommand) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	settings := &ResolveSettings{}
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
	var svc api.ServiceDTO
	if err := client.Do(ctx, "GET", "/v1/services/"+settings.Name, nil, &svc); err != nil {
		return err
	}
	return gp.AddRow(ctx, types.NewRow(
		types.MRP("name", svc.Name),
		types.MRP("kind", svc.Kind),
		types.MRP("status", svc.Status),
		types.MRP("endpoint", Endpoint(svc.Frontend.Host, svc.Frontend.Port)),
	))
}
