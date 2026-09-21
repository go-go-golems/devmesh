---
Title: Devmesh implementation review and focused simplification design v2
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
      Note: Shared state and conditional producer removal
    - Path: repo://internal/dockerwatch/watcher.go
      Note: Events plus simple periodic reconciliation
    - Path: repo://internal/proxy/http.go
      Note: One target selection and explicit fixed host routes
    - Path: repo://internal/runtime/service.go
      Note: Remove duplicate backend authority and simplify resource ownership
    - Path: repo://pkg/devmesh/client.go
      Note: Single lifecycle loop reused by CLI
    - Path: repo://ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/archive/01-implementation-review-v1.md
      Note: Historical detailed findings; broad roadmap superseded
ExternalSources: []
Summary: Focused v2 design to make devmesh usable through less duplicated state, one producer lease loop, direct HTTP routing, and three bounded delivery slices. Preserves review evidence while deferring elaborate machinery and low-impact edge-case work.
LastUpdated: 2026-09-20T21:12:01Z
WhatFor: Authoritative next implementation scope; supersedes the v1 eight-phase hardening roadmap.
WhenToUse: Implement the three slices below, not every possible improvement identified in the historical audit.
---

# Devmesh review and simplification design — v2

## 1. Decision: get the useful system working without building a larger system

Devmesh gives a development service a stable address even when its actual listening port changes. A **backend** is the current address owned by the application or container. A **frontend** is the address owned by devmesh and used by consumers. A **service name**, such as `checkout.postgres`, associates those addresses. The daemon registers the backend, binds the frontend, and forwards new connections to the current backend.

```text
Consumer                         devmeshd                 Producer
--------                         --------                 --------
pgx -> 127.0.0.1:15432 ----------> TCP proxy ------------> :49173
                                  same frontend           |
                                  after recreation ------> :49901
```

The architecture is appropriate. The next implementation cycle should make normal registration, container recreation, daemon restart, and HTTP consumption reliable. It should not turn this local developer tool into a generalized lifecycle framework.

**The governing rule for v2 is to remove duplicated state and supported combinations before adding mechanisms.** A refactor should make an ordinary failure harder to express, not introduce a subsystem whose main purpose is handling hypothetical rare cases. The previous review identified real defects, but its eight-phase roadmap was broader than needed to get useful behavior working. This document replaces that roadmap with three bounded implementation slices.

### 1.1 Scope guardrails

- Keep the existing daemon, Unix HTTP API, bind-first TCP allocator, Go client, Docker adapter, standard-library proxies, and Glazed command layer.
- Prefer one shared service state, one conditional backend removal, one producer lease loop, and one endpoint formatter.
- Retain the existing global daemon mutation mutex. Do not introduce actors, per-service lock hierarchies, event buses, transactional engines, or lock-free frameworks.
- Do not introduce event epochs, replay cursors, durable event logs, general deduplication frameworks, or generation/version-vector machinery for Docker.
- Do not add a public backend-update API until an actual consumer needs it. Ordinary native producers bind once, register, heartbeat, and close.
- Use explicit HTTP hostnames. Do not add hostname generation, aliases, route migration, or generalized backend-scheme negotiation in this cycle.
- Fix an inexpensive safety guard when working in that code. Do not let an exhaustive security/platform matrix become a prerequisite to shipping the local tool.
- Stop when the acceptance scenario in section 8 passes. Deferred items require a concrete user problem or reproducible material failure before becoming work.

“Keep it simple” is not permission to leave a confirmed routing or data-loss bug in a path we use. An old container clearing its replacement, a client renewing with the wrong TTL, and an HTTP proxy selecting an unintended target are important. Those have small, direct fixes. Perfect event history, configurable graceful-drain policies, and support for every possible hostname transition are not necessary for the first useful release.

### 1.2 What changed from v1

| v1 direction | v2 decision |
| --- | --- |
| Eight sequential hardening phases | Three useful delivery slices with explicit stopping criteria |
| Separate state objects coordinated through increasingly rich transitions | One current routing snapshot shared by registry, TCP, and HTTP |
| General publication/incarnation sketches | Compare existing registration ID or container ID; no generic generation service |
| Proposed authenticated public update endpoint | Defer; narrow public creation and retain existing heartbeat/delete |
| Snapshot/subscription epochs and replay alternatives | Keep events for responsiveness; periodic inventory reconciliation repairs gaps |
| Dynamic hostname/kind migration | Reject changes for an existing service during this daemon run |
| Native and CLI lease loops maintained separately | One shared client lifecycle implementation |
| Reaper/register synchronization work | Remove automatic idle runtime reaping for now; retain listeners until shutdown |
| Configurable graceful drain and force-close scheduler | Cancel and close owned connections promptly on shutdown; no drain-policy framework |
| Broad diagnostic/TLS/platform completion gate | Small operational fixes and the supported-path acceptance test |

These are design decisions for the next implementation, not claims that production code has changed. The code under review is still revision `5fbf70cb3bf0d215185351af3eac3bea0b195e0d`.

## 2. Evidence and document authority

The original review read the complete brief (`devmesh-implementation-guide.md`), expanded guide (`design-doc/01-devmesh-intern-analysis-and-implementation-guide.md`), and diary. It inspected the implementation and tests, ran baseline checks, and added isolated reproduction programs. Its complete findings and source analysis remain in [the archived v1 review](../archive/01-implementation-review-v1.md). That archive preserves investigation history; **its proposed APIs, priority labels, and eight-phase plan are no longer the implementation scope**.

