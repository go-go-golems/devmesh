---
Title: Implementation review and hardening design for a new intern
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
DocType: analysis
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://internal/daemon/daemon.go
      Note: Lifecycle coordination and stale removal findings
    - Path: repo://internal/dockerwatch/watcher.go
      Note: Reconciliation and incarnation identity findings
    - Path: repo://internal/lease/manager.go
      Note: TTL and atomic expiration findings
    - Path: repo://internal/proxy/http.go
      Note: Double backend lookup and route ownership findings
    - Path: repo://ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/scripts/01-review-probes.go
      Note: Reproducible P01-P12 domain and network evidence
    - Path: repo://ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/scripts/02-cli-api-probes.py
      Note: Reproducible P13-P17 binary and Unix API evidence
ExternalSources: []
Summary: Evidence-backed assessment of devmesh at 5fbf70c, with system orientation, reproduced lifecycle and routing defects, proposed ownership and shutdown contracts, and a phased intern implementation plan.
LastUpdated: 2026-09-20T21:00:00Z
WhatFor: Review the implementation against its original contract and define the next correctness-focused implementation cycle.
WhenToUse: Read before extending devmesh or treating the original implementation milestone as fully accepted.
---

# Devmesh implementation review and hardening design

## 1. What the system does, and what this review concludes

A development application can ask the operating system to choose its listening port by binding `127.0.0.1:0`. Docker can similarly publish a container's internal port on an automatically selected host port. Both avoid fixed-port conflicts, but the selected address can change on restart. A consumer, such as a PostgreSQL client, then needs a way to discover where to connect.

Devmesh separates the address a producer listens on from the address a consumer uses. The **backend** is the producer's current address. The **frontend** is a listening address owned by devmesh. A **logical service name**, such as `checkout.postgres`, identifies the association between them. The **registry** stores these associations, and the **proxy** forwards connections from frontend to backend. Producers register backends with a local daemon; consumers resolve names through that daemon and connect to its frontends.

For example, Docker can publish PostgreSQL on `127.0.0.1:49173` while devmesh listens on `127.0.0.1:15432`. Recreating the container can change the backend to `127.0.0.1:49901` without requiring the frontend to change. This is the intended value of the system, not a promise that every connection survives a database restart. Existing connections cannot be migrated to a new database process. New connections should use the replacement backend.

```text
Producer                      devmeshd                      Consumer
--------                      --------                      --------
PostgreSQL :49173 <-- bytes -- frontend :15432 <-- bytes ---- pgx
       |                         ^
       +-- Docker metadata ---- registry <-- resolve name -- CLI

After recreation:
PostgreSQL :49901 <-- bytes -- same frontend :15432
```

**Assessment:** retain this architecture, but run a correctness-hardening cycle before declaring the original acceptance contract complete. Bind-first allocation, component boundaries, ordinary TCP forwarding, and basic registration are good foundations. The present implementation also has confirmed defects in replacement ownership, lease expiration, HTTP routing, endpoint publication, persistence reporting, and resource shutdown. These are not reasons to introduce a different framework or a distributed registry. They are reasons to make identity, authority, and lifecycle transitions explicit.

The existing race-enabled suite passed during this review, including real-Docker and PostgreSQL tests. Seventeen ticket-local probes nevertheless demonstrated contract violations. A passing race detector does not rule out a logically incorrect sequence of individually synchronized operations. Several important failures here are completely sequential.

### 1.1 Reading order for a new intern

1. Read sections 2–4 to understand names, registrations, listeners, and the request paths.
2. Read findings F01–F04 before changing registration or Docker code. They explain the highest-priority correctness problems.
3. Read sections 7–9 for the proposed interfaces, transition rules, and implementation sequence.
4. Use section 10 as the acceptance-test checklist and section 12 as the source index.

All proposed APIs and algorithms below are **design sketches**, not claims that the code already implements them. File and line references describe revision `5fbf70cb3bf0d215185351af3eac3bea0b195e0d`; search by symbol after refactoring.

## 2. Review scope and evidence

### 2.1 Inputs and their authority

The original brief is `devmesh-implementation-guide.md` (2,476 lines). The ticket contains its byte-identical imported copy at `sources/devmesh-implementation-guide.md`. The expanded design is `design-doc/01-devmesh-intern-analysis-and-implementation-guide.md` (3,454 lines). The implementation history is `diary/01-devmesh-ticket-diary.md` (531 lines before this review). All three were read in full for this assessment.

These inputs have different roles. The brief specifies intended behavior. The design explains proposed implementation and contains later revisions to the CLI/configuration contract. The diary records decisions, experiments, and acknowledged omissions. None of them is a substitute for inspecting the implementation. In particular, the mirrored vault report in `various/vault-project-report--devmesh-deep-dive.md` overstates several guarantees that the code does not provide; section 11 records corrections without rewriting that historical artifact.

The review read the production domain packages, daemon orchestration, HTTP API and transports, public Go client, command implementations, configuration, relevant framework parsing code, and the unit/integration tests. No production Go files were changed. The added Go and Python programs are review artifacts under the ticket's `scripts/` directory.

### 2.2 Current validation

Commands executed against the implementation:

```bash
GOWORK=off go test -race ./... -count=1 -v
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off make glazed-lint
```

All returned success. The full race-test output is `analysis/evidence/01-baseline-race.txt`; lint output is `analysis/evidence/02-glazed-lint.txt`. The baseline ran before adding the review programs. Docker tests were not skipped in this run: `TestDockerDiscoveryAndRecreatePreservesFrontend` passed in 1.15 seconds and `TestPostgresThroughFrontend` passed in 1.60 seconds. These are test-run observations, not performance benchmarks.

Reproduction commands, run from the repository root:

```bash
T=ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide
GOWORK=off go run "$T/scripts/01-review-probes.go"
python3 "$T/scripts/02-cli-api-probes.py"
```

Outputs are retained in `analysis/evidence/03-review-probes.txt` and `04-cli-api-probes.txt`. The Go probes exercise real domain objects, real loopback connections, and a fake Docker inventory. The Python probes build temporary binaries, start an isolated daemon, and use the real Unix HTTP API and CLI. They do not operate on the developer's normal daemon. The HTTP misrouting probe uses only local test servers; no remote target was contacted.

The programs print observed behavior and are not a replacement for regression tests. Their successful exit means the reproduction ran, **not** that the intended product contract passed. Labels P01–P17 identify the observations cited below.

### 2.3 Boundaries of confidence

This is not a penetration test, a load test, a macOS certification, or a complete TLS interoperability study. Lifecycle interleavings and DNS behavior identified only through code inspection are labeled as such. Active TCP retention after `Daemon.Shutdown` was directly reproduced; an unbounded goroutine growth benchmark was not. The existing PostgreSQL test executes a query, but does not execute a second query after recreating the database.

## 3. Architecture and responsibilities

### 3.1 Four entry points, one domain service

`devmeshd` is the long-running process. `devmesh` is its command-line consumer. `pkg/devmesh` is a producer library for Go applications. `internal/dockerwatch` is a producer adapter driven by Docker metadata and events. The command layer uses Glazed for typed settings, output rows, help, and configuration sources. The domain code uses plain Go types, `net/http`, synchronization primitives, and `log/slog`.

```text
                    Control path: metadata and lifecycle

cmd/devmesh  ---- HTTP over Unix socket ----> internal/api
                                                   |
pkg/devmesh  ---- HTTP over Unix socket -----------+
                                                   v
Docker Engine --> dockerwatch callbacks ------> daemon.Daemon
                                                   |
                       +---------------------------+----------+
                       v                           v          v
                 registry.Registry           runtime.Manager lease.Manager
                       |                           |
                       |                    bind-first allocator
                       |                           |
                       |                        state.Store
                       v
                  HTTP Router

                      Data path: application bytes

TCP consumer --> ServiceRuntime.Accept --> proxy.TCP --> backend
HTTP consumer --> Router.ServeHTTP     --> ReverseProxy --> backend
```

A **control path** changes or queries routing metadata. A **data path** forwards application traffic. This distinction matters for locking: slow control-path operations should not block existing byte forwarding. The current TCP backend pointer is atomic, so a registry mutation lock is not acquired on every connection. However, atomic pointers alone do not make a multi-object service transition atomic.

### 3.2 Package map

