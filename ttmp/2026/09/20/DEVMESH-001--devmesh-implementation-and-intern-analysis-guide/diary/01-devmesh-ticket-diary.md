---
Title: Devmesh ticket diary
Ticket: DEVMESH-001
Status: active
Topics:
    - devmesh
    - architecture
    - go
    - docker
    - proxy
    - registry
    - tcp-http
DocType: diary
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/design-doc/01-devmesh-intern-analysis-and-implementation-guide.md
      Note: Deliverable produced in Step 2/3
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/sources/devmesh-implementation-guide.md
      Note: Imported source specification referenced by this diary
ExternalSources: []
Summary: |
    Implementation diary for the DEVMESH-001 ticket: importing the source specification and authoring the intern analysis/design/implementation guide, including the Glazed CLI and help-system revision.
LastUpdated: 2026-09-20T16:30:00-04:00
WhatFor: Ticket work history for the devmesh intern guide.
WhenToUse: Read on resume to recover current state without rereading the full guide.
---

# Devmesh ticket diary

## Step 1: Create the ticket and import the source specification

The ticket was created to hold an intern-facing analysis/design/implementation
guide derived from an existing `devmesh-implementation-guide.md`. The first
action was to stand up the docmgr workspace and import the source spec verbatim
into `sources/` so the ticket is self-contained and the provenance is checkable.

### Prompt Context
**User prompt (verbatim):** "Create a new docmgr ticket and read + import devmesh-implementation-guide.md (into the sources/ directory) and then Create  a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."
**Assistant interpretation:** Create a docmgr ticket, copy the guide to `sources/`, author a deep intern guide as a ticket doc, then upload to reMarkable.
**Inferred user intent:** Produce a durable, pedagogical onboarding artifact for the devmesh project and ship it to the e-reader.

### What I did
- Ran `docmgr init --seed-vocabulary` and added topics (`devmesh`, `architecture`, `go`, `docker`, `proxy`, `tcp-http`, `registry`) plus doc types (`analysis`, `diary`).
- Created `DEVMESH-001` at `ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide`.
- Copied `devmesh-implementation-guide.md` to `sources/devmesh-implementation-guide.md`.
- Verified byte identity with `sha256sum` on both paths.

### Why
- The source guide is the contract; keeping an exact copy avoids re-deriving behavior from memory.
- A ticket-local `sources/` copy survives even if the top-level file moves or changes.

### What worked
- Both files hashed to `6915000170755418cf93eaf6cbc2f223c6f255c1394f33e9d9688da57f0da6ed`.
- Ticket structure created with `design/`, `reference/`, `sources/`, `scripts/`, `playbook/`, `various/`, `archive/`.

### What didn't work
- No failures observed.

### What I learned
- This docmgr build uses opaque short task IDs (e.g. `2w30`) rather than sequential integers; `task check` accepts the IDs printed by `task add`.

### What warrants a second pair of eyes
- Nothing yet; this step is pure artifact management.

### What should be done in the future
- Keep the top-level and `sources/` copies in sync, or declare the `sources/` copy authoritative for the ticket.

### Code review instructions
- Confirm the two files are identical and that the ticket frontmatter topics are valid vocabulary entries.

### Technical details
- Source: `/home/manuel/code/wesen/2026-09-20--devmesh/devmesh-implementation-guide.md` (54,878 bytes).
- Imported copy: `.../sources/devmesh-implementation-guide.md`.

## Step 2: Author the intern analysis/design/implementation guide

I wrote a single long-form design doc that explains devmesh from first
principles in a teaching order: problem, conceptual model, architecture,
component-by-component contracts with pseudocode, failure semantics,
concurrency, shutdown, testing, and an incremental PR plan. It cross-references
the source specification section-by-section so nothing is lost.

### Prompt Context
**User prompt (verbatim):** "Create a new docmgr ticket and read + import devmesh-implementation-guide.md ..." (see Step 1)
**Assistant interpretation:** The guide must include prose, bullets, pseudocode, diagrams, API references, and file references for a new intern.
**Inferred user intent:** A single document that can onboard an engineer to implement the MVP without senior help.

### What I did
- Created `design-doc/01-devmesh-intern-analysis-and-implementation-guide.md` and wrote 34+ sections.
- Added ASCII architecture/data-flow diagrams, per-connection pseudocode, allocator pseudocode, lease/backoff schedules, and full `/v1` API DTOs.
- Added a requirements-traceability appendix mapping every source-guide section to the new guide.
- Added a file-reference index and a five-invariant review checklist.

### Why
- The source spec is dense and contract-heavy; an intern needs the *why* and the *order* alongside the *what*.
- Traceability keeps the new guide honest against the specification.

### What worked
- The guide reached ~3,400 lines and covers every source section.
- The five-invariant framing (producer backends, devmesh frontends, stable identity, Docker-as-adapter, TCP-is-host+port) gives one memorable model.

### What didn't work
- No failures observed while writing.

### What I learned
- The riskiest single rule is bind-first port allocation; it needed both a worked bad example and a worked correct example to be unambiguous.

### What was tricky to build
- Explaining why raw TCP cannot be demultiplexed by hostname required a precise note that the DNS name is gone after resolution; this was called out explicitly.

### What warrants a second pair of eyes
- Whether the Go client re-registration and lease-expiry sequence fully matches the daemon-side `404` behavior; this is testable and should be verified in PR 3.

### What should be done in the future
- Add a short "read me first" quiz or reading-order checklist if interns report the guide is too long.

### Code review instructions
- Skim sections 1, 7, 13, and 23 for the load-bearing decisions; validate the traceability appendix against the source headngs.

### Technical details
- Primary deliverable path: `.../design-doc/01-devmesh-intern-analysis-and-implementation-guide.md`.

## Step 3: Redefine the CLI and help system on Glazed

A follow-up asked for the CLI and help system to be defined with the Glazed
framework, referencing the project skills. I read the `glazed-command-authoring`
and `glazed-help-page-authoring` skills, then rewrote the CLI section into a
full Glazed command contract and updated the surrounding sections that depend on
it.

### Prompt Context
**User prompt (verbatim):** "use the glazed framework to define the CLI and the hellp system (see skills)" / "update the doc"
**Assistant interpretation:** Replace the ad-hoc `--json` CLI design with Glazed `GlazeCommand`s, the three universal output flags, custom sections, root logging+help wiring, embedded help pages, and `glazed-lint`.
**Inferred user intent:** Keep the intern guide aligned with the organization's standard CLI framework so implementation is consistent and lintable.

### What I did
- Read `glazed-command-authoring` and `glazed-help-page-authoring` skills.
- Rewrote §23 into "CLI and help system (Glazed framework)" with 17 subsections:
  canonical imports, a complete `services list` `GlazeCommand`, positional
  `resolve`, the structured-output contract and its scriptability tradeoffs, the
  shared `daemon` section, long-running `register`, `devmeshd serve`, structured
  `doctor`, mandatory root init, embedded help pages, the help-page catalog,
  `glazed-lint` Makefile wiring, and command tests.
- Updated §5 repo layout (Glazed command tree, `pkg/doc/`), §5.4 module deps,
  §8.6 Glazed sections, §24 logging wiring, §29.5 command tests, §30 PR 1,
  §34 file index, §35 traceability, and §36 checklists.
- Corrected the CLI examples from `devmesh list --json` to
  `devmesh services list --format json` and documented that removed
  jq/template/sort flags must not be reintroduced.

### Why
- Glazed injects exactly `--format`, `--output-fields`, `--max-output-rows` and
  provides a real help system; hand-rolled `--json` flags and default Cobra help
  are the inconsistency the skills exist to prevent.
- Keeping Glazed confined to `cmd/*` and `pkg/doc` preserves a
  framework-agnostic core, which is important because the daemon and Go client
  must be testable without Cobra.

### What worked
- The rewrite is internally consistent: every CLI example now uses Glazed terms,
  and the review checklist gained explicit Glazed items.

### What didn't work
- No failures observed. One design tension surfaced: the original requirement
  for `devmesh resolve NAME` to print a bare address conflicts with Glazed's
  header-bearing table output. I documented the options (use `--format jsonl`
  with `--output-fields endpoint`, or a raw Cobra command with a domain `--raw`
  flag) rather than reintroducing removed universal output flags.

### What I learned
- Glazed v1.4 deliberately removed many universal flags; script-friendliness is
  now a domain decision, not a framework flag.
