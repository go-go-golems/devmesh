# devmesh

`devmesh` gives development services stable, discoverable endpoints even when
the underlying process or container uses an arbitrary ephemeral port.

A logical service name owns a stable **frontend** (`127.0.0.1:15432`); producers
publish ephemeral **backends** (`127.0.0.1:49173`, selected by Docker or the
kernel); consumers resolve the name and always reach the frontend.

## Components

| Piece | Path | Responsibility |
| --- | --- | --- |
| Daemon | `cmd/devmeshd` | Registry, frontend listeners, TCP proxying, persistence, local API |
| CLI | `cmd/devmesh` | `services list/resolve/inspect`, `register`, `doctor`, `health` |
| Go client | `pkg/devmesh` | Bind `:0`, register the actual port, keep the lease alive |
| Docker adapter | `internal/dockerwatch` | Watch labeled containers and register published ports |

The CLI and help system are built with
[Glazed](https://github.com/go-go-golems/glazed): every command emits structured
output (`--format table|json|jsonl|csv|tsv|yaml`, `--output-fields`,
`--max-output-rows`) and help pages are embedded markdown.

## Install

Published releases provide both the `devmesh` client and the `devmeshd` daemon.

```bash
brew tap go-go-golems/go-go-go
brew install --cask devmesh
```

Or install the current Go module directly:

```bash
go install github.com/go-go-golems/devmesh/cmd/devmesh@latest
go install github.com/go-go-golems/devmesh/cmd/devmeshd@latest
```

## Quick start

```bash
make build

# start the daemon
./dist/devmeshd serve

# in another shell
./dist/devmesh health
./dist/devmesh services list

# register a manual backend for debugging
./dist/devmesh register --name test.echo --backend 127.0.0.1:25999 --once
./dist/devmesh services resolve test.echo
```

## Scripts and managed services

A backend must already be listening before it is registered. For a persistent
manual registration, leave `register` running; it maintains the lease and
re-registers after a daemon restart. `--once` is only a diagnostic registration
that expires after its TTL.

```bash
# A shell consumer gets exactly one endpoint line and waits at most 20 seconds
# for a producer to publish a ready backend. This proves registration, not that
# a database or HTTP application has completed its own readiness sequence.
ENDPOINT=$(./dist/devmesh services resolve checkout.postgres --raw --wait 20s)
export DATABASE_URL="postgres://dev:dev@${ENDPOINT}/app"
```

For HTTP registrations, pass an explicit `--http-host`; resolve returns the
complete URL including a configured non-default port. One hostname belongs to
one service during a daemon run. Listener frontends stay reserved until daemon
shutdown; devmesh does not evict inactive listeners automatically.

For a working Compose database → devmesh → devctl consumer example, see
[`examples/devctl-compose-postgres/`](examples/devctl-compose-postgres/). It
uses an ordinary devctl plugin and a shell launcher; no devctl core schema or
second Docker registration path is required.

## Development

```bash
make test            # unit and integration tests
make test-race       # race-enabled unit and integration tests
make lint            # pinned golangci-lint + glazed-lint
make logcopter-check # generated logging metadata is current
make build           # all packages
make build-bin       # dist/devmesh and dist/devmeshd
```

## Design

See the intern guide at
`ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/design-doc/01-devmesh-intern-analysis-and-implementation-guide.md`
and the source specification `devmesh-implementation-guide.md`.

### Invariants

1. Producer-owned backends: the app or Docker chooses the backend port.
2. Devmesh-owned frontends: frontends are allocated by binding, not checking.
3. Stable identity: names survive backend churn and daemon restarts.
4. Docker is an adapter, not the core.
5. Generic TCP is host+port; only HTTP can be multiplexed by hostname.

## Releases

Version tags matching `v*` use GoReleaser to build static Linux and macOS
archives containing both binaries. The release workflow has split build jobs
with a build-only Vault role and a final shared publication job. The latter
uses the caller repository `GITHUB_TOKEN` for its GitHub release and a
short-lived GitHub App token for the Homebrew tap; no long-lived tap token is
stored in this repository.

The initial release requires the reviewed Terraform application of the
`release-devmesh-builder` and `release-devmesh-publisher` Vault roles before a
tag is pushed. See the DEVMESH-001 release-contract note for the non-secret
credential inventory and validation sequence.