| Package or file | Current responsibility | First symbol to inspect |
| --- | --- | --- |
| `cmd/devmeshd/cmds/serve.go` | Decode configuration, create daemon, serve Unix API, initiate shutdown | `ServeCommand.Run` |
| `internal/daemon/daemon.go` | Coordinate service registration, leases, runtimes, HTTP routes and Docker | `Register`, `sweepLeases`, `ForgetByOwner` |
| `internal/api` | Versioned HTTP routes, JSON DTOs, error mapping | `Server.routes`, `handleRegister` |
| `internal/transport` | Unix listener and HTTP client | `Listen`, `Client.DoAuth` |
| `internal/registry` | Name-keyed metadata, ownership checks and read snapshots | `CreateOrReplaceOwned` |
| `internal/runtime` | Bound TCP listeners, backend pointers, idle reclamation | `EnsureTCPRuntime`, `Reap` |
| `internal/proxy` | TCP byte forwarding and hostname-based HTTP forwarding | `TCP`, `Router.ServeHTTP` |
| `internal/lease` | Lease IDs, secret tokens and expiration timestamps | `Renew`, `Expired` |
| `internal/state` | Durable preferred TCP port assignments | `SetPort`, `saveLocked` |
| `internal/dockerwatch` | Inspect publications, watch events, reconcile containers | `Watcher.Reconcile` |
| `pkg/devmesh` | Public producer handle and listener helper | `Register`, `ListenTCP` |
| `cmd/devmesh/cmds` | Structured CLI operations | `services.Endpoint`, register command |
| `pkg/doc` | Embedded help Markdown | `AddDocToHelpSystem` |

The domain packages correctly avoid importing Glazed. There is a separate dependency issue: `pkg/devmesh` imports `internal/api` for DTOs, while that package imports the entire daemon. Consequently, the public client has a dependency path into Docker and daemon implementation code. Moving wire types into a dependency-light package would improve the boundary without changing the transport or introducing code generation.

### 3.3 The identities that must not be confused

The current data model has a service name, an owner key, a registration ID, a lease token, and a Docker container ID. They answer different questions:

- **Service name:** which logical endpoint is this? Example: `checkout.postgres`.
- **Owner key:** which logical producer is allowed to replace its backend? Example: `docker:checkout:db:5432`.
- **Registration ID:** which particular lease entry is this? A random value is generated unless the API caller supplies one.
- **Lease token:** which secret authorizes heartbeat or deletion of that registration? It is generated with `crypto/rand`.
- **Container ID:** which concrete container incarnation supplied this backend? It changes on recreation even when the owner key remains the same.

The absent concept is a consistently enforced **current incarnation**: an identity for the exact backend publication now installed. Logical ownership allows a replacement. Incarnation identity prevents a delayed removal of the old backend from removing the replacement. Findings F01 and F02 arise because the implementation uses logical ownership where incarnation comparison is needed.

## 4. Current behavior, traced end to end

### 4.1 Native TCP registration

`pkg/devmesh.ListenTCP` binds `127.0.0.1:0`, then calls `Register` with the selected address. If registration fails, the listener is closed. This ordering is correct: the producer owns its backend socket before advertising it. `ListenerHandle.Close` closes that application listener and then the registration handle.

The daemon's registration path is serialized by `Daemon.mutate`. It validates the service name and loopback backend, derives an owner and registration ID, checks name ownership, ensures a TCP runtime, installs the registry record, sets the runtime backend, and finally creates a lease token/entry for non-Docker sources. A registration result contains the frontend and lease credentials.

```text
Current TCP register sequence
-----------------------------
validate request
  -> check logical owner
  -> bind/reuse frontend runtime
  -> publish registry record
  -> publish runtime backend
  -> generate token and add lease
  -> return registration response
```

This is a useful straight-line implementation, but the publication ordering deserves attention. The backend is visible before the lease exists. More importantly, replacement creates another lease without consistently retiring or invalidating the old lease. A later deletion of the old lease still operates by owner/name, not by current publication identity.

### 4.2 Bind-first allocation and durability

`internal/runtime/allocator.go:43` tries the remembered port, then the requested preferred port, then a deterministic FNV-1a offset across the configured range. Every attempt uses `net.Listen`. A successful allocation returns the port **and the still-open listener**. That prevents another process from claiming the port between checking it and using it.

The default range is 15000–19999. It is a scan range, not a hard restriction on remembered/preferred ports: those are tried first even outside the range. TCP stability is best effort across daemon restarts. If another process has acquired the remembered port, the allocator logs a warning and falls back. The state file contains preferences, not kernel reservations. Ports are rebound as producers re-register or Docker reconciliation discovers them, rather than eagerly reserving every persisted entry at daemon startup.

`state.Store` writes a temporary file in the destination directory, syncs it, closes it, and renames it over the state file. That avoids truncating the live file mid-write. It does not make failed writes successful: the current allocator logs a persistence error but still reports allocation success. F11 covers this distinction.

### 4.3 TCP forwarding and inactive runtimes

`ServiceRuntime.acceptLoop` loads the current backend, rejects connections if none is present, acquires a per-runtime semaphore slot, and starts `proxy.TCP`. The semaphore has capacity 4,096. The proxy dials with a three-second timeout and copies bytes in both directions, propagating `CloseWrite` on TCP connections. Existing connections retain their selected backend; later connections load the new backend pointer.

The idle reaper runs every thirty seconds and closes runtimes with no backend after the configured idle grace, default ten minutes. The registry record survives, so `unavailable` does not always mean the listener is still bound. A consumer should interpret that status as “no current backend,” not as proof of a connectable frontend. The current shutdown closes listening sockets but does not track or drain accepted connections; F10 addresses that omission.

### 4.4 Leases and client recovery

The lease manager holds entries in memory. Renewal and deletion verify the token with constant-time comparison. The daemon scans expired entries once a second. Docker publications do not use these leases because Docker lifecycle observations are intended to supply liveness.

The Go client sends heartbeats and, on HTTP 404, attempts a fresh registration. That is the intended daemon-restart recovery mechanism. Its current interval is based on a locally assumed TTL rather than the server's returned expiration; the interval is also clamped to at least one second. Custom TTL renewal and short TTL scheduling are inconsistent, as F05 explains. The baseline client test uses a one-second TTL and observes it for only 1.8 seconds, which is insufficient to establish robust deadline behavior under scheduling delays.

### 4.5 Docker discovery

Labels opt a container into registration. `RegistrationFromInspect` looks up the configured container port in `NetworkSettings.Ports`, prefers a loopback host binding, and produces a registration. The watcher lists running containers at startup, handles `start`/`restart` with inspect-and-register, handles `die`/`stop`/`destroy` with forget, and reconciles after stream failures.

This is the right adapter model: neither the registry nor TCP forwarding needs Docker APIs. The adapter does need stronger event semantics. A snapshot of running containers and an event subscription are not an atomic operation. In addition, the callback for forgetting an old container carries only owner and name, discarding the very container ID needed to distinguish an old incarnation from its replacement.

### 4.6 HTTP and TLS

The HTTP router maps normalized `Host` values to backend providers. Unlike generic TCP, HTTP carries a hostname in the request, so one listener can route multiple services. TLS wraps the same router and loads an existing PEM certificate/key pair. ACME is correctly absent.

Current behavior is narrower than the planned HTTP contract. Hostnames are explicitly supplied; `BaseDomain` is not used for generation. The router is constructed with frontend scheme `http`, omits the configured listener port from URLs, and always dials HTTP backends. Native/CLI registration does not expose `HTTPHost`, despite advertising an HTTP kind. The HTTP integration tests bypass these consumer interfaces by sending explicit Host headers to known listener addresses.

## 5. Findings, priorities, and evidence

Priority **P1** means fix before relying on the affected routing/lifecycle guarantee. **P2** means important operational or maintainability work after the immediate correctness changes. Priorities assume a local, single-user development tool. User-only Unix permissions remain the primary administrative boundary; the findings do not imply an unauthenticated remote admin API exists.

| ID | Priority | Finding | Evidence |
| --- | --- | --- | --- |
| F01 | P1 | Old Docker/removal events disable current backends | P02, P03 |
| F02 | P1 | Old leases and split expiry operations remove current work | P01, P07 |
| F03 | P1 | Public registration lets callers assert ownership and bypass leases | P14, P17 |
| F04 | P1 | HTTP backend is read twice; disappearance can proxy a caller URL | P10 |
| F05 | P1 | Lease TTL negotiation and heartbeat scheduling disagree | P06 and source |
| F06 | P1 | HTTP host conflicts and route lifecycle are not enforced | P04, P05 and source |
| F07 | P1 | HTTP endpoints, CLI, SDK, and listener readiness disagree | P13, P15, P16 |
| F08 | P1 | Docker reconciliation has subscription gaps and weak error semantics | Source; P03 illustrates replacement failure |
| F09 | P1 | Safety checks can accept mixed exposure or overwrite a regular file | P08, P09; DNS risk from source |
| F10 | P1 | Shutdown does not drain or force-close accepted TCP connections | P12 |
| F11 | P2 | Persistence failure is hidden from successful registration | P11 |
| F12 | P2 | Config validation, diagnostics, logging and tests are incomplete | Source and baseline inventory |