- `glazed-lint` must be built from the exact module version to avoid analyzer/API drift.

### What was tricky to build
- Mounting the shared `daemon` section exactly once: it must sit on a command
  group, not both parent and child, or flag registration collides. This is the
  main structural trap in the new design.

### What warrants a second pair of eyes
- Whether `pkg/doc` or `cmd/devmesh/doc` is the better home for embedded help
  pages in this repo's conventions; the guide chose `pkg/doc` to mirror the
  canonical `glaze` entrypoint.
- Whether `register` should be a `GlazeCommand` or a dual/raw Cobra command given
  its streaming, long-running behavior.

### What should be done in the future
- When PR 1 is implemented, verify with `devmesh services list --help` that the
  structured-output group contains only the three universal flags and that the
  `daemon` section appears once; then run `GOWORK=off make glazed-lint`.

### Code review instructions
- Read §23.5, §23.7, §23.9, and §23.13 first; they carry the command model,
  output contract, section ownership, and root wiring. Confirm the imports match
  the pinned Glazed module.

### Technical details
- Skill references: `glazed-command-authoring`, `glazed-help-page-authoring`.
- Primary API: `cli.BuildCobraCommandFromCommand` /
  `cli.AddCommandsToRootCommand`, `cmds.NewCommandDescription`,
  `cmds.WithFlags`/`WithArguments`/`WithSections`,
  `values.DecodeSectionInto`, `middlewares.Processor.AddRow`,
  `help.NewHelpSystem` + `help_cmd.SetupCobraRootCommand`,
  `logging.AddLoggingSectionToRootCommand` + `logging.InitLoggerFromCobra`.

## Step 4: Bookkeeping and validation

I recorded the work in docmgr (tasks, changelog, relations, diary) and validated
the documentation workspace. This checkpoint closes the authoring work; the
reMarkable upload remains pending.

### What I did
- Added five tasks; checked the first four; left the upload task open.
- Added a changelog entry with the source hash and the Glazed revision.
- Related the original source, imported source, and design doc to the design doc.
- Ran `docmgr doctor` for hygiene.

### What worked
- `docmgr doctor` reported the ticket and docs validate (see Evidence below).

### What didn't work
- (Record any doctor warnings here if the validation reports them.)

### Technical details
- Ticket: `ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide`.
- Docs: `sources/devmesh-implementation-guide.md`, `design-doc/01-...md`,
  `reference/01-imported-source-inventory-and-provenance.md`, this diary.

## Step 5: Implement the core TCP MVP (PR 1-3)

After the documentation phase, work moved to implementation. The goal was a
working TCP path end-to-end before touching Docker or HTTP: a native backend
registers, devmesh allocates a stable frontend by binding it, a consumer
connects through the frontend, and a lease expires if the producer dies.

### Prompt Context
**User prompt (verbatim):** "add + commit ticket. THen implement task by task, commit at appropriate intervals and keep a detailed diary as you work (using the diary format from the skill)"
**Assistant interpretation:** Commit the ticket, then implement the intern guide's PR plan incrementally, committing at boundaries and writing diary entries.
**Inferred user intent:** Turn the design document into a working, tested devmesh implementation with a reviewable history.

### What I did
- Created the Go module `github.com/wesen/devmesh` (Go 1.26.1 toolchain) and pinned Glazed v1.4.4.
- Implemented `internal/registry` (domain model, `ValidateName`, `ValidateBackend`, `IsLoopbackHost`, ownership rules), `internal/config` (JSON + env + defaults), `internal/state` (atomic temp+rename persistence, corrupt recovery, future-version refusal), `internal/lease` (tokens, TTL, constant-time compare, expiry), `internal/runtime` (bind-first allocator returning `{Port, Listener}`, `ServiceRuntime` with `atomic.Pointer[Backend]`, manager, idle reaper), `internal/proxy` (bidirectional copy with `CloseWrite` half-close), `internal/daemon` (orchestration and stable API error codes), `internal/api` (`/v1` HTTP over Unix socket), and `internal/transport` (stale-safe socket listener + JSON client with bearer support).
- Built the public client `pkg/devmesh` (`Register`, `ListenTCP`, heartbeat with 404 re-registration, best-effort `Close`).
- Built the Glazed CLI (`cmd/devmesh`) and daemon (`cmd/devmeshd`), embedded help pages in `pkg/doc`, and added `Makefile` + `README.md` + `examples/native-go`.
- Added unit tests (registry, allocator, lease, state) and integration tests (TCP round trip, lease lifecycle, backend replacement preserving frontend, unrelated-owner conflict, Go client heartbeat/liveness).

### Why
- The bind-first allocator is the load-bearing correctness decision; returning the open listener removes the allocation race.
- Docker and HTTP phases depend on this core, so they were deferred until the TCP path was proven.

### What worked
- `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` all pass.
- Manual smoke test: registered `test.echo` on a Python echo backend, resolved `127.0.0.1:25005`, and read back `ECHO:hello-devmesh` through the proxy.
- Daemon restart reused the remembered frontend (`frontend_reused`), a second daemon refused to start, and a SIGKILL'd daemon's stale socket was recovered.
- `devmesh services list --help` shows the `Daemon Connection` section once and exactly `format`/`output-fields`/`max-output-rows` under Structured output.
- `devmesh help devmesh-overview` renders the embedded page.

### What didn't work
- `go mod tidy` initially failed because Glazed v1.4.4 requires `go >= 1.26.1`; the toolchain auto-upgraded to go1.26.8 and pinned `go 1.26.1` in `go.mod`.
- Several first drafts had unused imports; fixed with `gofmt`/build iteration.
- `TestAllocateExhausted` assumed two ephemeral ports formed a contiguous two-port range; they did not, so the range still had free ports. Fixed by testing a single already-occupied port as the whole range.
- `glazed-lint` flagged `os.Getenv` in `internal/config` and `internal/transport`. These are legitimate documented domain-env reads, so I added `//glazedclilint:file-ignore <reason>` directives rather than removing the feature. `make glazed-lint` now passes.

### What I learned
- Glazed v1.4.4 auto-injects the structured-output section for `GlazeCommand`s, so commands must not add it manually.
- `AddCommandsToRootCommand` builds all commands before mounting, which catches schema collisions early.
- The public Go client's heartbeat must send the bearer token; the initial draft omitted it and was fixed before testing.

### What was tricky to build
- Lease lifecycle and ownership interact: after a lease expires or is deleted the registry keeps the record (so the frontend stays reserved), but the owner key changes on re-registration. I changed ownership conflict rules to allow takeover of a *dormant* (unavailable) name while still protecting a ready one.

### What warrants a second pair of eyes
- Whether a dormant-name takeover should also be gated on the idle grace period, rather than being immediately allowed.
- The `sync.Mutex` serialization in `runtime.Manager` and `daemon.Daemon.mutate` is simple and correct but global; fine for local-dev scale, worth revisiting if registration throughput matters.

### What should be done in the future
- Docker watcher (PR 4), `devmesh doctor` Docker capability test + PostgreSQL example (PR 5), then HTTP proxy and TLS (PR 6-7).

### Code review instructions
- Start with `internal/runtime/allocator.go`, `internal/proxy/tcp.go`, `internal/daemon/daemon.go`, and `internal/registry/registry.go`.
- Validate with: `go test -race ./...`, `make glazed-lint`, and the manual smoke commands in the README.

### Technical details
- Commit `97103c6` — ":sparkles: Implement devmesh TCP MVP: registry, bind-first port allocation, TCP proxy, leases, Go client, Glazed CLI".
- Dependencies: `github.com/go-go-golems/glazed v1.4.4`, `github.com/spf13/cobra v1.10.2`.

## Step 6: Docker watcher and PostgreSQL acceptance test (PR 4-5)

With the TCP core proven, the next milestone was Docker: labeled containers
should be discovered automatically, their ephemeral host-published ports mapped
to stable frontends, and container recreation should change only the backend.
This step also added a real `devmesh doctor` publication capability check and a
pgx end-to-end PostgreSQL test.

### Prompt Context
**User prompt (verbatim):** see Step 5.
**Assistant interpretation:** Continue the PR plan: implement the Docker adapter and diagnostics.
**Inferred user intent:** Reach the guide's key acceptance behavior — two PostgreSQL projects without host-port 5432 conflicts.

