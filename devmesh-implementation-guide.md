# Devmesh Implementation Guide

**Working name:** `devmesh`  
**Primary language:** Go  
**MVP platforms:** macOS and Linux  
**Purpose:** local service registry + stable endpoint broker + reverse proxy, with Docker/Compose auto-registration

---

## 1. What we are building

Build a small local daemon that gives development services stable, discoverable endpoints even when the underlying process or container is using an arbitrary ephemeral port.

The system has four pieces:

1. **`devmeshd` daemon** — owns the registry, stable frontend listeners, TCP/HTTP proxies, Docker watcher, persistence, and local API.
2. **`devmesh` CLI** — lists and resolves services, runs diagnostics, and provides manual registration/debug commands.
3. **Go client package** — lets native Go services bind to port `0`, register the actual selected backend port, and keep the registration alive with a lease.
4. **Docker adapter** — watches labeled Docker containers, discovers their actual host-published ports, and registers/unregisters them automatically.

The central abstraction is:

```text
logical service name
        |
        v
+------------------+
| devmesh registry |
+------------------+
        |
        +-----------------------+
        |                       |
        v                       v
stable frontend            current backend
127.0.0.1:15432            127.0.0.1:49173
        |                       ^
        +------ TCP proxy ------+
```

The backend can change on every restart. The frontend should stay the same.

For HTTP services, later phases add a shared hostname-based proxy:

```text
https://api-checkout.dev.example.com
                    |
                    v
                devmeshd :443
                    |
                    v
              127.0.0.1:49320
```

For databases and other generic TCP services, the public endpoint is a stable loopback host + port:

```text
checkout.postgres -> 127.0.0.1:15432
checkout.redis    -> 127.0.0.1:16379
```

Do **not** attempt to route generic TCP by hostname on one shared IP/port. The DNS hostname is not available at the TCP layer after name resolution.

---

## 2. Definition of done for the MVP

The MVP is complete when all of the following work:

### Native process flow

A Go process can do this:

```go
ln, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil {
    return err
}

registration, err := devmesh.Register(ctx, devmesh.Registration{
    Name:          "checkout.api",
    Kind:          devmesh.KindTCP,
    Backend:       ln.Addr().String(),
    PreferredPort: 8080,
})
if err != nil {
    return err
}
defer registration.Close()

log.Printf("stable endpoint: %s", registration.Endpoint())
```

If the process got backend port `49382`, devmesh may expose it as:

```text
127.0.0.1:18080 -> 127.0.0.1:49382
```

or as `127.0.0.1:8080` if devmesh can atomically bind that preferred frontend port.

### Docker/Compose flow

A labeled PostgreSQL container can use an ephemeral host-published port:

```yaml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_PASSWORD: dev
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

After `docker compose up -d`, this must work:

```bash
devmesh resolve checkout.postgres
```

Example output:

```text
127.0.0.1:15432
```

A third-party host process can connect to that address.

If Docker recreates the container and changes its host-published backend port from `49173` to `49782`, the result of `devmesh resolve checkout.postgres` must remain `127.0.0.1:15432` while new connections are sent to `49782`.

### Daemon restart flow

If `devmeshd` restarts:

1. It reloads remembered frontend port assignments.
2. It rebinds those frontend ports if available.
3. It reconciles already-running labeled Docker containers.
4. Native Go clients re-register automatically when their next lease heartbeat discovers that the daemon lost their registration.

---

## 3. Explicit non-goals for the MVP

Do not implement these until the core system is stable:

- Kubernetes integration.
- A full service mesh data plane.
- mTLS between every local service.
- Transparent interception of arbitrary application traffic.
- Local DNS server or per-service loopback IP assignment.
- UDP proxying.
- Protocol-specific PostgreSQL/MySQL/Redis proxy logic.
- Multi-host/distributed discovery.
- Cross-machine networking.
- Automatic ACME DNS-01 certificate issuance.
- Windows named-pipe support.
- Advanced load balancing or health checking.
- Mutating Docker containers after they have started.
- Requiring a Docker CLI plugin.

Build a reliable local registry and proxy first.

---

## 4. Architecture

```text
                                    +---------------------+
                                    | Docker Engine       |
                                    |                     |
                                    | postgres container  |
                                    | :5432               |
                                    +----------+----------+
                                               |
                                     Docker-published port
                                      127.0.0.1:49173
                                               |
                                               v
+-----------------+                   +---------------------+
| Native Go app   |                   |                     |
| backend :49382  |---- register ---->|      devmeshd       |
+-----------------+                   |                     |
                                      | registry            |
+-----------------+                   | port allocator      |
| devmesh CLI     |<---- local API -->| TCP proxy manager   |
+-----------------+                   | HTTP proxy          |
                                      | Docker watcher      |
+-----------------+                   | lease manager       |
| third-party app |                   | state persistence   |
| psql / DBeaver  |                   +----------+----------+
+--------+--------+                              |
         |                                       |
         |                              stable frontend
         +----------------------------> 127.0.0.1:15432
                                                |
                                                +----> 127.0.0.1:49173
```

The daemon should be the only component responsible for stable frontend ports. Producers only tell it where the current backend is.

---

## 5. Repository layout

Use one Go module initially.

```text
devmesh/
├── cmd/
│   ├── devmesh/
│   │   └── main.go
│   └── devmeshd/
│       └── main.go
├── pkg/
│   └── devmesh/
│       ├── client.go
│       ├── registration.go
│       └── types.go
├── internal/
│   ├── api/
│   │   ├── server.go
│   │   ├── handlers.go
│   │   └── dto.go
│   ├── config/
│   │   └── config.go
│   ├── dockerwatch/
│   │   ├── watcher.go
│   │   ├── inspect.go
│   │   └── labels.go
│   ├── lease/
│   │   └── manager.go
│   ├── proxy/
│   │   ├── tcp.go
│   │   └── http.go
│   ├── registry/
│   │   ├── registry.go
│   │   ├── model.go
│   │   └── errors.go
│   ├── runtime/
│   │   ├── manager.go
│   │   └── service.go
│   ├── state/
│   │   ├── store.go
│   │   └── model.go
│   └── transport/
│       ├── unix.go
│       └── client.go
├── integration/
│   ├── tcp_proxy_test.go
│   ├── lease_test.go
│   └── docker_test.go
├── examples/
│   ├── native-go/
│   └── compose-postgres/
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

