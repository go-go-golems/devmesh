---
Title: "Resolving services from scripts and applications"
Slug: "devmesh-resolve-workflow"
Short: "How consumers turn a logical service name into a stable endpoint."
Topics:
- devmesh
Commands:
- devmesh services resolve
- devmesh services list
Flags:
- format
- output-fields
- raw
- wait
IsTopLevel: false
IsTemplate: false
ShowPerDefault: true
SectionType: Example
---

Consumers should never read Docker's ephemeral host port. They resolve the
logical name and connect to the returned frontend.

## Human-readable

```bash
devmesh services resolve checkout.postgres
```

## Machine-readable

```bash
devmesh services resolve checkout.postgres --format json
```

## Bare endpoint for shell substitution

```bash
ENDPOINT=$(devmesh services resolve checkout.postgres --raw --wait 20s)
```

`--raw` emits exactly one frontend endpoint plus a newline: `host:port` for a
TCP service or a complete URL for an HTTP service. `--wait` retries unknown or
backendless services until its deadline. It proves that devmesh has a ready
backend registration; it is not an application-specific health check.

Without `--raw`, keep Glazed structured output for JSON, JSONL, CSV, or table
consumers:

```bash
devmesh services resolve checkout.postgres --format json
```

## Listing everything

```bash
devmesh services list --format csv
```

## Troubleshooting

| Problem | Cause | Solution |
| --- | --- | --- |
| Empty table | No services registered | Start a producer or register manually. |
| `unavailable` status | Frontend reserved, no live backend | Restart the producer, or use `--wait` in a bounded launcher. |
| `404` from resolve | Name is unknown to the daemon | Check spelling and `devmesh services list`; `--wait` can tolerate a producer still starting. |
| `--raw` exits nonzero | The service was unavailable when the deadline elapsed | Do not use an unavailable endpoint; inspect the producer or extend the bounded wait. |

## See Also

- `devmesh-overview`
- `devmesh-native-go`