### F01. Removal must identify the current backend incarnation

**Sources:** `internal/dockerwatch/watcher.go:118–151,173–194,239–244`; `internal/daemon/daemon.go:447–453`; `internal/registry/registry.go:72–86`.

`Watcher.Reconcile` registers the new container before forgetting tracked containers absent from the new list. Suppose it previously tracked A and now sees replacement B. A and B intentionally have the same logical owner key. The watcher registers B, then forgets A. `ForgetByOwner` marks the record unavailable using that shared owner key, so B is disabled immediately.

The deterministic P03 probe performs exactly two reconciliations. Its output is:

```text
Docker reconcile replacement: container=new status=unavailable
```

An event sequence `start(B)` followed by `destroy(A)` has the same issue. The present recreation test removes A before starting B, which avoids the overlap that reveals the bug.

There is a second failure. `ForgetByOwner` calls `Runtime.ClearBackend(name)` even if `Registry.MarkUnavailable(ownerKey)` returned false. P02 calls forget with an obsolete owner against a currently ready service and produces `registry=ready runtime_backend_nil=true`. The API says ready while the TCP data path closes new connections.

**Required change:** pass service name, owner key, and concrete incarnation through removal. Clear the registry and runtime only if all match the currently installed publication. A stale event must be a no-op, not a partial update.

### F02. Lease deletion and expiry need conditional, atomic removal

**Sources:** `internal/daemon/daemon.go:236–244,426–441`; `internal/lease/manager.go:62–92,110–138`; `internal/registry/registry.go:72–86`.

P01 registers A, registers B with the same owner/name, then deletes A using A's valid token. B becomes unavailable. A's token should authorize ending A's publication, not whichever backend happens to share its owner key later. The daemon never associates the service record with the currently active registration ID, and replacement does not retire the old entry.

Expiry also has a time-of-check/time-of-use problem:

```text
Sweeper                         Heartbeat
-------                         ---------
Expired(now) returns entry E
                                Renew(E) succeeds
Remove(E.ID) deletes renewed E
clear backend
```

`Expired` and `Remove` are separate locked operations. P07 reproduces this interleaving at the lease-manager boundary by selecting with an explicit future time, renewing, then applying the selected removal. It does not rely on thread timing. The result is zero leases after a successful renewal. In production, an entry selected just after its deadline can likewise be renewed before removal because `Renew` does not reject elapsed entries.

**Required change:** use a single `TakeExpired(now)` operation that selects and removes under the lease lock, reject renewal after the defined expiration boundary, and condition backend clearing on current publication identity. Define how replacement retires the previous lease. Tests should control time rather than sleep around a one-second sweep.

### F03. An owner key is not proof of authority

**Sources:** `internal/api/dto.go:56–69`; `internal/api/handlers.go:58–88`; `internal/daemon/daemon.go:315–335,379–401`.

The HTTP registration request accepts `source`, `owner_key`, `registration_id`, and `docker_container_id`, and the handler forwards them to the domain method. Selecting `source=docker` skips lease creation. P14 registers such a publication over the public Unix API and receives 201 with no token and no expiration. Unless a watcher tracks that publication, it has no automatic removal mechanism.

An API caller can also inspect another record's owner key, submit that owner key with a replacement backend, and replace the service without a token. P17 changes the backend from port 49900 to 49901 this way. Callers can supply colliding registration IDs as well: `AddWithTTL` replaces the entry in the ID map, potentially leaving an earlier service without its own lease record.

This does not bypass Unix filesystem permissions. It does mean the advertised lease authorization and ownership contract fails to protect against accidental or adversarial same-user producers. Treat owner keys as metadata, not secrets.

**Required change:** split external producer creation from trusted Docker upsert. Generate process/manual IDs and owners in the daemon. Reject reserved identity fields on the public create endpoint. Provide a token-authorized update operation if same-registration backend replacement is needed. Docker should use a typed internal method unavailable through ordinary HTTP registration.

### F04. HTTP routing must choose one immutable backend per request

**Sources:** `internal/proxy/http.go:43–64,91–104`.

`ServeHTTP` calls the provider to decide whether a route is available. `ReverseProxy.Director` calls it again to select the target. The backend can disappear between those calls. When the second call returns nil, the director does not clear or set the outgoing URL. With an ordinary origin-form request this can produce a proxy error rather than the promised 503. With an absolute-form request, the request already contains a scheme and target host.

P10 supplies an absolute URL to an unrelated local test server while setting Host to a registered route. The provider returns a backend on the first call and nil on the second. The response is:

```text
backend disappears between provider calls: status=200 body=UNINTENDED
```

The router forwarded to the caller-supplied URL, not a registered backend. This is a target-selection defect with server-side request-forgery potential. The reproduction contacted only a local server and does not claim a remote exploit was performed. Loopback frontend binding is not an authorization policy for where the proxy may dial.

**Required change:** load the route/backend snapshot once, reject nil, and unconditionally construct the outgoing scheme and authority from that snapshot. Never leave caller-supplied routing authority in the outgoing request. Use `ReverseProxy.Rewrite` with an explicit forwarded-header trust policy rather than preserving arbitrary inbound `X-Forwarded-Host`. Current code also omits a trustworthy `X-Forwarded-Proto` and rewrites `req.Host` to the normalized route hostname rather than preserving the original authority including its port.

### F05. The server and client do not agree on the effective lease TTL

**Sources:** `internal/lease/manager.go:21–27,62–92`; `pkg/devmesh/client.go:87–151`; `cmd/devmesh/cmds/register/register.go` heartbeat loop.

`AddWithTTL` applies the requested duration to initial expiration, but the entry does not store that duration. `Renew` always uses the manager's default. P06 requests sixty seconds and observes a renewal of approximately fifteen seconds. A client that schedules renewal every twenty seconds from the original sixty-second request can expire after its first successful heartbeat.

The public client ignores the server's `expires_at`, assumes fifteen seconds when no override is supplied, and creates its ticker once. A daemon configured for a shorter default therefore expires a default client before its first heartbeat. For a requested one-second TTL, the minimum one-second interval is at the expiration boundary, not safely before it. The retry loop sleeps for backoff and then waits for the regular ticker, so its timing is not the standalone retry schedule described in the guide. There is no jitter.

Other lifecycle gaps are visible in the same code: heartbeat context is derived from `context.Background`, not the caller context; initial registration is attempted once; `Close` captures the ID/token before waiting for an in-flight re-registration, which can publish a newer ID while close is using the old one. These are source-derived risks, not additional stress-test results.

**Required change:** store effective TTL per entry and return it as an explicit duration or a renewal recommendation. Use it after every create/renew response. Replace the fixed ticker with a context-aware timer that schedules either renewal or retry. Set a supported minimum TTL and request timeout that leave real renewal margin. Decide explicitly whether caller cancellation closes a handle or merely bounds creation; document and test that contract.

### F06. HTTP hostname ownership and replacement are incomplete

**Sources:** `internal/daemon/daemon.go:351–377`; `internal/proxy/http.go:43–80,107–119`; `internal/registry/registry.go:37–51`.

Different service names can claim the same HTTP hostname. `Router.Set` silently overwrites its map entry. P04 registers two services for `same.test`; the provider points to the second backend, with no conflict. The diary already acknowledges this in Step 7, whereas the mirrored report incorrectly says uniqueness is enforced.

Changing an HTTP service's hostname leaves the old route installed. No daemon call to `Router.Delete` accompanies replacement or kind changes. In addition, `CreateOrReplaceOwned` treats an empty `Frontend.Host` as “reuse the old frontend.” HTTP frontends intentionally carry their address in `URL`, with empty Host. P05 changes `http.a` to `new.test` and observes the record hostname `new.test` but advertised URL `http://same.test`.

Kind changes are also not controlled. Replacing TCP with HTTP can retain a TCP runtime and stale backend; replacing HTTP with TCP can leave the old hostname provider active. Because a provider resolves by name without checking kind, an old HTTP route can follow a later TCP record.

**Required change:** define a canonical hostname type, a hostname-to-service ownership index, and atomic route replacement. Make kind immutable for an existing service unless an explicit teardown/migration operation is introduced. Replace the “empty Host” sentinel with explicit frontend variants or a complete frontend value. Normalize port stripping and trailing-dot handling in one operation; the current early return after `SplitHostPort` means `example.test.:8088` is normalized differently from `example.test.`.

