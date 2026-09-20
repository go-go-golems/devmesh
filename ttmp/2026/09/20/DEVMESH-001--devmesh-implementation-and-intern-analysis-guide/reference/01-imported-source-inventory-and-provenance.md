---
Title: Imported source inventory and provenance
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
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/devmesh-implementation-guide.md
      Note: Original source file whose provenance is recorded here
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/design-doc/01-devmesh-intern-analysis-and-implementation-guide.md
      Note: Companion deliverable that consumes this source
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/sources/devmesh-implementation-guide.md
      Note: Byte-identical imported copy in the ticket
ExternalSources: []
Summary: |
    Inventory and provenance of the imported devmesh implementation guide used as the source specification for DEVMESH-001.
LastUpdated: 2026-09-20T16:35:00-04:00
WhatFor: Trace the imported source file and confirm it is unmodified.
WhenToUse: Use when verifying the origin of the intern guide's source material.
---

# Imported source inventory and provenance

## Goal

Record exactly what source material DEVMESH-001 imports, where it came from, and
how to verify it was imported without modification.

## Context

The intern guide in
`../design-doc/01-devmesh-intern-analysis-and-implementation-guide.md` is a
companion to an existing specification named `devmesh-implementation-guide.md`.
That specification was authored before this ticket and lives at the repository
root. To keep the ticket self-contained, it was copied verbatim into the
ticket's `sources/` directory.

## Quick Reference

| Item | Value |
| --- | --- |
| Original path | `/home/manuel/code/wesen/2026-09-20--devmesh/devmesh-implementation-guide.md` |
| Imported path | `.../DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/sources/devmesh-implementation-guide.md` |
| Size | 54,878 bytes |
| SHA-256 | `6915000170755418cf93eaf6cbc2f223c6f255c1394f33e9d9688da57f0da6ed` |
| Import method | `cp` (byte-for-byte) |
| Modification | None |

The document contains 41 numbered sections covering the problem statement,
definition of done, architecture, data model, daemon transport, local API,
registry, port allocation, TCP proxy, leases, Go client, Docker integration,
Compose contract, HTTP/TLS phases, persistence, configuration, CLI, logging,
security, failure semantics, concurrency, shutdown, tests, milestones, and
references.

## Usage Examples

Verify the imported copy is unchanged:

```bash
sha256sum \
  /home/manuel/code/wesen/2026-09-20--devmesh/devmesh-implementation-guide.md \
  /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/sources/devmesh-implementation-guide.md
```

Both lines must print the same digest shown above.

## Related

- Design deliverable: `../design-doc/01-devmesh-intern-analysis-and-implementation-guide.md`
- Ticket index: `../index.md`
- Diary: `../diary/01-devmesh-ticket-diary.md`
