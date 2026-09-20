package main

import (
	"fmt"
	"os"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	"github.com/spf13/cobra"

	"github.com/wesen/devmesh/cmd/devmeshd/cmds"
	"github.com/wesen/devmesh/pkg/doc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	rootCmd := &cobra.Command{
		Use:   "devmeshd",
		Short: "devmesh daemon: stable endpoint broker for local services",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logging.InitLoggerFromCobra(cmd)
		},
	}

	if err := logging.AddLoggingSectionToRootCommand(rootCmd, "devmeshd"); err != nil {
		return err
	}

	serveCmd, err := cli.BuildCobraCommand(cmds.NewServeCommand(),
		cli.WithParserConfig(cli.CobraParserConfig{AppName: "devmeshd"}))
	if err != nil {
		return err
	}
	rootCmd.AddCommand(serveCmd)

	helpSystem := help.NewHelpSystem()
	if err := doc.AddDocToHelpSystem(helpSystem); err != nil {
		return err
	}
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	return rootCmd.Execute()
}