### F07. Advertised HTTP endpoints are not usable through normal clients

**Sources:** `internal/daemon/daemon.go:68,141–187,351–357`; `internal/proxy/http.go:86–88`; `cmd/devmesh/cmds/services/list.go:97–113`; `services/resolve.go`, `services/inspect.go`; `pkg/devmesh/types.go`; `internal/daemon/daemon.go:85–99`.

The router is created with scheme `http` even when TLS is configured. Its frontend URL omits the configured nonstandard port. P15 reports `http://web.test` although the configured listener was on port 58255. For default HTTP/HTTPS high ports, consumers would go to port 80 rather than 8088 or 8443.

CLI list/resolve/inspect format only `Frontend.Host` and `Port`, discarding `URL`. P16 gets an empty endpoint for a ready HTTP service. `RegistrationOptions` has `KindHTTP` but no `HTTPHost`; the CLI also accepts kind without an HTTP-host field. Their request cannot satisfy the daemon's HTTP registration requirement. The Go client's endpoint formatter likewise ignores URL.

Configured HTTP listener failure is only logged inside an asynchronous goroutine. P13 holds the desired port open, starts devmeshd, and still gets health 200/ok and successful HTTP registration. The exact diagnostic is `http_proxy_listen_failed ... bind: address already in use`. The HTTP data path is not running, but the daemon advertises it.

TLS loads a parseable matching key pair, but does not validate SAN coverage, expiry policy, or key file permissions as required by brief §20. `BaseDomain` is a declared flag/config field without hostname-generation behavior. The HTTPS test disables certificate verification, so it proves encrypted forwarding, not certificate-name correctness.

**Required change:** synchronously acquire required listeners before reporting readiness; define the advertised authority and scheme independently from bind address; return a complete URL including nondefault port; make every consumer honor URL; expose HTTP registration fields consistently. Validate certificates against generated/explicit hostnames and document certificate permission policy. Docker failure may remain a degraded dependency, but a required proxy listener failure must not silently look like success.

### F08. Docker reconciliation needs a defined snapshot/event boundary

**Sources:** `internal/dockerwatch/watcher.go:47–85,118–170,197–236`.

The watcher lists and registers containers before subscribing to events, with no cursor or replay window. A container created after the list and before subscription can be missed until a later reconnect. A stop in the same interval can leave a stale backend. Successful reconciliation alone cannot eliminate this gap; the snapshot and event stream need coordinated ordering or periodic repair.

The `seen` set records only successfully registered inspections. If a running, previously tracked container has a transient inspect error, it is absent from `seen` and is forgotten as if removed. Failures for individual containers are logged, but `Reconcile` returns nil, so status can be connected despite failed publications. `eventLoop` reads channels without the `ok` result; a closed message channel can yield zero-value messages repeatedly while another channel remains open. Inspect calls have no per-operation timeout separate from the long-lived root context, and start retries block event processing serially.

These are source-level findings. The review reproduced the replacement reconciliation failure in F01 but did not run a long-duration Docker disconnect experiment. Do not generalize the basic fake reconciliation tests into reconnect coverage: there is no existing test of the `Run` reconnect state machine.

**Required change:** subscribe and buffer before taking the inventory snapshot, or use a supported event cursor with overlap/deduplication. Add periodic full reconciliation as a repair mechanism. Distinguish “absent from successful inventory” from “present but inspection failed.” Bound Docker calls and handle closed channels explicitly. Preserve incarnation identity through all callbacks.

### F09. Safety validation needs to protect the entire resource, not one selected value

**Sources:** `internal/dockerwatch/inspect.go:78–113`; `internal/transport/unix.go:29–53`; `internal/registry/model.go:115–151`.

P08 provides both a loopback publication and a wildcard publication for the same container port. `RegistrationFromInspect` chooses the loopback entry and accepts the container with non-loopback publications disallowed. Selecting a safe dial address does not prove the service is not exposed elsewhere. The brief's loopback-only security intent requires examining all publications for the managed target. An explicit non-loopback opt-in also blindly substitutes `127.0.0.1`, which is not necessarily reachable when Docker bound only a particular non-loopback host interface.

P09 puts a regular file at the configured Unix socket path. `Listen` sees that it exists, fails to dial it as a Unix socket, removes it, and binds a socket in its place. The valuable-file probe uses only a temporary file, but the same code can destroy a real misconfigured path. Use `Lstat` and reject non-sockets and symlinks before attempting stale-socket recovery. Existing directory permissions are not repaired by `MkdirAll`, and competing starters still need a precise ownership/locking strategy for unlinking a socket path.

Backend validation resolves arbitrary DNS names and then retains the original name for later dialing. A subsequent resolution can differ. `localhost` is accepted without resolution at validation time. This means “validated once as loopback” is not the same as “every dial targets loopback.” No remote DNS-rebinding experiment was performed. A narrow MVP solution is to accept literal loopback IPs and normalize localhost to a chosen loopback literal, or resolve once and store verified concrete addresses with a specified refresh policy.

**Required tests:** mixed IPv4/IPv6 exposures, wildcard plus loopback, specific-interface opt-in behavior, non-socket/symlink paths, concurrent daemon startup, and backend normalization. The doctor probe validates a Docker API publication, not the Compose parser's shorthand syntax; documentation must preserve that distinction.

### F10. Listener shutdown is not connection shutdown

**Sources:** `internal/runtime/service.go:17–31,53–84,127–135`; `internal/proxy/tcp.go:23–53`; `internal/daemon/daemon.go:248–267`.

Closing a TCP listener prevents new accepts but does not close already accepted sockets. The runtime stores neither accepted client connections nor upstream connections. It also has no wait group for per-connection workers. `Daemon.Shutdown` closes runtimes after waiting for background work, then flushes state, without draining or force-closing these connections.

P12 sends an echo byte through the frontend, calls `Daemon.Shutdown`, then successfully sends and receives another byte over the same connection. This is a direct observation of the embeddable domain method, not a claim that an operating-system process can keep sockets after it exits. Process exit eventually closes descriptors, but it is not the configurable graceful drain described by the brief.

HTTP shutdown helper goroutines are also not part of the daemon wait group. `http.Server.Shutdown` closes listeners before its active-request wait finishes; the serving goroutine can therefore end before the helper finishes draining. Errors are ignored, and the helper uses a hard-coded three seconds rather than the configured shutdown budget.

**Required change:** own accepted connections and copy workers, close listeners first, wait up to one overall deadline, then close remaining client/upstream sockets. Cancellation must also interrupt pending dials. Admission must be closed before waiting, so a concurrent registration cannot create another runtime during teardown. P12 should become a bounded-drain regression test.

### F11. Port persistence is best effort without an explicit durability signal

**Sources:** `internal/runtime/allocator.go:85–97`; `internal/state/store.go:21–48,60–68,99–125`.

P11 arranges a state path whose parent cannot be created. The allocator binds a frontend, logs the save error, and returns success. `SetPort` already mutated the in-memory map before writing; retrying the same value returns nil without attempting another write. The port assignment may work now but be lost across restart.

Corrupt-state handling also differs from the prose: the code copies bytes to a timestamped backup, ignores backup failure, leaves the original corrupt file in place, and returns empty state without a warning from this function. The next save may replace the corrupt original even if backup preservation failed. Atomic rename is a useful foundation, but it is not the same as verified recovery or successful durability.

**Proposed policy:** a first allocation should not be advertised as durably remembered until persistence succeeds. On failure, close the newly bound listener and return a typed persistence error; keep previously active runtimes operational. Alternatively, explicitly advertise degraded durability and retry with a dirty flag. Do not silently claim durable success. See decision D4 for the trade-off.

### F12. Operational surfaces and acceptance tests need completion

**Configuration.** `parseDuration` silently falls back on malformed input and accepts negative durations. The default/override port range has no central validation. Unknown JSON keys are ignored by `FileMapper`, allowing a misspelled security flag to disappear silently. Add a domain `Config.Validate` after framework decoding; retain Glazed ownership of env/file sources. `DEVMESH_STATE` is the current environment key, not the old `DEVMESH_STATE_PATH`. `--config` with an explicitly empty value currently falls back to the environment; document that policy or test `Flags().Changed` if explicit emptiness should disable the file.

**Diagnostics.** `doctor` passes `transport.DefaultSocketPath()` to its directory check instead of the selected socket path. It tests an arbitrary ephemeral loopback bind, not the configured frontend range. It does not inspect the configured state directory, event stream, or certificate/SAN/permissions. `Daemon.CheckFrontendRange` exists but is not exposed through an actual doctor path. Health contains only overall status, version, and Docker status, which is insufficient to explain listener and persistence failures.

