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
    - Path: /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/various/vault-project-report--devmesh-deep-dive.md
      Note: Byte-identical mirror of the published Obsidian vault project report
ExternalSources: []
Summary: |
    Inventory and provenance of the imported devmesh implementation guide (source specification) and the mirrored Obsidian vault project report for DEVMESH-001.
LastUpdated: 2026-09-20T17:05:00-04:00
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

## Mirrored project report (Obsidian vault)

The deep-dive project report published to the go-go-parc Obsidian vault is
mirrored into this ticket so the repository is self-contained. The mirror is a
byte-for-byte copy; edit the vault note and re-mirror rather than editing the
copy in place.

| Item | Value |
| --- | --- |
| Vault path | `/home/manuel/code/wesen/go-go-golems/go-go-parc/Projects/2026/09/20/PROJECT REPORT - Devmesh - Stable Local Endpoints for Ephemeral Backends - A Technical Deep Dive.md` |
| Mirror path | `.../DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/various/vault-project-report--devmesh-deep-dive.md` |
| Vault commit | `d85a07c` — `PROJECT REPORT - Devmesh: stable local endpoints, bind-first allocation, Docker and Glazed` |
| Size | 51,197 bytes |
| SHA-256 | `7f0d80ab7e3a6a98f25189fbdb40c2b26bcdefdcb5fe7b5845be48feecb8d3f5` |
| Import method | `cp` (byte-for-byte) |
| Modification | None |

Verify the mirror is unchanged:

```bash
sha256sum \
  "/home/manuel/code/wesen/go-go-golems/go-go-parc/Projects/2026/09/20/PROJECT REPORT - Devmesh - Stable Local Endpoints for Ephemeral Backends - A Technical Deep Dive.md" \
  /home/manuel/code/wesen/2026-09-20--devmesh/ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/various/vault-project-report--devmesh-deep-dive.md
```

## Related

- Design deliverable: `../design-doc/01-devmesh-intern-analysis-and-implementation-guide.md`
- Mirrored vault report: `../various/vault-project-report--devmesh-deep-dive.md`
- Ticket index: `../index.md`
- Diary: `../diary/01-devmesh-ticket-diary.md`
