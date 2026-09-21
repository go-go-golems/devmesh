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

## 2026-09-20

Reviewed implementation at 5fbf70c against complete brief/design/diary; baseline race/Docker/pgx and lint pass, but 17 isolated observations expose lifecycle, authority, HTTP, safety, durability and shutdown gaps. Added intern review, evidence programs, and eight open hardening phases; no production fixes.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/analysis/01-implementation-review-and-hardening-design-for-a-new-intern.md — Authoritative current implementation assessment and proposed hardening plan

## 2026-09-20

Uploaded Implementation Review and Hardening Design.pdf to /ai/2026/09/20/DEVMESH-001; cloud success receipt retained in analysis/evidence/06-remarkable-upload.txt. Review/delivery complete; eight proposed implementation phases remain open.

## 2026-09-20

Replaced broad v1 roadmap with focused v2: three delivery slices, shared state/client logic, fixed HTTP routes, periodic Docker repair, no idle reaping or drain-policy machinery. Archived v1 review and prior tasks; removed superseded tasks without marking implementation complete. Updated original guide/index to identify v2 as authoritative.

## 2026-09-20

Uploaded focused simplification design as a distinct v2 PDF to /ai/2026/09/20/DEVMESH-001; v1 preserved. Doctor and document checks pass. Only three v2 implementation slices remain in the active improvement plan; no production changes.

## 2026-09-20

Extended focused v2 design with practical CLI-first devctl integration: foreground lease keeper, bounded raw resolution, schema-free Compose consumer launcher, optional per-run backend-file wrapper contract, and loose orchestration/strict ownership semantics. No new transaction layer, production changes, or reMarkable upload.

## 2026-09-20

Implemented v2 Slice A: registry is the sole backend authority; conditional producer-ID removal protects replacements; per-entry TTL and atomic expiry; no idle reaper; narrowed public creation; shared CLI/SDK lease loop; periodic bounded Docker reconciliation; raw bounded resolve. Full race/build/vet/glazed-lint pass.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/internal/daemon/daemon.go — Conditional publication lifecycle and registry-backed runtime provider
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/dockerwatch/watcher.go — Simple periodic reconciliation and full forget identity
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/lease/manager.go — Per-entry TTL and atomic expiry take

## 2026-09-20

Implemented v2 Slice B: one-snapshot HTTP proxy rewrite, untrusted forwarded-header removal, fixed canonical hostname ownership, kind/hostname immutability, bind-before-start proxy listeners, and scheme/port-aware URLs through API/CLI/SDK. Full race/build/vet/glazed-lint pass.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/integration/http_proxy_test.go — Hostname/URL integration evidence
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/daemon/daemon.go — Synchronous proxy listener ownership and route validation
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/proxy/http.go — One backend snapshot and explicit outbound target

## 2026-09-20

Implemented v2 Slice C: prompt active TCP closure on shutdown; fail-fast durable allocation with dirty retries; regular-file/symlink socket protection; mixed Docker binding refusal; selected doctor socket correction; PostgreSQL query before/after overlapping replacement; scripting docs. Full Docker/race/build/vet/glazed-lint pass.

### Related Files

- /home/manuel/code/wesen/2026-09-20--devmesh/integration/postgres_test.go — Post-replacement PostgreSQL query acceptance
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/runtime/service.go — Active proxy admission, closure and worker waiting
- /home/manuel/code/wesen/2026-09-20--devmesh/internal/state/store.go — Durable state semantics and backup failure handling
