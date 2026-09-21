package services

import (
	"context"

	"github.com/go-go-golems/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/go-go-golems/devmesh/internal/api"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
)

// InspectSettings are the `services inspect` arguments.
type InspectSettings struct {
	Name string `glazed:"name"`
}

// InspectCommand shows backend and owner details for a service.
type InspectCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = (*InspectCommand)(nil)

// NewInspectCommand builds the inspect command.
func NewInspectCommand() (*InspectCommand, error) {
	section, err := daemonconn.NewSection()
	if err != nil {
		return nil, err
	}
	return &InspectCommand{CommandDescription: cmds.NewCommandDescription(
		"inspect",
		cmds.WithParents("services"),
		cmds.WithShort("Show backend and owner details for a service"),
		cmds.WithArguments(
			fields.New("name", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("Service name, e.g. checkout.postgres")),
		),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor emits the inspect row.
func (c *InspectCommand) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	settings := &InspectSettings{}
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
	var svc api.InspectDTO
	if err := client.Do(ctx, "GET", "/v1/services/"+settings.Name+"/inspect", nil, &svc); err != nil {
		return err
	}
	backend := ""
	if svc.Backend != nil {
		backend = Endpoint(svc.Backend.Host, svc.Backend.Port)
	}
	return gp.AddRow(ctx, types.NewRow(
		types.MRP("name", svc.Name),
		types.MRP("kind", svc.Kind),
		types.MRP("status", svc.Status),
		types.MRP("frontend", EndpointOf(svc.Frontend)),
		types.MRP("backend", backend),
		types.MRP("source", svc.Source),
		types.MRP("owner_key", svc.OwnerKey),
		types.MRP("producer_id", svc.ProducerID),
		types.MRP("docker_container_id", svc.DockerContainerID),
	))
}
