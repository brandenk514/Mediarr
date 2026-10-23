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
       │    ├─ cosign sign           release key (docs/cosign.pub)
       │    ├─ cosign verify         --key docs/cosign.pub
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

Images are signed with the repo's release key (public key committed at
`docs/cosign.pub`). Consumers verify with that public key:

```sh
cosign verify \
  --key docs/cosign.pub \
  ghcr.io/brandenk514/mediarr:vX.Y.Z
```

Signatures are also uploaded to the public Sigstore transparency log
(`--tlog-upload=true`), so `sigstore verify` works for consumers who trust
the log instead of the key.

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

## Signing key

Keyless (Fulcio/OIDC) signing requires account-level OpenID Connect, which
this GitHub account does not have — GitHub silently drops the `id-token:
write` request and cosign falls back to an interactive device flow that
times out in CI. So the pipeline signs with a **long-lived release keypair**:

| Where | What |
|---|---|
| `docs/cosign.pub` | public key (committed; used by `cosign verify`) |
| secret `COSIGN_KEY` | private key, `base64` of the `cosign generate-key-pair` output |
| secret `COSIGN_PASSWORD` | passphrase that encrypts the private key |

**Rotate the key:** `cosign generate-key-pair` (new keypair), commit the new
`docs/cosign.pub`, then `gh secret set COSIGN_KEY -b "$(base64 -w0 <newkey>)"`
and `gh secret set COSIGN_PASSWORD`. Release after the new key lands; old
signatures stay verifiable with the old public key. If the account ever
enables OIDC, swap the sign/verify steps back to keyless (the `id-token:
write` permission is already in place).

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
| v0.1.0 | 2026-10-16 | [39001054696](https://github.com/brandenk514/Mediarr/actions/runs/39001054696) — success, 4m22s | [39009920760](https://github.com/brandenk514/Mediarr/actions/runs/39009920760) — success, 13m42s | success — multi-arch build, Trivy clean (0 CRITICAL), SBOM, cosign sign + verify (repo release key), `:latest` promoted, [GitHub release](https://github.com/brandenk514/Mediarr/releases/tag/v0.1.0) with generated notes. Signing switched from keyless (account has no Actions OIDC) to the repo release key (`docs/cosign.pub`). Two re-tags along the way: key-based signing fix, then notes/load-test hardening (partial `/issues` list call on CI + single-sample p95 flake). |
| v0.1.1 | 2026-10-23 | [39906045175](https://github.com/brandenk514/Mediarr/actions/runs/39906045175) — success, 5m13s | [39906928541](https://github.com/brandenk514/Mediarr/actions/runs/39906928541) — success, 15m46s | success — **M8 shipped**: OpenAPI spec (#76), generated UI types (#77), 70-operation drift guard (#78), CI lockstep + contract doc (#79), typed-client skip decision (#80); plus the Go 1.27.3 security fix (GO-2026-6809) and CI hardening (#74/#83/#84/#85) unshipped since v0.1.0. Multi-arch build, Trivy, SBOM, cosign sign + verify — consumer-side `cosign verify --key docs/cosign.pub` re-run locally against ghcr (claims validated, tlog presence confirmed); `:latest` promoted and verified identical to the v0.1.1 manifest; [GitHub release](https://github.com/brandenk514/Mediarr/releases/tag/v0.1.1) with generated notes. First release where the new lockstep gate was live: it caught (and the PR fixed) a stale `types.gen.ts` caused by #78's spec change. |
