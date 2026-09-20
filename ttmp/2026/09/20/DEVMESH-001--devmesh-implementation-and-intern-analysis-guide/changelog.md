# Changelog

## 2026-09-20

- Initial workspace created

## 2026-09-20

Created ticket and imported the devmesh implementation guide into sources/ (sha256 6915000170755418cf93eaf6cbc2f223c6f255c1394f33e9d9688da57f0da6ed). Wrote the intern analysis/design/implementation guide covering architecture, data model, registry, bind-first port allocation, TCP proxy, leases, Go client, Docker watcher, state, security, failure semantics, concurrency, shutdown, testing and the PR plan. Updated the CLI and help design to use the Glazed framework: GlazeCommand command tree, universal structured output flags, custom daemon section, root logging+help wiring, embedded help pages, glazed-lint, and command tests.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/design-doc/01-devmesh-intern-analysis-and-implementation-guide.md — Intern guide deliverable
- /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/sources/devmesh-implementation-guide.md — Imported source specification

## 2026-09-20

Uploaded the intern guide to reMarkable as a bundle (source spec + design doc, ToC depth 2) to /ai/2026/09/20/DEVMESH-001/.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/design-doc/01-devmesh-intern-analysis-and-implementation-guide.md — Uploaded deliverable

## 2026-09-20

Implemented the devmesh PR plan in five commits: TCP MVP core (registry, bind-first allocator, TCP proxy, leases, Go client, Glazed CLI), Docker watcher + doctor probe + PostgreSQL pgx E2E, shared HTTP reverse proxy, and wildcard TLS with an existing PEM pair. All tests pass under -race; glazed-lint passes at Glazed v1.4.4; real-Docker tests run against Docker 25.0.2 with postgres:17.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/internal/dockerwatch/watcher.go — Docker discovery and reconciliation
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/proxy/http.go — Shared hostname-routed HTTP/HTTPS proxy
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/runtime/allocator.go — Bind-first stable frontend allocation

## 2026-09-20

Migrated devmeshd configuration to Glazed env/config middleware. Removed config.Load and all os.Getenv reads from internal/config and internal/transport; the existing JSON config shape is preserved via config.FileMapper. Deleted both glazed-lint file-ignore exceptions; make glazed-lint now passes clean.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/cmd/devmeshd/cmds/serve.go — Glazed section, middleware chain, configFromSettings
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/config/config.go — Domain config + JSON FileMapper

## 2026-09-20

Added a Glazed-based pre-parse so the daemon config-file path can come from DEVMESH_CONFIG as well as --config (flag > env), without any direct os.Getenv. Unit tests cover env path, flag precedence, and absence; guide 8.2 documents the behavior.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/cmd/devmeshd/cmds/serve.go — resolveConfigPath pre-parse

## 2026-09-20

Mirrored the published go-go-parc Obsidian vault project report (commit d85a07c, sha256 7f0d80ab...) into the ticket's various/ directory and recorded its provenance in the reference inventory.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/various/vault-project-report--devmesh-deep-dive.md — Byte-identical mirror of the vault report
