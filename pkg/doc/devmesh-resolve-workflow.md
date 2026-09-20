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
ENDPOINT=$(devmesh services resolve checkout.postgres \
  --output-fields endpoint --format jsonl | sed 's/.*"endpoint":"\([^"]*\)".*/\1/')
```

`--format jsonl` emits one compact JSON object per line; `--output-fields`
projects only the endpoint.

## Listing everything

```bash
devmesh services list --format csv
```

## Troubleshooting

| Problem | Cause | Solution |
| --- | --- | --- |
| Empty table | No services registered | Start a producer or register manually. |
| `unavailable` status | Frontend reserved, no live backend | Restart the producer. |
| `404` from resolve | Name is unknown to the daemon | Check spelling and `devmesh services list`. |

## See Also

- `devmesh-overview`
- `devmesh-native-go`