This v2 is authoritative for what to implement next. The original brief and guide remain historical requirements/design inputs. Where they request idle reclamation, automatic hostname generation, or richer graceful shutdown, v2 deliberately narrows the first-release behavior rather than silently pretending those features are implemented.

### 2.1 Verified implementation baseline

These commands passed during the code review:

```bash
GOWORK=off go test -race ./... -count=1 -v
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off make glazed-lint
```

The Docker recreation and PostgreSQL tests ran and passed, rather than skipping. Those tests do not cover every claimed lifecycle guarantee. For example, the recreation test checks metadata but does not issue a PostgreSQL query after replacement. A race-detector pass does not prove that a sequence of individually synchronized operations is logically correct.

The v2 revision changes documentation and scope, not implementation. It does not claim a new production test run or that any finding has been fixed.

### 2.2 Reproducible observations

From the repository root:

```bash
T=ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide
GOWORK=off go run "$T/scripts/01-review-probes.go"
python3 "$T/scripts/02-cli-api-probes.py"
```

The programs report observed defects; successful process exit means the reproduction ran, not that the intended behavior passed. They use isolated state, loopback servers, fake Docker inventory, and temporary real binaries. The unintended HTTP-target probe contacted only a local test server.

| Evidence | Contents |
| --- | --- |
| `analysis/evidence/01-baseline-race.txt` | Race-enabled baseline with explicit Docker test results |
| `analysis/evidence/02-glazed-lint.txt` | Framework lint result |
| `analysis/evidence/03-review-probes.txt` | P01–P12 domain and network observations |
| `analysis/evidence/04-cli-api-probes.txt` | P13–P17 real binary/Unix API observations |
| `analysis/evidence/05-symbol-index.txt` | Source symbol locations at the reviewed revision |
| `analysis/evidence/06-remarkable-upload.txt` | Historical v1 upload receipt |

## 3. System orientation for an intern

### 3.1 Control and data paths

The **control path** changes routing metadata: registration, heartbeat, deletion, Docker inspection, and name resolution. The **data path** forwards application bytes. Existing TCP byte copying does not need to acquire the daemon's mutation lock. A new connection needs only a snapshot of its selected backend.

```text
CLI / Go SDK -- HTTP over Unix socket --> API handlers --+
                                                        |
Docker watcher -- direct trusted callback ---------------+--> Daemon
                                                               |
                            +----------------------------------+
                            |                    |             |
                         Services             Leases       Port state
                            |
            +---------------+----------------+
            |                                |
TCP listener -> current backend      HTTP hostname -> current backend
            |                                |
        byte copy                       ReverseProxy
```

`internal/daemon` coordinates these operations. `internal/registry` stores service records. `internal/runtime` owns TCP listeners, currently with another copy of the backend pointer. `internal/lease` stores native/manual lease entries. `internal/dockerwatch` translates container metadata into registrations. `internal/state` persists remembered ports, not live registrations. `internal/api` exposes JSON DTOs. `pkg/devmesh` gives producers a convenient handle, while `cmd/devmesh` currently duplicates part of that handle's lifecycle.

Glazed belongs at the command layer. Keep its env/config middleware, existing JSON `FileMapper`, structured output, and help. This refactor does not justify a second configuration loader or replacing the command framework.

### 3.2 Names, owners, and current producers

A logical name identifies the service. An owner key groups logically equivalent producers, such as successive containers from the same Compose project/service. A registration ID identifies a particular native lease. A container ID identifies a particular container. A lease token authorizes heartbeat/deletion; an owner string is not a credential.

The distinction needed for correctness is small: **replacement is permitted by logical ownership, but removal must match the current concrete producer**. Reusing the same Docker owner key is correct for replacing A with B. Using that owner key alone when deleting A is incorrect because it also matches B.

### 3.3 Frontend allocation and actual guarantees

The allocator tries a remembered port, then a preferred port, then a deterministic scan of its range. Each attempt calls `net.Listen` and retains the listener. The allocation is a `{Port, Listener}` pair, not an integer that another process might claim before use. Keep this design unchanged.

Across daemon restarts, stability is best effort: another process may have acquired the remembered port. On conflict, devmesh selects another port and resolve reports it. No new mechanism should promise to reserve a port while the daemon is stopped.

For v2, keep allocated service listeners until daemon shutdown, including when their backend is unavailable. This removes the runtime reaper and its registration race. It deliberately trades automatic idle reclamation for fewer states and stronger within-run frontend stability. If a developer exhausts the available range through unusually high name churn, report exhaustion; restarting the daemon releases inactive listeners, while remembered preferences remain on disk. Do not build eviction until that workload is demonstrated.

## 4. Which findings matter now?

The original audit grouped findings F01–F12. This table retains traceability but replaces the implication that every possible hardening item is a release blocker.