**Logging.** Both roots initialize Glazed logging, but `ServeCommand.Run` constructs a new info-level text `slog` handler. The domain logger therefore does not follow the root's requested logging configuration. Fix this through explicit adapter/wiring, not by replacing all internal logging calls.

**Tests.** `TestAllocationsAreDistinct` allocates sequentially, despite historical claims of parallel-allocation coverage. `TestDockerDiscoveryAndRecreatePreservesFrontend` does not perform a database exchange after recreation; a successful `net.Dial` to a frontend alone proves neither upstream dialing nor successful PostgreSQL protocol traffic. `TestPostgresThroughFrontend` queries once before any recreation. The HTTP helper fatally fails on connection error inside a purported retry loop. Several helpers release an ephemeral port and then attempt to bind it later; test fixtures should not reproduce the check-then-bind race that production allocation avoids.

The documentation proposes eight help pages; only three are currently present. The current command tree uses `services list/resolve/inspect` and does not implement all suggested aliases or a `devmesh daemon` shortcut. These are lower priority than broken lifecycle semantics. Do not confuse a deliberately revised CLI contract with an unimplemented correctness requirement.

## 6. Requirement assessment

This matrix separates evidence of working behavior from complete acceptance. “Partial” means a path exists but required edge behavior or validation is absent.

| Brief requirement | Assessment at reviewed revision | Evidence or gap |
| --- | --- | --- |
| Producer binds backend before registration | Implemented | `ListenTCP` and Docker publication model |
| Bind-first frontend allocation | Implemented | Listener returned and retained; kernel-based tests |
| Persist and reuse frontend assignments | Partial | Ordinary round trip works; P11 failure handling |
| Native registration, heartbeat, deletion | Partial | Basic tests pass; F02/F05 lifecycle failures |
| Native recovery after daemon restart | Implemented path, insufficient acceptance coverage | 404 branch exists; no restart integration test |
| Same-owner backend replacement | Partial | Happy path passes; old lease/event removes replacement |
| Startup Docker discovery | Implemented basic inventory path | Fake reconciliation; live adapter tests |
| Recreate preserves frontend and connectivity | Partial | Port metadata tested; overlap/removal fails; no post-recreate query |
| Docker stream reconnect and missed-event repair | Partial | Loop exists, snapshot gap and no reconnect test |
| Refuse unsafe publications | Partial | Wildcard-only refused; mixed exposure accepted |
| Unknown HTTP host 404, unavailable 503 | Partial | Ordinary tests pass; double-read race misroutes |
| HTTP host uniqueness and stable URL | Not complete | P04/P05/P15/P16 |
| Existing-certificate TLS | Partial | Loads pair and forwards; no SAN/key-policy validation |
| Unix socket permissions and stale recovery | Partial | Basic behavior works; regular-file deletion |
| Graceful shutdown with bounded connection drain | Not implemented for TCP workers | P12 |
| Doctor validates effective daemon configuration | Partial | Wrong directory selection and missing checks |
| Glazed configuration and help | Substantially implemented | Middleware/lint pass; validation/logging/docs gaps |
| Two independent PostgreSQL projects plus recreation query | Not demonstrated by current test suite | Requires combined acceptance scenario |

The checked implementation task in `tasks.md` should be understood as a historical implementation milestone, not as proof that every row above is complete. This review does not silently rewrite that history or claim the proposed fixes have landed.

## 7. Proposed design: strengthen ownership rather than replace the architecture

### 7.1 D1 — Make backend incarnation part of every mutation

- **Context:** logical owner keys intentionally survive replacement, while removal events refer to particular producers.
- **Options:** use only owner keys; compare container IDs only in Docker; or introduce a shared publication identity for all producer types.
- **Decision (proposed):** add a shared publication identity with source-specific incarnation data. Process/manual publications use daemon-issued registration identity; Docker publications include container ID and, if restart ordering requires it, start incarnation metadata.
- **Rationale:** one compare-before-clear rule can protect registry, runtime, leases, and routes. Docker-only filtering would leave stale native leases broken.
- **Consequences:** callbacks and registry methods change, but consumer resolve responses need not. Dormant-name takeover remains possible after the previous publication is ended.

A proposed internal representation:

```go
// DESIGN SKETCH: not present in the current repository.
type PublicationID struct {
    Name       string
    OwnerKey   string
    Incarnation string // daemon ID or Docker container/start identity
}

type ServiceState struct {
    Current   PublicationID
    Kind      Kind
    Backend   Backend
    Available bool
    Frontend  Frontend
    Hostname  string
}

// Exact identity match is required; a stale removal returns false.
func (d *Daemon) EndPublication(expected PublicationID) bool
```

Do not use `OwnerKey` as a bearer credential. The owner identifies a logical relationship, while the token authenticates a native producer's operation. A generation counter can make publication ordering explicit, but a local counter does not tell you whether a delayed Docker observation is newer than another observation. Preserve Docker event identity/time and re-inspect uncertain terminal events.

### 7.2 D2 — Use one lifecycle coordinator for service transitions

- **Context:** registry and runtime are separately synchronized, while daemon mutation, lease removal, and reaping use different lock boundaries.
- **Options:** per-service locks immediately; an actor/event loop; or retain the daemon mutation lock and enforce its use consistently.
- **Decision (proposed):** retain a small global lifecycle coordinator for this local-scale system. Privatize mutable managers behind domain methods and require registration, conditional removal, and reaping to use the same transition protocol.
- **Rationale:** simpler review and deterministic correctness are more valuable here than registration throughput. Established connections already use their chosen backend without that lock.
- **Consequences:** DNS resolution and other slow validation must happen outside the coordinator. Listener preparation and state persistence need explicit commit/rollback rules. Never call the coordinator recursively from a manager callback while holding another manager lock.

Current source-derived reaper hazard: registration can obtain an existing backendless runtime, then the reaper can remove it before registration calls `SetBackend`; the boolean result is ignored. `SetBackend` itself obtains a pointer under the manager mutex and mutates it after releasing the mutex, allowing mutation of a removed runtime. This is a logical race, not necessarily a Go data race. Serialize the complete transition or update the manager API to apply the backend while ownership is protected.

```text
PROPOSED conditional end
------------------------
EndPublication(expected):
    lock lifecycle coordinator
    current = services[expected.Name]
    if current.identity != expected:
        unlock; return false
    publish unavailable state to current service/runtime
    retire only the matching lease
    record inactiveSince = now
    unlock
    emit structured removal event
    return true
```

For the first implementation slice, a coordinated update of existing objects is acceptable. A later refinement can have both registry projections and TCP/HTTP accessors read one immutable service snapshot. That reduces duplicated authoritative state. It is not necessary to add a database, event-sourcing system, or distributed lock.

### 7.3 Lease API and timing contract

Add effective TTL to `lease.Entry`. Define the expiration rule explicitly: at `now >= ExpiresAt`, renewal fails as missing/expired, prompting the client to register anew. This avoids resurrecting entries after their authority expired. If a grace period is wanted, represent and test it explicitly rather than depending on sweep timing.

```text
PROPOSED TakeExpired(now):
    lock lease map
    for each entry:
        if now >= entry.expiresAt:
            delete entry
            append immutable copy to expired
    unlock
    return expired

For each removed entry:
    EndPublication(entry.publicationID)
```

`TakeExpired` should not call daemon callbacks while holding its map lock. The lifecycle coordinator consumes the returned identities and conditionally clears only matching publications. If registration and expiry are coordinated under a wider lock, document that ordering as well. The critical property is that a successful renewal and removal of that same lease cannot both claim to have won.

Proposed response extension:

```json
{
  "registration_id": "daemon-issued-id",
  "lease_token": "returned-only-on-create",
  "frontend": {"host": "127.0.0.1", "port": 15432},
  "ttl_seconds": 15,
  "renew_after_seconds": 5,
  "expires_at": "2026-09-20T21:00:15Z"
}
```

The exact wire fields require a compatibility decision. An explicit renewal interval avoids clock-skew dependence when the client and server are not assumed to share a clock. For this local daemon, absolute expiry is also useful diagnostically. Bound TTL values before multiplying by `time.Second`; an unbounded integer multiplication can overflow a `time.Duration`.

