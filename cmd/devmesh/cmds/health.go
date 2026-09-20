package cmds

import (
	"context"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/wesen/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/wesen/devmesh/internal/api"
)

// HealthCommand reports daemon health.
type HealthCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = (*HealthCommand)(nil)

// NewHealthCommand builds the health command.
func NewHealthCommand() (*HealthCommand, error) {
	section, err := daemonconn.NewSection()
	if err != nil {
		return nil, err
	}
	return &HealthCommand{CommandDescription: cmds.NewCommandDescription(
		"health",
		cmds.WithShort("Check devmeshd health"),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor emits the health row.
func (c *HealthCommand) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	ds := &daemonconn.Settings{}
	if err := parsed.DecodeSectionInto(daemonconn.Slug, ds); err != nil {
		return err
	}
	client, err := ds.Client()
	if err != nil {
		return err
	}
	var health api.HealthResponse
	if err := client.Do(ctx, "GET", "/v1/health", nil, &health); err != nil {
		return err
	}
	return gp.AddRow(ctx, types.NewRow(
		types.MRP("status", health.Status),
		types.MRP("version", health.Version),
		types.MRP("docker", health.Docker),
	))
}
