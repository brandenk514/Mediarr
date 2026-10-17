# Releasing Mediarr (#48)

A tag on `main` produces a **signed, scanned, promoted** container image and
a GitHub release whose notes are generated from the milestone's closed
issues.

## Pipeline

```
git tag vX.Y.Z && git push origin vX.Y.Z
  └─ CI (ci.yml, on: push tags v*)
       ├─ Lint, vet, test            (unit + integration + e2e + govulncheck)
       ├─ Build + scan image
       │    ├─ multi-arch build      linux/amd64 + linux/arm64 (buildx)
       │    ├─ Trivy scan            exit 1 on CRITICAL/HIGH (unfixed ignored)
       │    ├─ SBOM (trivy SPDX)     artifact: mediarr-sbom
       │    ├─ cosign sign           keyless / Sigstore Fulcio (GITHUB_TOKEN)
       │    ├─ cosign verify         cert identity = this repo's workflow
       │    └─ push                  ghcr.io/brandenk514/mediarr:vX.Y.Z
       ├─ (tag only) Promote         :latest ← vX.Y.Z (buildx imagetools, no rebuild)
       └─ (tag only) Generate release notes → GitHub release (--latest)
```

Pushes to `main` (no tag) run the same build/scan/sign pipeline and publish
the test image `ghcr.io/brandenk514/mediarr:sha-<short>`, then the Docker
smoke test (real Postgres + Redis via `deploy/compose.dev.yml`).

## Image tags

| Tag | When | Purpose |
|---|---|---|
| `sha-<short>` | every push to `main` | rolling test image, used by the smoke test |
| `vX.Y.Z` | tag push | the release image (multi-arch, signed) |
| `latest` | tag push (promoted) | always the newest **tagged** release |

## Verifying a release

Images are signed keylessly by this repository's workflow (Sigstore/Fulcio).
Consumers verify without any shared key:

```sh
cosign verify \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --certificate-identity-regex "github-workflow-identity:brandenk514/Mediarr.*" \
  ghcr.io/brandenk514/mediarr:vX.Y.Z
```

The SBOM for a run is the `mediarr-sbom` artifact of that Actions run.

## Dry run

The workflow has a `workflow_dispatch` trigger with a `dry_run` boolean
(default `true`). A dry run does the **full build, Trivy scan, SBOM and
release-notes generation** but publishes nothing (no ghcr push, no signing,
no `:latest`, no GitHub release) — safe to point at any ref:

```sh
gh workflow run ci.yml --field dry_run=true
gh run watch <run-id>
```

The generated notes are the "Show generated notes" step output (and the
release-notes job is skipped entirely, so no release is created).

## Real run

```sh
# from a clean checkout of main
git fetch origin --tags
git tag v0.1.0
git push origin v0.1.0
```

Requires: the `GITHUB_TOKEN` package permission (automatic for public
repos), and — only if you scan/pull Docker Hub images anonymously at scale —
the `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` repo secrets (optional; CI
pulls anonymously otherwise).

## Release notes

`scripts/release-notes.sh <tag>` (run by the `release-notes` job) collects
every `#<issue>` referenced by commit subjects in the range since the
previous tag (or all history for the first release) and renders:

```
# vX.Y.Z
_range label · date · commit_
## Closed issues          ← one line per issue, number-ordered, with state
## Commits                ← full `git log --oneline` of the range
```

Heuristic: issues are "the milestone's closed issues" because every
milestone PR merges with a `(#NN)` subject reference. An issue that is not
closed yet is flagged inline (`_(still open — verify before closing the
milestone)_`) so a notes pass can catch it.

## Versioning

Semver (`MAJOR.MINOR.PATCH`), tag = version with `v` prefix. Minor bump per
milestone; patch for fixes between milestones.

## Run log

| Tag | Date | Dry run | Real run | Result |
|---|---|---|---|---|
| _(filled on first release)_ | | | | |