### What I did
- Added `internal/dockerwatch`:
  - `labels.go` — required/optional label parsing and a recreation-stable owner key (Compose project/service, else container name; never container ID).
  - `inspect.go` — `NetworkSettings.Ports` lookup, loopback preference, `ErrNotPublished` and `ErrNonLoopback`, opt-in non-loopback with loopback dialing.
  - `docker.go` — a `DockerAPI` interface plus an environment-aware, version-negotiated real client adapter.
  - `watcher.go` — startup reconciliation, event handling (`start`/`restart`/`die`/`stop`/`destroy`), bounded start-event retry, reconnect with backoff + re-reconcile, and forgetting disappeared containers.
  - `probe.go` — a disposable `busybox`/`alpine` loopback ephemeral-publication probe that never pulls an image.
- Wired the watcher into `Daemon.StartDockerWatcher` and `doctor`.
- Added `examples/compose-postgres/compose.yaml`.
- Tests: label whitespace (`"true"`), inspect loopback/non-loopback/not-published, watcher reconcile/forget with a fake API, a real-container discovery + recreate-preserves-frontend integration test, and a pgx `select 1` through the stable frontend.

### Why
- Docker is an adapter over the same registration model; the registry core still builds and tests without Docker.
- The pgx test is the guide's canonical acceptance check for the PostgreSQL use case.

### What worked
- `go mod tidy` resolved `github.com/docker/docker v28.5.1+incompatible` and `pgx/v5 v5.11.0`.
- Real Docker tests pass against Docker 25.0.2 with the local `postgres:17` image, including `TestDockerDiscoveryAndRecreatePreservesFrontend` and `TestPostgresThroughFrontend`.
- `make glazed-lint` continues to pass.

### What didn't work
- The first `go mod tidy` selected split Docker modules (`github.com/docker/docker/api@v1.56.0`, `github.com/docker/docker/client@v0.6.0`) that declare the module path `github.com/moby/moby/api` and failed to resolve. Fixed by pinning `github.com/docker/docker@v28.5.1+incompatible` before tidying.
- `container.NetworkSettings.Ports` is a `nat.PortMap` keyed by `nat.Port`, not `string`; string indexing failed to compile. Fixed with `nat.Port(...)`.
- The inspect test literal placed `Ports` directly on `NetworkSettings`, but it lives on the embedded `NetworkSettingsBase`; fixed the literal.
- The first recreate test used a different Compose service name, which produced a different owner key and would conflict. Fixed by removing the first container and recreating it with the same project/service.

### What I learned
- Docker's module split is easy to land on accidentally; pinning the `+incompatible` line keeps the API packages in one module.
- Docker publishes the host port at container start, before the application inside is ready; proxying to it is fine, but application-level readiness (pgx) needs its own retry loop.

### What warrants a second pair of eyes
- Whether the reconcile-after-reconnect should also forget tracked containers that stopped during the disconnect (it does, via the `seen` set).
- The probe's image allowlist and behavior when no small image exists (reports skip, never pulls).

### What should be done in the future
- HTTP kind Docker discovery is now supported via `io.devmesh.http-host`; add a real container HTTP E2E test.
- Consider health/readiness probing before advertising `ready`.

### Code review instructions
- Review `internal/dockerwatch/inspect.go` (safety), `watcher.go` (races/reconnect), then `integration/docker_test.go` and `integration/postgres_test.go`.
- Validate: `go test -race ./...` with Docker present; the Docker tests skip cleanly when it is absent.

### Technical details
- Commit `a399d71` — ":whale: Add Docker watcher, doctor publication probe, and PostgreSQL end-to-end tests".

## Step 7: Shared HTTP reverse proxy and TLS (PR 6-7)

The final phase layered hostname routing on the same registry: one HTTP/HTTPS
listener serves many local services because HTTP carries a Host header. TLS
reuses an existing PEM certificate/key pair; ACME automation remains out of
scope.

### What I did
- `internal/proxy/http.go`: a concurrent `Router` keyed by normalized hostname, backed by `httputil.ReverseProxy`; 404 for unknown hosts and 503 for unavailable backends.
- Daemon: `kind=http` registrations take an `http_host`, store a stable frontend URL, and install a route whose provider reads the current registry backend; the configured HTTP listener starts in `Start`.
- API/Docker: `http_host` field plus `io.devmesh.http-host` label.
- TLS: `daemon.loadTLS` validates an existing cert/key pair at startup and serves the same router over TLS; `MinVersion` TLS 1.2.
- Tests: `TestHTTPProxyRoutesByHost` (A/B routing, 404, 503), `TestHTTPSProxyWithExistingCertificate` (self-signed cert, real TLS request), and `internal/daemon` tests for partial/invalid cert config.

### What worked
- HTTP and HTTPS end-to-end tests pass; `go test -race ./...` and `make glazed-lint` remain green.

### What didn't work
- The first HTTPS test draft referenced three helpers that did not exist (`roundRobinBackend`, `trimHTTP`, `insecureTLSConfig`); replaced them with `httptest.NewServer` and inline `strings.TrimPrefix`/`tls.Config`.

### What I learned
- Because the router's provider reads the registry on each request, marking a service unavailable automatically yields 503 without mutating the route.

### What warrants a second pair of eyes
- Route ownership when two different services claim the same hostname (last write wins; no conflict rule is enforced yet).
- The guide's one-label wildcard rule for generated hostnames is documented but not yet auto-generated; registrations must pass an explicit `http_host`.

### What should be done in the future
- Enforce hostname uniqueness separately from service-name uniqueness; add auto hostname generation with the one-label wildcard rule; add ACME later.

### Code review instructions
- Review `internal/proxy/http.go` (host normalization and reverse-proxy director) and `daemon.loadTLS`.
- Validate: `go test ./integration/... -run 'HTTP'` and `go test ./internal/daemon/...`.

### Technical details
- Commits `5239164` (HTTP proxy) and `7f180c4` (TLS).

## Step 8: Full validation and commit history

Before wrapping up, I ran the complete validation gate across the whole tree.

### What I did
- `go build ./...`, `go vet ./...`, `go test ./... -count=1`, `go test -race ./... -count=1`, and `GOWORK=off make glazed-lint`.

### What worked
- All packages pass, including the real-Docker integration tests (Docker 25.0.2) and the pgx PostgreSQL test.
- `glazed-lint` passes at Glazed v1.4.4 after file-level suppressions with documented reasons for the intentional `DEVMESH_*` env reads.

### What didn't work
- No failures at this checkpoint.

### Technical details
- Commit sequence: `e4771fd` (ticket) → `97103c6` (TCP MVP) → `a399d71` (Docker/diagnostics) → `5239164` (HTTP proxy) → `7f180c4` (TLS).
- Full suite: `go test -race ./...` passes; Docker-dependent tests skip when Docker is unavailable.

## Step 9: Move env/config loading into Glazed middleware

A review of the CLI surface showed that the daemon still read `DEVMESH_*` via
`os.Getenv` and loaded its JSON file with a hand-written `config.Load`. That
required two `glazedclilint:file-ignore` exceptions and duplicated precedence
logic the framework already owns. This step replaced both with Glazed's env and
config-file middleware, keeping the existing JSON file shape through a
`ConfigFileMapper`.

### Prompt Context
**User prompt (verbatim):** "we should use the env/config middleware and not use manual os.Geenv or config load file I think, except if the config file has its own format that can't be matched to glazed flags."
**Assistant interpretation:** Migrate env/file config to Glazed middleware; keep the JSON format if it can be mapped to fields.
**Inferred user intent:** Remove manual env/file handling and the lint exceptions, while preserving the documented config file contract.

### What I did
- Rewrote `internal/config/config.go` to keep only the domain `Config` type, `Default()`, `DefaultStatePath()`, and a new `FileMapper` that maps the devmesh JSON shape (flat `tcp_frontend_*`/`runtime_idle_ttl`/... keys plus nested `docker`/`http` objects) onto the default Glazed section's kebab-case fields.
- Extended `cmd/devmeshd/cmds/serve.go` with every config field on its Glazed section and a custom `ParserConfig().MiddlewaresFunc` chain: `FromCobra → FromArgs → FromEnv("DEVMESH") → FromFile(path, WithConfigFileMapper(FileMapper)) → FromDefaults`. `configFromSettings` converts decoded values into `config.Config`.
- Removed `os.Getenv` from `internal/transport/unix.go`; `DefaultSocketPath` now only uses the home directory. Dropped the `XDG_RUNTIME_DIR` rung because Glazed env keys are `<APPPREFIX>_<FIELD>` and cannot express a standard `XDG_RUNTIME_DIR`.
- Deleted both `//glazedclilint:file-ignore` directives.
- Added tests: `FileMapper` flat/nested/partial/error cases, `configFromSettings`, and duration fallback.
- Updated guide sections 8.2 (precedence) and 9.2 (socket path).