Keep `internal/` components independently testable. Do not put Docker logic into the registry or proxy packages.

---

## 6. Core data model

Keep the routing model small.

### 6.1 Service name

A service has a logical name such as:

```text
checkout.postgres
checkout.api
billing.redis
```

Validation rule:

```regex
^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$
```

Rules:

- lowercase only;
- dot-separated namespaces are allowed;
- no spaces;
- no underscores;
- maximum 120 characters;
- names are unique within one daemon.

### 6.2 Service kind

MVP kinds:

```go
type Kind string

const (
    KindTCP  Kind = "tcp"
    KindHTTP Kind = "http"
)
```

`KindTCP` means opaque byte forwarding. PostgreSQL, Redis, MySQL, SMTP, custom binary protocols, etc. are all `tcp` from devmesh's perspective.

Add optional metadata:

```go
type AppProtocol string
```

Examples:

```text
postgres
redis
mysql
grpc
http
https
```

`AppProtocol` is a hint for UX only. It must not change proxy semantics in the MVP.

### 6.3 Backend

```go
type Backend struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}
```

MVP restriction:

```text
backend host must be a loopback address
```

Accept:

```text
127.0.0.1
::1
localhost (resolve and verify loopback)
```

Reject remote addresses unless the daemon is explicitly started with a future `allow_remote_backends` setting.

### 6.4 Registration

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

`Source`:

```go
type Source string

const (
    SourceProcess Source = "process"
    SourceDocker  Source = "docker"
    SourceManual  Source = "manual"
)
```

`OwnerKey` identifies who owns the logical service.

Examples:

```text
process:<registration UUID>
docker:<compose-project>:<compose-service>:<container-port>
manual:<registration UUID>
```

A Docker container recreation should have the same logical `OwnerKey`, even though the container ID changes. That lets the backend be replaced atomically without changing the frontend.

### 6.5 Frontend

```go
type Frontend struct {
    Host string `json:"host"`
    Port int    `json:"port"`
    URL  string `json:"url,omitempty"`
}
```

For `tcp`, `Host` should normally be `127.0.0.1`.

For `http`, the frontend may later contain a URL such as:

```text
https://api-checkout.dev.example.com
```

---

## 7. Separate service identity from backend registration

This distinction is important.

A **service runtime** owns the stable frontend. A **registration** supplies the current backend.

Conceptually:

```go
type ServiceRuntime struct {
    Name       string
    Kind       Kind
    Frontend   Frontend
    Listener   net.Listener
    Backend    atomic.Value // *Backend, nil when unavailable
    LastActive time.Time
}
```

On first registration:

```text
registration arrives
    -> allocate/bind frontend listener
    -> create runtime
    -> set backend
```

On Docker recreation:

```text
same logical service
    -> keep frontend listener open
    -> atomically replace backend
```

On backend disappearance:

```text
backend becomes nil
    -> keep frontend listener reserved for idle grace period
    -> TCP connections fail quickly
    -> HTTP returns 503
```

Default idle grace period:

```text
10 minutes
```

This prevents a short container restart from changing the frontend port.

After the grace period, the listener may be closed, but the remembered port assignment stays persisted so the daemon will try it again later.

---

## 8. Local daemon API transport

Do not expose the administrative API on a normal TCP port by default.

Use HTTP/JSON over a Unix domain socket.

### Socket path

Preferred order:

1. `$DEVMESH_SOCKET`, if set.
2. `$XDG_RUNTIME_DIR/devmesh/devmesh.sock`, if `XDG_RUNTIME_DIR` exists.
3. `~/.devmesh/run/devmesh.sock`.

Create parent directories with mode `0700` and socket permissions restricted to the user.

### Stale socket handling

On startup:

1. If socket path does not exist, bind it.
2. If it exists, attempt to connect.
3. If connection succeeds, another daemon is running: exit with an error.
4. If connection fails, remove the stale socket and bind.

Do not blindly delete an existing socket before probing it.

### HTTP over Unix socket

Server:

```go
ln, err := net.Listen("unix", socketPath)
if err != nil {
    return err
}

srv := &http.Server{Handler: mux}
return srv.Serve(ln)
```

Client transport:

```go
transport := &http.Transport{
    DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
        var d net.Dialer
        return d.DialContext(ctx, "unix", socketPath)
    },
}

client := &http.Client{Transport: transport}
```

Use a dummy base URL such as `http://devmesh` because the HTTP request still requires a URL host even though the actual connection uses the Unix socket.

---

## 9. Local API

Use a versioned API from the beginning.

Base path:

```text
/v1
```

Use `application/json` for bodies.

### 9.1 Health

```http
GET /v1/health
```

Response:

```json
{
  "status": "ok",
  "version": "0.1.0",
  "docker": "connected"
}
```

Docker being unavailable should not make the entire daemon unhealthy. Use a degraded status field instead.

### 9.2 List services

```http
GET /v1/services
```

Response:

```json
{
  "services": [
    {
      "name": "checkout.postgres",
      "kind": "tcp",
      "app_protocol": "postgres",
      "status": "ready",
      "frontend": {
        "host": "127.0.0.1",
        "port": 15432
      }
    }
  ]
}
```

Do not expose backend internals in the normal resolve/list API unless requested by an inspect/debug endpoint.

### 9.3 Resolve service

```http
GET /v1/services/{name}
```

Ready response:

