// Package cmds wires the devmeshd daemon command tree.
package cmds

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"

	"github.com/wesen/devmesh/internal/api"
	"github.com/wesen/devmesh/internal/config"
	"github.com/wesen/devmesh/internal/daemon"
	"github.com/wesen/devmesh/internal/transport"
)

// ServeSettings are the devmeshd serve flags.
type ServeSettings struct {
	Config      string `glazed:"config"`
	Socket      string `glazed:"socket"`
	FrontendMin int    `glazed:"frontend-min"`
	FrontendMax int    `glazed:"frontend-max"`
	StatePath   string `glazed:"state"`
	LeaseTTL    string `glazed:"lease-ttl"`
	Docker      bool   `glazed:"docker"`
}

// ServeCommand runs the devmesh daemon.
type ServeCommand struct {
	*cmds.CommandDescription
}

var _ cmds.BareCommand = (*ServeCommand)(nil)

// NewServeCommand builds the serve command.
func NewServeCommand() *ServeCommand {
	return &ServeCommand{CommandDescription: cmds.NewCommandDescription(
		"serve",
		cmds.WithShort("Run the devmesh daemon"),
		cmds.WithFlags(
			fields.New("config", fields.TypeString, fields.WithHelp("Path to a JSON config file")),
			fields.New("socket", fields.TypeString, fields.WithHelp("Override the Unix socket path")),
			fields.New("frontend-min", fields.TypeInteger, fields.WithDefault(15000), fields.WithHelp("Lowest stable frontend port")),
			fields.New("frontend-max", fields.TypeInteger, fields.WithDefault(19999), fields.WithHelp("Highest stable frontend port")),
			fields.New("state", fields.TypeString, fields.WithHelp("Override the state file path")),
			fields.New("lease-ttl", fields.TypeString, fields.WithDefault("15s"), fields.WithHelp("Lease TTL")),
			fields.New("docker", fields.TypeBool, fields.WithDefault(true), fields.WithHelp("Enable the Docker watcher")),
		),
	)}
}

// Run starts the daemon HTTP server over the Unix socket and blocks until ctx
// is canceled by a signal.
func (c *ServeCommand) Run(ctx context.Context, parsed *values.Values) error {
	settings := &ServeSettings{}
	if err := parsed.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}

	cfg, err := config.Load(settings.Config)
	if err != nil {
		return err
	}
	if settings.Socket != "" {
		cfg.Socket = settings.Socket
	}
	if cfg.Socket == "" {
		cfg.Socket = transport.DefaultSocketPath()
	}
	if settings.FrontendMin != 0 {
		cfg.TCPFrontendMin = settings.FrontendMin
	}
	if settings.FrontendMax != 0 {
		cfg.TCPFrontendMax = settings.FrontendMax
	}
	if settings.StatePath != "" {
		cfg.StatePath = settings.StatePath
	}
	if cfg.StatePath == "" {
		cfg.StatePath = config.DefaultStatePath()
	}
	if settings.LeaseTTL != "" {
		if d, derr := time.ParseDuration(settings.LeaseTTL); derr == nil {
			cfg.LeaseTTL = d
		}
	}
	cfg.Docker.Enabled = settings.Docker

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