### Why
- One precedence chain owned by Glazed replaces a parallel `config.Load` implementation.
- Provenance is now traceable: `--print-parsed-fields` records `source: config`.
- Removing manual env reads removes the need for lint suppressions.

### What worked
- End-to-end config file: a file with `tcp_frontend_min: 24000` produced frontend `127.0.0.1:24011`; `--print-parsed-fields` showed `defaults 15000` then `config 24000`.
- Env: `DEVMESH_TCP_FRONTEND_MIN=24500` produced `127.0.0.1:24501`.
- Precedence: a `--tcp-frontend-min 25000` flag beat both the env and the config file (`127.0.0.1:25035`).
- `go test -race ./...` passes and `make glazed-lint` passes with zero suppressions.

### What didn't work
- The first guide edit attempt targeted slightly different headings/whitespace; re-read the section and matched exactly.
- A first attempt to put the file-ignore deletion and socket-path rewrite in one edit call failed atomically because the directive had shifted into the doc-comment group; fixed with a single exact block replacement.

### What I learned
- Glazed's built-in parser chain orders sources as Defaults (innermost) → Config → Env → Args → Cobra (outermost), which yields the expected low→high precedence; a custom `MiddlewaresFunc` must reproduce that order.
- The built-in `ConfigPlanBuilder` path cannot inject a `ConfigFileMapper`, so keeping the devmesh JSON shape requires the custom `MiddlewaresFunc`.

### What warrants a second pair of eyes
- The config file path is read from the `--config` Cobra flag only; `DEVMESH_CONFIG` is not honored because env is applied after the config middleware executes. Decide whether to support an env-provided path (e.g. by adding a small pre-parse).
- Dropping `XDG_RUNTIME_DIR` is a behavior change for users who relied on it.

### What should be done in the future
- Re-add XDG support as an explicit `runtime-dir` field default if desired, or document `DEVMESH_SOCKET` as the supported override.

### Code review instructions
- Review `internal/config/config.go` (`FileMapper`), `cmd/devmeshd/cmds/serve.go` (`ParserConfig`/`configFromSettings`), and `internal/transport/unix.go`.
- Validate: `go test -race ./...`, `make glazed-lint`, and the manual `--print-parsed-fields` commands above.

### Technical details
- Commit `c07d2f1` — ":recycle: Move daemon env/config loading into Glazed middleware".
- Framework sources: `FromCobra`, `FromArgs`, `FromEnv`, `FromFile(WithConfigFileMapper)`, `FromDefaults` in `pkg/cmds/sources`.

## Step 10: Pre-parse DEVMESH_CONFIG for the config-file path

Step 9 left one gap: the config middleware read the path only from the `--config`
Cobra flag, so `DEVMESH_CONFIG` was ignored because Glazed applies env after the
config middleware runs. This step added a framework-based pre-parse so the path
can also come from the environment, without reintroducing `os.Getenv`.

### Prompt Context
**User prompt (verbatim):** "add a pre-parse for the env-provided path. commit at appropriate intervals and keep a detailed diary as you work (using the diary format from the skill)"
**Assistant interpretation:** Resolve the config path from `--config` or `DEVMESH_CONFIG`, using Glazed to read env rather than raw `os.Getenv`.
**Inferred user intent:** Close the env-config gap while keeping the no-manual-env policy and the clean lint.

### What I did
- Added `resolveConfigPath(desc, cmd)` in `cmd/devmeshd/cmds/serve.go`: it checks `cmd.Flags().GetString("config")` first (flag wins), then runs `cmd_sources.ExecuteWithSchema(desc.Schema, values.New(), cmd_sources.FromEnv("DEVMESH", ...))` in a throwaway pre-parse and decodes the `config` field.
- Wired `resolveConfigPath` into `ParserConfig().MiddlewaresFunc` before building the middleware chain.
- Added unit tests: env path, flag-beats-env, and no-path-absent.
- Updated guide §8.2 to document the path precedence and the pre-parse.

### Why
- Glazed's config middleware cannot see env-set fields because env is applied later; a pre-parse using the same source keeps everything framework-owned.
- Avoiding `os.Getenv` preserves the zero-suppression `glazed-lint` state from Step 9.

### What worked
- `DEVMESH_CONFIG=/tmp/devmesh-cfg.json devmeshd serve` produced frontend `127.0.0.1:24014` (file range 24000–24040), confirming the env path is honored.
- `--print-parsed-fields` with only `DEVMESH_CONFIG` showed `defaults 15000 → source: config 24000`.
- Unit tests for env path, flag precedence, and absence pass; `go test -race ./...` and `make glazed-lint` pass with no exceptions.

### What didn't work
- No failures at this step.

### What I learned
- `cmd_sources.ExecuteWithSchema` with a single `FromEnv` middleware is enough to read one field from the environment in isolation; the discarded `values.Values` does not affect the real parse.
- `MiddlewaresFunc` receives only the command-settings values, not the command schema, so the schema must be captured from the command description when building `ParserConfig`.

### What warrants a second pair of eyes
- The pre-parse loads all `DEVMESH_*` fields into a throwaway `values.Values`; if a future field type errors on an empty/malformed env value, that error would surface during path resolution. It currently returns a clear error, which seems acceptable.
- The `state` field maps to `DEVMESH_STATE`, while the old manual loader used `DEVMESH_STATE_PATH`; the config-file key remains `state_path`. Confirm the env rename is acceptable (it is not yet documented publicly).

### What should be done in the future
- Document the exact env variable list (DEVMESH_SOCKET, DEVMESH_CONFIG, DEVMESH_TCP_FRONTEND_MIN/MAX, DEVMESH_LEASE_TTL, DEVMESH_DOCKER_ENABLED, DEVMESH_STATE, ...) in the README/help page.

### Code review instructions
- Review `resolveConfigPath` and its tests in `cmd/devmeshd/cmds/`.
- Validate: `go test ./cmd/devmeshd/...` and the manual `DEVMESH_CONFIG` run above.

### Technical details
- Commit `d11d319` — ":mag: Pre-parse DEVMESH_CONFIG so the config path can come from env".

## Step 11: Mirror the published vault report into the ticket

After publishing the deep-dive project report to the go-go-parc Obsidian vault,
I mirrored it into the ticket so the devmesh repository is self-contained and the
report can be reviewed alongside the implementation it describes.

### Prompt Context
**User prompt (verbatim):** "mirror the report. commit."
**Assistant interpretation:** Copy the vault report byte-for-byte into the ticket, record provenance, and commit.
**Inferred user intent:** Keep the ticket self-contained without maintaining a second editable copy.

### What I did
- Copied the vault note to `various/vault-project-report--devmesh-deep-dive.md`.
- Verified byte identity: both files are 51,197 bytes with SHA-256 `7f0d80ab7e3a6a98f25189fbdb40c2b26bcdefdcb5fe7b5845be48feecb8d3f5`.
- Recorded the vault path, vault commit `d85a07c`, size, hash, and a verification command in the reference provenance doc, and related the mirror file.
- Added a changelog entry.

### What worked
- `docmgr doctor --ticket DEVMESH-001` reports all checks passed.

### What didn't work
- No failures observed. The mirror is read-only by convention; the vault note remains the editable source.

### Technical details
- Vault source: `/home/manuel/code/wesen/go-go-golems/go-go-parc/Projects/2026/09/20/PROJECT REPORT - Devmesh - Stable Local Endpoints for Ephemeral Backends - A Technical Deep Dive.md`.
- Mirror: `.../DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/various/vault-project-report--devmesh-deep-dive.md`.

## Step 12: Independent implementation review and hardening design

I reviewed the implementation at `5fbf70cb3bf0d215185351af3eac3bea0b195e0d` against the complete original brief, expanded design guide, and diary. The architecture remains appropriate, but several documented guarantees were not established by the implementation or existing tests. I kept this work analysis-only: production code is unchanged, and all new reproduction programs live under the ticket.

