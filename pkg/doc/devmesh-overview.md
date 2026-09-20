---
Title: "What devmesh is"
Slug: "devmesh-overview"
Short: "The problem devmesh solves and the mental model behind stable endpoints."
Topics:
- devmesh
- architecture
Commands:
- devmesh services list
- devmesh services resolve
IsTopLevel: false
IsTemplate: false
ShowPerDefault: true
SectionType: GeneralTopic
---

devmesh gives development services stable, discoverable endpoints even when the
underlying process or container uses an arbitrary ephemeral port. A logical
service name owns a stable frontend; producers publish ephemeral backends into
that name; consumers resolve the name and always reach the frontend.

## Core vocabulary

| Term | Meaning |
| --- | --- |
| Service name | Logical identity such as `checkout.postgres`. |
| Frontend | Stable `host:port` that consumers connect to. Owned by devmesh. |
| Backend | Current `host:port` a producer listens on. Ephemeral. |
| Registration | A producer's time-bounded assertion of a backend. |
| Owner key | Stable identity used for conflict rules across recreation. |

## The five invariants

1. Producer-owned backends: the app or Docker chooses the backend port.
2. Devmesh-owned frontends: frontends are allocated by binding, not checking.
3. Stable identity: names survive backend churn and daemon restarts.
4. Docker is an adapter, not the core.
5. Generic TCP is host+port; only HTTP can be multiplexed by hostname.

## See Also

- `devmesh-getting-started`
- `devmesh-resolve-workflow`
- `devmesh-docker-compose`
