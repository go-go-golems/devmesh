# Devmesh contributor guidance

## Project boundaries

- Module: `github.com/go-go-golems/devmesh`.
- Build the two shipped binaries with `make build-bin`: `devmesh` (client CLI) and `devmeshd` (daemon).
- `internal/` uses the standard library and `log/slog`; do not introduce Glazed into it.
- Glazed belongs in `cmd/` command configuration, structured output, and embedded help only.
- The registry is the single authority for the current backend. Do not restore a second runtime-owned backend cache.
- Docker watcher callbacks are the sole Docker registration owner. Do not add a second devctl Docker registration path.

## Local checks

```bash
make build
make test
make test-race
make lint
make logcopter-check
goreleaser check --config .goreleaser.yaml
goreleaser check --soft --config .goreleaser.yaml
```

Use `GOWORK=off` for project-local Go commands. Use tmux for long-running daemon or Docker smoke tests.

## Release boundary

A `v*` tag starts split Linux/macOS GoReleaser builds. Builders may read only the GoReleaser Pro license through `release-devmesh-builder`; the final shared publisher job uses `release-devmesh-publisher`. Do not add GitHub Action repository secrets, a tap PAT, or arbitrary Vault path inputs. The corresponding roles are Terraform-managed and must exist before the first release tag.
