# Devctl + devmesh Compose PostgreSQL example

This example is the first practical integration between the two tools.

- **devctl** supervises the foreground Compose process and the consumer process.
- **devmeshd** watches the labeled PostgreSQL container and owns its stable TCP frontend.
- The consumer launcher resolves the logical database name immediately before it `exec`s, then exports `DATABASE_URL`.

There is no devctl schema extension, no dependency graph, no plugin-held lease,
and no second Docker registration path. Docker labels make the devmesh watcher
the only owner of the database registration.

## Prerequisites

1. Build/install `devmesh` and `devmeshd`, or set `DEVMESH_BIN` to the devmesh binary.
2. Start `devmeshd serve` in another terminal with Docker enabled.
3. Have Docker and a local `postgres:17-alpine` image available.
4. Install or build `devctl` from `/home/manuel/code/wesen/go-go-golems/devctl`.

## Run

From the repository root:

```bash
make build
./dist/devmeshd serve
```

In another terminal:

```bash
cd examples/devctl-compose-postgres
DEVMESH_BIN=../../dist/devmesh \
  go run /home/manuel/code/wesen/go-go-golems/devctl/cmd/devctl plan
```

After inspecting the plan, run the environment with the same devctl binary.
The `database` service runs `docker compose up` in the foreground. The
`consumer` service waits up to 45 seconds for:

```bash
devmesh services resolve devctl.example.postgres --raw --wait 45s
```

It then starts with a connection string such as:

```text
postgres://dev:dev@127.0.0.1:15432/app?sslmode=disable
```

The wait means the container has been registered by devmesh. It does **not**
mean PostgreSQL is ready to accept SQL queries; a real application should keep
its normal database retry/readiness behavior.

Stop the environment through the same devctl instance:

```bash
go run /home/manuel/code/wesen/go-go-golems/devctl/cmd/devctl down
```

This stops only devctl-owned services. It does not stop the shared `devmeshd`
process or remove services from unrelated projects.

## Why the shell launcher exists

Devctl plugins terminate after planning. They cannot safely keep a devmesh
lease or resolve an endpoint early and assume it stays valid forever. The
persistent devctl wrapper owns `run-consumer.sh`; that script resolves the
endpoint at the boundary where the consumer environment is created.

The consumer does not self-register. If a future application needs a stable
frontend of its own, let it use `pkg/devmesh`, or add the separately designed
per-run backend-file wrapper contract after a concrete application needs it.
