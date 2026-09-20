// Package cmds wires the devmesh CLI command tree.
package cmds

import (
	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/spf13/cobra"

	"github.com/wesen/devmesh/cmd/devmesh/cmds/doctor"
	"github.com/wesen/devmesh/cmd/devmesh/cmds/register"
	"github.com/wesen/devmesh/cmd/devmesh/cmds/services"
)

// AddCommands builds and mounts every devmesh CLI command on root.
// All commands are built before any is mounted, so a schema/flag collision
// cannot leave a partially mutated tree.
func AddCommands(root *cobra.Command) error {
	listCmd, err := services.NewListCommand()
	if err != nil {
		return err
	}
	resolveCmd, err := services.NewResolveCommand()
	if err != nil {
		return err
	}
	inspectCmd, err := services.NewInspectCommand()
	if err != nil {
		return err
	}
	registerCmd, err := register.NewCommand()
	if err != nil {
		return err
	}
	doctorCmd, err := doctor.NewCommand()
	if err != nil {
		return err
	}
	healthCmd, err := NewHealthCommand()
	if err != nil {
		return err
	}

	commands := []cmds.Command{
		listCmd,
		resolveCmd,
		inspectCmd,
		registerCmd,
		doctorCmd,
		healthCmd,
	}
	return cli.AddCommandsToRootCommand(root, commands, nil,
		cli.WithParserConfig(cli.CobraParserConfig{AppName: "devmesh"}))
}
