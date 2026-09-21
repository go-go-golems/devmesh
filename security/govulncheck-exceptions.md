# Govulncheck exceptions

`./scripts/verify_govulncheck.py` runs `govulncheck -json ./...` and fails on every reachable advisory except the IDs below. This is not a general ignore mechanism: additions require a reviewed source change, an exact advisory ID, and a reason.

| Advisory | Current disposition | Reason |
| --- | --- | --- |
| `GO-2026-4883` | Accepted temporarily | The vulnerable behavior is Docker Engine legacy-plugin privilege validation. Devmesh imports the legacy Docker Go client to inspect and watch a local daemon; it neither implements Docker Engine nor configures plugin privileges. The `github.com/docker/docker` module reports no fixed version. |
| `GO-2026-4887` | Accepted temporarily | The vulnerable behavior is Docker Engine AuthZ-plugin request-body handling. Devmesh is a local Docker API client, not an AuthZ-enabled Engine. The `github.com/docker/docker` module reports no fixed version. |

The Docker watcher remains loopback-oriented and does not expose a Docker API. Operators are responsible for keeping their Docker Engine patched and must not treat these entries as a reason to expose its Unix socket or TCP API broadly.

Remove an exception immediately when the Docker client module publishes a fixed version, Devmesh can migrate to a fixed supported API, or an analysis shows Devmesh can exercise the vulnerable Engine behavior.