```text
PROPOSED producer loop:
    create registration with caller context
    schedule renewal using server response
    until handle closed:
        wait for timer or cancellation
        attempt heartbeat with bounded request context
        success -> update deadline and schedule next renewal
        missing -> attempt fresh registration
        transient -> schedule jittered retry, not another fixed ticker wait
    join in-flight operation
    read latest registration identity
    best-effort delete latest identity under a short independent timeout
```

### 7.4 External create versus trusted Docker upsert

Keep the `/v1` wire protocol, but narrow authority at its entry points. The public create request should contain only caller-controlled domain data: name, kind, backend, optional preferred frontend, TTL request, and HTTP routing fields. Source can be a restricted process/manual choice if UX needs it; it cannot select the no-lease Docker path.

Proposed domain boundaries:

```go
// DESIGN SKETCH.
CreateProducer(ctx, ProducerRequest) (RegistrationResult, error)
UpdateProducer(ctx, registrationID, token, UpdateRequest) error
UpsertDocker(ctx, DockerPublication) error
ForgetDocker(ctx, name, ownerKey, containerID string) bool
```

The first two authenticate and manage leases. The latter two are called by the in-process Docker adapter. `UpdateProducer` might become `PUT /v1/registrations/{id}` with bearer authorization; this is a new endpoint proposal, not an existing API. Reject caller-supplied IDs/owners at public creation even if they were accepted by this pre-release implementation. Update integration tests that currently inject manual owner keys to use the authenticated replacement operation.

### 7.5 Docker snapshot and event sequencing

A minimal correct watcher needs three concepts: the current inventory, a stream of changes, and a publication identity for conditional removal. It does not need high concurrency initially.

```text
PROPOSED connection epoch
-------------------------
open event subscription and buffer bounded events
  -> obtain running-container inventory with timeout
  -> inspect/upsert eligible inventory members
  -> mark absent tracked incarnations ended, conditionally
  -> consume buffered and live events idempotently
  -> periodically reconcile to repair missed observations

stream closure/overflow/error
  -> mark degraded
  -> cancel this epoch and discard its resources
  -> backoff with jitter
  -> establish a fresh epoch and reconcile again
```

A bounded buffer must have a defined overflow response: restart the epoch and reconcile, not silently drop events. If the Docker API/client cannot establish a subscription-ready boundary, capture a replay start time before listing and request overlapping events with deduplication, plus periodic reconciliation. Test that chosen contract against the pinned client.

For a stop event about an old container, compare against the currently installed container/start identity. On same-container restart, re-inspect when event ordering is ambiguous. Track inventory membership separately from successful registration so a temporary inspection failure is not mistaken for absence. The initial implementation can continue processing serially if operation timeouts are bounded.

### 7.6 D3 — Canonical HTTP routes and truthful endpoints

- **Context:** hostname identity, frontend URL, backend target, and listener readiness are currently assembled independently.
- **Options:** patch individual formatters; or define one route contract and one endpoint formatter shared by API consumers.
- **Decision (proposed):** use validated route/endpoint values, plus a separate hostname ownership index. Permit exactly one service per canonical hostname initially.
- **Rationale:** this fixes multiple inconsistencies at their source and makes DNS/TLS constraints testable.
- **Consequences:** URL rendering must know the advertised scheme and port. Binding to `127.0.0.1:8443` is not by itself an advertised URL; a hostname and certificate still have to be configured.

Reject full URLs where a hostname is expected. Either define `http_host` as an FQDN or as a single label relative to `base_domain`; do not accept both ambiguously. A practical contract is “FQDN when explicitly qualified, validated single label when a base domain is configured,” with precise examples. Generation from `checkout.api` needs collision handling because `checkout-api` can already be a service name. The hostname index, not string substitution alone, detects that conflict.

```text
PROPOSED HTTP request path:
    canonicalHost = normalize incoming authority
    route = lookup immutable route snapshot
    if missing -> 404
    backend = route.currentBackend exactly once
    if unavailable -> 503
    outgoing.URL.scheme = backend.scheme
    outgoing.URL.host = backend.verifiedAddress
    outgoing.Host = chosen public-authority policy
    remove untrusted forwarded headers
    add forwarded host/proto/client-address from this request
    proxy with configured dial/header/idle limits
```

For an unavailable registered backend that fails during dialing, 502 is an appropriate proxy-failure response; reserve 503 for a route known to have no backend. Both differ from 404, which means the hostname has no route. A single immutable backend selection prevents the double-read fallback shown in P10.

One endpoint formatter should prefer `Frontend.URL` when nonempty and otherwise format host/port. Use it in list, resolve, inspect, register output, and `Handle.Endpoint`. Add `HTTPHost` and optional backend scheme to SDK/CLI only when the daemon contract is ready. Keep backend scheme separate from `AppProtocol`, which remains a display hint.

### 7.7 Listener startup and shutdown ownership

Change startup to report errors. Acquire configured HTTP/HTTPS listeners synchronously and retain them. If any required listener fails, close all listeners acquired during that startup attempt and return the error. Docker connection failure remains nonfatal by design. If a future product policy permits partial proxy startup, expose per-listener failure explicitly and reject registrations requiring a failed listener.

```text
PROPOSED shutdown, one overall deadline
---------------------------------------
close admission to registrations
stop API acceptance and cancel watcher/sweeper/reaper
close frontend listening sockets
wait for admitted operations and active proxy connections to drain
at deadline: cancel dials, close remaining client/upstream connections
join owned workers where bounded
flush/check state durability
remove only this daemon's socket
return error if the deadline or flush contract failed
```

Track connection pairs under a mutex and use a wait group whose `Add` operations cannot race with a terminal `Wait`. Closing acceptance before waiting is necessary but an already-returned `Accept` must also check admission before registering a worker. Keep both endpoints available for forced close, or give the proxy a cancellation context that reliably closes both. Closing only the client side can leave an upstream read blocked after half-close.

### 7.8 D4 — Define success for durable port allocation

- **Context:** a bound port and a persisted assignment are separate resources with different failure modes.
- **Options:** best effort plus visible degraded durability; or fail a new registration when its assignment cannot be persisted.
- **Decision (proposed):** fail the new allocation on persistence failure for the first hardening slice, closing its listener. Preserve existing working publications.
- **Rationale:** this is the simplest honest implementation of “remembered stable frontend.” A silently volatile assignment is surprising for the central product feature.
- **Consequences:** a full disk can block new registrations even when ports are free. A future opt-in volatile mode can relax this with an explicit response field and health state.

Prepare a candidate state map, write it, and only then commit the in-memory durable map, or retain an explicit dirty state with reliable retries. Do not return nil merely because the unsaved in-memory value equals the requested value. Validate loaded port values and state version. Preserve corrupt input before replacing it, check backup errors, and report recovery in logs.

## 8. Compatibility and migration guidance

The project reports version 0.1.0 and is still under active development. Correctness fixes may deliberately tighten the API, but callers need clear migration instructions.

- **Owner/ID fields:** stop accepting them on public creation. Tests and scripts needing replacement should use the authorized update path. Return an explicit invalid-request error, not silent ignoring.
- **Dormant names:** keep the useful ability for a new producer to reclaim a name after liveness ends. Do not conflate retaining a frontend reservation with retaining live producer authority.
- **Kinds:** reject TCP/HTTP changes for an existing service in the first slice. A separate remove/reset operation can later allow explicit migration with predictable endpoint changes.
- **HTTP URLs:** returning the correct scheme and port changes incorrect current values. Keep the JSON `frontend.url` shape; update every consumer to prefer it.
- **TTL:** add effective renewal metadata and preserve existing fields. Define a minimum supported TTL; update tests that currently rely on one-second edge timing.
- **State:** retain the `version` and `tcp_ports` format unless new durable data is genuinely necessary. Do not persist live tokens or registrations.
- **Configuration:** preserve the nested JSON mapper and framework middleware. Document `DEVMESH_STATE`, the removed XDG socket default, and the actual CLI command tree.
- **Historical documents:** add a visible current-review link rather than silently rewriting the imported brief, original diary, or byte-identical vault mirror.

## 9. Implementation plan for an intern

Do not start with CLI polish or new service-discovery features. Fix the cross-component state transitions first; later endpoint and diagnostic work depends on them. Each phase below is a proposed reviewable PR, with its own focused tests. The goal is visible behavior, not merely a new abstraction.

### Phase 1 — Publication identity and removal correctness

**Primary files:** daemon, registry, lease, Docker callback types; regression tests in daemon and watcher packages.

Introduce current publication identity in the service record. Replace owner-only unavailable operations with name-plus-identity comparison. Make Docker forget carry the concrete container identity. Ensure runtime clearing occurs only when the current publication was actually ended. Define retirement of a replaced process/manual lease.