| Finding / observation | Current impact | Focused response |
| --- | --- | --- |
| F01, P02/P03: stale forget clears replacement or only clears runtime | Ordinary container recreation can stop working | Shared state and exact producer-ID comparison |
| F02, P01/P07: old lease or stale expiry removes newer work | Native service liveness becomes unreliable | Conditional removal and atomic expiration |
| F03, P14/P17: public caller asserts Docker source/owner | Can bypass lease lifecycle or overwrite another same-user producer | Remove reserved creation fields; no new public update API |
| F04, P10: second HTTP backend read leaves caller URL intact | Can forward to an unintended target | Select backend once and always set outgoing authority |
| F05, P06: renewal uses a different TTL | Supported TTL settings can repeatedly expire healthy clients | Store effective TTL and share one client loop |
| F06, P04/P05: duplicate hosts and stale routes | Ordinary HTTP services can collide or advertise wrong URL | Unique explicit host; reject host/kind mutation |
| F07, P13/P15/P16: failed listener, wrong URL, empty CLI endpoint | Normal HTTP usage is broken | Bind before success; shared endpoint formatting; HTTP fields end to end |
| F08: event subscription gap and transient inspection failures | Discovery can remain stale | Periodic repair, bounded calls, preserve seen inventory on inspect error |
| F09, P08/P09: mixed exposure accepted; regular file removed | Cheap checks prevent unsafe acceptance or data loss | Check all managed bindings; reject non-socket paths |
| F10, P12: accepted connections survive domain shutdown | Resource ownership is incomplete | Track/cancel connections; prompt close rather than configurable draining |
| F11, P11: persistence failure appears successful | Central remembered-port promise is misleading | Fail new allocation, close listener, keep in-memory state consistent |
| F12: broad operational/test gaps | Some are cheap; many are not first-release blockers | Fix misleading touched behavior; defer diagnostic/platform expansion |

Concrete historical outputs include:

```text
P03 Docker reconcile replacement: container=new status=unavailable
P06 requested TTL=60s: initial~60s renewed~15s
P10 backend disappears between provider calls: status=200 body=UNINTENDED
P16 CLI HTTP resolution: endpoint="", kind="http", status="ready"
```

Source-only concerns are not automatically promoted to new subsystems. Arbitrary DNS re-resolution, same-container event ordering under unusual delivery sequences, comprehensive certificate permission policy, and competing-daemon path locking remain bounded follow-ups unless a supported workflow requires them. Cheap validation can land alongside its owning slice; a generalized solution cannot silently expand that slice.

## 5. Simplified target design

All types and algorithms in this section are sketches for implementation. Use the smallest changes that establish the behavior; the names are not a mandate to introduce new packages.

### 5.1 One service object; no duplicated authoritative backend

Today, the registry record and TCP runtime can disagree about the backend. HTTP uses a provider closure that resolves the registry again. Replace these independent sources with one service entry whose current routing snapshot is read by all three paths.

```text
registry[name] --> Service entry
                   |-- identity / frontend / fixed kind / fixed HTTP host
                   |-- current immutable routing snapshot
                   |      backend + current registration/container ID
                   +-- TCP listener, if applicable

Resolve ----------- reads the same entry
TCP Accept -------- reads routing snapshot once
HTTP request ------ reads routing snapshot once
```

The registry can own the entry and give the runtime a read-only reference. No reference-counting framework is needed: entries live for the daemon's lifetime. Use ordinary mutex-protected snapshots or the existing atomic-pointer pattern. The goal is one authoritative backend value, not a specific synchronization trick.

Keep `Daemon.mutate` for mutation serialization. Internal managers should not expose alternate paths that bypass it. Do slow external inspection before acquiring it. Do not add a separate “lifecycle coordinator” package, prepare/commit transaction engine, or per-service actor.

```text
clearBackend(name, expectedProducerID):
    lock daemon mutation mutex
    service = services[name]
    if service absent or current producer ID differs:
        unlock; return false
    replace current routing snapshot with unavailable
    unlock; return true
```

Use the native registration ID or Docker container ID already available. Compare source as well if IDs occupy separate namespaces. This can be a small struct or a tagged string; it does not need generic generations or a registry of incarnations. Lease removal retires only the matching lease. Docker cleanup carries container ID instead of discarding it.

### 5.2 Remove automatic runtime reaping

Do not spend this cycle building a synchronization protocol around reclaiming idle listeners. Retain a name's listener until shutdown. Backend unavailability changes routing state, not listener ownership. This removes a timer, a removal/recreation path, and several opportunities for registry/runtime disagreement.

Remove the `runtime-idle-ttl` setting from the supported v2 surface when implementing this change. Reject an explicitly supplied obsolete option with a clear migration message rather than silently promising it works. Update README/help/config examples once, and record the deliberate change from the original ten-minute-grace design. No compatibility shim or replacement eviction policy is needed for this pre-release tool.

### 5.3 One lease loop shared by SDK and CLI

Store effective TTL on each entry, renew with that TTL, and remove expired entries atomically. `TakeExpired(now)` must select and remove under one lease-map lock, then release that lock before calling conditional service removal. `Renew` rejects an already expired lease according to one documented boundary.

Return effective `ttl_seconds` with the existing expiry data. Do not add both a renewal-policy negotiation subsystem and several interchangeable deadline formats. The client can derive a renewal interval from that TTL and use a bounded retry timer. Validate a practical minimum TTL so heartbeat/request timing leaves margin; it is acceptable to reject impractically short TTLs instead of engineering for them.

```text
shared producer handle:
    register; retain ID, token, endpoint, effective TTL
    timer fires -> heartbeat
        success: schedule next renewal from effective TTL
        404: register again
        transient failure: bounded retry timer
    close/cancel:
        stop loop; join in-flight request
        delete the latest ID/token with a short timeout
```

The CLI should use this handle instead of implementing another loop. If process/manual source labels matter, keep that small distinction in creation options while sharing all liveness logic. Extract wire DTOs from the server package only as needed to prevent the shared client from depending on daemon/Docker implementation. Do not build a generic transport/client framework.

### 5.4 Narrow the public API, rather than expanding it

Public create accepts name, kind, backend, preferred port, requested TTL, and explicit HTTP host where relevant. The daemon generates registration identity and lease credentials. Ordinary callers cannot submit `owner_key`, `registration_id`, `docker_container_id`, or choose the lease-free Docker source.

Docker retains a trusted in-process upsert operation. The watcher supplies its stable logical owner and concrete container identity. That does not require a new HTTP endpoint.