```json
{
  "name": "checkout.postgres",
  "kind": "tcp",
  "app_protocol": "postgres",
  "status": "ready",
  "frontend": {
    "host": "127.0.0.1",
    "port": 15432
  }
}
```

Known service with no backend:

```json
{
  "name": "checkout.postgres",
  "kind": "tcp",
  "status": "unavailable",
  "frontend": {
    "host": "127.0.0.1",
    "port": 15432
  }
}
```

Unknown service:

```text
404 Not Found
```

### 9.4 Register a process/manual backend

```http
POST /v1/registrations
```

Request:

```json
{
  "name": "checkout.api",
  "kind": "tcp",
  "app_protocol": "http",
  "backend": {
    "host": "127.0.0.1",
    "port": 49382
  },
  "preferred_port": 8080,
  "ttl_seconds": 15
}
```

Response:

```json
{
  "registration_id": "01J...",
  "lease_token": "random-secret-token",
  "name": "checkout.api",
  "frontend": {
    "host": "127.0.0.1",
    "port": 18080
  },
  "expires_at": "2026-09-20T20:00:15Z"
}
```

Use a cryptographically random lease token. Do not use the registration ID itself as authorization for renewal/deletion.

### 9.5 Heartbeat

```http
POST /v1/registrations/{id}/heartbeat
Authorization: Bearer <lease-token>
```

Response:

```json
{
  "expires_at": "2026-09-20T20:00:20Z"
}
```

If the daemon restarted and no longer knows the registration, return `404`; the client library should then re-register automatically.

### 9.6 Delete registration

```http
DELETE /v1/registrations/{id}
Authorization: Bearer <lease-token>
```

Return `204 No Content`.

### 9.7 Inspect/debug endpoint

```http
GET /v1/services/{name}/inspect
```

Include backend and source details here:

```json
{
  "name": "checkout.postgres",
  "status": "ready",
  "frontend": {
    "host": "127.0.0.1",
    "port": 15432
  },
  "backend": {
    "host": "127.0.0.1",
    "port": 49173
  },
  "source": "docker",
  "owner_key": "docker:checkout:db:5432",
  "docker_container_id": "..."
}
```

---

## 10. Error model

All JSON API errors should have one format:

```json
{
  "error": {
    "code": "name_conflict",
    "message": "service checkout.postgres is already owned by another registration"
  }
}
```

Define stable codes:

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

Do not make clients parse human-readable error strings.

---

## 11. Registry implementation

Create an in-memory registry protected by a mutex.

```go
type Registry struct {
    mu       sync.RWMutex
    services map[string]*ServiceRecord
}
```

Do not perform network I/O while holding the registry mutex.

A useful service record:

