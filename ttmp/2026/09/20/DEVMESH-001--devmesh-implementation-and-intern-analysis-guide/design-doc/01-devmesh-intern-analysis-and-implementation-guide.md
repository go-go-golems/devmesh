---
Title: Devmesh intern analysis and implementation guide
Ticket: DEVMESH-001
Status: draft
Topics:
    - devmesh
    - architecture
    - go
    - docker
    - proxy
    - registry
    - tcp-http
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/devmesh-implementation-guide.md
      Note: Original source specification (what/contract) that this guide explains
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/design-doc/01-devmesh-intern-analysis-and-implementation-guide.md
      Note: Primary intern deliverable (analysis + design + implementation guide)
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/sources/devmesh-implementation-guide.md
      Note: Imported, byte-identical source copy for the ticket
ExternalSources: []
Summary: |
    A deep, pedagogical walkthrough of the devmesh local service registry, stable endpoint broker, TCP/HTTP reverse proxy, native Go lease client, and Docker/Compose auto-registration. The CLI and help system are defined with the Glazed framework. Written for a new intern who must implement the MVP PR-by-PR.
LastUpdated: 2026-09-20T16:10:00-04:00
WhatFor: ""
WhenToUse: ""
---

# Devmesh intern analysis and implementation guide

This document is the reading companion to the imported source specification
`devmesh-implementation-guide.md` (see `../sources/devmesh-implementation-guide.md`).
The source guide is the *what* and the *contract*. This document is the *why*,
the *how*, and the *in what order*, written so that a new intern can go from
zero context to a working MVP without needing a senior engineer in the room.