- Convert P01, P02, and P03 into desired-behavior tests.
- Test new-container registration before old-container destroy, including reconcile ordering.
- Test unrelated stale owner removal as a no-op in both API metadata and TCP forwarding.
- Test two service names sharing a supplied logical owner through trusted internal paths; removal must target exactly one service.

**Acceptance:** old lease deletion and old Docker removal cannot disable a new publication. Registry and runtime agree after each completed transition. No new network dependency or framework is needed.

### Phase 2 — Close public ownership bypasses and fix leases

**Primary files:** API DTOs/handlers, daemon producer methods, lease manager, public client, manual register command.

Split public creation from trusted Docker upsert. Add authenticated replacement if needed. Store effective TTL per entry, implement atomic expiration removal, and schedule renewal from the negotiated contract. Use a fake clock for exact boundary tests. Decide and test cancellation semantics and close/re-registration ordering.

- Public `source=docker`, caller IDs, and caller owners must be rejected.
- Missing/bad token cannot replace or terminate another publication.
- Requested sixty-second TTL remains sixty seconds after renewal.
- Different daemon defaults must not cause a default client to expire before its first heartbeat.
- Renewal versus expiration has one deterministic winner.
- Close during successful re-registration cleans up the latest identity.

**Acceptance:** P06/P07/P14/P17 no longer demonstrate the old failures, and native recovery is tested across a real daemon restart using the same state file and socket.

### Phase 3 — Safe HTTP target selection and host ownership

**Primary files:** HTTP router, daemon HTTP registration, registry frontend handling.

First remove the double provider read and enforce outgoing target selection. Then add canonical hostname ownership, route replacement cleanup, and kind immutability. These belong together because they define how one request identifies one authorized target.

- Convert P04/P05/P10 into regression tests with known A/B bodies.
- Test absolute-form URLs, untrusted forwarded headers, and backend disappearance.
- Test hostname case, port suffix, trailing dot, and invalid syntax.
- Test old hostname behavior after explicit route change; no stale alias unless requested by a defined alias contract.

**Acceptance:** requests can reach only the selected registered backend; different services cannot silently share a canonical hostname. Run tests under `-race`, but also assert exact target identity.

### Phase 4 — Truthful HTTP/TLS startup and consumer APIs

**Primary files:** daemon startup, config validation, SDK types, CLI service formatters, TLS tests.

Bind listeners before success, publish scheme/authority/port correctly, and propagate HTTP registration/URL fields through SDK and CLI. Define FQDN/base-domain behavior and certificate coverage checks. Avoid adding ACME.

- Occupied configured listener fails startup or produces an explicitly tested degraded mode.
- Resolve returns a usable high-port HTTP/HTTPS URL.
- SDK and CLI can register HTTP and display that same URL.
- TLS tests use a trusted test CA and `ServerName`; no `InsecureSkipVerify` for acceptance.
- Wrong SAN, expired certificate policy, missing key, mismatched key, and unsafe permissions are tested.

**Acceptance:** register and connect using only the public response, not a test harness's private listener address. A successful health result cannot conceal a required listener failure.

### Phase 5 — Docker convergence and publication safety

**Primary files:** watcher loop, inspect conversion, Docker interfaces, fake Docker tests.

Implement the snapshot/subscription boundary, explicit channel closure, bounded calls, periodic repair, and classification of transient inspect errors. Validate all bindings for the managed target, not merely the selected backend binding. Separate the read-only watcher interface from the mutating doctor-probe interface where practical.

- Inject start/stop events at the list/subscription boundary.
- Close the message channel without closing the error channel and verify bounded reconnection.
- Return transient inspect failure for a still-listed container; do not fabricate absence.
- Test mixed wildcard/loopback and IPv4/IPv6 publications.
- Recreate after disconnect and verify both metadata and a real application query.

**Acceptance:** missed or reordered events converge to the actual running inventory, and old incarnations cannot clear current ones. Docker unavailability does not stop native services.

### Phase 6 — Runtime reaping, shutdown and transport safety

**Primary files:** daemon lifecycle coordinator, runtime manager/service, TCP proxy, Unix transport.

Serialize reaping with registration publication, track accepted connections, enforce one shutdown deadline, and reject registrations once shutdown begins. Reject non-socket paths and unsafe stale-socket replacement. Add backend address normalization outside mutation locks.

- Deterministic barrier test between runtime lookup and backend publication while reaping is attempted.
- Long-lived TCP connection drains normally, then forced close occurs at deadline for a stuck peer.
- Slow upstream dial cancels during shutdown.
- Half-close test sends request EOF and still receives the complete response.
- Concurrent startup and preexisting regular-file/symlink tests preserve unrelated files.

**Acceptance:** P09/P12 are replaced by desired-behavior tests, and repeated start/stop cycles do not retain owned listeners or copy workers. No daemon-wide lock is acquired for each copied byte.

### Phase 7 — Durability, validation and diagnostics

**Primary files:** state store, allocator, configuration conversion, doctor, logging wiring.

Implement the selected persistence policy and error propagation. Validate config once after Glazed decoding, reject unknown keys where appropriate, and wire domain logging to the selected root configuration. Expose a redacted daemon diagnostic view so doctor checks the effective paths/listeners/range rather than its own guessed defaults.

- Inject create/write/sync/rename failures and assert listener rollback plus state consistency.
- Recover corrupt state without silently losing the only original copy.
- Test malformed/negative durations, port bounds, and contradictory listener settings.
- Doctor with a nondefault socket checks the selected directory and effective state path.
- `--log-level`/format affect domain logs; tokens and private keys remain absent.

**Acceptance:** newly successful allocations have the promised durability, and diagnostic success corresponds to the effective daemon configuration.

### Phase 8 — Acceptance package and documentation update

**Primary files:** integration tests, README, embedded help, ticket task/checklist documents, wire DTO package extraction if still needed.

Run one combined scenario with two independent PostgreSQL services, query each, recreate one, query it again, verify the other remains reachable, restart the daemon, and verify automatic rediscovery/re-registration. Extend HTTP acceptance to use actual SDK/CLI-returned URLs and trusted TLS validation. Update user documentation only after these behaviors are validated.

**Acceptance gate:** build, vet, race suite, Glazed lint, Docker-enabled acceptance with explicit skip reporting, and docmgr hygiene. Record which scenarios ran; do not treat a skipped Docker job as equivalent to a Docker acceptance pass. Keep ACME, local DNS, per-service IPs, and multi-backend load balancing deferred.

## 10. Test strategy: prove transitions, not only happy endpoints

### 10.1 Four layers of tests

**Pure domain tests** should verify identity matching, TTL arithmetic, hostname canonicalization, config validation, and state-map transitions. Use an injected clock and explicit error injection. These tests should be fast and deterministic.

**Coordinated concurrency tests** should place barriers around lifecycle boundaries: expiry selection versus renewal, runtime lookup versus reaping, and close versus re-registration. The assertion is not merely “no race detector warning.” It is that a specific legal winner exists and the final registry/runtime state matches it.

**Transport integration tests** should use real Unix sockets and loopback listeners to exercise DTO validation, token checks, endpoint formatting, half-close, and cancellation. A successful frontend handshake is not sufficient: send payload and assert the backend's identity-specific response.

**External integration tests** should use Docker and PostgreSQL where available, with a CI job that requires rather than skips those dependencies. Container identity and query results are stronger evidence than assuming that Docker will always choose a different numeric port on recreation.

### 10.2 Regression matrix

| Scenario | Required assertion |
| --- | --- |
| Old lease deleted after replacement | New backend remains ready and responds |
| Docker B registered before A removed | B remains current after A's terminal event |
| Renew races expiry | Exactly one outcome wins; no successful renewal silently deleted |
| Reap races re-registration | Installed ready state has a live listener/runtime |
| HTTP backend disappears during selection | 503 or selected-backend failure; never caller URL |
| Duplicate canonical HTTP host | Conflict, with previous route intact |
| HTTP hostname change | Registry URL and installed route agree |
| HTTP high-port/TLS response | Consumer connects using returned URL alone |
| Mixed Docker publication | Refused by default, with offending binding identified |
| Persistence fails after bind | No false durable success; no leaked listener |
| Shutdown with idle active TCP | Closed by overall deadline, workers joined |
| Custom socket path | CLI connects and doctor checks the same effective path |
| Daemon restart with native client | Re-registers automatically and reuses port if free |
| Two DB projects, one recreated | Queries reach correct database before/after recreation |

### 10.3 What not to optimize yet