The review includes 12 finding groups, 17 labeled observations, an intern-oriented system explanation, proposed APIs and lifecycle pseudocode, portable text diagrams, and eight implementation phases. The baseline race suite passed including Docker and PostgreSQL, while targeted probes reproduced stale-removal, lease, routing, endpoint, persistence, and shutdown defects. Passing the baseline therefore does not close the original acceptance contract.

### Prompt Context
**User prompt (verbatim):** "Analyze the implementation of devmesh, read the diary, design doc, original brief, and write your review and assessment and plan for improvements (if necessary). Create  a detailed analysis / design / review that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."
**Assistant interpretation:** Conduct an evidence-backed review, explain current architecture and deficiencies to a new intern, propose prioritized improvements, store artifacts in DEVMESH-001, and upload the report.
**Inferred user intent:** Obtain an independent technical assessment and an actionable next implementation plan rather than another optimistic implementation summary.
**Reviewed code revision:** `5fbf70c`; no production fixes authored during this review.

### What I did
- Read all 2,476 lines of the brief, 3,454 lines of the design guide, and 531 lines of existing diary, then inspected production packages and tests.
- Ran `GOWORK=off go test -race ./... -count=1 -v`, `GOWORK=off go build ./...`, `GOWORK=off go vet ./...`, and `GOWORK=off make glazed-lint` successfully.
- Added `scripts/01-review-probes.go` and `scripts/02-cli-api-probes.py`; ran them against isolated domain objects, loopback listeners, fake Docker inventory, and temporary real binaries/Unix sockets.
- Preserved outputs and a symbol index under `analysis/evidence/`.
- Wrote `analysis/01-implementation-review-and-hardening-design-for-a-new-intern.md`, related primary evidence/code files, linked it from the index, and added eight open hardening tasks.
- Validated report YAML and fence balance; reviewed text diagrams against the code paths and proposed transitions. Compiled the review Go package with `go test` (no test functions; observations come from running the probe).

### Why
- Logical owner identity must survive replacement, but removal must compare the exact backend incarnation. The current code does not maintain that distinction consistently.
- An intern needs to understand both intended behavior and observed behavior; proposed APIs are marked as sketches, and source-only concerns are separated from directly reproduced failures.
- The original brief, old diary entries, and byte-identical mirror remain intact. The current review explicitly corrects historical overstatements instead of silently editing evidence.

### What worked
- Baseline tests pass; live Docker recreation and PostgreSQL tests ran rather than skipping.
- Seventeen labeled observations show defects reproducibly; scripts exit successfully because they report current behavior, not because the intended contracts pass.
- The black-box SIGTERM control exited 0 and removed its socket. Inspection of pinned Glazed code confirmed the framework installs signal-aware contexts; an apparent missing-signal-handler concern was rejected.

### What didn't work
- Product observations include `P03 Docker reconcile replacement: container=new status=unavailable (desired ready)` and `P10 backend disappears between provider calls: status=200 body=UNINTENDED (desired 503, never caller URL)`; full exact outputs are in `analysis/evidence/03-review-probes.txt`.
- The occupied-listener probe emitted `http_proxy_listen_failed ... bind: address already in use`, while health remained 200/ok. The full diagnostic and API observations are in `analysis/evidence/04-cli-api-probes.txt`.
- An exploratory lookup used `rg -n 'envKey|envName|GetSlug|prefix' /home/manuel/go/pkg/mod/github.com/go-go-golems/glazed@v1.4.4/pkg/cmds/sources/env.go` and failed with `No such file or directory (os error 2)`. Located `FromEnv` in `sources/update.go` and read the actual implementation instead. No product finding depends on the failed lookup.

### What I learned
- Bind-first allocation is correctly implemented, but does not establish persistence or lifecycle correctness.
- Race-free map operations can still compose into incorrect transitions: expiry snapshots can remove renewed leases, and old Docker identity can clear a replacement.
- The HTTP double-provider lookup can retain an absolute caller URL when the backend disappears. The reproduction used only a local target, not an external system.
- Existing tests prove less than earlier summaries claim: allocation distinctness is sequential, recreation checks metadata without a post-recreate DB query, and HTTPS verification is disabled in its test client.

### What was tricky to build
- Separating actual defects from framework behavior and from incomplete acceptance coverage required checking call ordering across packages, not only reading individual methods.
- The lease interleaving probe uses explicit logical time at the manager boundary, avoiding an unreliable thread-timing test; it demonstrates the unsafe operation sequence without claiming a production stress test.
- Portable fenced text diagrams keep the report useful in Markdown and the reMarkable PDF without adding a separate diagram-rendering dependency.

### What warrants a second pair of eyes
- Proposed publication identity across Docker same-container restart versus container replacement.
- The HTTP target-selection fix and forwarded-header trust policy.
- Lock ordering among lifecycle coordination, lease expiry, runtime reaping, and bounded shutdown.
- The proposed choice to reject new allocations when durable state cannot be saved.

### What should be done in the future
- Execute the eight open review phases, starting with conditional incarnation-aware removal and authorized producer lifecycle operations.
- Convert observations into desired-behavior regression tests before fixing them.
- Keep ACME, local DNS, multi-host discovery, and load balancing deferred until combined lifecycle acceptance passes.

### Code review instructions
- Start with the review's findings F01–F04 and design decisions D1–D2.
- Reproduce with `go run <ticket>/scripts/01-review-probes.go` and `python3 <ticket>/scripts/02-cli-api-probes.py` from the repository root.
- Read `internal/daemon/daemon.go`, `internal/dockerwatch/watcher.go`, `internal/lease/manager.go`, and `internal/proxy/http.go` alongside their cited observations.

### Technical details
- Review document: `analysis/01-implementation-review-and-hardening-design-for-a-new-intern.md` (approximately 11,000 words).
- Evidence receipts: `analysis/evidence/01-baseline-race.txt` through `05-symbol-index.txt`.
- `docmgr doctor --ticket DEVMESH-001 --stale-after 30` passed; `git diff --check` passed.
- Delivery command: `remarquee upload bundle <review-path> --name 'DEVMESH-001 Implementation Review and Hardening Design' --remote-dir '/ai/2026/09/20/DEVMESH-001' --toc-depth 2 --non-interactive`.
- Delivery result: `OK: uploaded DEVMESH-001 Implementation Review and Hardening Design.pdf -> /ai/2026/09/20/DEVMESH-001` (receipt: `analysis/evidence/06-remarkable-upload.txt`). This proves cloud upload, not physical-device synchronization.
- Review and delivery tasks completed; eight hardening implementation phases remain open. No production files or historical vault mirror were changed.

## Step 13: Focus the design on simplification and three delivery slices (v2)

The user asked to turn the broad hardening roadmap into a practical, focused design that does not add machinery for low-impact rare cases. I preserved the complete v1 review and previous task list in the archive, then rewrote the current review/design as a shorter authoritative v2. The three slices prioritize normal TCP/Docker lifecycle, usable HTTP endpoints, and minimal resource/durability cleanup.

The revision reduces supported combinations as well as implementation duplication: one service state, one lease loop, fixed explicit HTTP routes, no idle runtime reaper, prompt connection closure instead of a drain-policy framework, and periodic Docker repair instead of event epochs/replay. These are proposed implementation changes; production code remains unchanged.

### Prompt Context
**User prompt (verbatim):** "Ok, update the design doc. We want to get this going, so stay focused, and make it clear we don't want to add complex new machinery that would for example only address rare edge cases without any importance or so.

Upload as v2."
**Assistant interpretation:** Make simplification and scope limits authoritative, replace the oversized roadmap, and deliver a distinct v2 PDF without overwriting v1 annotations.
**Inferred user intent:** Get the supported workflow working soon and prevent review findings from becoming an open-ended architecture project.

### What I did
- Archived v1 as `archive/01-implementation-review-v1.md` (SHA-256 `f5be8e7bf36304ff7a925ab31a85c8614ddbdb9932a73f7a4fd3b6aba31559bf`) and the old tasks as `archive/02-tasks-before-focused-v2.txt`.
- Rewrote the current analysis/design with explicit guardrails, a findings-to-minimal-fix table, three slices, a deferral/trigger table, and a supported-workflow stopping point.
- Added a scope notice to the original design guide and updated the ticket index so the historical roadmap is not accidentally treated as additional required work.
- Used docmgr to remove eight superseded tasks without marking them implemented, and added three focused v2 implementation tasks plus a revision/delivery task.
- Checked frontmatter, relative links, fenced blocks, and the text diagrams against the new architecture; no production tests rerun for this documentation-only revision.

