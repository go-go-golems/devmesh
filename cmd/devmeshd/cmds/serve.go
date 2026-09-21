// Package cmds wires the devmeshd daemon command tree. Configuration is owned
// by Glazed's middleware chain: defaults < config file < env < args < flags.
package cmds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	cmd_sources "github.com/go-go-golems/glazed/pkg/cmds/sources"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/spf13/cobra"

	"github.com/go-go-golems/devmesh/internal/api"
	"github.com/go-go-golems/devmesh/internal/config"
	"github.com/go-go-golems/devmesh/internal/daemon"
	"github.com/go-go-golems/devmesh/internal/transport"
)

// ServeSettings holds every daemon configuration field. Field names define the
// environment variables (DEVMESH_<NAME>, hyphens become underscores) and the
// config-file keys after FileMapper maps them.
type ServeSettings struct {
	Config                               string `glazed:"config"`
	Socket                               string `glazed:"socket"`
	TCPFrontendHost                      string `glazed:"tcp-frontend-host"`
	TCPFrontendMin                       int    `glazed:"tcp-frontend-min"`
	TCPFrontendMax                       int    `glazed:"tcp-frontend-max"`
	RuntimeIdleTTL                       string `glazed:"runtime-idle-ttl"`
	LeaseTTL                             string `glazed:"lease-ttl"`
	ShutdownTimeout                      string `glazed:"shutdown-timeout"`
	StatePath                            string `glazed:"state"`
	DockerEnabled                        bool   `glazed:"docker-enabled"`
	DockerAllowNonLoopbackPublishedPorts bool   `glazed:"docker-allow-non-loopback-published-ports"`
	HTTPEnabled                          bool   `glazed:"http-enabled"`
	HTTPAddr                             string `glazed:"http-addr"`
	HTTPSAddr                            string `glazed:"https-addr"`
	HTTPBaseDomain                       string `glazed:"http-base-domain"`
	HTTPCertFile                         string `glazed:"http-cert-file"`
	HTTPKeyFile                          string `glazed:"http-key-file"`
}

// ServeCommand runs the devmesh daemon.
type ServeCommand struct {
	*cmds.CommandDescription
}

var _ cmds.BareCommand = (*ServeCommand)(nil)

// NewServeCommand builds the serve command with Glazed field defaults.
func NewServeCommand() *ServeCommand {
	d := config.Default()
	return &ServeCommand{CommandDescription: cmds.NewCommandDescription(
		"serve",
		cmds.WithShort("Run the devmesh daemon"),
		cmds.WithFlags(
			fields.New("config", fields.TypeString, fields.WithHelp("Path to a JSON config file")),
			fields.New("socket", fields.TypeString, fields.WithHelp("Unix socket path (env DEVMESH_SOCKET)")),
			fields.New("tcp-frontend-host", fields.TypeString, fields.WithDefault(d.TCPFrontendHost), fields.WithHelp("Frontend bind host")),
			fields.New("tcp-frontend-min", fields.TypeInteger, fields.WithDefault(d.TCPFrontendMin), fields.WithHelp("Lowest stable frontend port")),
			fields.New("tcp-frontend-max", fields.TypeInteger, fields.WithDefault(d.TCPFrontendMax), fields.WithHelp("Highest stable frontend port")),
			fields.New("lease-ttl", fields.TypeString, fields.WithDefault(d.LeaseTTL.String()), fields.WithHelp("Default lease TTL")),
			fields.New("shutdown-timeout", fields.TypeString, fields.WithDefault(d.ShutdownTimeout.String()), fields.WithHelp("Graceful shutdown timeout")),
			fields.New("state", fields.TypeString, fields.WithHelp("State file path")),
			fields.New("docker-enabled", fields.TypeBool, fields.WithDefault(d.Docker.Enabled), fields.WithHelp("Enable the Docker watcher")),
			fields.New("docker-allow-non-loopback-published-ports", fields.TypeBool, fields.WithDefault(d.Docker.AllowNonLoopbackPublishedPorts), fields.WithHelp("Allow non-loopback Docker publications")),
			fields.New("http-enabled", fields.TypeBool, fields.WithDefault(d.HTTP.Enabled), fields.WithHelp("Enable the shared HTTP proxy listener")),
			fields.New("http-addr", fields.TypeString, fields.WithDefault(d.HTTP.HTTPAddr), fields.WithHelp("HTTP proxy listen address")),
			fields.New("https-addr", fields.TypeString, fields.WithDefault(d.HTTP.HTTPSAddr), fields.WithHelp("HTTPS proxy listen address")),
			fields.New("http-base-domain", fields.TypeString, fields.WithDefault(d.HTTP.BaseDomain), fields.WithHelp("Base domain for generated HTTP hostnames")),
			fields.New("http-cert-file", fields.TypeString, fields.WithHelp("PEM certificate for the HTTPS listener")),
			fields.New("http-key-file", fields.TypeString, fields.WithHelp("PEM private key for the HTTPS listener")),
		),
	)}
}