**No public `PUT`/update endpoint in this cycle.** A native producer normally binds its backend once and keeps its handle. If it deliberately replaces its listener, close the old registration and create a new one; the service frontend remains reserved. This admits a short explicit unavailable interval rather than inventing an atomic hot-update contract without a caller. Tests should not create a production API just to inject owner strings conveniently; test trusted internal replacement where that is the behavior under test.

### 5.5 Docker: responsive events plus simple periodic repair

Keep the current watcher and event path. Add periodic reconciliation at a modest fixed interval, for example five seconds, serviced by the watcher loop. Bound individual Docker operations so the periodic work cannot be stalled indefinitely. Use events for prompt updates and reconciliation for repair; missed events need not have perfect historical replay.

```text
watcher loop:
    startup inventory -> upsert current labeled containers
    select:
        event -> inspect/upsert, or conditionally forget exact container
        periodic tick -> reconcile current inventory
        stream error -> reconnect with bounded backoff; reconcile
        context canceled -> return
```

This deliberately accepts a short discovery/repair delay after an event gap. It does not promise instantaneous observation or exactly-once events. The acceptance test should check convergence within the interval plus a reasonable operation budget, not an exact millisecond deadline.

Track “present in the successful list” separately from “inspection/registration succeeded.” A transient inspect failure must not fabricate absence. Handle closed event channels with the `ok` result. Before removing the current container on an ambiguous stop notification, a bounded re-inspect can avoid clearing a running same-ID restart; keep that local to the watcher. Do not introduce start-generation counters, replay logs, epochs, or deduplication infrastructure unless this simple approach fails a concrete important test.

Check all publications for the managed target when loopback-only policy is enabled. A safe chosen dial address is not proof that another publication is safe. This is a small validation loop, not a new security subsystem.

### 5.6 HTTP: fixed explicit routes and one target selection

Use explicit, validated hostnames such as `api-checkout.test`. The user supplies matching DNS/hosts configuration. No base-domain generation or aliases are needed. Retain one hostname-to-service index for conflict checking and request lookup; normalize case, trailing dot, and request port consistently.

Kind and hostname are fixed for an existing service entry during a daemon run. A conflicting change returns an error with instructions to use a new service name or restart for reconfiguration. Rejecting mutation is a valid correction for P05; successful dynamic migration is not an acceptance requirement. Do not add reset/delete-service machinery solely to recover that optional flexibility.

```text
route(request):
    service = hostname index lookup
    missing -> 404
    backend = current routing snapshot read once
    unavailable -> 503
    always set outgoing URL scheme and authority from backend
    set forwarded headers from this request, not untrusted input
    forward
```

Keep backend transport HTTP-only for this slice; TLS terminates at devmesh. Do not overload `AppProtocol` to infer transport behavior. TLS backend support can wait for an actual application requiring it. Removing the double read and unconditionally setting the outgoing target is essential even in a local tool.

Bind configured HTTP/HTTPS listeners synchronously before startup succeeds. Reject HTTP registration when no supported HTTP frontend is active. Publish a full URL with the actual configured nondefault port; prefer HTTPS when configured, otherwise HTTP. Do not add a selectable endpoint-preference framework. One formatter chooses `Frontend.URL` when present and falls back to host/port for TCP. Use it in resolve/list/inspect/register and the SDK handle.

Expose the explicit HTTP-host field in the SDK and CLI so these supported interfaces can actually register HTTP. Keep existing certificate loading, and use a trusted local test certificate plus `ServerName` in acceptance. Document certificate/DNS setup. Automatic issuance, certificate reload, configurable permission enforcement, and exhaustive certificate-policy checks are deferred. Do not claim v2 implements those parts of the original brief.

### 5.7 Simple resource cleanup and fail-fast persistence

Shutdown must close resources devmesh owns. For this release, choose prompt cancellation and closure of active proxy connections rather than a configurable graceful-drain policy. A small connection set, cancellation, and wait group are sufficient. Stop admissions/acceptance before waiting; ensure cancellation closes both ends and interrupts pending dials. An existing overall shutdown timeout is a bound on cleanup, not a promise to preserve active transactions for a drain interval.

This deliberately narrows the original brief's graceful-drain behavior. Do not introduce connection lifecycle states, multiple grace timers, per-service budgets, or draining APIs. Verify that an active connection is closed and workers finish; an operating-system process exit is not the sole cleanup implementation.

For a newly allocated frontend, persistence failure should return an error and close that new listener. Write candidate state successfully before considering it committed in memory. Existing registered services keep running. This is simpler than dirty-state retry workers, volatile mode, or a degraded-durability protocol. Preserve the existing state format and atomic temp-file rename.

At the Unix socket path, reject regular files and symlinks before stale-socket deletion. Check errors while preserving corrupt state. Validate basic config bounds and reject malformed durations instead of silently substituting defaults. These are small local fixes. They do not authorize a new filesystem locking protocol, recovery framework, or full platform compatibility project.

## 6. The three implementation slices

Implement and test one useful slice at a time. Do not turn the archived audit into a checklist that must be exhausted before moving on. Changes should remove old paths, not leave two implementations selectable by flags.

### Slice A — Simplify service state and producer lifecycle

**Files:** `internal/daemon`, `internal/registry`, `internal/runtime`, `internal/lease`, `internal/dockerwatch`, `pkg/devmesh`, CLI register command, and API DTOs/handlers as needed.

