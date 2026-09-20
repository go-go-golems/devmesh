---
Title: "Getting started with devmesh"
Slug: "devmesh-getting-started"
Short: "Run the daemon, list services, and resolve your first stable endpoint."
Topics:
- devmesh
Commands:
- devmeshd serve
- devmesh services list
- devmesh services resolve
Flags:
- format
IsTopLevel: false
IsTemplate: false
ShowPerDefault: true
SectionType: Tutorial
---

This tutorial takes you from an empty machine to a resolved stable endpoint.

## 1. Start the daemon

The daemon owns the registry, frontend listeners, and persistence:

```bash
devmeshd serve
```

By default it listens on `$DEVMESH_SOCKET`, or
`$XDG_RUNTIME_DIR/devmesh/devmesh.sock`, or `~/.devmesh/run/devmesh.sock`.

## 2. Check health

```bash
devmesh health
```

## 3. List services

```bash
devmesh services list
devmesh services list --format json
```

## 4. Resolve a service

```bash
devmesh services resolve checkout.postgres
```

The default output is a human table. Scripts should use `--format json` or
`--format jsonl --output-fields endpoint`.

## 5. Diagnose problems

```bash
devmesh doctor
```

## Troubleshooting

| Problem | Cause | Solution |
| --- | --- | --- |
| `daemon unreachable` | devmeshd is not running | Start `devmeshd serve`. |
| `another devmeshd is already listening` | A daemon already owns the socket | Reuse it or stop the other process. |
| `unknown service` | No producer has registered that name | Register a backend or start a labeled container. |

## See Also

- `devmesh-overview`
- `devmesh-resolve-workflow`