```go
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

The registry should expose operations such as:

```go
CreateOrReplaceOwned(...)
Resolve(name string)
MarkUnavailable(ownerKey string)
Remove(ownerKey string)
List()
```

### Ownership rule

If `checkout.postgres` is owned by:

```text
docker:checkout:db:5432
```

then a replacement registration with that same owner key may update the backend.

A registration with another owner key must get a conflict.

This prevents an unrelated local service from silently hijacking a name.

---

## 12. Stable frontend port allocation

The frontend port allocator must **bind**, not check.

Bad implementation:

```text
check whether port 5432 is free
close check socket
later listen on 5432
```

That has a race.

Correct implementation:

```go
ln, err := net.Listen("tcp", "127.0.0.1:5432")
```

If this succeeds, devmesh owns the port immediately and keeps that listener open.

Go also supports asking the kernel to choose an available port by listening on port `0`, but devmesh should use its own configured stable range for frontend ports so assignments can be persisted and are easier to inspect.

### Default frontend range

Use a configurable range, for example:

```text
15000-19999
```

Configuration:

```json
{
  "tcp_frontend_min": 15000,
  "tcp_frontend_max": 19999
}
```

### Allocation algorithm

For a service named `checkout.postgres`:

1. If persistent state already remembers a port for this service, try to bind that port.
2. Otherwise, if registration requests `preferred_port`, try to bind it.
3. Otherwise, calculate a deterministic starting index within the configured range from a hash of the service name.
4. Probe the range by actually calling `net.Listen` on each candidate.
5. The first successful listener is the allocation. Keep it open.
6. Persist the chosen port immediately.
7. If the entire range is exhausted, return `port_exhausted`.

Hashing the name before scanning spreads services across the range instead of filling ports linearly.

Example:

```go
func startOffset(name string, size int) int {
    h := fnv.New32a()
    _, _ = h.Write([]byte(name))
    return int(h.Sum32() % uint32(size))
}
```

The allocation function should return the **open listener**, not only the numeric port.

```go
type Allocation struct {
    Port     int
    Listener net.Listener
}
```

This removes the allocation-to-bind race entirely.

---

## 13. TCP proxy

Each TCP service runtime owns one stable frontend listener.

### Accept loop

Pseudocode:

```go
for {
    clientConn, err := listener.Accept()
    if err != nil {
        if shuttingDown {
            return nil
        }
        log error
        continue
    }

    go proxyConnection(clientConn)
}
```

### Per-connection routing

For each accepted connection:

1. Load the current backend atomically.
2. If no backend exists, close the connection quickly.
3. Dial the backend with a short timeout, e.g. 3 seconds.
4. Start bidirectional copying.
5. Close both sides when finished.

Pseudo-implementation:

```go
func proxyTCP(client net.Conn, backend Backend) {
    defer client.Close()

    upstream, err := net.DialTimeout(
        "tcp",
        net.JoinHostPort(backend.Host, strconv.Itoa(backend.Port)),
        3*time.Second,
    )
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
```

Implement `CloseWrite` when the connection is a `*net.TCPConn` so protocols that rely on half-closing behave correctly.

### Backend changes

Existing connections continue talking to the backend they were connected to.

New connections use the newly registered backend.

Do not forcibly migrate established TCP connections.

---

## 14. Runtime manager

The runtime manager bridges registry state and open listeners.

Suggested responsibilities:

```go
type Manager struct {
    mu       sync.Mutex
    runtimes map[string]*ServiceRuntime
    allocator *PortAllocator
    state     *state.Store
}
```

Operations:

```go
EnsureTCPRuntime(name, preferredPort)
SetBackend(name, backend)
ClearBackend(name)
RemoveRuntime(name)
```

`EnsureTCPRuntime` must be idempotent. If a runtime already exists, return it without recreating its listener.

This is how Docker backend churn can happen without changing the frontend.

---

## 15. Leases for native processes

Native process registrations should not live forever after a crash.

Use leases.

### Defaults

```text
lease TTL:       15 seconds
heartbeat every: 5 seconds
```

### Daemon behavior

Maintain a timer structure or periodic sweeper.

A simple MVP implementation may sweep once per second:

```text
for each process registration:
    if now > lease_expires:
        mark service backend unavailable
        delete registration
```

The number of local registrations will be small enough that a simple sweep is fine.

### Client behavior

The Go client should:

1. Register.
2. Start a heartbeat goroutine.
3. Heartbeat every `TTL / 3`.
4. If heartbeat succeeds, continue.
5. If heartbeat returns `404`, re-register.
6. If the daemon is temporarily unreachable, retry with bounded exponential backoff.
7. On `Close()`, attempt `DELETE` but do not block process shutdown indefinitely.

Suggested backoff:

```text
100 ms
250 ms
500 ms
1 s
2 s
max 5 s
```

Add jitter.

---

## 16. Go client package

Package path example:

```text
github.com/yourorg/devmesh/pkg/devmesh
```

### Primary API

```go
type RegistrationOptions struct {
    Name          string
    Kind          Kind
    AppProtocol   string
    Backend       string
    PreferredPort int
}

type Handle interface {
    Endpoint() string
    Close() error
}

func Register(ctx context.Context, opts RegistrationOptions) (Handle, error)
```

### Convenience API for listeners

Provide a helper that atomically allocates the backend port using the OS:

```go
type ListenerHandle struct {
    net.Listener
    Registration Handle
}

func ListenTCP(ctx context.Context, name string, preferredPort int) (*ListenerHandle, error)
```

Implementation order:

```go
ln, err := net.Listen("tcp", "127.0.0.1:0")
```

Only after the listener exists should it register its actual address with devmesh.

Usage:

```go
ln, err := devmesh.ListenTCP(ctx, "checkout.api", 8080)
if err != nil {
    return err
}
defer ln.Close()

log.Printf("public endpoint: %s", ln.Registration.Endpoint())
return http.Serve(ln, handler)
```

Closing the wrapper should:

1. close the application listener;
2. stop heartbeat;
3. unregister the lease.

Do not have devmesh select an application's backend port and return it before the application binds it. The application should bind port `0` itself.

---

## 17. Docker integration

Do not start by writing a Docker plugin. A daemon that watches Docker events plus Compose labels is enough for the MVP.

Use the official Docker Engine Go client and API version negotiation.

The watcher has two jobs:

1. **startup reconciliation** — inspect already-running containers when devmeshd starts;
2. **continuous event handling** — react to container lifecycle changes.

### 17.1 Docker connection

Use environment-aware Docker client configuration so Docker Desktop/contexts work.

Conceptually:

```go
cli, err := client.NewClientWithOpts(
    client.FromEnv,
    client.WithAPIVersionNegotiation(),
)
```

If Docker is unavailable:

- log the error;
- mark Docker integration degraded;
- keep the rest of devmesh running;
- reconnect periodically.

### 17.2 Labels

Use reverse-DNS style labels.

Required labels:

```text
io.devmesh.enable=true
io.devmesh.name=checkout.postgres
io.devmesh.container-port=5432
io.devmesh.kind=tcp
```

Optional labels:

```text
io.devmesh.app-protocol=postgres
io.devmesh.preferred-port=5432
```

For HTTP later:

```text
io.devmesh.kind=http
io.devmesh.http-host=api-checkout
io.devmesh.backend-scheme=http
```

### 17.3 Compose owner key

Docker Compose sets labels containing project and service identity. Use those values if available to form a stable owner key.

Conceptually:

```text
docker:<compose-project>:<compose-service>:<container-port>
```

If Compose labels are unavailable, fall back to:

```text
docker:<container-name>:<container-port>
```

Do not use container ID as the logical owner key because container IDs change on recreation.

### 17.4 Discover host-published backend port

On `start`:

1. Inspect the container.
2. Check `io.devmesh.enable=true`.
3. Parse `io.devmesh.container-port`.
4. Look up the port in `NetworkSettings.Ports`, e.g. `5432/tcp`.
5. Find the host binding.
6. Register the backend.

Expected inspect shape conceptually:

```json
{
  "5432/tcp": [
    {
      "HostIp": "127.0.0.1",
      "HostPort": "49173"
    }
  ]
}
```

If no published binding exists, do not try to connect directly to the container IP in the MVP. Log an actionable error:

```text
container checkout-db is labeled for devmesh but 5432/tcp is not published to the host
```

Direct container-network routing is not portable across Docker Engine and Docker Desktop and should be a later optimization.

### 17.5 Loopback safety

Prefer bindings where `HostIp` is loopback.

If Docker reports `0.0.0.0`, the backend may still be reachable at `127.0.0.1:<port>`, but the container is also exposed on other host interfaces.

Default behavior:

```text
warn loudly and refuse registration unless config explicitly allows non-loopback Docker publications
```

Configuration:

```json
{
  "docker": {
    "allow_non_loopback_published_ports": false
  }
}
```

### 17.6 Startup reconciliation

When devmeshd starts:

1. List running containers.
2. Filter those with `io.devmesh.enable=true`.
3. Inspect each.
4. Register each valid backend.
5. Continue into the event stream.

This is mandatory. Relying only on future Docker events would miss containers that started before devmesh.

### 17.7 Event handling

Subscribe to Docker container events.

Relevant events:

```text
start
restart
die
stop
destroy
```

Behavior:

- `start` / `restart`: inspect and upsert registration.
- `die` / `stop` / `destroy`: clear the backend if the stopped container currently owns it.

Docker event streams can disconnect. Reconnect with backoff, then run a full reconciliation again before resuming events.

### 17.8 Timing race after container start

Immediately after a start event, port bindings may not always be usable at the exact instant your handler runs.

Implement bounded retry around inspect/register:

```text
50 ms
100 ms
200 ms
400 ms
800 ms
1.5 s
2 s
```

Stop once the expected published port is visible or the container is no longer running.

---

## 18. Docker Compose contract

The simplest initial contract is labels + an ephemeral host-published port.

Example:

```yaml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_PASSWORD: dev
      POSTGRES_DB: app
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

The intent is:

```text
container :5432
     |
     v
Docker-selected loopback backend port, e.g. 49173
     |
     v
devmesh stable frontend, e.g. 15432
```

### Important portability note

Docker supports ephemeral published host ports, but Compose versions/platforms have had differences around combinations of an explicit host IP and an omitted host port.

Therefore:

1. Add an integration test in `devmesh doctor` that verifies the local Docker setup can create a loopback-only ephemeral publication.
2. Never silently fall back to publishing a database on `0.0.0.0`.
3. If `127.0.0.1::5432` does not work in the user's Docker/Compose environment, report that clearly and provide the tested alternative for that environment.

A Docker bridge network can also be configured to use `127.0.0.1` as its default host binding address, allowing a service to publish only the container port and have Docker choose a random host port. Treat this as an environment-specific fallback and test it before relying on it.

### Do not require the Compose service to use host port 5432

That is exactly the conflict devmesh is intended to remove.

The container should always listen on its normal internal port. Docker gets an arbitrary backend host port; devmesh owns the stable frontend.

---

## 19. HTTP routing phase

Implement this after TCP + Docker registration are working.

One HTTP listener can serve many local services because the request contains a hostname.

```text
api-checkout.dev.example.com ----+
                                 |
web-checkout.dev.example.com ----+--> devmeshd :443/:8443
                                 |
auth-checkout.dev.example.com ---+
```

### HTTP registration model

For an HTTP service, store:

```go
type HTTPRoute struct {
    Hostname      string
    Backend       Backend
    BackendScheme string // http or https
}
```

### Reverse proxy

Use `net/http/httputil.ReverseProxy`.

For every request:

1. normalize `r.Host` by removing the port;
2. look up the route;
3. if no route exists, return `404`;
4. if route exists but backend is unavailable, return `503`;
5. proxy to the current backend;
6. add standard forwarded headers.

Preserve the original host where useful so local applications can see the public dev hostname.

### Initial HTTP listener

Do not block the TCP MVP on privileged ports.

Start with configurable ports such as:

```text
HTTP  127.0.0.1:8088
HTTPS 127.0.0.1:8443
```

Then add an install/setup command for ports 80/443 later.

---

## 20. Wildcard DNS and TLS phase

Once the routing code works, support a real development domain.

Example DNS:

```text
*.dev.example.com  A     127.0.0.1
*.dev.example.com  AAAA  ::1
```

Then configure devmesh with:

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

### MVP TLS scope

For the first TLS implementation, only support loading an existing PEM certificate and key from disk.

Do **not** implement ACME automation at the same time as the proxy.

Validate at startup that:

- certificate parses;
- private key matches;
- wildcard/SAN covers the configured hostname scheme;
- key file permissions are not obviously unsafe;
- listener can bind.

### Hostname generation

A certificate for:

```text
*.dev.example.com
```

covers exactly one label before `dev.example.com`.

Therefore a logical name like:

```text
checkout.api
```

must not automatically become:

```text
checkout.api.dev.example.com
```

because that is two labels.

Use either:

1. an explicit `http-host` label, e.g. `api-checkout`; or
2. a deterministic slug, e.g. `checkout-api`.

Enforce hostname uniqueness separately from service-name uniqueness.

---

## 21. Persistent state

Do not persist active registrations. They are derived from live producers.

Persist only durable preferences/state such as stable frontend port assignments.

Example state file:

```json
{
  "version": 1,
  "tcp_ports": {
    "checkout.postgres": 15432,
    "checkout.redis": 16379
  }
}
```

Store under an OS-appropriate user config/data directory.

A simple implementation may use `os.UserConfigDir()` and a `devmesh/state.json` file.

### Atomic writes

Write state like this:

1. serialize into a temporary file in the same directory;
2. `fsync` if practical;
3. close;
4. rename temp file over the existing state file.

Never truncate the live state file and then write into it directly.

### Corruption behavior

If state cannot be parsed:

- rename it to `state.json.corrupt.<timestamp>`;
- start with empty state;
- log a warning;
- do not prevent the daemon from starting.

---

## 22. Configuration

Example config:

```json
{
  "tcp_frontend_host": "127.0.0.1",
  "tcp_frontend_min": 15000,
  "tcp_frontend_max": 19999,
  "runtime_idle_ttl": "10m",
  "lease_ttl": "15s",
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

Precedence:

```text
CLI flags > environment variables > config file > defaults
```

Do not create dozens of flags initially. Keep config focused on networking and integration behavior.

---

## 23. CLI behavior

Implement the CLI against the same local API used by the Go client.

### Start daemon

```bash
devmeshd
```

Development shortcut:

```bash
devmesh daemon
```

### List

```bash
devmesh list
```

Example:

```text
NAME                 KIND  APP       STATUS       ENDPOINT
checkout.postgres    tcp   postgres  ready        127.0.0.1:15432
checkout.api         tcp   http      ready        127.0.0.1:18080
billing.redis        tcp   redis     unavailable  127.0.0.1:16379
```

Add `--json` from the beginning.

### Resolve

```bash
devmesh resolve checkout.postgres
```

Default output should be intentionally script-friendly:

```text
127.0.0.1:15432
```

JSON:

```bash
devmesh resolve checkout.postgres --json
```

### Inspect

```bash
devmesh inspect checkout.postgres
```

Show source and backend details.

### Doctor

```bash
devmesh doctor
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

This command will save substantial debugging time.

### Manual registration

For debugging:

```bash
devmesh register \
  --name test.echo \
  --kind tcp \
  --backend 127.0.0.1:49000 \
  --preferred-port 9000
```

The CLI should maintain the lease until interrupted.

---

## 24. Logging

Use Go's `log/slog`.

Default human-readable logs for foreground mode; optionally JSON for service/CI use.

Important structured fields:

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

Useful event names:

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

Do not log lease tokens or secrets.

---

## 25. Security requirements

This is a local developer tool, but basic boundaries matter.

### Administrative API

- Unix socket only by default.
- User-only filesystem permissions.
- No unauthenticated TCP admin endpoint.

### Frontends

- Bind stable proxy listeners to loopback by default.
- Never default to `0.0.0.0`.

### Backends

- Process/manual registrations may target loopback only in the MVP.
- Docker-published backends should be loopback-only by default.

### Lease authorization

- Generate random lease tokens with `crypto/rand`.
- Compare tokens safely.
- Tokens are returned only once when a registration is created.

### Docker socket

Access to the Docker daemon is highly privileged on many systems. Keep Docker access isolated inside the Docker adapter and only perform read/inspect/event operations for the MVP.

### TLS private keys

If TLS is enabled:

- never include key material in logs;
- prefer key files accessible only to the current user;
- do not copy a shared wildcard private key into generated project directories.

---

## 26. Failure semantics

Define these now so behavior is predictable.

### Daemon unavailable

- `devmesh resolve`: fail clearly.
- Go client: retry registration/heartbeats with backoff.
- Existing application listener continues serving its raw backend port; only the devmesh stable endpoint is unavailable.

### Backend unavailable

TCP:

```text
frontend remains bound
new connection accepted
no backend -> close quickly
```

HTTP:

```text
503 Service Unavailable
```

### Docker unavailable

- daemon still serves manual/native registrations;
- Docker integration reports degraded status;
- reconnect in the background;
- after reconnect, perform full container reconciliation.

### Port assignment conflict after daemon restart

If the remembered frontend port is now occupied by another process:

1. log a warning;
2. allocate another port;
3. update state;
4. expose the new endpoint through resolve.

Do not kill or interfere with the process that owns the old port.

### Duplicate service name

If an unrelated owner tries to claim an existing name, return a conflict. Never silently replace it.

---

## 27. Concurrency rules

Follow these rules to avoid subtle deadlocks/races:

1. Registry mutex protects in-memory metadata only.
2. Never dial a backend while holding registry/runtime locks.
3. Never call Docker API while holding registry/runtime locks.
4. Never write state files while holding a lock that blocks connection handling.
5. Backend pointer should be replaceable atomically.
6. Service runtime creation/removal must be serialized per service name.
7. Listener ownership belongs to exactly one `ServiceRuntime`.
8. Shutdown closes listeners before waiting for proxy goroutines indefinitely.

Run tests with:

```bash
go test -race ./...
```

Treat race-detector failures as blockers.

---

## 28. Graceful shutdown

On SIGINT/SIGTERM:

1. stop accepting new API requests;
2. stop Docker event watcher;
3. stop lease sweeper;
4. close frontend listeners;
5. wait briefly for active proxy goroutines;
6. flush state;
7. remove Unix socket;
8. exit.

Use a root context canceled by signal handling.

Do not wait forever on active TCP clients. Use a configurable shutdown timeout such as 5 seconds and then force close.

---

## 29. Unit tests

### Name validation

Test:

```text
checkout.postgres     valid
checkout-api          valid
Checkout.Api          invalid
checkout_postgres     invalid
.checkout             invalid
checkout.             invalid
```

### Port allocator

Tests:

- preferred port free -> returns that port;
- preferred port occupied -> returns a fallback;
- remembered port free -> reuses it;
- remembered port occupied -> chooses fallback;
- parallel allocations never return the same listener port;
- exhausted range returns typed error.

Use real loopback listeners rather than mocking the kernel for the critical allocation tests.

### Registry ownership

Tests:

- first owner can register;
- same owner can replace backend;
- different owner gets conflict;
- removing owner marks backend unavailable;
- resolve never exposes backend details.

### Leases

Tests:

- registration remains alive while heartbeats arrive;
- expired lease is removed;
- incorrect token cannot renew/delete;
- daemon-side missing registration causes client re-registration behavior.

### State store

Tests:

- load/save round trip;
- atomic replacement;
- corrupt state recovery;
- unknown future version handled clearly.

---

## 30. TCP integration tests

Write an echo server bound to `127.0.0.1:0`.

Test:

```text
client -> devmesh stable frontend -> echo backend
```

Verify payloads in both directions.

Then replace backend:

```text
stable frontend -> backend A
update registration
stable frontend -> backend B
```

Verify new connections reach B without changing the frontend.

Also verify an established connection to A is not unexpectedly moved.

---

## 31. Docker integration tests

Run these only when Docker is available; otherwise skip with a clear reason.

Use a lightweight container for the basic port-discovery tests. Keep one PostgreSQL end-to-end test because that is a key use case.

### Test A: labeled TCP service discovery

1. Start a container with a random loopback-published port.
2. Add devmesh labels.
3. Wait for watcher reconciliation.
4. Resolve service.
5. Connect through stable frontend.

### Test B: container recreation

1. Resolve service and record frontend.
2. Recreate container so Docker backend port changes.
3. Wait for watcher update.
4. Resolve again.
5. Assert frontend is unchanged.
6. Connect and verify traffic reaches new container.

### Test C: daemon starts after container

1. Start container first.
2. Start devmeshd second.
3. Ensure startup reconciliation finds it.

### Test D: unsafe publication

Start a labeled container published on `0.0.0.0`.

Default behavior must refuse it and surface a diagnostic.

### Test E: Docker event reconnect

Simulate watcher stream interruption if practical; otherwise unit-test reconnect state machine with an interface around the Docker client.

---

## 32. PostgreSQL end-to-end acceptance test

Create `examples/compose-postgres/compose.yaml`:

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

Acceptance script:

```bash
docker compose up -d

devmesh resolve checkout.postgres
# -> 127.0.0.1:<stable-port>
```

Use a Go integration test with `pgx` so CI does not require a host-installed `psql` binary.

Then recreate the DB container:

```bash
docker compose up -d --force-recreate db
```

Verify the frontend endpoint remains unchanged and a new DB connection succeeds.

---

## 33. `devmesh doctor` Docker port test

Because ephemeral host-port publication syntax can behave differently across Docker/Compose environments, make this a real runtime capability test.

The doctor command should create a tiny disposable container or use the Docker API directly to test:

```text
container target port -> randomly selected host port -> loopback only
```

Check the inspected `HostIp` and `HostPort`.

Always remove the test container afterward, including on failure.

Output should be actionable, for example:

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
       devmesh will not auto-register this container because it would expose the database on your LAN.
```

---

## 34. Implementation milestones / PR plan

The intern should build this as small reviewable PRs. Do not attempt everything in one branch.

### PR 1 — skeleton, daemon transport, API, CLI

Implement:

- repository layout;
- `devmeshd` startup/shutdown;
- Unix socket transport;
- `/v1/health`;
- `devmesh daemon`, `devmesh list`, `devmesh resolve` plumbing;
- typed API errors;
- `slog` logging;
- config loading.

Acceptance:

```text
devmeshd starts
devmesh health/list can talk over Unix socket
second daemon refuses to start on same socket
stale socket is recovered safely
```

### PR 2 — registry, stable port allocator, TCP proxy

Implement:

- service/registration types;
- name validation;
- ownership rules;
- stable frontend port allocator;
- runtime manager;
- opaque TCP proxy;
- persistent port mapping state;
- manual registration API.

Acceptance:

```text
manual backend can be registered
resolve returns a stable frontend
traffic traverses proxy
backend can be replaced without changing frontend
```

### PR 3 — leases and Go package

Implement:

- lease tokens;
- heartbeat API;
- lease expiry;
- Go API client;
- `Register`;
- `ListenTCP` convenience helper;
- automatic re-registration after daemon restart.

Acceptance:

```text
native Go example binds :0
registers automatically
stable frontend works
kill -9 native process -> registration expires
restart daemon -> client eventually re-registers
```

### PR 4 — Docker watcher

Implement:

- Docker client abstraction;
- startup reconciliation;
- label parser;
- container inspect -> backend discovery;
- event stream;
- reconnect/reconcile loop;
- loopback publication safety;
- Docker fields in inspect output.

Acceptance:

```text
running labeled container is discovered
new labeled container is discovered
stopped container becomes unavailable
recreated container changes backend without changing frontend
```

### PR 5 — Docker/Compose diagnostics and PostgreSQL example

Implement:

- `devmesh doctor`;
- disposable Docker publication capability test;
- Compose PostgreSQL example;
- pgx end-to-end integration test;
- clear error messages for missing/unsafe host publication.

Acceptance:

```text
fresh developer can run the example from README
psql-equivalent pgx connection succeeds through devmesh
```

### PR 6 — shared HTTP proxy

Implement:

- HTTP route registry;
- reverse proxy;
- hostnames/aliases;
- high-port HTTP/HTTPS listeners;
- unavailable -> 503 behavior;
- Docker HTTP label support.

Acceptance:

```text
two HTTP services share one devmesh HTTP listener
hostname chooses the correct backend
container recreation does not change URL
```

### PR 7 — wildcard TLS support

Implement:

- base domain config;
- certificate/key file loading;
- hostname generation/validation;
- TLS listener;
- documentation for wildcard DNS pointing at loopback.

Do not add automatic ACME yet.

---

## 35. Code quality expectations

Before each PR is considered complete:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
```

Add a linter only if the repository already standardizes on one. Do not spend the first week configuring a large lint stack.

Prefer standard library components unless a dependency removes substantial complexity.

Reasonable external dependencies:

- official Docker Go client;
- ULID/UUID package, or use a small internal random ID implementation;
- `pgx` only for PostgreSQL integration tests/example.

Avoid adding a web framework. Modern Go's `net/http` is sufficient for this local API.

---

## 36. Important design decisions that should not be changed casually

### Backend ports are producer-owned

Native applications bind `127.0.0.1:0` themselves. Docker chooses its own ephemeral published backend port.

Devmesh does not hand a supposedly free backend port to another process and hope it remains free.

### Frontend ports are devmesh-owned

Devmesh allocates frontend ports by binding them and keeping the listener open.

This is where preferred ports and stable assignments belong.

### Docker is an adapter, not the core abstraction

The core registry should work without Docker.

Docker watcher code converts:

```text
container metadata + published port
```

into the same registration model native processes use.

### Generic TCP is host + port

Do not try to make multiple arbitrary TCP services simultaneously appear at the same `127.0.0.1:<port>` based only on different DNS names.

If we later need every database to keep its canonical port, implement per-service loopback IPs + local DNS as a separate advanced mode.

### Stable frontend and backend are intentionally different

This indirection is the core value of the project.

```text
frontend: stable, consumer-facing
backend: ephemeral, producer-facing
```

---

## 37. Future extensions after MVP

Only consider these after the acceptance tests above are reliable.

### Local DNS + unique loopback IPs

Allocate each service an IP from a loopback range so multiple PostgreSQL services can all use `:5432`:

```text
checkout.postgres -> 127.77.0.10:5432
billing.postgres  -> 127.77.0.11:5432
```

This requires local DNS/resolver integration and OS-specific setup.

### Docker CLI plugin

A Docker CLI plugin could eventually provide:

```bash
docker devmesh list
docker devmesh doctor
docker devmesh compose up
```

It should be a thin UX layer over the daemon, not a second implementation.

### Compose wrapper/generator

A future:

```bash
devmesh compose up
```

could generate an override file from `x-devmesh` annotations and automatically add labels/port publication.

Do not make this necessary for basic operation.

### Connection string rendering

Examples:

```bash
devmesh url checkout.postgres
# postgres://127.0.0.1:15432
```

or:

```bash
devmesh exec \
  --set 'DATABASE_URL=postgres://dev:dev@{checkout.postgres}/app' \
  -- ./server
```

### HTTP ACME automation

Later support DNS-01 for a real wildcard development domain. Keep provider credentials scoped as narrowly as possible and avoid distributing one long-lived wildcard private key to every developer machine.

### Health checks

Add optional active health probes before advertising a service as ready.

### Multiple backends/load balancing

The data model can eventually allow multiple registrations for one service and select a backend per connection/request.

Do not add this until single-backend ownership semantics are proven.

### Windows

Abstract daemon transport now so a later Windows implementation can replace Unix sockets with named pipes without changing the API model.

---

## 38. Suggested first-week task sequence

Day 1:

- initialize module/repository;
- implement socket path/config;
- start HTTP server over Unix socket;
- build health endpoint and CLI client.

Day 2:

- implement service model/name validation;
- implement state store;
- implement atomic frontend port allocator.

Day 3:

- implement TCP runtime and forwarding;
- build manual registration/resolve endpoints;
- add echo-server integration test.

Day 4:

- implement leases and heartbeat sweeper;
- build Go client library;
- add automatic re-registration.

Day 5:

- harden shutdown, logging, race tests, error messages;
- merge PRs 1-3 before starting Docker integration if possible.

Second week begins with Docker startup reconciliation, then event handling, then Compose/PostgreSQL tests.

---

## 39. End-to-end example: expected final MVP behavior

Developer starts devmesh:

```bash
devmeshd
```

Developer runs two projects, each with PostgreSQL on internal port 5432.

Project A's Docker backend happens to be:

```text
127.0.0.1:49173
```

Project B's backend happens to be:

```text
127.0.0.1:49281
```

Devmesh registry:

```text
checkout.postgres
  frontend 127.0.0.1:15432
  backend  127.0.0.1:49173

billing.postgres
  frontend 127.0.0.1:17112
  backend  127.0.0.1:49281
```

Third-party programs do not care about Docker's ports:

```bash
devmesh resolve checkout.postgres
# 127.0.0.1:15432

devmesh resolve billing.postgres
# 127.0.0.1:17112
```

Project A recreates its DB container. Docker now publishes:

```text
127.0.0.1:49901
```

Devmesh updates only the backend:

```text
checkout.postgres
  frontend 127.0.0.1:15432   # unchanged
  backend  127.0.0.1:49901   # changed
```

That is the fundamental behavior the implementation must preserve.

---

## 40. References

Use these as the primary implementation references rather than blog posts:

- Docker Compose service/port specification: <https://docs.docker.com/reference/compose-file/services/#ports>
- Docker port publishing: <https://docs.docker.com/get-started/docker-concepts/running-containers/publishing-ports/>
- Docker Engine API: <https://docs.docker.com/reference/api/engine/>
- Docker events: <https://docs.docker.com/reference/cli/docker/system/events/>
- Docker inspect: <https://docs.docker.com/reference/cli/docker/inspect/>
- Docker Compose networking: <https://docs.docker.com/compose/how-tos/networking/>
- Go `net` package: <https://pkg.go.dev/net>
- Go `net/http/httputil`: <https://pkg.go.dev/net/http/httputil>

One Docker/Compose detail deserves an integration test rather than an assumption: **loopback-only ephemeral host-port publishing has had implementation differences across Compose/platform versions**. `devmesh doctor` should verify this capability on the actual developer machine and refuse unsafe `0.0.0.0` fallback by default.

---

## 41. Final acceptance checklist

Before calling the MVP complete, verify all of these manually and in automated tests where practical:

- [ ] Daemon runs as an unprivileged user.
- [ ] Admin API is reachable only through the local Unix socket.
- [ ] `devmesh list`, `resolve`, and `inspect` work.
- [ ] Native application can register a backend bound to port `0`.
- [ ] Native lease expires after a crashed process.
- [ ] Native client re-registers after daemon restart.
- [ ] TCP frontend port is allocated by binding, not checking.
- [ ] Stable TCP port mapping is persisted across daemon restart when possible.
- [ ] TCP byte forwarding works bidirectionally.
- [ ] Docker watcher discovers already-running containers on startup.
- [ ] Docker watcher discovers new labeled containers.
- [ ] Docker watcher removes/marks unavailable stopped containers.
- [ ] Docker container recreation changes backend without changing frontend.
- [ ] Unsafe non-loopback Docker publications are refused by default.
- [ ] Two independent PostgreSQL projects can run simultaneously without both needing host port 5432.
- [ ] A host process can connect to each database through its devmesh endpoint.
- [ ] `devmesh doctor` identifies Docker/Compose publication problems.
- [ ] `go test -race ./...` passes.
- [ ] Daemon exits cleanly and removes its Unix socket.
- [ ] No lease tokens, TLS keys, or credentials appear in logs.

If these work, the core architecture is sound. HTTP wildcard routing, TLS automation, local DNS, per-service loopback IPs, and Docker CLI integration can then be layered on without redesigning the registry.
