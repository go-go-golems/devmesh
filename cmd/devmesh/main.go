package main

import (
	"fmt"
	"os"

	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	"github.com/spf13/cobra"

	"github.com/go-go-golems/devmesh/cmd/devmesh/cmds"
	"github.com/go-go-golems/devmesh/pkg/doc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	rootCmd := &cobra.Command{
		Use:   "devmesh",
		Short: "Stable local dev endpoints for ephemeral services",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logging.InitLoggerFromCobra(cmd)
		},
	}

	if err := logging.AddLoggingSectionToRootCommand(rootCmd, "devmesh"); err != nil {
		return err
	}
	if err := cmds.AddCommands(rootCmd); err != nil {
		return err
	}

	helpSystem := help.NewHelpSystem()
	if err := doc.AddDocToHelpSystem(helpSystem); err != nil {
		return err
	}
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	return rootCmd.Execute()
}
