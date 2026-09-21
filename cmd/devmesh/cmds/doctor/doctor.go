// Package doctor implements `devmesh doctor`, emitting one structured row per
// diagnostic check so it serves both humans and CI.
package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/wesen/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/wesen/devmesh/internal/api"
	"github.com/wesen/devmesh/internal/dockerwatch"
	"github.com/wesen/devmesh/internal/transport"
)

// Check is one diagnostic result.
type Check struct {
	Name   string
	Status string // ok | fail | skip
	Detail string
}

// Command runs diagnostics.
type Command struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = (*Command)(nil)

// NewCommand builds the doctor command.
func NewCommand() (*Command, error) {
	section, err := daemonconn.NewSection()
	if err != nil {
		return nil, err
	}
	return &Command{CommandDescription: cmds.NewCommandDescription(
		"doctor",
		cmds.WithShort("Diagnose devmesh, Docker, and port configuration"),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor emits one row per check.
func (c *Command) RunIntoGlazeProcessor(ctx context.Context, parsed *values.Values, gp middlewares.Processor) error {
	ds := &daemonconn.Settings{}
	if err := parsed.DecodeSectionInto(daemonconn.Slug, ds); err != nil {
		return err
	}
	client, err := ds.Client()
	if err != nil {
		return err
	}

	socket := ds.Socket
	if socket == "" {
		socket = transport.DefaultSocketPath()
	}
	checks := runChecks(ctx, client, socket)
	failed := false
	for _, chk := range checks {
		if chk.Status == "fail" {
			failed = true
		}
		if err := gp.AddRow(ctx, types.NewRow(
			types.MRP("check", chk.Name),
			types.MRP("status", chk.Status),
			types.MRP("detail", chk.Detail),
		)); err != nil {
			return err
		}
	}
	if failed {
		return fmt.Errorf("doctor found failing checks")
	}
	return nil
}

func runChecks(ctx context.Context, client *transport.Client, socket string) []Check {
	var checks []Check

	// Daemon reachability + Docker status.
	var health api.HealthResponse
	if err := client.Do(ctx, "GET", "/v1/health", nil, &health); err != nil {
		checks = append(checks, Check{"daemon-reachable", "fail", err.Error()})
	} else {
		checks = append(checks, Check{"daemon-reachable", "ok", "version " + health.Version})
		switch health.Docker {
		case "connected":
			checks = append(checks, Check{"docker", "ok", "connected"})
		case "disabled":
			checks = append(checks, Check{"docker", "skip", "disabled by configuration"})
		default:
			checks = append(checks, Check{"docker", "fail", "status: " + health.Docker})
		}
		if health.Docker != "disabled" {
			checks = append(checks, probeDockerPublication(ctx)...)
		}
	}

	// Loopback binding.
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err != nil {
		checks = append(checks, Check{"loopback-bind", "fail", err.Error()})
	} else {
		_ = ln.Close()
		checks = append(checks, Check{"loopback-bind", "ok", "bound " + ln.Addr().String()})
	}

	// State/config directory writable.
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		checks = append(checks, Check{"socket-dir", "fail", err.Error()})
	} else if f, err := os.CreateTemp(dir, ".doctor-*"); err != nil {
		checks = append(checks, Check{"socket-dir", "fail", err.Error()})
	} else {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		checks = append(checks, Check{"socket-dir", "ok", dir + " writable"})
	}

	return checks
}

// probeDockerPublication verifies loopback-only ephemeral host-port publishing
// with a disposable container. It never pulls an image.
func probeDockerPublication(ctx context.Context) []Check {
	api, err := dockerwatch.NewClient()
	if err != nil {
		return []Check{{"docker-publication-probe", "fail", err.Error()}}
	}
	defer func() { _ = api.Close() }()

	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := dockerwatch.ProbeLoopbackPublication(probeCtx, api)
	switch {
	case err != nil:
		return []Check{{"docker-publication-probe", "fail", err.Error()}}
	case res.Skipped:
		return []Check{{"docker-publication-probe", "skip", res.Detail}}
	case res.Supported:
		return []Check{{"docker-publication-probe", "ok", res.Detail}}
	default:
		return []Check{{"docker-publication-probe", "fail", res.Detail + "; devmesh will not auto-register containers exposed beyond loopback"}}
	}
}