### Why
- The prior findings are still useful evidence, but not every conceivable improvement should block release.
- Removing idle reaping and dynamic route migration avoids implementing synchronization/lifecycle machinery solely to support optional flexibility.
- Periodic reconciliation deliberately permits short repair delays rather than claiming perfect event delivery.

### What worked
- The current design is approximately 5,600 words versus approximately 11,000 in v1, while retaining evidence links and intern-oriented architecture explanations.
- The concrete important routing, TTL, and ownership failures remain in scope; advanced event history, generic generations, public hot-update APIs, and policy frameworks are explicitly deferred.

### What didn't work
- Initial `docmgr doctor --ticket DEVMESH-001 --stale-after 30` rejected the archived task Markdown: `frontmatter delimiters '---' not found`, and warned that the v1 archive lacked a numeric prefix. Renamed the raw task snapshot to numbered `.txt` and the review archive to numbered Markdown, preserving bytes.
- Two exact-edit attempts to repair archive references failed without changing files: a short path matched twice, then an absolute metadata path did not match because docmgr stored `repo://` paths. Read the actual frontmatter and applied unique replacements.

### What I learned
- Scope reduction must update the active tasks and original-guide entry point as well as the new prose; otherwise the broad roadmap remains operationally authoritative.

### What was tricky to build
- Distinguishing a narrower honest contract from an unfixed bug: prompt shutdown is acceptable if documented, while wrong HTTP target selection is not. Fixed routes are acceptable if mutation is rejected without side effects.

### What warrants a second pair of eyes
- Confirm the deliberate no-reaper and prompt-close policies match intended local usage. They simplify the implementation but differ from the original brief.

### What should be done in the future
- Implement only the three v2 slices; require a concrete user impact and test before promoting a deferred item.

### Code review instructions
- Read v2 sections 1, 5, 6, 7, and 8 for guardrails, design, work sequence, deferrals, and stopping criteria.
- The archived v1 findings remain evidence, not a second backlog.

### Technical details
- Current document remains `analysis/01-implementation-review-and-hardening-design-for-a-new-intern.md`.
- Final validation: `docmgr doctor --ticket DEVMESH-001 --stale-after 30` and `git diff --check` passed; YAML, relative links, and fence checks passed. Text diagrams were reviewed against the simplified paths.
- Upload command: `remarquee upload bundle <current-document> --name 'DEVMESH-001 Implementation Review and Focused Simplification Design v2' --remote-dir '/ai/2026/09/20/DEVMESH-001' --toc-depth 2 --non-interactive`.
- Receipt: `OK: uploaded DEVMESH-001 Implementation Review and Focused Simplification Design v2.pdf -> /ai/2026/09/20/DEVMESH-001`, retained in `analysis/evidence/07-remarkable-upload-v2.txt`. This confirms cloud delivery, not device synchronization; v1 was not overwritten.
- Design/delivery complete; three implementation slices remain open. No production code changed.

## Step 14: Specify practical CLI-first devctl integration

I extended the current v2 design with the practical integration discussed in chat: first reuse foreground registration and endpoint resolution with a devctl-supervised shell launcher, then add native wrapper support only when an application needs it. The section distinguishes proposed CLI flags and launch-plan fields from implemented interfaces and keeps process ownership in devctl and frontend ownership in devmesh.

### Prompt Context
**User prompt (verbatim):** "Ok, update the design doc."
**Assistant interpretation:** Incorporate the preceding practical CLI-first implementation proposal into the current focused design.
**Inferred user intent:** Preserve an actionable integration plan without expanding into transactional orchestration machinery.

### What I did
- Added section 10 to the current review/design: registration keeper contract, `resolve --raw --wait`, Compose consumer launcher, optional per-run backend-file convention, precise devctl insertion points, and tests.
- Specified registration readiness versus application health, plugin lifetime versus persistent wrapper lifetime, bounded failure cleanup, response-loss recovery, and endpoint snapshot limitations.
- Added one CLI-first integration task dependent on Slice A; native wrapper schema work remains conditional follow-up.

### Why
- Planning plugins terminate after planning and cannot safely hold service leases. Existing persistent wrappers already own the relevant child lifetime.
- Ordered startup with bounded cleanup is sufficient; no cross-service atomic transaction or rollback coordinator is needed.

### What worked
- Existing architecture and prior devctl inspection at `bac5d91` provide concrete integration sites without adding a daemon or registry.

### What didn't work
- No implementation failures; this is a documentation-only update.

### What I learned
- The first working Compose-to-API integration can use existing devctl command/environment facilities without schema changes.

### What was tricky to build
- Making the optional native address-file convention explicit without implying arbitrary applications already support it or that port scanning is safe allocation.

### What warrants a second pair of eyes
- The future Glazed command implementation must actually flush early JSON output and support domain raw rendering without recreating universal output machinery.

### What should be done in the future
- Implement the CLI improvements after core lease/removal fixes, then prove the Compose example before adding native wrapper fields.

### Code review instructions
- Read current design section 10 alongside devctl `pkg/engine/types.go`, `pkg/operator/planner.go`, and `cmd/devctl/cmds/wrap_service.go`.

### Technical details
- No production files changed. No upload requested for this turn; the previously uploaded v2 PDF does not contain this new integration section.
- Document validation is recorded in the final handoff; no production tests are warranted for the prose-only change.

## Step 15: Implement Slice A — one routing authority and producer lifecycle

This slice changed devmesh’s lifecycle model rather than adding a new lifecycle framework. The registry is now the single authoritative source for a service’s current backend; TCP runtimes retain only their frontend listener and ask the registry for one backend snapshot per accepted connection, as the HTTP router already does. The automatic idle reaper was removed, so a service frontend remains reserved until daemon shutdown instead of moving through another reaper/recreation state.

The implementation also closes the observed stale-removal and lease timing failures. A record now carries its concrete producer ID (registration ID for process/manual registrations, container ID for Docker). Lease expiration, deletion, and Docker forget events clear a backend only when that producer ID still matches the current record. Docker keeps responsive events but adds five-second periodic reconciliation as a deliberately simple repair mechanism; no event epoch, replay cursor, or deduplication subsystem was added.

### Prompt Context
**User prompt (verbatim):** "Implement, commit at appropriate intervals and keep a detailed diary as you work (using the diary format from the skill)"
**Assistant interpretation:** Implement the focused v2 design incrementally, validate each coherent slice, commit at boundaries, and preserve reasoning and failures in the ticket diary.
**Inferred user intent:** Deliver working behavior without expanding the design into rare-edge-case machinery.

### What I did
- Replaced the TCP runtime’s mutable backend pointer with a provider that reads the registry’s current ready backend, so registry, TCP, and HTTP no longer have separate authoritative backend values.
- Added `ProducerID` to `registry.ServiceRecord` and `ClearBackendIf(name, producerID)`. `Daemon.endPublication` uses this conditional operation for lease expiry, explicit deletion, and Docker forget callbacks.
- Retired an old same-owner process/manual lease on trusted replacement. An old token can no longer clear a newer backend.
- Added per-entry effective TTL, strict expiry-at-deadline behavior, and atomic `TakeExpired(now)` removal. The daemon rejects TTLs outside 3–3600 seconds before listener allocation or registry mutation.
- Removed runtime idle reaping and the `runtime_idle_ttl` field/flag/config mapping. A legacy config key now returns a migration error instead of being ignored.
- Narrowed public registration: Docker remains a trusted in-process source; ordinary HTTP callers may create only process/manual registrations and cannot submit owner, registration, or container identity fields.
- Extracted daemon Docker callbacks for fake-watcher tests; added bounded Docker list/inspect calls, periodic reconciliation, full registration forget callbacks, channel-close handling, and present-versus-inspect-failure reconciliation semantics.
- Moved CLI foreground registration onto the shared Go client handle. Added explicit `--http-host`, retained `--once` as a non-renewed diagnostic path, and added `services resolve --raw --wait` for scripts.
- Added/updated regression coverage for stale lease deletion, stale Docker removal, per-entry TTL renewal, atomic expiry, public Docker-source rejection, port/TLL validation, public replacement by delete/re-register, and overlapping real-Docker container replacement.