Do not replace `sync.Mutex` with lock-free structures to fix logical races. Do not add a timing wheel for a small local lease map. Do not introduce a general event framework to carry a handful of Docker transitions. Do not add database-specific proxy logic to compensate for insufficient byte-forwarding tests. The current standard-library approach is adequate once ownership and lifetime boundaries are correct.

## 11. Documentation reconciliation and lessons from the diary

The diary is useful because it preserves both the implementation sequence and explicit uncertainty. Step 5 calls out dormant-owner takeover and global mutation serialization. Step 7 explicitly notes missing hostname uniqueness. Steps 9–10 explain why configuration moved into Glazed and why config-path resolution uses a framework pre-parse. Those decisions should inform the next cycle rather than be rediscovered.

However, phrases such as “core proven,” “all checks pass,” and the checked implementation milestone cannot substitute for the missing scenarios. The later mirrored report promotes several intended properties to implemented guarantees. Preserve that report as a historical snapshot and use this review as the current assessment.

Specific corrections for future docs:

- Hostname uniqueness is not enforced in the reviewed code; Step 7 correctly says last-write-wins.
- Shutdown does not track active TCP copy workers; a timeout on `Daemon.Shutdown` does not implement the described forced connection close.
- The allocator's distinctness test is sequential, not parallel.
- The Docker recreation test does not prove a PostgreSQL query succeeds against the replacement.
- Current HTTP URLs omit high ports and remain HTTP even with TLS configured.
- `ready` means a backend address is registered, not that an application readiness probe has succeeded.
- Corrupt state is copied aside with ignored backup errors, not safely renamed and logged as described.
- Plain DNS names are validated once, then resolved again by the dialer; loopback-only dialing is not fully established.
- A Docker Engine API publication probe does not certify Compose shorthand parsing on every platform.
- Returning `{Port, Listener}` prevents allocation-to-bind races; it does not by itself establish lifecycle correctness, durability, or application readiness.

One suspected issue was explicitly rejected during review: absence of `signal.NotifyContext` in the binary entry files does **not** imply signals are unhandled. The pinned Glazed builder installs signal-aware contexts (`pkg/cli/cobra.go:176,197`). The black-box control test terminated normally with SIGTERM, exit 0, and socket removal. This is why framework behavior must be checked before recording a finding.

## 12. API and source reference

### 12.1 Current administrative API

The API uses HTTP/JSON over a user-restricted Unix socket. `internal/api/server.go:32–39` mounts these method-qualified Go `ServeMux` patterns. Unknown routes and method mismatches may use standard mux responses rather than the custom JSON error envelope; clients should not assume every possible HTTP error is JSON.

| Method and path | Current successful response | Important implementation reference |
| --- | --- | --- |
| `GET /v1/health` | 200, status/version/Docker | `handleHealth`, `HealthResponse` |
| `GET /v1/services` | 200, services array | `handleList`, `serviceDTO` |
| `GET /v1/services/{name}` | 200, frontend and status | `Daemon.Resolve`, `handleResolve` |
| `GET /v1/services/{name}/inspect` | 200, backend/owner/container/hostname | `Daemon.Inspect`, `inspectDTO` |
| `POST /v1/registrations` | 201, ID/frontend/token/expiry where leased | `handleRegister`, `Daemon.Register` |
| `POST /v1/registrations/{id}/heartbeat` | 200, expiration | `Daemon.Heartbeat`, `Lease.Renew` |
| `DELETE /v1/registrations/{id}` | 204 | `Daemon.DeleteRegistration` |

Current JSON error mapping in `internal/api/errors.go`: invalid request/name/backend/kind → 400; unauthorized → 401; unknown registration/service → 404; name conflict → 409; exhausted ports/Docker-unavailable code → 503; internal error → 500. Code strings should remain stable. A heartbeat 404 is the client's re-registration signal; other failures must not be treated as success.

`decodeJSON` currently limits the decoder input to 1 MiB and disallows unknown fields, but decodes only one value and then drains the original body without the same size bound. A second JSON value is not rejected explicitly. The future API hardening slice should use `http.MaxBytesReader`, require EOF after one object, and configure server header/read limits. This is a source-derived robustness issue, not a demonstrated resource-exhaustion attack.

### 12.2 Public Go API

Current exported entry points are `Register(ctx, RegistrationOptions)`, `ListenTCP(ctx, name, preferredPort)`, `Handle.Endpoint`, `Handle.Close`, and `ListenerHandle.Close`. `RegistrationOptions.Socket` allows an explicit socket override, but the convenience `ListenTCP` does not expose options. The SDK does not itself load `DEVMESH_SOCKET`; its default path helper uses the home directory. Glazed environment handling is a command-layer feature, not behavior inherited by library calls.

When introducing HTTP options or lifecycle changes, preserve the simple TCP helper and add an options-bearing variant rather than making producers reconstruct wire JSON. Keep errors usable by external callers; currently the typed transport error lives under `internal`, so a public error contract would be a useful follow-up when extracting DTOs.

### 12.3 Source reading index

Paths below are relative to `/home/manuel/code/wesen/2026-09-20--devmesh`.

| Topic | Reference |
| --- | --- |
| Core register flow | `internal/daemon/daemon.go:296–402` |
| Expiry and deletion | `internal/daemon/daemon.go:236–244,426–453` |
| Listener startup and TLS | `internal/daemon/daemon.go:85–99,114–190` |
| Registry replacement and unavailable state | `internal/registry/registry.go:24–86` |
| Backend validation | `internal/registry/model.go:115–151` |
| Bind-first allocator | `internal/runtime/allocator.go:43–98` |
| Runtime creation/update/reap | `internal/runtime/manager.go:34–119` |
| Per-runtime TCP acceptance and close | `internal/runtime/service.go:53–84,127–135` |
| TCP copy semantics | `internal/proxy/tcp.go:23–53` |
| HTTP route selection and target rewrite | `internal/proxy/http.go:43–119` |
| Lease data and timing | `internal/lease/manager.go:21–138` |
| Docker reconciliation and events | `internal/dockerwatch/watcher.go:47–244` |
| Docker publication choice | `internal/dockerwatch/inspect.go:50–117` |
| Persistent write/recovery | `internal/state/store.go:21–48,60–68,99–125` |
| Unix path safety | `internal/transport/unix.go:29–59` |
| Producer heartbeat/close | `pkg/devmesh/client.go:87–182` |
| Config conversion and logger | `cmd/devmeshd/cmds/serve.go`, `Run`, `configFromSettings`, `parseDuration` |
| Diagnostic coverage | `cmd/devmesh/cmds/doctor/doctor.go`, `runChecks` |
| Current test limits | `integration/docker_test.go`, `postgres_test.go`, `client_test.go`, `https_test.go` |

A machine-generated symbol index is preserved at `analysis/evidence/05-symbol-index.txt`.

### 12.4 Standard-library and Docker API references

These are reference entry points for implementing the proposed changes, not claims that external documentation was freshly audited during this code review:

- Go `net`: <https://pkg.go.dev/net> — listener ownership, TCP half-close, dial contexts.
- Go `net/http`: <https://pkg.go.dev/net/http> — server shutdown, request limits, transport timeouts.
- Go `httputil.ReverseProxy`: <https://pkg.go.dev/net/http/httputil#ReverseProxy> — `Rewrite`, `ProxyRequest`, forwarded headers.
- Go `crypto/x509.Certificate.VerifyHostname`: <https://pkg.go.dev/crypto/x509#Certificate.VerifyHostname> — hostname coverage, separate from trust verification.
- Go `sync`: <https://pkg.go.dev/sync> — mutex and wait-group ownership.
- Docker Engine API: <https://docs.docker.com/reference/api/engine/> — event and inspect contracts.
- Docker events: <https://docs.docker.com/reference/cli/docker/system/events/> — lifecycle observations and event filtering.
- Compose port specification: <https://docs.docker.com/reference/compose-file/services/#ports> — publication syntax versus Engine API bindings.

## 13. Final recommendation

Devmesh has the right core abstraction and enough implementation to make the defects concrete. Keep the bind-first allocator, opaque TCP forwarding, Unix administrative API, Docker adapter, and Glazed command layer. Do not expand scope until stale events and leases can no longer disable replacements, HTTP requests cannot choose unintended targets, and advertised endpoints correspond to listeners the daemon actually owns.

The intern's first task should be Phase 1, with P01–P03 converted into regression tests. The next task should combine public ownership restrictions with atomic lease expiry. Those changes establish the identity and lifecycle contract that HTTP routing, Docker convergence, and graceful shutdown all depend on. Completion should be demonstrated through the combined acceptance scenarios, not inferred from the presence of packages or a green baseline suite.
