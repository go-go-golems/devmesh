---
Title: Devmesh release contract and project setup
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
DocType: playbook
Intent: long-term
Owners: []
RelatedFiles:
    - Path: abs:///home/manuel/code/wesen/terraform/vault/github-actions/envs/k3s/main.tf
      Note: Terraform source for the live Devmesh builder and publisher roles
    - Path: repo://.github/workflows/dependency-scanning.yml
      Note: Hosted vulnerability and GoSec checks
    - Path: repo://.github/workflows/lint.yml
      Note: Hosted lint scope mirrors Makefile and excludes ticket probes
    - Path: repo://.github/workflows/release.yaml
      Note: Split OIDC builder and shared publisher workflow
    - Path: repo://.goreleaser.yaml
      Note: Dual-binary release archive and distribution targets
    - Path: repo://AGENT.md
      Note: Contributor and release-boundary rules
    - Path: repo://Makefile
      Note: Reproducible local validation and release targets
    - Path: repo://scripts/verify_govulncheck.py
      Note: Narrow JSON-based reachable-vulnerability gate
    - Path: repo://security/govulncheck-exceptions.md
      Note: Reviewed no-fix Docker Engine advisory disposition
ExternalSources:
    - /home/manuel/code/wesen/go-go-golems/go-go-parc/Research/playbooks/infra/PLAYBOOK - Vault Backed Go Binary Releases.md
Summary: Project/release setup, authorization boundaries, validation commands, and the first-release prerequisite for Devmesh.
LastUpdated: 2026-09-21T01:36:38-04:00
WhatFor: Make Devmesh buildable, linted, testable, and releasable as a standard go-go-golems dual-binary project without repository-stored publication credentials.
WhenToUse: Use when developing Devmesh locally, reviewing its CI/release workflow, or preparing the first version tag.
---

# Devmesh release contract and project setup

## Purpose

Devmesh is published as two coordinated Go binaries:

- `devmesh` is the user-facing CLI and script integration client.
- `devmeshd` owns the local registry, stable frontend listeners, proxies, leases, Docker watcher, and Unix-socket API.

A release archive must contain both binaries. Releasing only the CLI would leave a new user unable to run the service it contacts; releasing only the daemon would omit the supported command and script interface. The Go module identity is `github.com/go-go-golems/devmesh`; it is not a `github.com/wesen/...` module.

The upstream repository is public at `https://github.com/go-go-golems/devmesh` (GitHub numeric repository ID `1378931603`). The local working tree remains `/home/manuel/code/wesen/2026-09-20--devmesh` while the project is being established; directory names do not determine the published module identity.

## Local development contract

Use the Makefile from the repository root. Project-local Go commands set `GOWORK=off` so an enclosing workspace cannot substitute unrelated module versions.

```bash
make build                 # go generate + build every package
make build-bin             # dist/devmesh and dist/devmeshd
make test                  # all packages and integration tests
make test-race             # race-enabled test suite
make lint                  # pinned golangci-lint + Glazed command lint
make logcopter-check       # generated logcopter files are current
goreleaser check --config .goreleaser.yaml
goreleaser check --soft --config .goreleaser.yaml
GORELEASER_ARGS='--skip=sign --snapshot --clean' \
  GORELEASER_TARGET='--single-target' make goreleaser
```

`lefthook install` installs the repository pre-commit and pre-push hooks. The pre-commit hook runs lint and tests for changed Go files. The pre-push hook also runs the snapshot GoReleaser build; this is intentionally a substantial local gate, so contributors may run the listed commands before pushing.

`logcopter_generate.go` is the generator entry point. Generated `logcopter.go` files are tracked source, not disposable build output. Regenerate them with `make logcopter-generate` after changing package structure or logging declarations, then require `make logcopter-check` to pass.

The module minimum is Go `1.26.6`, selected to receive the standard-library vulnerability fixes reported by Govulncheck. Run `make govulncheck` and `make gosec` in addition to the normal development gate.

## CI contract

The checked-in workflows are intentionally separated by purpose:

| Workflow | Trigger | Responsibility |
| --- | --- | --- |
| `push.yml` | `main` pushes and pull requests | `go test ./...` plus both binary help smoke commands. |
| `lint.yml` | `main`, pull requests, release tags | Pinned golangci-lint configuration. |
| `dependency-scanning.yml` | main, pull requests, weekly | Dependency review, `govulncheck`, and GoSec. |
| `codeql-analysis.yml` | main, pull requests, weekly | GitHub CodeQL analysis for Go. |
| `secret-scanning.yml` | main and pull requests | TruffleHog history/content scan. |
| `release.yaml` | push of a `v*` tag | Split GoReleaser builds and one final publisher. |

CI intentionally does not start Docker or publish a release on ordinary pushes. Docker acceptance remains a local/controlled integration check because it depends on a daemon and image availability. The real PostgreSQL recreate test remains in the repository’s integration suite and is run in the project validation gate when Docker is available.

The first-push TruffleHog case is guarded because GitHub supplies an all-zero previous SHA and the scanner requires two different commits. Later pushes and pull requests scan an explicit before/after or base/head range. The Govulncheck verifier accepts only `GO-2026-4883` and `GO-2026-4887`, two no-fix Docker Engine plugin advisories that Devmesh reaches through its local Docker client dependency; their scope, exposure analysis, and removal condition are recorded in `security/govulncheck-exceptions.md`. Any other reachable advisory fails CI.