1. Introduce one authoritative routing snapshot shared by registry, TCP, and HTTP; retain the existing mutation mutex.
2. Use existing producer IDs for conditional removal. Retire replaced lease entries where applicable.
3. Remove automatic runtime reaping and obsolete idle-TTL configuration; listeners live until shutdown.
4. Narrow public create inputs; keep Docker upsert internal. Do not add public update.
5. Fix per-entry TTL and atomic expiration; share the producer handle between CLI and SDK.
6. Add periodic Docker repair with bounded operations and conditional container removal.

**Tests that matter:** replacement B remains reachable after removal of A; a stale owner does not leave registry/runtime disagreement; renewal cannot be removed by an old expiry snapshot; custom/default TTL works; native registration recovers after daemon restart; a missed Docker event is repaired within the documented interval. Use direct IDs and controlled time where helpful, not a generic simulation framework.

**Done means:** ordinary TCP registration, native recovery, and Docker recreation work end to end, with fewer authoritative state copies and one lease loop. Stop this slice when those tests pass. Do not add advanced event ordering or reclamation policies.

### Slice B — Make the supported HTTP path correct and usable

**Files:** HTTP router, daemon startup/HTTP registration, SDK options, CLI formatters and register fields, HTTP/TLS integration tests.

1. Select one backend per request and unconditionally set outgoing target; fix the reproduced unintended-target path first.
2. Enforce one service per explicit hostname. Reject hostname/kind changes rather than implementing migration.
3. Bind required listeners before success and return correct scheme/port in `frontend.url`.
4. Use one endpoint formatter and expose explicit HTTP-host options consistently in SDK/CLI.
5. Test the returned URL through the public interfaces, including a verified local TLS handshake when TLS is enabled.

**Tests that matter:** duplicate-host conflict preserves the first service; attempted host/kind mutation is rejected without side effects; a disappearing backend cannot forward to a caller-supplied URL; an occupied listener fails startup; two explicit hostnames reach distinct bodies; CLI/SDK display and use the returned URL.

**Done means:** a developer can register HTTP through supported client interfaces and connect using the returned endpoint. No generated hostnames, aliases, backend HTTPS, route migration, or certificate automation.

### Slice C — Close resources, preserve promises, and demonstrate the workflow

**Files:** runtime/TCP cleanup, allocator/state, Unix transport, small config/doctor/logging fixes, integration tests and README/help.

1. Cancel and close active proxy connections on shutdown; remove leaks without implementing graceful-drain policy.
2. Fail new allocations on state-write errors and avoid leaving unsaved state marked committed.
3. Add cheap guards for non-socket path deletion, mixed Docker exposure, port/duration bounds, and corrupt-state backup failure.
4. Fix the selected-socket doctor check and obvious logging/config documentation mismatches when touching those paths. Do not build a diagnostics API or every check envisioned in the original brief.
5. Run the combined acceptance scenario below; publish accurate setup and limitations.

**Done means:** the normal workflow is reproducible, confirmed inexpensive safety defects are closed, and the implementation no longer claims resource/durability behavior it lacks. A full diagnostics suite, platform matrix, and advanced recovery are not gating work.

## 7. Deliberate deferrals and when to revisit them

| Deferred item | Reason to leave it out | Concrete trigger to reconsider |
| --- | --- | --- |
| Event epochs, replay cursors, deduplication framework | Periodic repair gives adequate local discovery convergence | Repeated important failures cannot be fixed with bounded inspect/reconcile |
| Generation framework for every producer | Registration/container IDs cover the observed replacement bugs | Same-ID restart ordering causes a reproducible outage not repaired adequately |
| Idle listener eviction | Adds lifetime races; local name counts are small | Actual high-churn use exhausts ports/resources during a normal session |
| Public atomic backend-update endpoint | No current native caller requires it | A real producer must replace its listener without an unavailable interval |
| Hostname/kind migration, aliases, auto-generation | Creates route cleanup/collision/compatibility combinations | A concrete workflow cannot use fixed explicit hostnames |
| HTTPS backends and transport negotiation | Current applications can use HTTP behind local TLS termination | An application requires upstream TLS and cannot serve loopback HTTP |
| Configurable graceful connection draining | Prompt cleanup is enough for a restartable local tool | Real workflows need a bounded transaction-preserving shutdown |
| Volatile persistence mode or background retry worker | Fail-fast is simpler and truthful | Users need new registrations while durable storage is unavailable |
| Comprehensive TLS permission/expiry/reload policies | Larger than loading an existing user-managed development certificate | Deployment/security requirements explicitly demand enforcement |
| General DNS refresh/rebinding machinery | Not needed for literal loopback backends | Arbitrary backend DNS becomes a supported required feature |
| Competing-daemon locking and exhaustive platform/path threat model | More machinery than the present demonstrated workflow needs | Reproducible startup race or a deployment requiring stronger isolation |
| Full diagnostic API and every doctor check | Useful later; not needed to prove routing | Recurring support/debugging problems need visibility unavailable in logs |

For backend addresses, prefer literal loopback IPs and document that supported path. It is acceptable to narrow acceptance of arbitrary DNS names rather than implementing a refresh subsystem. A cheap normalization of `localhost` is also preferable to hidden general DNS behavior. Do not weaken an existing basic safety check in the name of simplification.

**Escalation rule:** before adding a deferred mechanism, state the concrete failure, user impact, simplest alternative, and acceptance test. If the only justification is that an edge case is theoretically possible, leave it deferred. Do not reopen unrelated scope while implementing a slice.

## 8. Acceptance scenario and stopping point

The release gate is a small set of real workflows plus regression tests for the bugs fixed in each slice. It is not an attempt to prove every possible concurrent schedule or Docker/platform combination.