Read this entire document once before writing code. Then use
[Section 30 (Milestones)](#30-milestones-and-pr-plan) as your working plan and
the [section 10 API reference](#10-local-http-api-reference) as your contract.

---

## 0. How to use this guide

### 0.1 What you are building

`devmesh` is a small local daemon plus CLI plus Go library that gives development
services **stable, discoverable endpoints** even when the underlying process or
container is listening on an arbitrary ephemeral port.

Concretely: a PostgreSQL container may be published by Docker on
`127.0.0.1:49173` today and `127.0.0.1:49901` after a `--force-recreate`.
Devmesh keeps `checkout.postgres` pinned at a *stable* frontend such as
`127.0.0.1:15432` and silently re-points new connections at whatever backend is
currently live.

### 0.2 The mental model you must internalize

Everything else is detail. This one sentence is the project:

> **A logical service name owns a stable frontend; producers publish ephemeral
> backends into that name; consumers resolve the name and always reach the
> frontend.**

```text
        name                     stable frontend               ephemeral backend
   checkout.postgres   --->   127.0.0.1:15432   --->   127.0.0.1:49173 (today)
                                                    --->   127.0.0.1:49901 (after recreate)
```

### 0.3 Prerequisites

- Comfortable reading Go: interfaces, `context`, goroutines, channels, `sync`.
- Basic networking: TCP handshake, loopback, what a port is, `net.Listen`.
- Basic Docker: images, containers, port publishing, Compose.
- Willingness to run `go test -race ./...` constantly and treat races as bugs.

### 0.4 How each section is laid out

Most sections follow the same shape:

1. **Intent** — what this component is responsible for, in one paragraph.
2. **Why it exists** — the failure mode it prevents.
3. **Contract** — the exact types, endpoints, or algorithm.
4. **Pseudocode** — near-Go sketches you can turn into real code.
5. **Pitfalls** — mistakes that have system-wide consequences.
6. **File map** — the planned files that implement it.

### 0.5 The one rule you must never break

Never "check whether a port is free" and then later bind it. Always **bind
first**, and keep the listener. See [Section 13](#13-stable-frontend-port-allocation).
A check-then-bind is a race even on a single developer machine, and it is the
single most common way this kind of project gets corrupted.

---

## 1. Executive summary

### 1.1 What devmesh is

`devmesh` is a local developer tool with four cooperating pieces:

| Piece | Binary / path | Responsibility |
| --- | --- | --- |
| Daemon | `devmeshd` | Registry, frontend listeners, TCP/HTTP proxying, Docker watcher, persistence, local API |
| CLI | `devmesh` | `list`, `resolve`, `inspect`, `doctor`, `register`, `daemon` (Glazed commands + help) |
| Go client | `pkg/devmesh` | Bind :0, register the actual port, keep the lease alive |
| Docker adapter | `internal/dockerwatch` | Watch labeled containers, map published ports into registrations |

### 1.2 The value proposition

Development has a port-collision problem. Two projects both want PostgreSQL on
`5432`. Two services both want `8080`. The usual workarounds are:

- hard-code different ports per developer (drift, docs go stale);
- give up on fixed ports and read the port from a file (no discoverability);
- use `/etc/hosts` and per-host ports (does not work for raw TCP: DNS names are
  gone by the time a TCP socket opens).

Devmesh fixes this without touching application code by inserting a stable
indirection:

```text
consumer  -->  stable frontend (devmesh-owned)  -->  current backend (producer-owned)
```

### 1.3 Scope of the MVP

The MVP is TCP-first. HTTP hostname routing and TLS are later phases, layered on
the same registry. The definition of done is in the source guide §2 and repeated
as acceptance tests in [Section 29](#29-testing-strategy).

### 1.4 The architecture in one diagram

```text
                                    +---------------------+
                                    | Docker Engine       |
                                    | postgres container  |
                                    | internal :5432      |
                                    +----------+----------+
                                               | docker publishes
                                               | 127.0.0.1:49173
                                               v
+-----------------+                   +---------------------+
| Native Go app   |  register         |                     |
| backend :49382  |  (lease)  ------->|      devmeshd       |
+-----------------+                   |                     |
                                      |  registry           |
+-----------------+                   |  port allocator     |
| devmesh CLI     |  unix socket ---->|  TCP proxy manager  |
+-----------------+   HTTP/JSON       |  HTTP proxy         |
                                      |  Docker watcher     |
+-----------------+                   |  lease manager      |
| third-party app |                   |  state persistence  |
| psql / DBeaver  |                   +----------+----------+
+--------+--------+                              |
         |                            stable frontend
         +--------------------------> 127.0.0.1:15432
                                                |
                                                +--> 127.0.0.1:49173
```

### 1.5 The five invariants

If you remember nothing else, remember these. Every design decision in this
document protects one of them.

1. **Producer-owned backends.** The application (or Docker) chooses the backend
   port. Devmesh never hands out a "free" backend port and hopes it stays free.
2. **Devmesh-owned frontends.** Devmesh allocates frontend ports by binding them
   and keeping the listener open for the lifetime of the service runtime.
3. **Stable identity.** A logical service name survives backend churn and daemon
   restarts; the frontend endpoint a consumer already configured keeps working.
4. **Docker is an adapter.** The registry core works with no Docker installed.
   Docker metadata is translated into the same registration model native
   processes use.
5. **Generic TCP is host+port.** You cannot multiplex arbitrary TCP services on
   one `127.0.0.1:<port>` by hostname. HTTP can, because HTTP carries a Host
   header; raw TCP cannot.

---

## 2. The problem, stated precisely

### 2.1 Ephemeral ports

When a program calls `net.Listen("tcp", "127.0.0.1:0")`, the kernel picks an
unused high port (the *ephemeral port range*, commonly 32768–60999). Docker does
the same when you publish `"127.0.0.1::5432"`: the container always listens on
`5432` internally, but the host-side port is chosen by Docker and changes when
the container is recreated.

The kernel does not guarantee the same ephemeral port twice. In practice it
almost never reuses the same one immediately. So:

```text
run 1:  docker publishes 127.0.0.1:49173  -> works today
run 2:  docker publishes 127.0.0.1:49901  -> every stored connection string is now wrong
```

### 2.2 Why fixed ports do not scale

If two projects both pin host `5432`, the second `docker compose up` fails with
`bind: address already in use`. Developers then either change one project's port,
or stop the other project, or run one database at a time. None of these scale to
"five services across three repos".

### 2.3 Why DNS alone does not fix raw TCP

It is tempting to say "use `checkout.postgres` as a DNS name". That works for
HTTP because the HTTP request contains `Host: checkout.postgres`. It does **not**
work for PostgreSQL, Redis, MySQL, or any other raw TCP protocol:

```text
Postgres client resolves "checkout.postgres" -> 127.0.0.1
Then opens TCP to 127.0.0.1:5432.
The name is no longer present in the connection. The kernel only sees 127.0.0.1:5432.
```

If two PostgreSQL containers both need canonical `:5432`, they need distinct IP
addresses, not distinct names. That is a future feature (per-service loopback
IPs + local DNS, source guide §37), explicitly **not** in the MVP.

### 2.4 The solution shape

Keep one stable, devmesh-owned listener per logical service. Route each accepted
connection to the current backend, read atomically at connection time.

```text
consumer's config: postgres://127.0.0.1:15432/checkout   (never changes)
devmesh registry:  checkout.postgres -> backend 127.0.0.1:<whatever is live now>
```

---

## 3. Conceptual model

This section defines the vocabulary. Use these words consistently in code,
comments, commit messages, and the API.

### 3.1 Entities

```text
+--------------+     owns      +----------------+     supplies     +-----------+
| Service name | ------------> | ServiceRuntime | <-------------- | Backend   |
| logical id   |               | frontend       |                 | host:port |
+--------------+               | listener       |                 +-----------+
                               +----------------+
                                       ^
                                       | expressed by
                                +--------------+
                                | Registration |
                                | producer     |
                                | lease, owner |
                                +--------------+
```

- **Service name** — the logical identity, e.g. `checkout.postgres`. Validated,
  unique per daemon.
- **ServiceRuntime** — the long-lived daemon-side object that owns the stable
  frontend listener for a service. Survives backend churn.
- **Registration** — a producer's assertion that "right now, this service's
  backend is `host:port`", plus who owns it and when the assertion expires.
- **Backend** — the current `host:port` the proxy dials.
- **Frontend** — the stable `host:port` (or URL) consumers connect to.
- **Owner key** — a stable string identifying *who* owns the logical name. It
  must survive container recreation and process restart semantics where it can.

### 3.2 Producer vs consumer

- A **producer** creates capacity: a native Go app, a Docker container, or an
  operator using `devmesh register`.
- A **consumer** uses capacity: `psql`, `pgx`, `DBeaver`, a browser, a frontend
  dev server, another service.

Producers talk to the registry. Consumers talk to the frontend. The two never
share a port, and that separation is the whole point.

### 3.3 Lifecycle of a service name

```text
                 first registration
   (nothing)  ------------------------>  runtime created, frontend bound
                                            |
                                            | backend updated on
                                            | every new registration
                                            v
                                        (ready)  <---- periodic heartbeats
                                            |
                          backend disappears | or lease expires
                                            v
                                      (unavailable)   frontend still bound
                                            |
                       idle grace (10m)     |
                                            v
                                    runtime may be closed,
                                    port assignment remembered
                                            |
                          registration again |
                                            v
                                     runtime re-created,
                                     remembered port retried
```

### 3.4 Status values

The API exposes a `status` field. Define these early and keep them stable:

| Status | Meaning |
| --- | --- |
| `ready` | A backend is present. New connections should succeed. |
| `unavailable` | Frontend is bound, but no backend is currently registered. |
| `degraded` | Reserved for daemon-level health (e.g. Docker down), not a service state in the MVP. |

For an unknown name, return HTTP `404`, not a service object with a status.

---

## 4. System architecture

### 4.1 Process and responsibility boundaries

```text
+------------------------------ devmeshd process ------------------------------+
|                                                                              |
|  transport/unix.Server ---> api.Server (net/http mux over unix socket)       |
|        |                          |                                          |
|        |                          +-- /v1/health                             |
|        |                          +-- /v1/services                           |
|        |                          +-- /v1/registrations                      |
|        |                                                                     |
|        v                                                                     |
|  registry.Registry  <-- owner-key rules, name index                          |
|        |                                                                     |
|        v                                                                     |
|  runtime.Manager  ---> ServiceRuntime per name                               |
|        |                    |                                                |
|        |                    +-- allocator (bind-first port allocation)       |
|        |                    +-- proxy.TCPProxy (accept loop per frontend)    |
|        |                    +-- proxy.HTTPProxy (shared listener, later)     |
|        |                                                                     |
|  lease.Manager  ---> sweeper goroutine, expiry of SourceProcess registrations|
|                                                                              |
|  dockerwatch.Watcher ---> Docker Engine events + startup reconciliation      |
|                                                                              |
|  state.Store ---> atomic JSON persistence of frontend port assignments       |
+------------------------------------------------------------------------------+
```

### 4.2 Data flow: native Go process

```text
1. App calls net.Listen("tcp", "127.0.0.1:0")        <- producer owns backend
2. App calls devmesh.ListenTCP(ctx, "checkout.api")  <- client library
3. Client POST /v1/registrations {backend: actual}
4. Daemon allocates frontend (bind-first), creates runtime
5. Response includes frontend host:port
6. App serves http.Serve(listener, handler)
7. Client heartbeats every TTL/3
8. App Close() -> DELETE registration; listener closed by app
```

### 4.3 Data flow: Docker container

```text
1. docker compose up -d  (container listens internally on 5432)
2. Docker publishes 127.0.0.1:49173 -> container:5432
3. Watcher receives `start` event (or startup reconciliation finds it)
4. Watcher inspects container, reads labels
5. Watcher reads NetworkSettings.Ports["5432/tcp"][0] = {127.0.0.1, 49173}
6. Watcher builds Registration{Name, Kind, Backend, Source: docker, OwnerKey}
7. Daemon allocates/reuses frontend 15432, sets backend
8. `devmesh resolve checkout.postgres` -> 127.0.0.1:15432
```

### 4.4 Data flow: consumer connection

```text
1. psql connects to 127.0.0.1:15432
2. TCP proxy Accept() returns client conn
3. Proxy atomically loads current backend (127.0.0.1:49173)
4. Proxy dials backend with 3s timeout
5. Proxy io.Copy in both directions
6. On close, half-close propagation (CloseWrite) preserves protocol semantics
```

### 4.5 Why the daemon owns frontend ports

If each producer allocated its own stable port, then:

- two producers could race for the same "stable" port;
- no component would own the persistent mapping;
- the mapping could not be asserted atomically at allocation time.

Centralizing frontend allocation in the daemon means exactly one component binds
and remembers. It is also the component that persists state and survives
producer restarts.

---

## 5. Repository layout and module map

### 5.1 Planned tree

```text
devmesh/
├── cmd/
│   ├── devmesh/
│   │   ├── main.go            # CLI entrypoint (Glazed root)
│   │   └── cmds/
│   │       ├── root.go        # mounts all command groups
│   │       ├── services/
│   │       │   ├── root.go
│   │       │   ├── list.go    # GlazeCommand
│   │       │   ├── resolve.go # GlazeCommand
│   │       │   └── inspect.go # GlazeCommand
│   │       ├── register/
│   │       │   ├── root.go
│   │       │   └── register.go
│   │       └── doctor/
│   │           ├── root.go
│   │           └── doctor.go
│   └── devmeshd/
│       ├── main.go            # daemon entrypoint (Glazed root)
│       └── cmds/
│           └── serve.go
├── pkg/
│   ├── devmesh/
│   │   ├── client.go          # Go registration client
│   │   ├── registration.go    # Handle, ListenTCP
│   │   └── types.go           # public Kind/Backend/RegistrationOptions
│   └── doc/                   # embedded Glazed help pages
│       ├── doc.go             # //go:embed + AddDocToHelpSystem
│       ├── devmesh-overview.md
│       ├── devmesh-resolve-workflow.md
│       └── devmesh-docker-compose.md
├── internal/
│   ├── api/
│   │   ├── server.go          # mux + http.Server wiring
│   │   ├── handlers.go        # endpoint handlers
│   │   └── dto.go             # request/response structs
│   ├── config/
│   │   └── config.go          # load/merge/default config
│   ├── dockerwatch/
│   │   ├── watcher.go         # event loop + reconnect
│   │   ├── inspect.go         # inspect -> backend discovery
│   │   └── labels.go          # label parsing + owner key
│   ├── lease/
│   │   └── manager.go         # tokens, expiry sweeper
│   ├── proxy/
│   │   ├── tcp.go             # bidirectional TCP copy
│   │   └── http.go            # httputil.ReverseProxy
│   ├── registry/
│   │   ├── registry.go        # in-memory map + locks
│   │   ├── model.go           # ServiceRecord/ServiceRuntime
│   │   └── errors.go          # typed errors
│   ├── runtime/
│   │   ├── manager.go         # EnsureTCPRuntime, SetBackend, ...
│   │   └── service.go         # ServiceRuntime
│   ├── state/
│   │   ├── store.go           # atomic load/save
│   │   └── model.go           # State JSON model
│   └── transport/
│       ├── unix.go            # unix socket listener + stale handling
│       └── client.go          # unix-socket http.Client
├── integration/
│   ├── tcp_proxy_test.go
│   ├── lease_test.go
│   └── docker_test.go
├── examples/
│   ├── native-go/
│   └── compose-postgres/
│       └── compose.yaml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 5.2 Dependency direction (must stay acyclic)

```text
cmd/*  --->  internal/api  --->  internal/runtime  --->  internal/proxy
                  |                    |                     |
                  v                    v                     v
             internal/registry   internal/state        internal/transport
                  ^                                            
                  |                                            
             internal/lease ---> registry                       
             internal/dockerwatch ---> registry (via runtime/api)
             internal/config ---> (everyone; leaf)
```

Rules:

- `registry` never imports `dockerwatch`, `proxy`, or `state`.
- `proxy` never imports `registry`; it receives a backend accessor function.
- `state` never imports `runtime`; it stores plain data.
- `config` is a leaf; it imports nothing internal.
- **Glazed lives only at the command layer.** `cmd/devmesh`, `cmd/devmeshd`,
  and `pkg/doc` may import `github.com/go-go-golems/glazed`; `internal/*` and
  `pkg/devmesh` must not. Domain code stays framework-agnostic so the Go client
  and daemon can be embedded or tested without Cobra/Glazed.
- Domain data flows into commands as decoded settings structs; commands flow
  into the daemon over the same local HTTP API every other client uses.

### 5.3 Public vs internal

Everything under `internal/` may change freely. The only stable API for external
users is `pkg/devmesh`. Keep `pkg/devmesh` small: `Register`, `ListenTCP`,
`Handle`, `RegistrationOptions`, `Kind`, `Backend`.

### 5.4 Module dependencies

```text
go.mod requires:
  github.com/go-go-golems/glazed      # CLI, structured output, help (command layer only)
  github.com/docker/docker            # Docker Engine client (dockerwatch only)
  github.com/spf13/cobra              # via glazed; root command types
  github.com/jackc/pgx/v5             # PostgreSQL integration tests/example only
```

Do not add a web framework. `net/http`, `net/http/httputil`, and the standard
library cover the daemon API and both proxies. Keep Glazed and Docker imports out
of the registry/proxy/state core so it stays embeddable and testable.

---

## 6. Core data model

This is the canonical model. The source guide defines it in §6; this section
explains each field's purpose and the rules around it.

### 6.1 Service name

```go
// ValidateName reports whether name is a legal devmesh service name.
func ValidateName(name string) error
```

Grammar:

```regex
^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$
```

Rules:

- lowercase only;
- dot-separated namespaces (`checkout.postgres`, `billing.api.v2`);
- no spaces, no underscores;
- max 120 characters;
- unique within one daemon.

Why so strict? Names become part of CLI arguments, log fields, state keys, and
(later) HTTP hostnames. A permissive grammar creates escaping problems later.
Reject early, reject loudly.

Examples:

```text
checkout.postgres    valid
checkout-api         valid
billing.api.v2       valid
Checkout.Api         invalid  (uppercase)
checkout_postgres    invalid  (underscore)
.checkout            invalid  (empty label)
checkout.            invalid  (trailing dot)
```

### 6.2 Service kind

```go
type Kind string

const (
    KindTCP  Kind = "tcp"
    KindHTTP Kind = "http"
)
```

`KindTCP` is opaque byte forwarding. PostgreSQL, Redis, MySQL, SMTP and custom
binary protocols are all `tcp`. `KindHTTP` is reserved for the hostname-routed
reverse proxy phase and is **not** implemented in the TCP MVP.

### 6.3 AppProtocol

```go
type AppProtocol string
```

Examples: `postgres`, `redis`, `mysql`, `grpc`, `http`, `https`.

`AppProtocol` is a UX hint only. **It must not change proxy semantics in the
MVP.** Its only jobs are CLI display ("APP" column) and future `devmesh url`
rendering (`postgres://...`). If you find yourself branching on it in the proxy,
stop.

### 6.4 Backend

```go
type Backend struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}
```

MVP restriction: the backend host must be a loopback address.

Accept:

```text
127.0.0.1
::1
localhost   (resolve, then verify every result is loopback)
```

Reject remote addresses unless a future `allow_remote_backends` setting is on.
The regex-only check on `localhost` is not enough; resolve it and check the
resulting IPs.

### 6.5 Source

```go
type Source string

const (
    SourceProcess Source = "process"
    SourceDocker  Source = "docker"
    SourceManual  Source = "manual"
)
```

Source tells you *how the registration arrived*. It matters for lease
management (only `process` and `manual` registrations expire; Docker
registrations are driven by container lifecycle).

### 6.6 Registration

```go
type Registration struct {
    ID            string
    Name          string
    Kind          Kind
    AppProtocol   string
    Backend       Backend
    Source        Source
    OwnerKey      string
    PreferredPort int
    LeaseExpires  *time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

Field notes:

- `ID` — per-registration instance ID (ULID/UUID). Not an authorization secret.
- `OwnerKey` — logical owner; see §6.7.
- `PreferredPort` — provider's request for a frontend port. Advisory.
- `LeaseExpires` — nil for Docker/manual; set for process leases.

### 6.7 OwnerKey: the heart of stable identity

```text
process:<registration UUID>
docker:<compose-project>:<compose-service>:<container-port>
manual:<registration UUID>
```

The rule:

> Two registrations with the same `OwnerKey` may replace each other's backend.
> Different owner keys may never silently replace each other.

Why `container-port` in the Docker key? Because a single container can publish
multiple ports (e.g. `5432/tcp` and `9187/tcp` for metrics). Each published port
maps to a distinct logical service, so the key must disambiguate by container
port. Do **not** use the container ID: it changes on every recreate, which would
break the whole "recreate without changing frontend" promise.

Worked example:

```text
docker:checkout:db:5432
  run 1 container abc123 backend 127.0.0.1:49173
  run 2 container def456 backend 127.0.0.1:49901
Same owner key -> backend replaced, frontend 15432 unchanged.
```

### 6.8 Frontend

```go
type Frontend struct {
    Host string `json:"host"`
    Port int    `json:"port"`
    URL  string `json:"url,omitempty"`
}
```

- For `tcp`, `Host` is normally `127.0.0.1`.
- For `http` (later), `URL` may be `https://api-checkout.dev.example.com`.

### 6.9 Status

```go
type Status string

const (
    StatusReady       Status = "ready"
    StatusUnavailable Status = "unavailable"
)
```

---

## 7. Service identity vs backend registration

This is the most important conceptual split in devmesh, and the source guide
calls it out in §7. If you get this wrong, container recreation and daemon
restart both break.

### 7.1 The two lifetimes

```text
ServiceRuntime:  long-lived, owns frontend listener, survives backend churn
Registration:    short-lived, supplies current backend, replaced often
```

### 7.2 ServiceRuntime

```go
type ServiceRuntime struct {
    Name       string
    Kind       Kind
    Frontend   Frontend
    Listener   net.Listener
    Backend    atomic.Value // stores *Backend; nil when unavailable
    LastActive time.Time
}
```

`atomic.Value` (or `atomic.Pointer[Backend]` on modern Go) lets the accept loop
read the backend without taking a lock on the hot path.

### 7.3 State transitions

On first registration:

```text
registration arrives
    -> allocate/bind frontend listener (idempotent)
    -> create ServiceRuntime
    -> set backend atomically
```

On Docker recreation / new backend:

```text
same logical service (same name, same owner key)
    -> keep frontend listener open
    -> atomically replace backend pointer
    -> existing connections keep their old backend (do not migrate)
```

On backend disappearance:

```text
backend becomes nil
    -> keep frontend listener bound for the idle grace period
    -> TCP: accept then close quickly (no backend)
    -> HTTP: 503 Service Unavailable
```

### 7.4 Idle grace period

Default: **10 minutes**.

```go
const DefaultIdleTTL = 10 * time.Minute
```

Why grace? A container restart is often seconds long. If devmesh freed the
frontend port immediately, Docker would be free to grab it for something else,
and the consumer's configured endpoint would break even though the outage was
transient. The grace period keeps the port reserved through a normal restart.

After the grace period the listener may be closed, but the remembered port
assignment stays in `state.json` so the daemon retries it on the next
registration.

### 7.5 Pseudocode: apply a registration

```go
func (m *Manager) ApplyRegistration(reg Registration) (*ServiceRuntime, error) {
    // 1. Ensure runtime exists (idempotent; allocates frontend only once).
    rt, err := m.EnsureTCPRuntime(reg.Name, reg.PreferredPort)
    if err != nil {
        return nil, err
    }
    // 2. Atomically swap the backend pointer.
    rt.SetBackend(&Backend{Host: reg.Backend.Host, Port: reg.Backend.Port})
    rt.Touch()
    return rt, nil
}
```

`EnsureTCPRuntime` must be safe to call concurrently for the same name; serialize
per-name creation (see §27).

---

## 8. Configuration

### 8.1 Shape

```json
{
  "tcp_frontend_host": "127.0.0.1",
  "tcp_frontend_min": 15000,
  "tcp_frontend_max": 19999,
  "runtime_idle_ttl": "10m",
  "lease_ttl": "15s",
  "shutdown_timeout": "5s",
  "docker": {
    "enabled": true,
    "allow_non_loopback_published_ports": false
  },
  "http": {
    "enabled": false,
    "http_addr": "127.0.0.1:8088",
    "https_addr": "127.0.0.1:8443"
  }
}
```

### 8.2 Precedence

```text
CLI flags > environment variables > config file > compiled defaults
```

Implement this with **Glazed's env and config-file middleware**, not a bespoke
`config.Load` that reads `os.Getenv` and files itself. `devmeshd serve` declares
every config field on its Glazed section; the `DEVMESH` env prefix gives
`DEVMESH_TCP_FRONTEND_MIN`, `DEVMESH_DOCKER_ENABLED`, and so on, and the
config-file middleware loads a file through `config.FileMapper`.

`config.FileMapper` is the compatibility shim that keeps the documented JSON
shape (flat snake_case keys plus nested `docker`/`http` objects) and maps it onto
the default Glazed section's kebab-case fields. Because it is a mapper rather
than a second loader, the file participates in the same provenance chain
(`--print-parsed-fields` shows `source: config`) and the precedence rules above
remain framework-owned. The in-process `config.Config` struct still exists as the
domain representation; `configFromSettings` converts decoded fields into it.

The config-file path itself follows flag > env precedence: `--config` first, then
`DEVMESH_CONFIG`. Because the config middleware executes before the main env
source is applied, the path is resolved by a small pre-parse that runs Glazed's
`FromEnv` against the command schema and decodes the `config` field — so even the
path lookup stays inside the framework and application code never calls
`os.Getenv`.

### 8.3 Environment variables

```text
DEVMESH_SOCKET
DEVMESH_CONFIG
DEVMESH_TCP_FRONTEND_MIN
DEVMESH_TCP_FRONTEND_MAX
DEVMESH_DOCKER_ENABLED
```

### 8.4 Defaults

| Key | Default | Rationale |
| --- | --- | --- |
| `tcp_frontend_host` | `127.0.0.1` | Never expose to LAN by default |
| `tcp_frontend_min` | `15000` | Below the ephemeral range so allocator output is stable |
| `tcp_frontend_max` | `19999` | 5000 slots; plenty for local dev |
| `runtime_idle_ttl` | `10m` | Survives container restart |
| `lease_ttl` | `15s` | Fast crash detection |
| `shutdown_timeout` | `5s` | Do not hang on stuck clients |

### 8.5 Pitfall

Do not create dozens of flags in PR 1. Networking and integration behavior only.
Every new config knob is a compatibility surface; earn it.

### 8.6 Glazed sections and values

The CLI parses flags through Glazed **sections** and decodes them into settings
structs through `cmds/values`. Rather than a bespoke flag parser, define small
reusable sections:

```go
// Daemon connection settings, shared by every CLI command.
func NewDaemonSection() (schema.Section, error) {
    return schema.NewSection(
        "daemon",
        "Daemon Connection",
        schema.WithFields(
            fields.New("socket", fields.TypeString,
                fields.WithHelp("Path to the devmeshd unix socket")),
            fields.New("timeout", fields.TypeString,
                fields.WithDefault("5s"),
                fields.WithHelp("Per-request HTTP timeout")),
        ),
    )
}
```

Decode with the slug the section was registered under: `schema.DefaultSlug`
for the universal section, or the custom slug (`"daemon"`) for a named section.
The config file precedence in §8.2 can be implemented as a Glazed middleware
source chain, but you must re-add every required source explicitly if you
replace the default chain. Do not duplicate a section on both a parent and a
child command; mount it once at the group level.

---

## 9. Daemon transport: HTTP over a Unix socket

### 9.1 Intent

The administrative API must not be reachable from the network. It is exposed as
HTTP/JSON over a Unix domain socket with user-only permissions. This gives you
the ergonomics of HTTP (mux, JSON, standard client) with filesystem-based access
control.

### 9.2 Socket path resolution

Preferred order:

1. the `socket` field, populated by `--socket` or `DEVMESH_SOCKET` (**Glazed env
   middleware**);
2. `~/.devmesh/run/devmesh.sock` (the compiled default).

The old `$XDG_RUNTIME_DIR` rung was removed when env handling moved to Glazed:
Glazed env keys are `<APPPREFIX>_<FIELD>`, so a standard `XDG_RUNTIME_DIR`
cannot be expressed as a field. Use `DEVMESH_SOCKET` instead. All env reads and
config-file loading happen in the Glazed middleware chain; `internal/config`,
`internal/transport`, and `internal/daemon` never call `os.Getenv`.

Create parent directories with mode `0700`. The socket itself should be
accessible only to the current user.

### 9.3 Stale socket handling

This is subtle and a common source of "second daemon" bugs.

```text
on startup:
  if socket path does not exist:
      bind it
  else:
      try to connect
      if connect succeeds:
          another daemon is alive -> exit with error
      else:
          remove the stale socket
          bind it
```

Never `os.Remove` the socket before probing. If you delete a live daemon's
socket, the running daemon stays alive but becomes unreachable, and a second
daemon can bind a new one — now you have two registries.

### 9.4 Server wiring

```go
ln, err := net.Listen("unix", socketPath)
if err != nil {
    return err
}
defer os.Remove(socketPath)

srv := &http.Server{Handler: mux}
go func() { _ = srv.Serve(ln) }()
```

### 9.5 Client transport

```go
func UnixHTTPClient(socketPath string) *http.Client {
    return &http.Client{
        Transport: &http.Transport{
            DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
                var d net.Dialer
                return d.DialContext(ctx, "unix", socketPath)
            },
        },
    }
}
```

The request URL still needs a host even though the dial ignores it. Use a dummy:
`http://devmesh/v1/health`. The CLI and the Go client share this helper.

### 9.6 File map

```text
internal/transport/unix.go     # path resolution + stale handling + Serve
internal/transport/client.go   # UnixHTTPClient
```

---

## 10. Local HTTP API reference

This is the contract. Treat it as frozen once PR 1 lands. Version prefix `/v1`
from day one. Bodies are `application/json`.

### 10.1 Endpoint summary

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/v1/health` | Daemon + Docker dependency health |
| GET | `/v1/services` | List services (no backend details) |
| GET | `/v1/services/{name}` | Resolve a service to its frontend |
| GET | `/v1/services/{name}/inspect` | Debug view incl. backend + owner |
| POST | `/v1/registrations` | Register a process/manual backend |
| POST | `/v1/registrations/{id}/heartbeat` | Renew a lease |
| DELETE | `/v1/registrations/{id}` | Remove a registration |

### 10.2 GET /v1/health

Response `200`:

```json
{
  "status": "ok",
  "version": "0.1.0",
  "docker": "connected"
}
```

`docker` may be `connected`, `degraded`, or `disabled`. Docker being down must
**not** make the whole daemon unhealthy.

### 10.3 GET /v1/services

Response `200`:

```json
{
  "services": [
    {
      "name": "checkout.postgres",
      "kind": "tcp",
      "app_protocol": "postgres",
      "status": "ready",
      "frontend": { "host": "127.0.0.1", "port": 15432 }
    }
  ]
}
```

Do not expose backends here. Listing is for consumers; backend internals belong
to `inspect`.

### 10.4 GET /v1/services/{name}

Ready (`200`):

```json
{
  "name": "checkout.postgres",
  "kind": "tcp",
  "app_protocol": "postgres",
  "status": "ready",
  "frontend": { "host": "127.0.0.1", "port": 15432 }
}
```

Known but no backend (`200`):

```json
{
  "name": "checkout.postgres",
  "kind": "tcp",
  "status": "unavailable",
  "frontend": { "host": "127.0.0.1", "port": 15432 }
}
```

Unknown (`404`) uses the standard error envelope.

### 10.5 POST /v1/registrations

Request:

```json
{
  "name": "checkout.api",
  "kind": "tcp",
  "app_protocol": "http",
  "backend": { "host": "127.0.0.1", "port": 49382 },
  "preferred_port": 8080,
  "ttl_seconds": 15
}
```

Response `201`:

```json
{
  "registration_id": "01J...",
  "lease_token": "random-secret-token",
  "name": "checkout.api",
  "frontend": { "host": "127.0.0.1", "port": 18080 },
  "expires_at": "2026-09-20T20:00:15Z"
}
```

The `lease_token` is returned **only** here. It is used as
`Authorization: Bearer <token>` for heartbeat and delete. Use `crypto/rand`.
Never use the registration ID as the authorization secret.

### 10.6 POST /v1/registrations/{id}/heartbeat

```http
Authorization: Bearer <lease-token>
```

Response `200`:

```json
{ "expires_at": "2026-09-20T20:00:20Z" }
```

`404` means the daemon no longer knows the registration (typically after a
daemon restart). The client must then re-register.

### 10.7 DELETE /v1/registrations/{id}

```http
Authorization: Bearer <lease-token>
```

Response `204 No Content`.

### 10.8 GET /v1/services/{name}/inspect

```json
{
  "name": "checkout.postgres",
  "status": "ready",
  "frontend": { "host": "127.0.0.1", "port": 15432 },
  "backend": { "host": "127.0.0.1", "port": 49173 },
  "source": "docker",
  "owner_key": "docker:checkout:db:5432",
  "docker_container_id": "..."
}
```

### 10.9 DTO sketches

```go
type RegisterRequest struct {
    Name          string  `json:"name"`
    Kind          string  `json:"kind"`
    AppProtocol   string  `json:"app_protocol,omitempty"`
    Backend       BackendDTO `json:"backend"`
    PreferredPort int     `json:"preferred_port,omitempty"`
    TTLSeconds    int     `json:"ttl_seconds,omitempty"`
}

type RegisterResponse struct {
    RegistrationID string   `json:"registration_id"`
    LeaseToken     string   `json:"lease_token"`
    Name           string   `json:"name"`
    Frontend       FrontendDTO `json:"frontend"`
    ExpiresAt      string   `json:"expires_at"`
}
```

Keep DTOs separate from domain types. The wire format is a product decision; the
domain model is an implementation decision.

---

## 11. Error model

### 11.1 Envelope

Every JSON error uses one shape:

```json
{
  "error": {
    "code": "name_conflict",
    "message": "service checkout.postgres is already owned by another registration"
  }
}
```

### 11.2 Stable codes

```text
invalid_request
invalid_name
invalid_backend
name_conflict
port_exhausted
registration_not_found
unauthorized
service_not_found
unsupported_kind
docker_unavailable
internal_error
```

### 11.3 Mapping to HTTP status

| Code | HTTP status |
| --- | --- |
| `invalid_request`, `invalid_name`, `invalid_backend`, `unsupported_kind` | 400 |
| `unauthorized` | 401 |
| `registration_not_found`, `service_not_found` | 404 |
| `name_conflict` | 409 |
| `port_exhausted` | 503 |
| `docker_unavailable` | 503 (only on Docker-specific endpoints) |
| `internal_error` | 500 |

### 11.4 Typed errors in Go

```go
type Error struct {
    Code    string
    Message string
    Status  int
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
```

Helpers: `ErrInvalidName(name)`, `ErrNameConflict(name)`, `ErrPortExhausted()`.
Handlers call one `writeError(w, err)` function that recognizes `*Error` and maps
it; anything else becomes `internal_error` and is logged with detail.

### 11.5 Pitfall

Never let clients parse human-readable strings. If the CLI needs to distinguish
"daemon restarted" from "unknown service", it must check `code`.

---

## 12. Registry

### 12.1 Intent

The registry is the in-memory source of truth for logical services and their
current backend/frontend/owner metadata. It is intentionally dumb: a map, a
mutex, and ownership rules. It performs **no network I/O**.

### 12.2 Shape

```go
type Registry struct {
    mu       sync.RWMutex
    services map[string]*ServiceRecord
}

type ServiceRecord struct {
    Name        string
    Kind        Kind
    AppProtocol string
    OwnerKey    string
    Source      Source
    Backend     Backend
    Frontend    Frontend
    Status      Status
    UpdatedAt   time.Time
}
```

### 12.3 Operations

```go
func (r *Registry) CreateOrReplaceOwned(rec *ServiceRecord) error
func (r *Registry) Resolve(name string) (*ServiceRecord, error)
func (r *Registry) MarkUnavailable(ownerKey string) error
func (r *Registry) Remove(ownerKey string) error
func (r *Registry) List() []*ServiceRecord
```

### 12.4 Ownership algorithm

```text
CreateOrReplaceOwned(rec):
  lock
  existing := services[rec.Name]
  if existing == nil:
      services[rec.Name] = rec
      return nil
  if existing.OwnerKey != rec.OwnerKey:
      return ErrNameConflict(rec.Name)
  // same owner: update allowed
  existing.Backend = rec.Backend
  existing.Status = rec.Status
  existing.UpdatedAt = now
  return nil
```

`Resolve` must never expose backend details to the caller. Keep the projection
in the API layer, not the registry.

### 12.5 Locking rules

- Hold `RWMutex` only for map reads/writes.
- Never call `net.Listen`, `net.Dial`, Docker, or filesystem writes while
  holding it.
- Return copies (or pointers to immutable snapshots) so callers cannot mutate
  registry state without the lock.

### 12.6 File map

```text
internal/registry/registry.go   # map + methods
internal/registry/model.go      # ServiceRecord, Status
internal/registry/errors.go     # typed errors
```

---

## 13. Stable frontend port allocation

### 13.1 The bind-not-check rule

Bad:

```text
if portIsFree(5432):   # closes its probe socket before returning
    later: net.Listen(5432)   # RACE: another process may have bound it
```

Correct:

```go
ln, err := net.Listen("tcp", "127.0.0.1:5432")
// success => we own it now and keep the listener open
```

The probe socket *is* the thing you use. There is no window.

### 13.2 Why not always use port 0

`net.Listen("tcp", "127.0.0.1:0")` asks the kernel for any free port, but the
result is unpredictable and lands in the ephemeral range, which makes it
unstable across daemon restarts and hard to inspect. Devmesh uses its own
configured range (`15000-19999`) so assignments are stable and legible.

### 13.3 Allocation algorithm

For a service named `checkout.postgres`:

```text
1. if state remembers a frontend port for this name:
       try to bind it; on success, return it
2. if registration requests preferred_port:
       try to bind it; on success, return it
3. compute start := hash(name) mod range_size
4. probe ports start, start+1, ..., wrapping, by actually calling net.Listen
5. first success => allocation; keep the listener open
6. persist the chosen port immediately
7. range exhausted => ErrPortExhausted
```

### 13.4 The deterministic offset

```go
func startOffset(name string, size int) int {
    h := fnv.New32a()
    _, _ = h.Write([]byte(name))
    return int(h.Sum32() % uint32(size))
}
```

Hashing spreads services across the range instead of packing them linearly from
15000, which reduces collision-based scan cost when many services exist.

### 13.5 The allocation result includes the listener

```go
type Allocation struct {
    Port     int
    Listener net.Listener
}
```

This is the entire point. Returning only a port reintroduces the race between
allocation and binding.

### 13.6 Pseudocode

```go
func (a *PortAllocator) Allocate(name string, remembered, preferred int) (Allocation, error) {
    if remembered != 0 {
        if ln, err := a.tryBind(remembered); err == nil {
            return a.done(name, remembered, ln)
        }
    }
    if preferred != 0 {
        if ln, err := a.tryBind(preferred); err == nil {
            return a.done(name, preferred, ln)
        }
    }
    size := a.max - a.min + 1
    start := startOffset(name, size)
    for i := 0; i < size; i++ {
        port := a.min + (start+i)%size
        if ln, err := a.tryBind(port); err == nil {
            return a.done(name, port, ln)
        }
    }
    return Allocation{}, ErrPortExhausted()
}

func (a *PortAllocator) tryBind(port int) (net.Listener, error) {
    return net.Listen("tcp", net.JoinHostPort(a.host, strconv.Itoa(port)))
}
```

`done` persists `name -> port` and returns `{port, ln}`.

### 13.7 Pitfalls

- Do not release the listener after probing. The listener **is** the allocation.
- Do not use `SO_REUSEPORT` to "share" frontend ports; two runtimes must never
  bind the same frontend.
- Remembered and preferred ports are advisory: fall back, never fail hard.
- On fallback, update persisted state to the new port and log a warning.

### 13.8 File map

```text
internal/runtime/manager.go   # owns the allocator instance
internal/runtime/service.go   # holds the listener
internal/state/model.go       # tcp_ports map
```

---

## 14. Runtime manager

### 14.1 Intent

The runtime manager is the bridge between registry metadata and live listeners.
It owns `ServiceRuntime` objects and serializes their creation/removal per name.

### 14.2 Shape

```go
type Manager struct {
    mu        sync.Mutex
    runtimes  map[string]*ServiceRuntime
    allocator *PortAllocator
    state     *state.Store
    logger    *slog.Logger
}
```

### 14.3 Operations

```go
func (m *Manager) EnsureTCPRuntime(name string, preferred int) (*ServiceRuntime, error)
func (m *Manager) SetBackend(name string, b Backend) error
func (m *Manager) ClearBackend(name string) error
func (m *Manager) RemoveRuntime(name string) error
```

### 14.4 EnsureTCPRuntime must be idempotent

```go
func (m *Manager) EnsureTCPRuntime(name string, preferred int) (*ServiceRuntime, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if rt, ok := m.runtimes[name]; ok {
        return rt, nil          // <-- do not rebind
    }
    remembered := m.state.Port(name)
    alloc, err := m.allocator.Allocate(name, remembered, preferred)
    if err != nil {
        return nil, err
    }
    rt := NewServiceRuntime(name, alloc)
    rt.StartAcceptLoop()
    m.runtimes[name] = rt
    return rt, nil
}
```

This idempotence is exactly how container recreation and repeated registrations
avoid changing the frontend.

### 14.5 Backend updates

```go
func (rt *ServiceRuntime) SetBackend(b *Backend) {
    rt.Backend.Store(b)   // atomic
    rt.Touch()
}

func (rt *ServiceRuntime) ClearBackend() {
    rt.Backend.Store((*Backend)(nil))
}
```

Existing connections keep their old backend; only new `Accept()`s read the new
pointer.

### 14.6 File map

```text
internal/runtime/manager.go
internal/runtime/service.go
```

---

## 15. TCP proxy

### 15.1 Intent

Forward opaque bytes from the stable frontend listener to the current backend,
bidirectionally, with correct half-close semantics.

### 15.2 Accept loop

```go
func (rt *ServiceRuntime) StartAcceptLoop() {
    go func() {
        for {
            client, err := rt.Listener.Accept()
            if err != nil {
                if rt.shuttingDown() {
                    return
                }
                rt.logger.Error("accept failed", "error", err)
                continue
            }
            go rt.handle(client)
        }
    }()
}
```

### 15.3 Per-connection routing

```text
1. backend := atomic load; if nil -> close client immediately
2. dial backend with 3s timeout
3. on dial error -> close client, log tcp_dial_failed
4. spawn two io.Copy goroutines
5. close both sides when both directions finish
6. propagate half-close via CloseWrite
```

### 15.4 Implementation

```go
func proxyTCP(client net.Conn, backend Backend, dialTimeout time.Duration) {
    defer client.Close()

    upstream, err := net.DialTimeout("tcp",
        net.JoinHostPort(backend.Host, strconv.Itoa(backend.Port)),
        dialTimeout)
    if err != nil {
        return
    }
    defer upstream.Close()

    done := make(chan struct{}, 2)

    go func() {
        _, _ = io.Copy(upstream, client)
        closeWrite(upstream)
        done <- struct{}{}
    }()
    go func() {
        _, _ = io.Copy(client, upstream)
        closeWrite(client)
        done <- struct{}{}
    }()

    <-done
    <-done
}

func closeWrite(c net.Conn) {
    type closeWriter interface{ CloseWrite() error }
    if cw, ok := c.(closeWriter); ok {
        _ = cw.CloseWrite()
        return
    }
    _ = c.Close()
}
```

### 15.5 Why half-close matters

Protocols such as PostgreSQL, Redis, and SMTP rely on one side finishing its
write while the other side may still send data. If you `Close()` a connection
when one direction's `io.Copy` finishes, the peer sees an abrupt RST and may
report "connection reset by peer" mid-query. `CloseWrite` sends FIN for the write
half only.

### 15.6 Backend changes are not migrations

Existing connections keep talking to the backend they dialed. Do **not** force
them to move. This is correct even for a database: a live transaction should not
be yanked to a restarted server.

### 15.7 Pitfalls

- Always set a dial timeout; otherwise a dead backend can exhaust goroutines.
- Always `defer client.Close()` and `defer upstream.Close()`.
- Do not buffer unboundedly; `io.Copy` streams with a fixed buffer.
- Count and cap concurrent connections per runtime (a simple semaphore) to avoid
  unbounded goroutine growth from a runaway producer.

### 15.8 File map

```text
internal/proxy/tcp.go
internal/runtime/service.go   # calls proxyTCP
```

---

## 16. Leases for native processes

### 16.1 Intent

A native process that crashes must not leave a stale registration forever. A
lease with heartbeats gives the daemon a bounded time to notice death.

### 16.2 Defaults

```text
lease TTL:       15s
heartbeat every: TTL/3 = 5s
sweep interval:  1s
```

### 16.3 Daemon behavior

```go
func (m *LeaseManager) sweep(now time.Time) {
    for _, reg := range m.store.ProcessRegistrations() {
        if reg.LeaseExpires != nil && now.After(*reg.LeaseExpires) {
            m.runtime.ClearBackend(reg.Name)
            m.registry.MarkUnavailable(reg.OwnerKey)
            m.store.Delete(reg.ID)
            m.logger.Info("lease_expired", "service", reg.Name)
        }
    }
}
```

A 1-second sweep is fine for local-dev scale. Do not build a heap/timing wheel
yet.

### 16.4 Token verification

```go
func Verify(provided, stored string) bool {
    return subtle.ConstantTimeCompare([]byte(provided), []byte(stored)) == 1
}
```

Store only what you need. For an MVP, keeping the token in memory is fine;
never log it.

### 16.5 Client heartbeat algorithm

```text
register -> get token
loop every TTL/3:
    heartbeat(token)
      ok      -> continue
      404     -> re-register from scratch
      network -> retry with bounded exponential backoff + jitter
```

Backoff schedule:

```text
100ms, 250ms, 500ms, 1s, 2s, max 5s
+/- 20% jitter
```

### 16.6 Idempotent re-registration

After a daemon restart the registry is empty, so the client's heartbeat returns
`404`. The client re-registers, and if the frontend port is still free the daemon
reuses the remembered assignment from `state.json`, preserving the consumer's
endpoint.

### 16.7 File map

```text
internal/lease/manager.go
```

---

## 17. Go client package

### 17.1 Intent

Give native Go services a one-call way to bind an ephemeral backend port and
publish it under a stable name, keeping the lease alive until `Close`.

### 17.2 Public API

```go
package devmesh

type Kind string

const (
    KindTCP  Kind = "tcp"
    KindHTTP Kind = "http"
)

type RegistrationOptions struct {
    Name          string
    Kind          Kind
    AppProtocol   string
    Backend       string // "127.0.0.1:49382"
    PreferredPort int
}

type Handle interface {
    Endpoint() string
    Close() error
}

func Register(ctx context.Context, opts RegistrationOptions) (Handle, error)
```

### 17.3 ListenTCP convenience

```go
type ListenerHandle struct {
    net.Listener
    Registration Handle
}

func ListenTCP(ctx context.Context, name string, preferredPort int) (*ListenerHandle, error)
```

Order matters:

```go
func ListenTCP(ctx context.Context, name string, preferred int) (*ListenerHandle, error) {
    ln, err := net.Listen("tcp", "127.0.0.1:0") // app owns backend
    if err != nil {
        return nil, err
    }
    handle, err := Register(ctx, RegistrationOptions{
        Name:          name,
        Kind:          KindTCP,
        Backend:       ln.Addr().String(),
        PreferredPort: preferred,
    })
    if err != nil {
        _ = ln.Close()
        return nil, err
    }
    return &ListenerHandle{Listener: ln, Registration: handle}, nil
}
```

### 17.4 Usage

```go
ln, err := devmesh.ListenTCP(ctx, "checkout.api", 8080)
if err != nil {
    return err
}
defer ln.Close()

log.Printf("public endpoint: %s", ln.Registration.Endpoint())
return http.Serve(ln, handler)
```

### 17.5 Handle lifecycle

```text
Register:
  POST /v1/registrations
  store registration_id + lease_token
  start heartbeat goroutine (ctx-aware)
  return handle

Endpoint():
  cached frontend host:port from the register response

Close():
  cancel heartbeat ctx
  DELETE /v1/registrations/{id}
  bounded by a short timeout; never block process shutdown forever
```

### 17.6 Why the app binds :0 itself

Never ask devmesh to choose the backend port and return it. Between the daemon's
choice and the app's bind, any other process could take it, and you have
recreated the original problem. The producer owns its backend: it binds first,
then tells devmesh the actual address.

### 17.7 Re-registration on daemon restart

The heartbeat goroutine treats `404` as "re-register". The new registration may
get a different registration ID but the same frontend (from remembered state).
`Endpoint()` should be updated atomically.

### 17.8 File map

```text
pkg/devmesh/client.go
pkg/devmesh/registration.go
pkg/devmesh/types.go
```

---

## 18. Docker watcher

### 18.1 Intent

Translate Docker container metadata into the same registration model native
processes use. Do not write a Docker plugin; a daemon that watches events and
reads labels is enough for the MVP.

### 18.2 Docker client

```go
cli, err := client.NewClientWithOpts(
    client.FromEnv,
    client.WithAPIVersionNegotiation(),
)
```

`FromEnv` respects Docker Desktop contexts and `DOCKER_HOST`. If Docker is
unavailable: log, mark degraded, keep the rest of devmesh running, reconnect
periodically.

### 18.3 Labels

Required:

```text
io.devmesh.enable=true
io.devmesh.name=checkout.postgres
io.devmesh.container-port=5432
io.devmesh.kind=tcp
```

Optional:

```text
io.devmesh.app-protocol=postgres
io.devmesh.preferred-port=5432
```

Later HTTP phases add `io.devmesh.kind=http`, `io.devmesh.http-host=...`,
`io.devmesh.backend-scheme=...`.

### 18.4 Label parsing

```go
type Labels struct {
    Enabled       bool
    Name          string
    ContainerPort int
    Kind          Kind
    AppProtocol   string
    PreferredPort int
}

func ParseLabels(m map[string]string) (Labels, error)
```

Validate the name and kind immediately. A malformed label should produce a
clear `docker_registration_failed` log, not a panic.

### 18.5 Owner key construction

```text
docker:<compose-project>:<compose-service>:<container-port>
```

Compose sets `com.docker.compose.project` and `com.docker.compose.service`.
Fall back to:

```text
docker:<container-name>:<container-port>
```

Never use the container ID: it changes on recreate.

### 18.6 Discovering the host-published backend port

On `start`:

```text
1. inspect container
2. Labels.Enabled must be true
3. target := fmt.Sprintf("%d/tcp", Labels.ContainerPort)
4. bindings := inspect.NetworkSettings.Ports[target]
5. if len(bindings) == 0 -> actionable error, do not register
6. pick a binding; prefer HostIp == 127.0.0.1 or ::1
7. backend := Backend{Host: hostIp, Port: atoi(binding.HostPort)}
```

Inspect shape:

```json
{
  "5432/tcp": [{ "HostIp": "127.0.0.1", "HostPort": "49173" }]
}
```

### 18.7 Loopback safety

If the Docker binding's `HostIp` is `0.0.0.0` or `::`, the container is exposed
on every host interface. Default behavior:

> Warn loudly and refuse registration unless
> `docker.allow_non_loopback_published_ports` is true.

Never silently fall back to `0.0.0.0`. Exposing a dev database to the LAN is a
security incident, not a convenience.

### 18.8 Startup reconciliation (mandatory)

When `devmeshd` starts:

```text
1. list running containers
2. filter containers with io.devmesh.enable=true
3. inspect each
4. register each valid backend
5. then subscribe to events
```

Without this, containers started before devmesh would never be discovered,
because their `start` events already fired.

### 18.9 Event handling

Events: `start`, `restart`, `die`, `stop`, `destroy`.

```text
start / restart -> inspect + upsert registration
die / stop / destroy -> clear backend if this container currently owns it
```

### 18.10 Reconnect

Docker event streams disconnect. On disconnect:

```text
1. log docker_disconnected
2. backoff reconnect
3. after reconnect, run full reconciliation
4. then resume event handling
```

Reconciliation on reconnect prevents missed events during the gap.

### 18.11 Start-event race

Immediately after `start`, port bindings may not be readable yet. Use bounded
retry:

```text
50ms, 100ms, 200ms, 400ms, 800ms, 1.5s, 2s
```

Stop when the expected published port appears or the container is no longer
running.

### 18.12 File map

```text
internal/dockerwatch/watcher.go
internal/dockerwatch/inspect.go
internal/dockerwatch/labels.go
```

---

## 19. Docker Compose contract

### 19.1 Canonical example

```yaml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: dev
      POSTGRES_PASSWORD: dev
      POSTGRES_DB: app
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U dev -d app"]
      interval: 1s
      timeout: 1s
      retries: 30
    ports:
      - "127.0.0.1::5432"
    labels:
      io.devmesh.enable: "true"
      io.devmesh.name: "checkout.postgres"
      io.devmesh.container-port: "5432"
      io.devmesh.kind: "tcp"
      io.devmesh.app-protocol: "postgres"
      io.devmesh.preferred-port: "5432"
```

### 19.2 What each line means

- `ports: ["127.0.0.1::5432"]` — publish container `5432` on an ephemeral host
  port bound to loopback only. Docker chooses the host port.
- `io.devmesh.container-port` — which container port to look up in
  `NetworkSettings.Ports`. Must match the published target.
- `io.devmesh.preferred-port` — the desirable *frontend* port. Advisory.
- `io.devmesh.name` — the logical service name consumers resolve.

### 19.3 Portability warning

The `127.0.0.1::5432` syntax (explicit host IP, omitted host port) has had
version-dependent behavior across Docker/Compose platforms. Therefore:

1. `devmesh doctor` must actively test this capability on the developer machine;
2. never silently fall back to publishing on `0.0.0.0`;
3. if the syntax does not work, report it clearly and document the tested
   alternative for that environment.

### 19.4 Do not require host port 5432

The whole point is to remove that requirement. The container always listens on
its normal internal port; the host port is ephemeral; the frontend is stable.

---

## 20. HTTP routing phase (after TCP MVP)

### 20.1 Why HTTP can share one listener

HTTP requests carry a Host header, so many logical services can share one
listener and be demultiplexed by hostname. Raw TCP cannot.

```text
api-checkout.dev.example.com ----+
web-checkout.dev.example.com ----+--> devmeshd :443/:8443
auth-checkout.dev.example.com ---+
```

Only implement this after TCP + Docker registration are solid.

### 20.2 Route model

```go
type HTTPRoute struct {
    Hostname      string
    Backend       Backend
    BackendScheme string // http or https
}
```

### 20.3 Reverse proxy

```go
proxy := &httputil.ReverseProxy{
    Director: func(r *http.Request) {
        host := stripPort(r.Host)
        route, ok := routes.Lookup(host)
        // set scheme/host, preserve original host for app visibility
    },
}
```

On each request:

```text
1. normalize r.Host (strip port)
2. lookup route
3. none -> 404
4. route but backend unavailable -> 503
5. proxy to current backend
6. add X-Forwarded-* headers
```

### 20.4 Initial listeners

Do not block the MVP on privileged ports. Start with:

```text
HTTP  127.0.0.1:8088
HTTPS 127.0.0.1:8443
```

Add a privileged-port setup command later.

### 20.5 Wildcard TLS hostname rule

A wildcard `*.dev.example.com` covers exactly **one** label. So
`checkout.api` must not become `checkout.api.dev.example.com` (two labels).
Use an explicit `http-host` label (`api-checkout`) or a deterministic slug
(`checkout-api`). Enforce hostname uniqueness separately from service-name
uniqueness.

---

## 21. TLS phase

### 21.1 MVP scope

Load an existing PEM certificate and key from disk. Do **not** implement ACME in
the same change as the proxy.

### 21.2 Startup validation

```text
- certificate parses
- private key matches the certificate
- wildcard/SAN covers the configured hostname scheme
- key file permissions are not obviously unsafe
- listener binds
```

### 21.3 Config

```json
{
  "http": {
    "base_domain": "dev.example.com",
    "https_addr": "127.0.0.1:443",
    "cert_file": "/path/to/wildcard-cert.pem",
    "key_file": "/path/to/wildcard-key.pem"
  }
}
```

### 21.4 Security

Never log key material. Prefer user-only key file permissions. Do not copy a
shared wildcard private key into generated project directories.

---

## 22. Persistent state

### 22.1 What to persist and what not to

Persist only durable preferences/state:

- stable frontend port assignments.

Do **not** persist active registrations. They are derived from live producers
and would be stale on restart.

### 22.2 State file

```json
{
  "version": 1,
  "tcp_ports": {
    "checkout.postgres": 15432,
    "checkout.redis": 16379
  }
}
```

Location: `os.UserConfigDir()/devmesh/state.json` for a simple implementation.

### 22.3 Atomic write

```text
1. serialize to a temp file in the same directory
2. fsync if practical
3. close
4. rename temp over state.json
```

Never truncate the live file and write in place. A crash mid-write would leave
unparseable state.

### 22.4 Corruption behavior

```text
if parse fails:
    rename state.json -> state.json.corrupt.<timestamp>
    start with empty state
    log a warning
    do not prevent daemon startup
```

### 22.5 Unknown future version

If `version` is greater than the running daemon's known version, refuse to
overwrite it silently. Either back it up first or fail startup with a clear
message. Losing a developer's port map is annoying; corrupting it is worse.

### 22.6 File map

```text
internal/state/store.go
internal/state/model.go
```

---

## 23. CLI and help system (Glazed framework)

The `devmesh` CLI and the `devmeshd` daemon root **use the Glazed framework**
(`github.com/go-go-golems/glazed`) for command definition, flag parsing,
structured output, and the help system. Do not hand-roll a flag parser or rely
on default Cobra help. This section is the authoritative CLI contract; if a
Glazed API here disagrees with the installed module version, trust the module
and re-read `glaze help commands-reference` and `glaze help 32-structured-output`.
The conventions below follow the pinned `glazed-command-authoring` and
`glazed-help-page-authoring` skills; when those skills and this document
disagree, the skills win because they track the installed framework version.

### 23.0 Design rule

The CLI talks to the same local API the Go client uses. There is no separate
back channel. Every command's domain behavior is a call over the Unix-socket
HTTP API from §10.

### 23.1 Why Glazed instead of plain Cobra

- **One structured-output contract.** Every `GlazeCommand` automatically gets
  `--format table|json|jsonl|csv|tsv|yaml`, `--output-fields`, and
  `--max-output-rows`. We do not invent a `--json` flag per command, and we do
  not add jq/template/sort flags that were removed from the framework.
- **Settings decode.** Flags are declared as Glazed `fields` and decoded into a
  typed settings struct, so command code never reads Cobra flags directly.
- **Reusable sections.** The daemon socket/timeout settings, and later the
  shared logging settings, are defined once as sections and mounted on command
  groups.
- **A real help system.** Embedded markdown pages with frontmatter, slug
  lookup (`devmesh help resolve-workflow`), section types, topics/commands/flags
  tags, and origin tracking — instead of whatever Cobra prints by default.
- **Analyzer support.** `glazed-lint` catches malformed commands and removed
  APIs at the pinned module version.

### 23.2 Dependencies

```text
github.com/go-go-golems/glazed         # command framework, output, help
  pkg/cli                              # Cobra builders, structured-output injection
  pkg/cmds, pkg/cmds/fields            # descriptions, flags, arguments
  pkg/cmds/schema, pkg/cmds/values     # sections and decoded values
  pkg/middlewares, pkg/types           # row processing
  pkg/help, pkg/help/cmds              # help system + help command
  pkg/logging                          # slog integration
```

Pin one Glazed version in `go.mod` and build `glazed-lint` from that exact
version (see §23.14).

### 23.3 Command tree

Mirror the CLI tree in folders, per the Glazed repository convention:

```text
cmd/devmesh/
  main.go                 # Glazed root + logging + help wiring, Execute()
  cmds/
    root.go               # AddCommandsToRootCommand for every group
    services/
      root.go             # "services" parent group (aliases: ls, svc)
      list.go             # devmesh services list   (GlazeCommand)
      resolve.go          # devmesh services resolve <name> (GlazeCommand)
      inspect.go          # devmesh services inspect <name> (GlazeCommand)
    register/
      root.go
      register.go         # devmesh register ... (long-running GlazeCommand)
    doctor/
      root.go
      doctor.go           # devmesh doctor (GlazeCommand, one row per check)
cmd/devmeshd/
  main.go                 # Glazed root + logging + help wiring
  cmds/
    serve.go              # devmeshd serve (daemon)
```

User-facing command names:

```text
devmesh services list [--format json]
devmesh services resolve <name> [--format json]
devmesh services inspect <name>
devmesh register --name test.echo --kind tcp --backend 127.0.0.1:49000 --preferred-port 9000
devmesh doctor
devmeshd serve
devmesh help <slug>
```

Add short aliases (`devmesh ls`, `devmesh resolve`) as Cobra aliases mounted by
the group registration function, not as duplicate command implementations.

### 23.4 Canonical imports

```go
import (
    "context"

    "github.com/go-go-golems/glazed/pkg/cli"
    "github.com/go-go-golems/glazed/pkg/cmds"
    "github.com/go-go-golems/glazed/pkg/cmds/fields"
    "github.com/go-go-golems/glazed/pkg/cmds/schema"
    "github.com/go-go-golems/glazed/pkg/cmds/values"
    "github.com/go-go-golems/glazed/pkg/middlewares"
    "github.com/go-go-golems/glazed/pkg/types"
)
```

Common invalid imports to avoid:

```text
glazed/pkg/cmds/parameters/fields   -> use glazed/pkg/cmds/fields
glazed/pkg/cmds/middlewares         -> use glazed/pkg/middlewares
glazed/pkg/values                   -> use glazed/pkg/cmds/values
glazed/pkg/settings/schema          -> use glazed/pkg/cmds/schema
```

### 23.5 A complete GlazeCommand: `devmesh services list`

```go
package services

type ListCommand struct {
    *cmds.CommandDescription
}

type ListSettings struct {
    NameFilter string `glazed:"name"`
    Status     string `glazed:"status"`
}

func NewListCommand() *ListCommand {
    return &ListCommand{CommandDescription: cmds.NewCommandDescription(
        "list",
        cmds.WithShort("List services known to devmeshd"),
        cmds.WithFlags(
            fields.New("name", fields.TypeString,
                fields.WithHelp("Only show services whose name contains this substring")),
            fields.New("status", fields.TypeChoice,
                fields.WithChoices("", "ready", "unavailable"),
                fields.WithDefault(""),
                fields.WithHelp("Only show services with this status")),
        ),
    )}
}

func (c *ListCommand) RunIntoGlazeProcessor(
    ctx context.Context,
    parsed *values.Values,
    gp middlewares.Processor,
) error {
    settings := &ListSettings{}
    if err := parsed.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
        return err
    }

    // Resolve daemon connection settings from the shared section.
    d := &DaemonSettings{}
    if err := parsed.DecodeSectionInto("daemon", d); err != nil {
        return err
    }

    client := transport.NewClient(d.Socket)
    services, err := client.List(ctx, ListParams{
        NameFilter: settings.NameFilter,
        Status:     settings.Status,
    })
    if err != nil {
        return err
    }

    for _, svc := range services {
        endpoint := ""
        if svc.Frontend.Port != 0 {
            endpoint = net.JoinHostPort(svc.Frontend.Host, strconv.Itoa(svc.Frontend.Port))
        }
        if err := gp.AddRow(ctx, types.NewRow(
            types.MRP("name", svc.Name),
            types.MRP("kind", svc.Kind),
            types.MRP("app_protocol", svc.AppProtocol),
            types.MRP("status", string(svc.Status)),
            types.MRP("endpoint", endpoint),
        )); err != nil {
            return err
        }
    }
    return nil
}
```

Notes that mirror the Glazed contract:

- Do **not** add a structured-output section manually; the Cobra builder injects
  it for every `GlazeCommand`.
- `types.NewRow` returns a value; pass it directly to `AddRow`.
- Do not close the processor: the Cobra `RunE` path owns it and closes it once.
- No `cobra.CheckErr`/`os.Exit` in command code; return errors to `Execute()`.

### 23.6 `resolve` and positional arguments

`resolve` takes the service name as a positional argument:

```go
func NewResolveCommand() *ResolveCommand {
    return &ResolveCommand{CommandDescription: cmds.NewCommandDescription(
        "resolve",
        cmds.WithShort("Resolve a service name to its stable frontend endpoint"),
        cmds.WithArguments(
            fields.New("name", fields.TypeString,
                fields.WithIsArgument(true),
                fields.WithHelp("Service name, e.g. checkout.postgres")),
        ),
    )}
}

func (c *ResolveCommand) RunIntoGlazeProcessor(
    ctx context.Context, parsed *values.Values, gp middlewares.Processor,
) error {
    s := &ResolveSettings{}
    if err := parsed.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
        return err
    }
    svc, err := client.Resolve(ctx, s.Name)
    if err != nil {
        return err // typed api.Error; root maps exit code
    }
    return gp.AddRow(ctx, types.NewRow(
        types.MRP("name", svc.Name),
        types.MRP("kind", svc.Kind),
        types.MRP("status", string(svc.Status)),
        types.MRP("endpoint", net.JoinHostPort(svc.Frontend.Host, strconv.Itoa(svc.Frontend.Port))),
    ))
}
```

### 23.7 Structured output is the scripting contract

The three universal flags replace every ad-hoc output flag:

```text
--format table|json|jsonl|csv|tsv|yaml     default table
--output-fields field1,field2,...          project emitted rows
--max-output-rows N                        cap serialized rows; 0 = unlimited
```

Consequences for the CLI:

- **Human default:** `devmesh services list` prints the table from §23.8.
- **Scripts:** `devmesh services resolve checkout.postgres --format json`
  returns `{"name":...,"endpoint":"127.0.0.1:15432", ...}`.
- **Bare address:** `devmesh services resolve checkout.postgres \\
  --output-fields endpoint --format jsonl` emits a one-key JSON line; there is
  no template/jq flag to add because those were removed from the framework.
- `--max-output-rows` caps serialization only; it must not silently change the
  request sent to the daemon. A real server-side page size is a domain flag.

If we later want `devmesh services resolve NAME` to print exactly
`127.0.0.1:15432` with no header for shell substitution, implement that as a
**domain behavior**, not a framework flag: either a separate raw Cobra command
using `cli.AddStructuredOutputFlagsToCobraCommand` plus a `--raw` domain flag,
or a documented recommendation to use `--format jsonl`. Do not reintroduce
removed universal flags.

Keep DTO field names stable once published, because `--output-fields` names are
the public projection vocabulary.

### 23.8 List output (default `--format table`)

```text
NAME                 KIND  APP       STATUS       ENDPOINT
checkout.postgres    tcp   postgres  ready        127.0.0.1:15432
checkout.api         tcp   http      ready        127.0.0.1:18080
billing.redis        tcp   redis     unavailable  127.0.0.1:16379
```

Field order is exactly the order of `AddRow` `MRP` calls. Tabular formats
preserve requested `--output-fields` order, including sparse rows (a row missing
an emitted field simply has an empty cell).

### 23.9 Shared daemon section

Every command that talks to the daemon mounts one reusable section instead of
duplicating `--socket`/`--timeout` flags:

```go
func NewDaemonSection() (schema.Section, error) {
    return schema.NewSection(
        "daemon",
        "Daemon Connection",
        schema.WithFields(
            fields.New("socket", fields.TypeString,
                fields.WithHelp("Path to the devmeshd unix socket")),
            fields.New("timeout", fields.TypeString,
                fields.WithDefault("5s"),
                fields.WithHelp("Per-request HTTP timeout")),
        ),
    )
}
```

Mount the section on the command description with `cmds.WithSections(...)` and
decode it with `parsed.DecodeSectionInto("daemon", &DaemonSettings{})`. Never
mount the same section on both a parent and a child; a duplicated section mounts
the same flags twice and fails registration.

### 23.10 `register` keeps the lease

`devmesh register ...` must maintain the lease until interrupted, so it is a
long-running command. Implement it as a `GlazeCommand` whose run loop:

```text
1. parse --name/--kind/--backend/--preferred-port/--app-protocol
2. POST /v1/registrations
3. emit one row: {name, endpoint, registration_id, expires_at}
4. block on ctx.Done(), heartbeating every TTL/3 (re-register on 404)
5. on interrupt: DELETE with a short timeout, best effort
6. return nil (or a typed error if registration failed)
```

Because it never returns until interrupted, do not buffer output in a way that
withholds the first row; use a streaming format (`jsonl`) or emit early.

### 23.11 `devmesh daemon` shortcut and `devmeshd serve`

The daemon binary's root is also Glazed-wired so it gets the logging section and
help. `devmeshd serve` (and the `devmesh daemon` alias, which execs or calls the
same command) is a command with its own settings struct for the config path and
override flags. The daemon does not emit tabular rows; it runs the server and
returns on signal-canceled context. It is acceptable for this command to be a
plain Cobra command that shares the logging/help root, but if it uses
`GlazeCommand`, do not add a structured-output section by hand.

### 23.12 `doctor` as a structured command

`devmesh doctor` emits one row per check so the same command serves humans and
CI:

```go
for _, check := range checks {
    res := check.Run(ctx)
    if err := gp.AddRow(ctx, types.NewRow(
        types.MRP("check", check.Name()),
        types.MRP("status", string(res.Status)), // ok | fail | skip
        types.MRP("detail", res.Detail),
    )); err != nil {
        return err
    }
}
```

Checks:

- daemon reachable;
- state/config directories writable;
- Docker reachable if enabled;
- Docker event stream accessible;
- loopback-only ephemeral Docker port publishing works;
- configured TCP frontend range has at least one bindable port;
- HTTP/TLS certificate configuration valid if enabled;
- privileged HTTP ports bindable if configured.

Human output:

```text
[ok] Docker Engine reachable
[ok] Docker API version negotiation
[ok] container events available
[ok] random published host port allocated
[ok] published port bound to 127.0.0.1 only
```

or:

```text
[fail] random port was published on 0.0.0.0
       devmesh will not auto-register this container because it would expose
       the database on your LAN.
```

Machine output: `devmesh doctor --format json`. Exit non-zero only if a `fail`
check ran; `skip` (for example Docker disabled) must not fail the command. Map
that decision in the root's exit-code logic, not inside the command.

### 23.13 Root initialization: logging + help (mandatory)

This is the canonical Glazed root, adapted from the framework's own `glaze`
entrypoint. Both binaries must do this:

```go
func main() {
    rootCmd := &cobra.Command{
        Use:   "devmesh",
        Short: "Stable local dev endpoints for ephemeral services",
        PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
            return logging.InitLoggerFromCobra(cmd)
        },
    }

    if err := logging.AddLoggingSectionToRootCommand(rootCmd, "devmesh"); err != nil {
        panic(err)
    }

    if err := cmd.AddCommandsToRoot(rootCmd); err != nil {
        panic(err) // registration/schema collision surfaced before mounting
    }

    helpSystem := help.NewHelpSystem()
    if err := doc.AddDocToHelpSystem(helpSystem); err != nil {
        panic(err)
    }
    help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

    if err := rootCmd.Execute(); err != nil {
        os.Exit(mapExitCode(err))
    }
}
```

Rules:

- `logging.AddLoggingSectionToRootCommand` + `PersistentPreRunE` at the root,
  so every command has `--log-level`, `--log-format`, etc.
- `help_cmd.SetupCobraRootCommand` is called **exactly once** on the root.
- Child packages register command groups through one function; they must not
  create independent help systems.
- Prefer `cli.AddCommandsToRootCommand` over adding commands one at a time: it
  builds all commands and aliases first, so a schema/flag collision does not
  leave a partially mutated tree.
- If a custom parser `MiddlewaresFunc` replaces the default source chain, re-add
  every required source (config file, env, args) explicitly. Setting `AppName`
  alone does not restore environment loading.

### 23.14 Embedding help pages

Help pages live as markdown under `pkg/doc/` and are embedded and loaded into the
help system:

```go
package doc

import (
    "embed"

    "github.com/go-go-golems/glazed/pkg/help"
)

//go:embed *.md
var docFS embed.FS

func AddDocToHelpSystem(helpSystem *help.HelpSystem) error {
    return helpSystem.LoadSectionsFromFS(docFS, ".")
}
```

Each page uses exact Glazed frontmatter:

```yaml
---
Title: "Resolving services from scripts and applications"
Slug: "devmesh-resolve-workflow"
Short: "How consumers turn a logical service name into a stable endpoint."
Topics:
- devmesh
- resolve
Commands:
- devmesh services resolve
Flags:
- format
IsTopLevel: false
IsTemplate: false
ShowPerDefault: true
SectionType: Example
---
```

Rules and conventions:

- `SectionType` is one of `GeneralTopic`, `Example`, `Application`, `Tutorial`;
  choose the type that matches the page's intent, not the page's length.
- Every `Slug` must be globally unique; verify with `devmesh help <slug>`.
- `Short` is a one-sentence summary; do not paste the intro paragraph into it.
- Do **not** add a top-level `#` heading in page content; Glazed renders the
  title from frontmatter.
- Use present tense, active voice, and explain motivation/failure modes, not
  just mechanics.
- End each page with a troubleshooting table (`Problem | Cause | Solution`) and
  a `See Also` section cross-referencing related slugs.
- Confirm discoverability with `devmesh help devmesh-overview` and
  `devmesh help --all` after embedding.

### 23.15 Help page catalog for devmesh

| Slug | SectionType | Covers |
| --- | --- | --- |
| `devmesh-overview` | GeneralTopic | What devmesh is, frontend vs backend, the five invariants |
| `devmesh-getting-started` | Tutorial | Install, run `devmeshd serve`, first `list`/`resolve` |
| `devmesh-resolve-workflow` | Example | Script and application consumption patterns |
| `devmesh-native-go` | Example | `ListenTCP` end-to-end for a Go service |
| `devmesh-docker-compose` | Application | Labels, ephemeral publish, PostgreSQL example |
| `devmesh-doctor` | Example | What each doctor check means and how to fix failures |
| `devmesh-troubleshooting` | GeneralTopic | Port conflicts, Docker 0.0.0.0, stale socket, daemon restart |
| `devmesh-security-model` | GeneralTopic | Loopback-only bindings, unix socket, lease tokens |

Keep this catalog in sync with the actual `.md` files; a missing or duplicated
slug breaks `devmesh help <slug>`.

### 23.16 glazed-lint wiring (required for a Glazed repo)

Build the analyzer from the exact Glazed module version so command APIs and the
linter cannot drift:

```make
GLAZED_LINT_BIN ?= /tmp/glazed-lint
GLAZED_LINT_PKG ?= github.com/go-go-golems/glazed/cmd/tools/glazed-lint
GLAZED_VERSION ?= $(shell go list -m -f '{{.Version}}' github.com/go-go-golems/glazed 2>/dev/null)
GLAZED_LINT_DIRS ?= ./cmd/... ./pkg/... ./internal/...
GLAZED_LINT_FLAGS ?=

.PHONY: glazed-lint-build glazed-lint

glazed-lint-build:
	@echo "Building glazed-lint from the selected Glazed module..."
	@if [ -n "$(GLAZED_VERSION)" ] && [ "$(GLAZED_VERSION)" != "(devel)" ]; then \
		echo "Installing $(GLAZED_LINT_PKG)@$(GLAZED_VERSION)"; \
		GOBIN=$(dir $(GLAZED_LINT_BIN)) go install $(GLAZED_LINT_PKG)@$(GLAZED_VERSION); \
	else \
		GOBIN=$(dir $(GLAZED_LINT_BIN)) go install $(GLAZED_LINT_PKG); \
	fi

glazed-lint: glazed-lint-build
	GOWORK=off go vet -vettool=$(GLAZED_LINT_BIN) $(GLAZED_LINT_FLAGS) $(GLAZED_LINT_DIRS)
```

Add `glazed-lint-build` and the analyzer invocation to the normal `lint` target
so local hooks and CI cannot silently skip it, and keep the binary outside the
repository (never commit it).

### 23.17 Testing requirements for Glazed commands

At minimum, test:

1. Settings decode into the correct section with correct defaults.
2. A command emits expected rows and propagates processor errors.
3. `cli.BuildCobraCommandFromCommand` adds exactly `format`, `output-fields`,
   and `max-output-rows` for a `GlazeCommand`.
4. Domain fields do not collide with framework fields or with the mounted
   section. (This is the failure `AddCommandsToRootCommand` catches early.)
5. Typed errors reach `root.Execute()` when exit-code mapping matters.
6. Sparse rows preserve requested tabular projection order:

   ```text
   requested: [name, missing, endpoint]
   row 1:     {name: checkout.postgres}
   row 2:     {endpoint: 127.0.0.1:15432}
   columns:   [name, endpoint]
   ```

7. Streaming/long-running commands (`register`) stop under context cancellation.

Validate with:

```bash
gofmt -w <changed-go-files>
GOWORK=off go test ./... -count=1
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off make glazed-lint
git diff --check
```

Before committing, run `devmesh services list --help` and confirm the
structured-output group contains only the three universal flags and that the
`daemon` section appears exactly once.

---

## 24. Logging

### 24.1 Library and wiring

Use `log/slog` behind Glazed's logging package. The root command wires it once
(see §23.13) so every subcommand shares the same flags and configuration:

```go
rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
    return logging.InitLoggerFromCobra(cmd)
}
if err := logging.AddLoggingSectionToRootCommand(rootCmd, "devmesh"); err != nil {
    return err
}
```

This gives `--log-level` and `--log-format` on every command without each
command declaring them. Human-readable handler is the foreground default; the
JSON handler is the choice for CI/service use. Domain packages (`internal/*`,
`pkg/devmesh`) take a `*slog.Logger` and never configure logging themselves.

### 24.2 Structured fields

```text
service
registration_id
owner_key
source
container_id
frontend
backend
event
error
```

### 24.3 Event names

Use a small, stable vocabulary:

```text
daemon_started
service_registered
service_backend_updated
service_backend_removed
frontend_allocated
frontend_reused
tcp_dial_failed
docker_connected
docker_disconnected
docker_container_discovered
docker_registration_failed
lease_expired
state_loaded
state_saved
```

### 24.4 Never log

- lease tokens;
- TLS private keys;
- credentials of any kind.

If you must reference a token for debugging, log a hash prefix, never the token.

---

## 25. Security requirements

### 25.1 Administrative API

- Unix socket only by default.
- User-only filesystem permissions.
- No unauthenticated TCP admin endpoint.

### 25.2 Frontends

- Bind stable proxy listeners to loopback by default.
- Never default to `0.0.0.0`.

### 25.3 Backends

- Process/manual registrations may target loopback only in the MVP.
- Docker-published backends should be loopback-only by default.

### 25.4 Lease authorization

- Generate lease tokens with `crypto/rand`.
- Compare tokens with constant-time comparison.
- Return the token only once, at creation.

### 25.5 Docker socket

Access to the Docker daemon is highly privileged. Keep Docker access isolated in
the Docker adapter and only perform read/inspect/event operations for the MVP.

### 25.6 TLS keys

Never include key material in logs. Prefer user-only key file permissions. Do not
copy shared wildcard private keys into project directories.

---

## 26. Failure semantics

Define these now so behavior is predictable and testable.

### 26.1 Daemon unavailable

- `devmesh resolve`: fail clearly.
- Go client: retry registration/heartbeats with backoff.
- The application listener keeps serving its raw backend port; only the devmesh
  stable endpoint is unavailable.

### 26.2 Backend unavailable

TCP:

```text
frontend remains bound
accept the connection, then close quickly
```

HTTP (later):

```text
503 Service Unavailable
```

### 26.3 Docker unavailable

- daemon still serves manual/native registrations;
- Docker integration reports degraded;
- reconnect in the background;
- after reconnect, full reconciliation.

### 26.4 Frontend port conflict after restart

If the remembered frontend port is now occupied by another process:

```text
1. log a warning
2. allocate another port
3. update state
4. expose the new endpoint through resolve
```

Do not kill or interfere with the process that owns the old port.

### 26.5 Duplicate service name

An unrelated owner trying to claim an existing name gets a conflict. Never
silently replace.

---

## 27. Concurrency rules

These rules prevent subtle deadlocks and races. Violating any of them is a bug.

1. The registry mutex protects in-memory metadata only.
2. Never dial a backend while holding registry/runtime locks.
3. Never call the Docker API while holding registry/runtime locks.
4. Never write state files while holding a lock that blocks connection handling.
5. Backend pointers are replaced atomically.
6. Runtime creation/removal is serialized per service name.
7. Listener ownership belongs to exactly one `ServiceRuntime`.
8. Shutdown closes listeners before waiting for proxy goroutines indefinitely.

### 27.1 Lock ordering

If you must hold more than one lock, define a strict order. A simple, safe
approach for the MVP: never hold two internal locks at once. Perform I/O
outside locks, then re-acquire to commit.

### 27.2 Race detector is a gate

```bash
go test -race ./...
```

Treat race-detector failures as blockers. Do not merge with a race.

---

## 28. Graceful shutdown

### 28.1 Sequence

```text
on SIGINT / SIGTERM:
  1. stop accepting new API requests
  2. stop the Docker event watcher
  3. stop the lease sweeper
  4. close frontend listeners
  5. wait briefly for active proxy goroutines
  6. flush state
  7. remove the Unix socket
  8. exit
```

### 28.2 Root context

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

Pass `ctx` to every long-running component; each component returns when `ctx`
is done.

### 28.3 Timeout

Do not wait forever on active TCP clients. Use a configurable shutdown timeout
(default 5s), then force-close remaining connections.

---

## 29. Testing strategy

### 29.1 Unit tests

**Name validation**

```text
checkout.postgres     valid
checkout-api          valid
Checkout.Api          invalid
checkout_postgres     invalid
.checkout             invalid
checkout.             invalid
```

**Port allocator** (use real loopback listeners, not mocks)

```text
preferred free         -> returns it
preferred occupied     -> fallback
remembered free        -> reuse
remembered occupied    -> fallback
parallel allocations   -> never the same port
range exhausted        -> typed error
```

**Registry ownership**

```text
first owner registers
same owner replaces backend
different owner -> conflict
remove owner -> backend unavailable
resolve never exposes backend details
```

**Leases**

```text
alive while heartbeats arrive
expired lease removed
wrong token cannot renew/delete
missing registration -> client re-registers
```

**State store**

```text
round trip
atomic replacement
corrupt recovery
unknown future version handled clearly
```

### 29.2 TCP integration tests

Echo server on `127.0.0.1:0`:

```text
client -> devmesh frontend -> echo backend; verify both directions
```

Backend replacement:

```text
frontend -> backend A
update registration
frontend -> backend B, same frontend
established connection to A is not moved
```

### 29.3 Docker integration tests

Skip cleanly when Docker is absent. Test A: labeled TCP discovery. Test B:
container recreation preserves frontend. Test C: daemon starts after container
(startup reconciliation). Test D: unsafe `0.0.0.0` publication refused. Test E:
event-stream reconnect via an interface around the Docker client.

### 29.4 PostgreSQL end-to-end

Use `pgx` so CI does not need host `psql`:

```text
1. docker compose up -d
2. resolve checkout.postgres -> stable frontend
3. connect with pgx through the frontend
4. docker compose up -d --force-recreate db
5. resolve again -> same frontend
6. connect with pgx again -> succeeds to new container
```

### 29.5 Glazed command tests

In addition to the domain tests above, test the command layer (§23.17): settings
decode with the right defaults; `list`/`resolve` emit the expected rows and
propagate processor errors; `BuildCobraCommandFromCommand` injects exactly
`format`, `output-fields`, and `max-output-rows`; domain flags do not collide
with framework or section flags; typed errors reach `root.Execute()`; and the
long-running `register` command stops under context cancellation.

### 29.6 Commands before every PR

```bash
gofmt -w .
GOWORK=off go vet ./...
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go build ./...
GOWORK=off make glazed-lint
```

Use `GOWORK=off` when the module may be nested under a mismatched Go workspace.
Treat `glazed-lint` failures as build failures; do not merge with a linter or
race-detector failure.

---

## 30. Milestones and PR plan

Build this as small, reviewable PRs. Do not attempt everything in one branch.

### PR 1 — skeleton, transport, API, CLI (Glazed), help system

Implement: repo layout; `devmeshd` startup/shutdown; Unix socket transport;
`/v1/health`; **Glazed root wiring for both binaries** (logging section +
`help_cmd.SetupCobraRootCommand`); typed API errors; `slog`; config loading; the
Glazed command tree (`services list`, `services resolve`, `register`, `doctor`
stubs) and the embedded help pages from §23.15.

Acceptance:

```text
devmeshd serve starts
devmesh services list / resolve talk over the Unix socket
second daemon refuses to start on the same socket
stale socket recovered safely
devmesh services list --help shows only format/output-fields/max-output-rows
  in the structured-output group, and the daemon section exactly once
devmesh help devmesh-overview renders the embedded page
make glazed-lint passes
```

### PR 2 — registry, port allocator, TCP proxy

Implement: service/registration types; name validation; ownership rules;
bind-first allocator; runtime manager; opaque TCP proxy; persistent port state;
manual registration API.

Acceptance:

```text
manual backend can be registered
resolve returns a stable frontend
traffic traverses the proxy
backend can be replaced without changing the frontend
```

### PR 3 — leases and Go package

Implement: lease tokens; heartbeat API; expiry; Go API client; `Register`;
`ListenTCP`; automatic re-registration.

Acceptance:

```text
native Go example binds :0
registers automatically
stable frontend works
kill -9 native process -> registration expires
restart daemon -> client re-registers
```

### PR 4 — Docker watcher

Implement: Docker client abstraction; startup reconciliation; label parser;
inspect -> backend discovery; event stream; reconnect/reconcile; loopback safety;
Docker fields in inspect.

Acceptance:

```text
running labeled container discovered
new labeled container discovered
stopped container becomes unavailable
recreated container changes backend, not frontend
```

### PR 5 — Docker/Compose diagnostics and PostgreSQL example

Implement: `devmesh doctor`; disposable Docker publication capability test;
Compose PostgreSQL example; pgx end-to-end test; clear error messages.

Acceptance:

```text
fresh developer runs the example from the README
pgx connection succeeds through devmesh
```

### PR 6 — shared HTTP proxy

Implement: HTTP route registry; reverse proxy; hostnames/aliases; high-port
HTTP/HTTPS listeners; unavailable -> 503; Docker HTTP labels.

### PR 7 — wildcard TLS

Implement: base domain config; cert/key loading; hostname generation/validation;
TLS listener; DNS docs. No ACME yet.

---

## 31. Glossary

| Term | Meaning |
| --- | --- |
| Backend | The current `host:port` a producer is listening on |
| Frontend | The stable `host:port` (or URL) consumers connect to |
| Producer | Anything that publishes a backend (app, container, operator) |
| Consumer | Anything that connects to a frontend |
| Registration | Producer's time-bounded assertion of a backend |
| Lease | TTL + heartbeat mechanism that expires process registrations |
| Owner key | Stable logical owner identity for conflict rules |
| Service runtime | Daemon object owning the frontend listener |
| Allocation | `{port, listener}` returned by the bind-first allocator |
| Idle grace | Period a frontend stays bound with no backend |
| Reconciliation | Enumerating existing Docker containers at startup/reconnect |
| Ephemeral port | Kernel-chosen high port, unstable across restarts |

---

## 32. Appendix A: end-to-end lifecycle walkthroughs

### 32.1 Native Go app from start to stopped

```text
1.  App starts, binds 127.0.0.1:0 -> 49382
2.  ListenTCP registers "checkout.api", backend 127.0.0.1:49382
3.  Daemon EnsureTCPRuntime allocates frontend 127.0.0.1:18080
4.  Daemon persists {checkout.api: 18080}
5.  Response returns frontend 18080; Handle.Endpoint() == "127.0.0.1:18080"
6.  Consumer connects to 18080; proxy dials 49382
7.  App runs; heartbeats every 5s renew the 15s lease
8.  App crashes (no DELETE)
9.  Sweeper sees expiry, clears backend; service -> unavailable; port 18080 retained
10. App restarts, binds a new ephemeral port, re-registers
11. Daemon reuses remembered frontend 18080
12. Consumers reconnect to the same 18080
```

### 32.2 Docker recreate

```text
1.  container A publishes 49173; registration owner docker:checkout:db:5432
2.  frontend 15432 -> backend 49173
3.  docker compose up -d --force-recreate db
4.  container B publishes 49901; same owner key
5.  watcher `start` event -> inspect -> upsert
6.  ApplyRegistration: runtime already exists, backend swapped to 49901
7.  frontend 15432 unchanged
8.  New connections reach container B
```

### 32.3 Daemon restart

```text
1.  daemon stops: closes listeners, flushes state, removes socket
2.  daemon starts: loads state {checkout.postgres: 15432}
3.  reconcile running containers -> re-register checkout.postgres
4.  allocator tries remembered 15432 -> binds it
5.  native clients heartbeat -> 404 -> re-register
6.  resolve returns the same 15432
```

---

## 33. Appendix B: planned Go types (consolidated)

```go
package devmesh

type Kind string

const (
    KindTCP  Kind = "tcp"
    KindHTTP Kind = "http"
)

type Source string

const (
    SourceProcess Source = "process"
    SourceDocker  Source = "docker"
    SourceManual  Source = "manual"
)

type Status string

const (
    StatusReady       Status = "ready"
    StatusUnavailable Status = "unavailable"
)

type Backend struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

type Frontend struct {
    Host string `json:"host"`
    Port int    `json:"port"`
    URL  string `json:"url,omitempty"`
}

type Registration struct {
    ID            string
    Name          string
    Kind          Kind
    AppProtocol   string
    Backend       Backend
    Source        Source
    OwnerKey      string
    PreferredPort int
    LeaseExpires  *time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

---

## 34. Appendix C: file reference index

| Concern | Files |
| --- | --- |
| Daemon entry (Glazed root) | `cmd/devmeshd/main.go`, `cmd/devmeshd/cmds/serve.go` |
| CLI entry (Glazed root) | `cmd/devmesh/main.go`, `cmd/devmesh/cmds/root.go` |
| CLI command groups | `cmd/devmesh/cmds/{services,register,doctor}/` |
| Embedded help pages | `pkg/doc/doc.go`, `pkg/doc/*.md` |
| Public client API | `pkg/devmesh/{client,registration,types}.go` |
| HTTP API | `internal/api/{server,handlers,dto}.go` |
| Registry | `internal/registry/{registry,model,errors}.go` |
| Runtime + allocator | `internal/runtime/{manager,service}.go` |
| TCP/HTTP proxy | `internal/proxy/{tcp,http}.go` |
| Leases | `internal/lease/manager.go` |
| Docker watcher | `internal/dockerwatch/{watcher,inspect,labels}.go` |
| State store | `internal/state/{store,model}.go` |
| Transport | `internal/transport/{unix,client}.go` |
| Config | `internal/config/config.go` |
| glazed-lint wiring | `Makefile` (`glazed-lint`, `lint` targets) |
| Integration tests | `integration/{tcp_proxy,lease,docker}_test.go` |
| Examples | `examples/native-go/`, `examples/compose-postgres/compose.yaml` |

---

## 35. Appendix D: traceability to the source specification

| Source guide | This guide |
| --- | --- |
| §1 What we are building | §1, §3 |
| §2 Definition of done | §29, §30 |
| §3 Non-goals | §1.3, §20, §21 |
| §4 Architecture | §4 |
| §5 Repository layout | §5 |
| §6 Core data model | §6 |
| §7 Identity vs registration | §7 |
| §8 Daemon transport | §9 |
| §9 Local API | §10 |
| §10 Error model | §11 |
| §11 Registry | §12 |
| §12 Port allocation | §13 |
| §13 TCP proxy | §15 |
| §14 Runtime manager | §14 |
| §15 Leases | §16 |
| §16 Go client | §17 |
| §17 Docker integration | §18 |
| §18 Compose contract | §19 |
| §19 HTTP routing | §20 |
| §20 DNS + TLS | §20.5, §21 |
| §21 Persistent state | §22 |
| §22 Configuration | §8 |
| §23 CLI | §23 (Glazed command tree, help system, glazed-lint) |
| §24 Logging | §24 (Glazed logging section wiring) |
| §25 Security | §25 |
| §26 Failure semantics | §26 |
| §27 Concurrency | §27 |
| §28 Shutdown | §28 |
| §29–33 Tests | §29 |
| §34 Milestones | §30 |
| §36 Immutable decisions | §1.5, §13, §17.6 |
| §37 Future work | §1.3, §20, §21 |

---

## 36. Appendix E: the five-invariant review checklist

Before you request review on any PR, verify each invariant is intact:

- [ ] Producer-owned backends: no code hands out a backend port.
- [ ] Devmesh-owned frontends: allocation always returns an open listener.
- [ ] Stable identity: owner keys survive recreation; remembered ports persist.
- [ ] Docker is an adapter: registry builds and tests without Docker.
- [ ] Generic TCP is host+port: no hostname demultiplexing of raw TCP.
- [ ] Glazed is confined to the command layer: `internal/*` and `pkg/devmesh`
      do not import the framework, and no `GlazeCommand` adds the
      structured-output section by hand.

And the Glazed CLI checklist:

- [ ] Both roots call `logging.AddLoggingSectionToRootCommand` and set
      `PersistentPreRunE` to `logging.InitLoggerFromCobra`.
- [ ] `help_cmd.SetupCobraRootCommand` is called exactly once per root.
- [ ] Help pages live in `pkg/doc/*.md`, are embedded, and every `Slug` is
      unique and resolves via `devmesh help <slug>`.
- [ ] `--help` shows exactly `format`, `output-fields`, `max-output-rows` in the
      structured-output group.
- [ ] Custom sections (`daemon`) are mounted once and decode with the correct slug.
- [ ] `GOWORK=off make glazed-lint` passes at the pinned Glazed version.

And the operational checklist from source guide §41:

- [ ] Daemon runs as an unprivileged user.
- [ ] Admin API reachable only via the Unix socket.
- [ ] `list`, `resolve`, `inspect` work.
- [ ] Native app can register a backend bound to `:0`.
- [ ] Native lease expires after a crash.
- [ ] Native client re-registers after daemon restart.
- [ ] Frontend port allocated by binding, not checking.
- [ ] Stable TCP mapping persisted across restart when possible.
- [ ] TCP forwarding works bidirectionally.
- [ ] Docker watcher finds already-running containers at startup.
- [ ] Docker watcher finds new labeled containers.
- [ ] Stopped containers become unavailable.
- [ ] Container recreation changes backend, not frontend.
- [ ] Unsafe non-loopback publications refused by default.
- [ ] Two PostgreSQL projects coexist without both needing host 5432.
- [ ] Host process can connect through each devmesh endpoint.
- [ ] `devmesh doctor` diagnoses Docker/Compose publication problems.
- [ ] `go test -race ./...` passes.
- [ ] Daemon exits cleanly and removes its Unix socket.
- [ ] No tokens, keys, or credentials in logs.