## Release authorization boundary

The authorization is live as of Terraform commit `ab0d974` in `wesen/terraform`. `AWS_PROFILE=manuel terraform apply /tmp/devmesh-vault-roles.tfplan` created exactly four resources—`gha-release-devmesh-builder`, `gha-release-devmesh-publisher`, `release-devmesh-builder`, and `release-devmesh-publisher`—with no changes or destroys; the immediate subsequent plan reported no changes. This clears the policy prerequisite for the controlled `v0.1.0` release.

The controlled `v0.1.0` release succeeded at [GitHub release v0.1.0](https://github.com/go-go-golems/devmesh/releases/tag/v0.1.0) through run [`35553737420`](https://github.com/go-go-golems/devmesh/actions/runs/35553737420): both split builders and the shared publisher passed. It published Linux and macOS archives, Linux deb/rpm packages, and checksums. The generated Homebrew Cask at `go-go-golems/homebrew-go-go-go/Casks/devmesh.rb` is version `0.1.0` and installs both `devmesh` and `devmeshd`.


A version tag matching `v*` starts two independent build jobs:

```text
v* tag push
  -> Linux split build (release-devmesh-builder)
  -> macOS split build (release-devmesh-builder)
  -> upload named dist artifacts
  -> shared infra-tooling publisher (release-devmesh-publisher)
  -> GitHub Release + Homebrew Cask + Fury packages
```

The builder role can read only `kv/data/ci/release/shared/goreleaser-pro` for the GoReleaser Pro build license. It cannot read the Homebrew GitHub App credential or the Fury token. The final publisher invokes the shared `go-go-golems/infra-tooling` workflow and selects the Terraform-owned `homebrew-fury` profile. The publisher receives a caller-repository `GITHUB_TOKEN` for the GitHub Release and mints a short-lived GitHub App installation token restricted to `go-go-golems/homebrew-go-go-go` for Cask publication.

No Devmesh workflow reads repository Action secrets for a GoReleaser key, GPG key, Homebrew token, Fury token, or private key. The caller workflow supplies no Vault secret path as an input.

The required permanent Vault roles are:

- `release-devmesh-builder`
- `release-devmesh-publisher`

They must bind the immutable repository ID, `go-go-golems/devmesh`, `push`, a `refs/tags/v*` ref, the exact Devmesh release workflow reference, and—on the publisher—the exact shared infra-tooling workflow reference. Their policy and profile selection belong in `/home/manuel/code/wesen/terraform/vault/github-actions/envs/k3s`, not in this repository.

## Distribution shape

`.goreleaser.yaml` builds static Linux and macOS binaries for amd64 and arm64. Its archive stanza selects both build IDs, so each platform archive contains `devmesh` and `devmeshd`. Linux package artifacts are Debian and RPM packages. Homebrew distribution uses the current GoReleaser `homebrew_casks` format rather than the deprecated `brews` stanza; the cask exposes both binaries.

```bash
brew tap go-go-golems/go-go-go
brew install --cask devmesh
```

The Cask model is macOS-specific. Linux users can consume the release archive or package artifact; a Linux Homebrew formula is intentionally not generated by the current GoReleaser model.

## First-release checklist

Before creating the first tag, require all of the following:

1. Review and merge the Devmesh project setup and release workflow.
2. Add Devmesh to Terraform’s `release_publishers` allowlist with repository ID `R_kgDOUjDTkw`, the `homebrew-fury` profile, and the exact workflow ref `go-go-golems/devmesh/.github/workflows/release.yaml@refs/tags/v*`.
3. Run `terraform fmt`, backend-free validation, and a normal remote-state plan in the Vault Terraform environment. The plan must contain only the reviewed Devmesh release-role additions.
4. Apply the reviewed Terraform plan through the authorized infrastructure process; do not apply unrelated drift or pass arbitrary paths from a workflow.
5. Confirm the shared publisher contract still accepts `linux_artifact`, `darwin_artifact`, `credential_profile`, and `vault_role` as used by Devmesh.
6. Run the local quality gate and strict/soft GoReleaser checks from this document.
7. Push a controlled tag such as `v0.1.0`, inspect the GitHub run, verify the GitHub Release contains both binaries, confirm the Cask update reaches only the intended tap, and verify package publication.
8. Record run URLs and non-secret Terraform evidence in this ticket diary. Never record credentials, Vault tokens, or private keys.

## Evidence from this setup

The project setup validation completed on 2026-09-21 before its source commit:

```text
make lint                  PASS
make test                  PASS
make test-race             PASS
make logcopter-check       PASS
make build                 PASS
make build-bin             PASS
make govulncheck           PASS (two documented no-fix Docker Engine exceptions)
make gosec                 PASS
goreleaser check           PASS
goreleaser check --soft    PASS
snapshot GoReleaser build  PASS
```

The snapshot produced a Linux amd64 archive containing both `devmesh` and `devmeshd`, plus Debian and RPM package artifacts. It did not publish because snapshot mode skips announcement and publishing.

## Exit criteria

The repository source setup is complete when the module/import identity, checked-in CI, Makefile, generated logging files, dual-binary GoReleaser configuration, hooks, and local validation are committed and pushed to `go-go-golems/devmesh`.

A **production release** is not complete until the Terraform roles are applied and a controlled version tag has successfully published every expected destination. Do not conflate source configuration with evidence that external authorization or publication has occurred.