1. Start the daemon with isolated socket/state paths and Docker enabled. Register a native loopback echo service through the shared client; send bytes through its returned frontend.
2. Start two independently named PostgreSQL containers with ephemeral loopback publications. Resolve and query both through devmesh.
3. Recreate one container. Query the replacement through the same frontend and query the other database again. Assert backend/container identity, not an assumption that Docker must choose a different numeric port.
4. Restart devmeshd using the same state and socket paths. Verify Docker rediscovery and native client re-registration; reuse remembered frontends when free.
5. Register two HTTP services with explicit hostnames through the supported API/SDK/CLI. Use the returned high-port URLs, not private harness addresses, to obtain distinct backend responses. Verify TLS with a trusted test certificate when configured.
6. Stop the daemon with active test connections. They close and owned workers finish. No idle listener or active copy worker is retained by the embedded daemon after shutdown.

Focused regressions must also cover conditional stale removal, per-entry TTL/atomic expiry, duplicate-host rejection, the absolute-URL target-selection defect, failed listener startup, persistence failure, and the cheap safety guards. Reuse ordinary tests and fake Docker inventory; no new testing framework is needed.

At the final integration boundary run:

```bash
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -race ./... -count=1
GOWORK=off make glazed-lint
```

Run Docker-dependent acceptance explicitly and report skips honestly. Do not repeat the entire pipeline after every minor edit; run focused tests during a slice and the complete gate at its integration boundary.

**Stop after these behaviors pass and documentation reflects the narrowed contract.** Remaining audit observations are not automatically unfinished release tasks. New work needs the escalation justification in section 7.

## 9. API, file references, and migration notes

### 9.1 Keep the administrative endpoint surface small

The existing HTTP-over-Unix API remains:

| Endpoint | Purpose |
| --- | --- |
| `GET /v1/health` | Daemon/version/Docker status |
| `GET /v1/services` | Consumer service list |
| `GET /v1/services/{name}` | Frontend and availability |
| `GET /v1/services/{name}/inspect` | Backend and producer details |
| `POST /v1/registrations` | Create leased producer registration |
| `POST /v1/registrations/{id}/heartbeat` | Token-authorized renewal |
| `DELETE /v1/registrations/{id}` | Token-authorized removal |

Change create-field validation and add effective TTL metadata as needed; do not invent another administrative protocol. Keep existing typed error codes wherever possible: invalid request 400, unauthorized 401, missing registration/service 404, ownership conflict 409, exhausted ports 503, internal failure 500. The client treats missing registration as a re-registration signal.

The supported public helpers remain `Register`, `ListenTCP`, `Handle.Endpoint`, and `Handle.Close`, with a small options extension for explicit HTTP registration and shared lifecycle reuse. A frontend formatter prefers URL, then host/port. A dependency-light DTO package is justified if it removes the SDK-to-daemon dependency path, but it should contain wire types rather than another abstraction layer.

### 9.2 Source map at reviewed revision

| Concern | Starting reference |
| --- | --- |
| Registration and conditional removal | `internal/daemon/daemon.go:296–402,426–453` |
| Expiry coordination | `internal/daemon/daemon.go:236–244` |
| Registry backend/ownership state | `internal/registry/registry.go:24–86` |
| Runtime duplicate state and reaper removal | `internal/runtime/manager.go:34–119`, `service.go:17–31,87–105` |
| Bind-first allocator | `internal/runtime/allocator.go:43–98` |
| TCP forwarding/cleanup | `internal/proxy/tcp.go:23–53`, `internal/runtime/service.go:53–84,127–135` |
| HTTP double read and URL generation | `internal/proxy/http.go:43–119` |
| Listener startup | `internal/daemon/daemon.go:114–190` |
| Lease TTL and expiry | `internal/lease/manager.go:21–138` |
| Docker reconciliation and callbacks | `internal/dockerwatch/watcher.go:47–244` |
| Docker binding safety | `internal/dockerwatch/inspect.go:50–117` |
| Native lifecycle duplicated by CLI | `pkg/devmesh/client.go:87–182`, `cmd/devmesh/cmds/register/register.go` |
| CLI endpoint formatting | `cmd/devmesh/cmds/services/list.go`, `resolve.go`, `inspect.go` |
| Persistence failure | `internal/state/store.go:60–68,99–125` |
| Non-socket path safety | `internal/transport/unix.go:29–53` |
| Framework config and duration conversion | `cmd/devmeshd/cmds/serve.go`, `internal/config/config.go` |

The complete v1 source analysis remains useful when implementing a targeted fix. Read the relevant finding, not its superseded roadmap, and use symbol names if line numbers change.

### 9.3 Compatibility decisions to make explicit in implementation

- Public callers can no longer impersonate Docker or choose IDs/owners. Reject reserved fields with an actionable error.
- Native hot replacement uses close/create for now, not a new update endpoint. Docker replacement remains internal and frontend-preserving.
- Kind and explicit HTTP host are fixed for a service during a daemon run. Mutation attempts must not partially alter routing.
- Listeners stay reserved until shutdown. Remove the obsolete idle-reaper option rather than silently ignoring it.
- HTTP consumers must honor `frontend.url`; incorrect old high-port URLs are corrected, not compatibility-preserved.
- Prompt connection closure replaces the proposed configurable graceful-drain promise for this release. Document the behavior plainly.
- Keep Glazed precedence and the existing JSON mapper. Document actual environment names such as `DEVMESH_STATE` and current socket defaults.
- Do not edit the imported brief or byte-identical vault mirror to make history look consistent. Link this v2 as the current implementation scope.

