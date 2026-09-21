#!/usr/bin/env bash
set -euo pipefail

# This script is run by devctl's persistent service wrapper. It does not own
# the database registration: Docker labels make devmeshd's watcher the sole
# producer owner. Resolve immediately before exec so a planning plugin never
# retains a stale endpoint in a launch plan.
DEVMESH_BIN="${DEVMESH_BIN:-devmesh}"
DEVMESH_SERVICE="${DEVMESH_SERVICE:-devctl.example.postgres}"
DEVMESH_WAIT="${DEVMESH_WAIT:-45s}"

endpoint="$(${DEVMESH_BIN} services resolve "${DEVMESH_SERVICE}" --raw --wait "${DEVMESH_WAIT}")"
export DATABASE_URL="postgres://dev:dev@${endpoint}/app?sslmode=disable"

printf 'devctl consumer resolved %s as %s\n' "${DEVMESH_SERVICE}" "${endpoint}" >&2
exec python3 consumer.py
