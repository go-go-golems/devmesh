package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-go-golems/devmesh/cmd/devmesh/cmds/daemonconn"
	"github.com/go-go-golems/devmesh/internal/api"
	"github.com/go-go-golems/devmesh/internal/transport"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
)

// ResolveSettings are the `services resolve` arguments.
type ResolveSettings struct {
	Name string `glazed:"name"`
	// Raw prints exactly the frontend endpoint (host:port or URL) on stdout
	// and nothing else, for shell substitution.
	Raw bool `glazed:"raw"`
	// Wait bounds how long to retry when the service is unknown or has no
	// backend yet. Empty means a single lookup.
	Wait string `glazed:"wait"`
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
		cmds.WithLong(`Resolve a logical service name to the stable endpoint consumers connect to.

--raw prints exactly the endpoint (host:port for tcp, URL for http) with no
table decoration, for shell substitution. --wait 20s retries unknown or
backendless services until the deadline. Resolution proves registration, not
application readiness: a registered database may still be initializing.`,
		),
		cmds.WithArguments(
			fields.New("name", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("Service name, e.g. checkout.postgres")),
		),
		cmds.WithFlags(
			fields.New("raw", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("Print only the endpoint, for shell substitution")),
			fields.New("wait", fields.TypeString,
				fields.WithHelp("Retry until the service is ready, e.g. 20s")),
		),
		cmds.WithSections(section),
	)}, nil
}

// RunIntoGlazeProcessor emits the resolved service row, or the raw endpoint
// when --raw is set.
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

	wait := time.Duration(0)
	if settings.Wait != "" {
		wait, err = time.ParseDuration(settings.Wait)
		if err != nil {
			return fmt.Errorf("invalid --wait duration %q: %w", settings.Wait, err)
		}
	}

	deadline := time.Now().Add(wait)
	for {
		var svc api.ServiceDTO
		err := client.Do(ctx, "GET", "/v1/services/"+settings.Name, nil, &svc)

		if err == nil {
			endpoint := Endpoint(svc.Frontend.Host, svc.Frontend.Port)
			if svc.Frontend.URL != "" {
				endpoint = svc.Frontend.URL
			}
			if svc.Status == "ready" || (wait == 0 && !settings.Raw) {
				if settings.Raw {
					// Raw output must never print an unavailable endpoint as
					// success; fail with a nonzero exit instead.
					if svc.Status != "ready" {
						return fmt.Errorf("service %s has no registered backend (status %s)", svc.Name, svc.Status)
					}
					fmt.Fprintln(os.Stdout, endpoint)
					return nil
				}
				return gp.AddRow(ctx, types.NewRow(
					types.MRP("name", svc.Name),
					types.MRP("kind", svc.Kind),
					types.MRP("status", svc.Status),
					types.MRP("endpoint", endpoint),
				))
			}
			// Registered without a backend: retry while a wait is active.
			if wait > 0 && time.Until(deadline) > 0 {
				if sleepCtx(ctx, retryInterval) {
					return ctx.Err()
				}
				continue
			}
			if settings.Raw {
				return fmt.Errorf("service %s has no registered backend (status %s)", svc.Name, svc.Status)
			}
			return gp.AddRow(ctx, types.NewRow(
				types.MRP("name", svc.Name),
				types.MRP("kind", svc.Kind),
				types.MRP("status", svc.Status),
				types.MRP("endpoint", endpoint),
			))
		}

		// A lookup error is retryable while a bounded wait is active: an
		// unknown service may appear, and the daemon may be restarting.
		if wait > 0 && time.Until(deadline) > 0 && retryableLookupError(err) {
			if sleepCtx(ctx, retryInterval) {
				return ctx.Err()
			}
			continue
		}
		return err
	}
}

const retryInterval = 250 * time.Millisecond

func retryableLookupError(err error) bool {
	var te *transport.Error
	if errors.As(err, &te) {
		return te.Status == 404 || te.Status >= 500
	}
	// Unreachable daemon (transport failure): retry while the deadline allows.
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}