### Why
- A single backend authority eliminates registry/runtime disagreement; frontends remain a runtime concern and backends remain registry state.
- Conditional identity checks are small and sufficient for the observed stale removal failures. Logical owner keys still permit Docker recreation; concrete producer IDs prevent the old container from removing the new one.
- Retaining listeners removes the reaper’s registration race and makes within-run endpoint behavior simpler. Port reclamation is deferred until real high-churn usage demonstrates a need.
- The SDK and CLI now share lease behavior so their heartbeat, re-registration, cancellation, and cleanup rules cannot drift.

### What worked
- Full validation after the final single-authority refactor passed:

  ```bash
  GOWORK=off go build ./...
  GOWORK=off go vet ./...
  GOWORK=off go test -race ./... -count=1
  GOWORK=off make glazed-lint
  ```

- The updated review verification probe reported:

  ```text
  P01 old lease DELETE after replacement: status=ready stale_delete_rejected=true
  P02 stale foreign forget: registry=ready runtime_backend_nil=false
  P03 Docker reconcile replacement: container=new status=ready
  P06 requested TTL=60s: initial~60s renewed~60s
  P07 atomic expiry take: taken=1 renew_rejected=true leases=0
  ```

  Full output: `analysis/evidence/08-slice-a-review-probes.txt`.
- Manual isolated CLI smoke test built temporary binaries, used a temporary Unix socket/state file and Python loopback backend, and produced immediate output:

  ```text
  once={"endpoint":"127.0.0.1:16253",...}
  raw=127.0.0.1:16253
  keeper_bytes_before_interrupt=133
  ```

  The foreground Glazed command flushed its JSONL row before interrupt; `resolve --raw --wait` returned only the endpoint.
- Existing real Docker and PostgreSQL integration tests remained enabled and passed. The Docker recreation test now creates B before removing A, exercising the stale-old-container path.

### What didn't work
- The first compile after changing public registration fields failed in tests that still constructed `api.RegisterRequest{OwnerKey: ...}` and used `ForgetByOwner`; updated public tests to use daemon-issued identity or trusted daemon calls. Relevant diagnostic: `unknown field OwnerKey in struct literal of type api.RegisterRequest`.
- The first TTL regression assertion compared two consecutive `now + 60s` expirations and measured only call spacing: `renewal window = 5.139411ms, want ~60s`. Corrected the test to inspect time remaining after renewal.
- The initial review probe still called `DeleteRegistration` through a retired same-owner lease and panicked because retirement now correctly returns registration-not-found. Updated it to assert stale deletion rejection.
- No production behavior was rolled back. These were test/probe adaptation failures caused by the intentionally narrower public API.

### What I learned
- The existing Glazed processor emits the foreground `register --format jsonl` row before the command blocks; the manual smoke test established this rather than assuming `AddRow` streams.
- Validation must happen before frontend allocation. An early TTL bounds test exposed that validation at the end of `Register` left a name/runtime behind on a rejected request; moved TTL validation before all allocation and registry mutation.
- A race detector does not prove lifecycle semantics, but the conditional producer-ID path is now covered by ordinary regression tests and the historical probes.

### What was tricky to build
- The public API must not accept Docker source/identity, while the Docker watcher still needs stable owner key and container identity. Keeping the latter as a typed in-process daemon callback avoided a second admin endpoint.
- Removing duplicate backend state without changing TCP hot-path behavior required a provider closure that returns a registry snapshot at accept time. Existing connections still retain the backend selected at their accept.

### What warrants a second pair of eyes
- The deliberate policy to retain all service listeners until daemon shutdown; it is simpler than idle reaping but trades automatic port reclamation for stability.
- The five-second Docker repair interval and ten-second call bound; both are local-development defaults, not a claim of exactly-once event delivery.
- Caller context cancellation now stops a Go client’s heartbeat loop. This is documented in `pkg/devmesh`; verify it matches expected embedding usage.

### What should be done in the future
- Implement Slice B next: safe one-read HTTP target selection, fixed explicit host ownership, truthful frontend URLs, and synchronous listener binding.
- Then implement Slice C: prompt active-connection closure, fail-fast state persistence, socket-file safety, and combined acceptance behavior.
- Do not add a public hot-update API, event replay system, or listener eviction until a concrete workflow requires it.

### Code review instructions
- Start with `internal/registry/registry.go` (`ClearBackendIf`), `internal/daemon/daemon.go` (`endPublication`, `registerLocked`), `internal/runtime/service.go` (`CurrentBackend`), and `internal/lease/manager.go` (`TakeExpired`).
- Then review `internal/dockerwatch/watcher.go` and the CLI/client lifecycle pair.
- Reproduce with `GOWORK=off go run ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/scripts/01-review-probes.go` and run the full validation gate above.

### Technical details
- Slice A task: `5c9w`.
- Evidence: `analysis/evidence/08-slice-a-review-probes.txt`.
- The task is complete after the associated implementation commit; Slice B and C remain open.

## Step 16: Implement Slice B — safe fixed HTTP routes and truthful endpoints

This slice made the supported HTTP path small and explicit. A route has one canonical explicit hostname, and a service’s kind/hostname cannot mutate during a daemon run. HTTP routing selects one current backend snapshot per request and always overwrites the outbound target from that snapshot. This removes the prior double-read failure where a backend change could leave an absolute caller URL in the proxy request.

Configured proxy listeners are now bound in `daemon.New`, before the Unix administrative API is served. A daemon with an occupied required HTTP/HTTPS listener exits rather than reporting a healthy daemon that cannot serve its advertised endpoint. The router builds frontend URLs from the actual public scheme and non-default configured port, and the CLI/SDK preserve `frontend.url` rather than formatting an empty host/port for HTTP services.

### Prompt Context
**User prompt (verbatim):** see Step 15.
**Assistant interpretation:** Continue the focused implementation plan with the supported HTTP path, keeping explicit hostnames and avoiding route-migration or backend-protocol machinery.
**Inferred user intent:** Make real local HTTP use reliable through current interfaces, not build a generalized gateway.

### What I did
- Replaced `ReverseProxy.Director` with `Rewrite` plus a request-context route snapshot. `ServeHTTP` calls the registry-backed provider once, returns 503 if nil, and `Rewrite` unconditionally uses the selected loopback backend.
- Removed caller-controlled `X-Forwarded-*` values before calling `SetXForwarded`; preserved the canonical public route host for applications.
- Added public scheme/port-aware router URLs. HTTP uses `http://host:nondefault-port`; TLS uses `https://host:nondefault-port`.
- Bound HTTP/HTTPS listeners synchronously in `daemon.New`, closed a previously bound HTTP listener if HTTPS binding fails, and changed `Start` to serve those listeners.
- Added daemon checks for kind immutability, hostname immutability, canonical hostname conflicts, and malformed explicit HTTP host values.
- Removed the registry fallback that reused a prior frontend whenever `Frontend.Host` was empty; URL frontends are now written explicitly.
- Added proxy, daemon, HTTP integration, HTTPS URL, and black-box probe coverage.

### Why
- The registered backend and the proxy target must be the same request-local value. A single snapshot is simpler and safer than coordinating two provider calls.
- Rejecting host/kind mutation is intentionally simpler than implementing route migration, stale alias cleanup, or dynamic endpoint updates.
- Binding first is the same correctness principle used by TCP frontend allocation: a successful startup must own the advertised listener now, not merely schedule a future attempt.

### What worked
- `internal/proxy.TestRouterSelectsBackendOnce` sends an absolute-form request toward an unintended local server and confirms `200/SAFE` from the registered backend with exactly one provider call.
- `TestHTTPHostnameConflictAndMutationRejected` confirms duplicate canonical hostnames fail and leave the original route intact.
- `TestHTTPListenerMustBindBeforeStartupSucceeds` confirms an occupied listener makes `daemon.New` fail.
- Black-box receipt `analysis/evidence/10-slice-b-cli-api-probes.txt` reports:

  ```text
  P13 occupied HTTP listener rejected=True socket_created=False
  P14 public Docker-source rejected=True code=400
  P15 HTTP registration status=201 url=http://web.test:50801
  P16 CLI HTTP resolution endpoint=http://web.test:50801
  P17 public owner replay rejected=True backend_unchanged=True
  ```

- Full validation passed: `go build`, `go vet`, `go test -race ./... -count=1`, and `make glazed-lint`.