Standard-library references for the focused work: [net](https://pkg.go.dev/net), [http.Server](https://pkg.go.dev/net/http#Server), [httputil.ReverseProxy](https://pkg.go.dev/net/http/httputil#ReverseProxy), and [sync](https://pkg.go.dev/sync). Docker inventory and event contracts are documented in the [Engine API](https://docs.docker.com/reference/api/engine/). No new framework is implied by these references.

## 10. Practical CLI-first devctl integration

This section records the next concrete integration, based on inspection of devctl at `bac5d91` in `/home/manuel/code/wesen/go-go-golems/devctl`. It extends the focused design without introducing another registry or orchestration framework. All flags and fields explicitly marked proposed below still require implementation.

**Responsibility split:** devctl owns application processes, their logs, health checks, restart, and run records. Devmesh owns stable frontend listeners, backend registration, and forwarding. Applications or Docker bind backends. Neither tool should select an allegedly free backend port, release it, and assume the child can bind it later.

### 10.1 What exists and where integration belongs

Devctl plugins produce configuration, preparation results, and `launch.plan` service specifications. `pkg/engine/types.go` currently provides command, environment, executable, and TCP/HTTP health fields, but no devmesh publication fields. `pkg/operator/planner.go` closes plugin clients after preparing the plan. Consequently a planning plugin cannot own a long-lived registration lease.

The persistent wrapper in `cmd/devctl/cmds/wrap_service.go` owns the application's lifetime after the initiating CLI returns. That is the appropriate future home for wrapper-managed native registration. The controller currently starts selected services before completing their health checks; it does not provide dependency-aware readiness scheduling. The first integration should not require changing that controller.

```text
First integration: existing supervision, no new dependency graph

 devctl --> foreground Compose service
                    |
                    v
 Docker binds ephemeral loopback backend
                    |
                    v
 devmesh watcher registers database --> stable frontend
                                              ^
 devctl --> application shell launcher --------+
               wait for resolve; set DATABASE_URL
               exec application
```

Each producer has exactly one registration owner:

- Docker containers use devmesh's existing watcher; devctl must not register them again.
- A devmesh-aware application uses its own SDK handle; its wrapper must not duplicate it.
- An unmodified native application can later use wrapper-managed registration if it can report its bound address.

Treat devmeshd as shared local infrastructure. Project-specific `devctl down` must not stop it or remove other projects' registrations. Validate availability when required and give an actionable error; automatic global daemon installation/startup is not part of this integration.

### 10.2 First deliverable: reliable foreground registration

Retain the existing command shape and implement its lifecycle using the shared producer handle from section 5.3:

```bash
devmesh register \
  --name checkout.api \
  --backend 127.0.0.1:49173 \
  --format jsonl
```

The backend must already be bound. On successful registration, emit and immediately flush one JSON row containing the name, frontend endpoint, registration ID, and expiry. Do not emit lease tokens. Keep stdout machine-readable and put diagnostics on stderr. Verify early output with a subprocess test while the registration process is still alive; a call to `AddRow` alone is not proof that an output pipeline has flushed.

Maintain the lease using the effective server TTL, recover after devmeshd restart, and close the latest registration with a short timeout on cancellation. A killed keeper falls back to lease expiry. The keeper is not an application supervisor: if it is separate from the producer, the caller must stop it when that producer exits. Do not suggest running an unrelated keeper forever against a dead process.

`--once` remains a diagnostic, expiring registration without renewal. It is not a persistent publication contract and must be described that way in help. Do not expose separate token-handling heartbeat/delete shell commands just to recreate the keeper loop in every script.

**Implementation sites:** devmesh `pkg/devmesh/client.go`, `cmd/devmesh/cmds/register/register.go`, and the daemon/lease correctness changes in Slice A. No new administrative endpoint is required.

### 10.3 Second deliverable: raw, bounded resolution

Proposed domain options:

```bash
devmesh services resolve checkout.postgres --wait 20s --raw
```

- `--raw` emits exactly the frontend endpoint plus one newline: `host:port` for TCP or the complete URL for HTTP. Emit no table headers or progress on stdout.
- Without `--raw`, retain the existing Glazed structured-output contract. Implement raw rendering using a supported command-layer path; do not add a new universal output format or bypass the framework throughout the CLI.
- `--wait` retries an unknown/unavailable service and transient daemon connection failures until one overall deadline. Use a modest fixed polling interval and cap each request by remaining time. No watcher stream or subscription protocol is needed.
- Invalid flags, invalid requests, and non-retryable errors fail immediately. Timeout/cancellation exits nonzero with a useful stderr diagnostic and no endpoint on stdout.
- Without `--wait`, preserve the normal one-request structured lookup; raw lookup must fail rather than print an unavailable endpoint as success.
- Use the shared endpoint formatter from section 5.6 so HTTP URLs are not lost.

A registered backend is not application readiness. A database may be registered before it accepts SQL connections. Keep retry/readiness behavior in the application or its existing health check; do not make resolve a database-specific probe.

**Implementation site:** devmesh `cmd/devmesh/cmds/services/resolve.go`, command registration as needed for raw output, and the common frontend formatter. Poll the existing `/v1/services/{name}` endpoint.

### 10.4 Working example before any devctl schema extension

Use a foreground Compose service with devmesh labels and loopback ephemeral publication. Launch the consumer through this small script:

```bash
#!/usr/bin/env bash
set -euo pipefail

# --raw and --wait are proposed additions, not current flags.
db_endpoint=$(
  devmesh services resolve checkout.postgres --wait 20s --raw
)

# Example local-development credentials; use project configuration in practice.
export DATABASE_URL="postgres://dev:dev@${db_endpoint}/app"
exec ./api
```

Return that script as the application's existing `ServiceSpec.Command`. Environment is derived immediately before `exec`, rather than frozen during an earlier plugin planning phase. Devctl supervises the process and logs as it already does. Preserve bounded startup behavior and configure its readiness timeout to accommodate the resolver wait and application initialization.

Demonstrate this with two explicit steps first: start infrastructure, then launch the consumer. It can also fit a launch plan containing both services because the consumer launcher waits, but prove that concrete workflow before claiming generalized dependency support. No dependency graph or new launch phase is required for this example.

A planning plugin may resolve already-running external dependencies without mutating them, but must not create registrations during `config.mutate`, `launch.plan`, or dry-run planning. Avoid pulling endpoint allocation into the planning transaction.

### 10.5 Optional later deliverable: native wrapper registration

Only add this after selecting a real non-self-registering application that needs it. Proposed addition to the plugin's JSON launch-plan service object, shown as YAML for readability:

```yaml
devmesh:
  name: checkout.api
  backend_file: api.addr
```

This is not a new top-level `.devctl.yaml` configuration format. Adding it requires updating `engine.ServiceSpec`, relevant plugin schema/SDK contracts, wrapper request serialization, and tests. Keep it optional so ordinary services are unchanged.

The wrapper interprets `backend_file` inside its fresh private per-run directory, validates that the path stays there, and passes the absolute path as `DEVCTL_BACKEND_FILE`. The application must bind its listener first and atomically publish one small JSON object:

```json
{"backend":"127.0.0.1:49173"}
```

A generic process does not automatically implement this convention. For an application that cannot report a dynamic address, use an explicitly configured backend address initially or keep registration in an application-specific launcher. Do not add log scraping, port scanning, or socket activation to solve that general case.

```text
persistent devctl wrapper:
    establish fresh per-run address path
    start child with DEVCTL_BACKEND_FILE
    wait for complete valid address, child exit, or startup deadline
    register through shared devmesh Go client
    mark registration setup complete
    maintain handle while child runs
    child exits or wrapper stops -> close registration
```

Tie successful registration to the wrapper's startup handshake before claiming setup complete; keep application health separate. Ensure child waiting, log capture, signal forwarding, and registration failure cleanup retain their existing ownership guarantees. On required-registration timeout, terminate only the owned child/process group and report startup failure. A fresh address path prevents restart from consuming a previous run's backend. Tokens stay in memory, not status output or persisted run metadata.

**Devctl implementation sites:** `pkg/engine/types.go`, `pkg/supervise/wrapper_request.go`, the request construction in `pkg/supervise/supervisor.go`, `cmd/devctl/cmds/wrap_service.go`, and affected protocol/SDK fixtures. The existing wrapper replaces the need for another `devmesh run` subprocess layer.

### 10.6 Transaction boundary: ordered setup with bounded cleanup

Do not attempt an atomic transaction across process creation, Docker, registration, and consumer startup. Intermediate states are acceptable and recoverable:

| Failure | Required simple behavior |
| --- | --- |
| Producer bound, registration fails | Retry within startup budget; stop owned producer if registration is required |
| Keeper crashes | Lease expires; no separate garbage-collection coordinator |
| Producer exits | Its owner closes registration; expiry remains fallback |
| Consumer fails to launch | Report failure; do not roll back independently owned infrastructure |
| devmeshd restarts | Keeper re-registers and consumer resolves/reconnects as appropriate |
| Create succeeds but response is lost | Retry within budget; conflict may require waiting for the first lease to expire |

For response loss, initially accept bounded recovery through expiry. Never let retries overwrite an owner simply because the name/backend matches. A request-id/idempotency mechanism is deferred unless that recovery delay is a material problem in practice.

The daemon's local correctness boundaries remain strict: registration success means a valid published frontend/backend association, and stale cleanup must not remove a replacement. Loose orchestration does not mean loose ownership.

A script receives an endpoint snapshot, not live environment updates. If a remembered frontend becomes occupied during daemon downtime and devmesh selects another one, the consumer must resolve again/restart. Do not promise its already-running environment variables can change automatically.

### 10.7 Delivery order, tests, and explicit non-goals

1. Finish Slice A's removal/lease prerequisites; harden foreground register and add raw bounded resolve.
2. Prove the Compose database → devmesh → devctl application-launcher example without changing devctl's schema.
3. Add native wrapper registration only when the selected application requires it; this is conditional follow-up, not a prerequisite to shipping steps 1–2.

Test early JSON output, cancellation, timeout/no-stdout behavior, HTTP URL formatting, and registration recovery. In the working example query through the resolved database frontend, recreate the database, query again through the same frontend, restart devmeshd, and verify rediscovery. For the optional native path, test child exit and registration failure cleanup plus stale address-file isolation.

Do not add `devmesh run`, a public hot-update API, frontend reservations, backend free-port probes, dependency graphs, socket-activation infrastructure, cross-service rollback, or a general scripting protocol. The integration should reuse the supported process and endpoint owners instead of duplicating them.

## 11. Final direction

Build the three slices, keep the useful architecture, and delete the redundant paths. The target is one backend state per service, one conditional removal, one producer lifecycle loop, one HTTP target selection, and one endpoint formatter. Small identity checks, simple periodic repair, and explicit resource ownership are sufficient for the supported local workflows.

The v1 findings justify correcting important behavior. They do not justify a larger architecture or treating every rare edge case as a release blocker. When choosing between a new mechanism and a narrower, honest contract that serves the real workflow, choose the narrower contract first.