// ParserConfig returns the Glazed parser configuration for devmeshd. It enables
// env loading with the DEVMESH prefix and config-file loading through a custom
// mapper that preserves the devmesh JSON shape.
func ParserConfig() cli.CobraParserConfig {
	desc := NewServeCommand().Description()
	return cli.CobraParserConfig{
		AppName: "devmeshd",
		MiddlewaresFunc: func(_ *values.Values, cmd *cobra.Command, args []string) ([]cmd_sources.Middleware, error) {
			path, err := resolveConfigPath(desc, cmd)
			if err != nil {
				return nil, err
			}
			chain := []cmd_sources.Middleware{
				cmd_sources.FromCobra(cmd, fields.WithSource("cobra")),
				cmd_sources.FromArgs(args, fields.WithSource("arguments")),
				cmd_sources.FromEnv("DEVMESH", fields.WithSource("env")),
			}
			if path != "" {
				chain = append(chain, cmd_sources.FromFile(path,
					cmd_sources.WithConfigFileMapper(config.FileMapper),
					cmd_sources.WithParseOptions(fields.WithSource("config")),
				))
			}
			chain = append(chain, cmd_sources.FromDefaults(fields.WithSource(fields.SourceDefaults)))
			return chain, nil
		},
	}
}

// resolveConfigPath determines the config file path with flag > env precedence.
// The flag is checked on the Cobra command directly; when it is unset, Glazed's
// env source is executed in a throwaway pre-parse so DEVMESH_CONFIG can supply
// the path without any direct os.Getenv call in application code.
func resolveConfigPath(desc *cmds.CommandDescription, cmd *cobra.Command) (string, error) {
	if path, err := cmd.Flags().GetString("config"); err == nil && path != "" {
		return path, nil
	}

	parsed := values.New()
	if _, err := cmd_sources.ExecuteWithSchema(desc.Schema, parsed,
		cmd_sources.FromEnv("DEVMESH", fields.WithSource("env")),
	); err != nil {
		return "", err
	}
	var s struct {
		Config string `glazed:"config"`
	}
	if err := parsed.DecodeSectionInto(schema.DefaultSlug, &s); err != nil {
		return "", err
	}
	return s.Config, nil
}

// Run starts the daemon HTTP server over the Unix socket and blocks until ctx
// is canceled by a signal.
func (c *ServeCommand) Run(ctx context.Context, parsed *values.Values) error {
	settings := &ServeSettings{}
	if err := parsed.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}

	cfg, err := configFromSettings(settings)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	d, err := daemon.New(cfg, logger)
	if err != nil {
		return err
	}

	ln, err := transport.Listen(cfg.Socket)
	if err != nil {
		return err
	}
	defer transport.RemoveSocket(cfg.Socket)

	srv := api.NewServer(d, logger)
	httpSrv := &http.Server{Handler: srv.Handler()}

	d.Start(ctx)

	errCh := make(chan error, 1)
	go func() {
		if serr := httpSrv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			errCh <- serr
		}
	}()

	logger.Info("listening", "socket", cfg.Socket)

	select {
	case <-ctx.Done():
	case serr := <-errCh:
		_ = d.Shutdown(context.Background())
		return serr
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return d.Shutdown(shutdownCtx)
}

// configFromSettings converts decoded Glazed fields into the domain config.
// Malformed duration values are rejected rather than silently replaced with
// defaults.
func configFromSettings(s *ServeSettings) (config.Config, error) {
	def := config.Default()
	leaseTTL, err := parseDuration(s.LeaseTTL, def.LeaseTTL)
	if err != nil {
		return config.Config{}, fmt.Errorf("lease-ttl: %w", err)
	}
	shutdownTimeout, err := parseDuration(s.ShutdownTimeout, def.ShutdownTimeout)
	if err != nil {
		return config.Config{}, fmt.Errorf("shutdown-timeout: %w", err)
	}
	cfg := config.Config{
		Socket:          s.Socket,
		TCPFrontendHost: s.TCPFrontendHost,
		TCPFrontendMin:  s.TCPFrontendMin,
		TCPFrontendMax:  s.TCPFrontendMax,
		LeaseTTL:        leaseTTL,
		ShutdownTimeout: shutdownTimeout,
		StatePath:       s.StatePath,
		Docker: config.DockerConfig{
			Enabled:                        s.DockerEnabled,
			AllowNonLoopbackPublishedPorts: s.DockerAllowNonLoopbackPublishedPorts,
		},
		HTTP: config.HTTPConfig{
			Enabled:    s.HTTPEnabled,
			HTTPAddr:   s.HTTPAddr,
			HTTPSAddr:  s.HTTPSAddr,
			BaseDomain: s.HTTPBaseDomain,
			CertFile:   s.HTTPCertFile,
			KeyFile:    s.HTTPKeyFile,
		},
	}
	if cfg.Socket == "" {
		cfg.Socket = transport.DefaultSocketPath()
	}
	if cfg.StatePath == "" {
		cfg.StatePath = config.DefaultStatePath()
	}
	return cfg, nil
}

func parseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}