### What didn't work
- The historical verification probe initially panicked at P04 because it still treated duplicate host acceptance and host mutation as expected behavior. Rewrote it to assert conflict/mutation rejection and a safe one-read proxy result.
- The old black-box probe assumed an occupied HTTP listener still exposed a healthy Unix API. It now uses two phases: assert failed startup first, then start a healthy daemon for public-API assertions.
- No production failures remained after updating the evidence programs; these were expected changes to historical probes after correcting the contract.

### What I learned
- Go’s `httputil.ProxyRequest.SetURL` resets the outbound Host; explicitly restoring the canonical public route host is necessary when applications route on Host.
- `SetXForwarded` appends existing forwarded values, so removing incoming forwarded headers first is necessary when they are not trusted.
- URL correctness includes scheme and listener port. A stable hostname alone is not a usable endpoint when devmesh listens on 8088/8443.

### What was tricky to build
- Listener ownership begins before `Start`, while service shutdown is still context-driven. Binding during `New` made the startup contract truthful without adding a separate readiness service.
- The router remains dynamic without duplicate reads by carrying a copied backend in request context. The backend is selected once per request; existing TCP behavior remains unchanged.

### What warrants a second pair of eyes
- Explicit hostname validation is intentionally narrow (no scheme, path, or port); expand only if an actual supported hostname form needs it.
- HTTPS URL selection currently prefers HTTPS whenever a certificate is configured, even if the separate plain HTTP listener is also enabled. That is intentional for a single public endpoint choice.

### What should be done in the future
- Implement Slice C: prompt close of active TCP connections, fail-fast persistent port allocation, Unix socket path safety, minimal doctor corrections, and combined acceptance behavior.
- Do not add aliases, generated names, HTTP backend TLS, ACME, or route migration without a concrete workflow.

### Code review instructions
- Read `internal/proxy/http.go` (`ServeHTTP`, `Rewrite`), `internal/daemon/daemon.go` (`bindProxyListeners`, HTTP registration validation), then `integration/http_proxy_test.go` and `internal/proxy/http_test.go`.
- Run `python3 ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/scripts/02-cli-api-probes.py` for isolated binary-level evidence.

### Technical details
- Slice B task: `nipk`.
- Evidence: `analysis/evidence/09-slice-b-review-probes.txt` and `analysis/evidence/10-slice-b-cli-api-probes.txt`.
- The task is complete after the associated implementation commit; Slice C remains open.

## Step 17: Implement Slice C — prompt cleanup, durable allocation, and acceptance evidence

This final focused slice closes the resources devmesh owns without reintroducing a configurable drain policy. TCP runtimes now track accepted client sockets and proxy workers. Shutdown stops listener admission, closes active client sockets promptly, and waits under the existing overall shutdown context. This intentionally favors a quick, deterministic local restart over preserving in-flight transactions through a separate draining lifecycle.

New frontend allocation now treats persistence as part of success. If the remembered port assignment cannot be written, the newly bound listener is closed and registration fails. The state store remembers that the write is dirty so retrying the same value cannot falsely report success. Unix socket stale recovery now refuses to replace a regular file or symlink. The Docker adapter rejects a mixed loopback/wildcard publication for a managed target, rather than selecting one safe binding and ignoring another exposed binding.

### Prompt Context
**User prompt (verbatim):** see Step 15.
**Assistant interpretation:** Complete the focused v2 implementation with minimal resource ownership, durability, safety, and supported-workflow acceptance behavior.
**Inferred user intent:** Finish reliable local workflows without expanding into drain policies, persistence modes, or platform/security frameworks.

### What I did
- Added runtime connection tracking and proxy wait groups. `runtime.Manager.Shutdown(ctx)` closes listeners, closes active client sockets, and waits for workers with the caller’s existing deadline.
- Changed daemon shutdown to use the runtime shutdown path.
- Made allocator persistence failures close the candidate listener and return an error; added state dirty tracking so same-value persistence retries do real I/O.
- Made corrupt-state backup failure explicit instead of silently continuing without preserving bytes.
- Added `Lstat` safety checks to reject regular files and symlinks at the Unix socket path.
- Rejected mixed Docker loopback/wildcard bindings by checking every binding for the managed container target.
- Corrected `doctor` to inspect the selected `--socket`/`DEVMESH_SOCKET` path rather than always the default path.
- Extended PostgreSQL acceptance: query through the stable frontend, start replacement B before removing A, remove A, then query through the unchanged frontend again.
- Updated README and resolve help to document `register` lifetime, `--raw --wait`, explicit HTTP host behavior, and no automatic listener eviction.

### Why
- Closing a listener alone stops new accepts but leaves existing TCP copies alive. Tracking owned client sockets is enough to interrupt existing `io.Copy` paths; it avoids a complex per-connection drain state machine.
- A stable frontend promise includes remembered state. A listener with an unsaved assignment is not durable enough to advertise as successful.
- Socket-file and mixed-publication checks are inexpensive targeted guards with direct data-loss/exposure impact; they do not require a broad platform threat-model project.

### What worked
- New regression tests passed for persistence failure listener cleanup, dirty-state retry, regular-file/symlink socket refusal, mixed Docker publication refusal, and active TCP shutdown closure.
- Updated verification receipt `analysis/evidence/11-slice-c-review-probes.txt` reports all historical P01–P12 observations corrected or deliberately deferred. Relevant final lines:

  ```text
  P08 mixed loopback+wildcard rejected=true
  P09 regular socket path rejected=true file_preserved=true
  P11 allocation persistence failure rejected=true ... retry_rejected=true
  P12 established TCP after daemon Shutdown: read_error=EOF ...
  ```

- Real Docker acceptance ran explicitly and passed:

  ```text
  TestDockerDiscoveryAndRecreatePreservesFrontend (1.66s)
  TestPostgresThroughFrontend (2.12s)
  ```

  The PostgreSQL test now performs `select 1` through the stable endpoint both before and after the overlapping replacement/removal sequence.
- Final gate passed:

  ```bash
  GOWORK=off go build ./...
  GOWORK=off go vet ./...
  GOWORK=off go test -race ./... -count=1
  GOWORK=off make glazed-lint
  ```

### What didn't work
- The first active-connection shutdown assertion reported that the connection survived. The test had read only two bytes from the three-byte `A:a` echo response, leaving one buffered byte that made the post-shutdown read appear successful. Changed the test to consume all three bytes; it then observed EOF after shutdown.
- No production retry/fix loop was required beyond that test correction.

### What I learned
- Closing the client side of an active proxy connection is sufficient here: `proxy.TCP`’s existing defers close the upstream side once copying unblocks.
- A state store must distinguish an equal in-memory value from a successfully persisted value; a dirty bit is enough for this single-file store.
- The important Docker publication safety condition concerns all host bindings for the managed target, not merely the binding devmesh chooses to dial.

### What was tricky to build
- Preventing `sync.WaitGroup` Add/Wait misuse required closing runtime admission under the same connection mutex used to add proxy workers. This is limited ownership bookkeeping, not a general connection lifecycle framework.
- The overlapping Docker replacement test starts B before removing A and validates the real PostgreSQL protocol after removal; it is stronger than comparing port metadata alone.

### What warrants a second pair of eyes
- Promptly closing active local TCP connections is an intentional contract change from the original graceful-drain suggestion. It is appropriate for the focused local tool but should be revisited only if a concrete workflow needs transaction-preserving shutdown.
- Corrupt-state backup now fails startup if the corrupt bytes cannot be saved aside. This protects the user’s only state copy but may require an explicit recovery command if real users encounter filesystem permission problems.

### What should be done in the future
- The three v2 slices are complete. Implement the separate CLI-first devctl integration task only after reviewing the dedicated plan: prove Compose-to-shell-launcher behavior before adding optional native wrapper fields.
- Keep event replay, idle eviction, volatile persistence mode, ACME, generated hostnames, and generalized diagnostics deferred until a concrete user impact meets the v2 escalation rule.

### Code review instructions
- Read `internal/runtime/service.go` and `manager.go` for admission/closure ordering; then `internal/runtime/allocator.go`, `internal/state/store.go`, and `internal/transport/unix.go`.
- Review `integration/postgres_test.go` for the post-recreation query acceptance path.
- Run the final validation gate and the review probe listed above.

### Technical details
- Slice C task: `zo9w`.
- Evidence: `analysis/evidence/11-slice-c-review-probes.txt`.
- The v2 devmesh implementation is complete at the associated commit. The separate devctl integration task remains open.
