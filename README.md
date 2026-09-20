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

## Development

```bash
make test          # go test ./...
make test-race     # go test -race ./...
make lint          # vet + glazed-lint
make build
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
