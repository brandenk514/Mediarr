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

### Browser verification (UI)

Since v0.3.0 the image ships the built UI, served same-origin on `:8080`
(#86 decision A, shipped in #87/#88). Release acceptance includes a
real-browser pass against the published image: **sign in → dashboard → one
per-kind page** (`/movies`, `/tv`, `/music`, `/books`).

Prerequisites: Docker (compose not required — this is the documented minimal
run), a browser, and `cosign` (for the verify step above). Credentials use
overridable placeholders in the `E2E_USER`/`E2E_PASSWORD` pattern
(`ui/e2e/login.ts`) — pick your own username (3–32 chars of
`[A-Za-z0-9_-]`) and password (≥ 10 chars); never paste the minted token
anywhere:

```sh
export E2E_USER="<username>"
export E2E_PASSWORD="<password, at least 10 chars>"
```

1. **Pull and verify the image** (the cosign step above):

   ```sh
   docker pull ghcr.io/brandenk514/mediarr:vX.Y.Z
   cosign verify --key docs/cosign.pub ghcr.io/brandenk514/mediarr:vX.Y.Z
   ```

2. **Run the minimal stack** — throwaway Postgres + Redis + the published
   image on one network, with the same constraints as the compose deployment
   (read-only rootfs, `/tmp` tmpfs, the two declared media volumes):

   ```sh
   docker network create mediarr-verify
   docker run -d --name mediarr-verify-pg --network mediarr-verify \
     -e POSTGRES_USER=mediarr -e POSTGRES_PASSWORD=*** -e POSTGRES_DB=mediarr \
     postgres:16-alpine
   docker run -d --name mediarr-verify-redis --network mediarr-verify \
     redis:7-alpine
   docker run -d --name mediarr-verify --network mediarr-verify \
     -p 8080:8080 \
     --read-only --tmpfs /tmp:rw,noexec,nosuid \
     -v mediarr-verify-downloads:/downloads \
     -v mediarr-verify-media:/media \
     -e MEDIARR_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
     -e 'MEDIARR_POSTGRES_DSN=postgres://mediarr:***@mediarr-verify-pg:5432/mediarr?sslmode=disable' \
     -e MEDIARR_REDIS_URL=redis://mediarr-verify-redis:6379 \
     -e MEDIARR_DOWNLOADS_DIR=/downloads -e MEDIARR_MEDIA_ROOT=/media/movies \
     ghcr.io/brandenk514/mediarr:vX.Y.Z
   ```

   (The `mediarr`/`mediarr` Postgres pair is the documented dev default in
   `deploy/compose.dev.yml` — it is not a secret; the throwaway stack is
   destroyed in step 6.)

3. **Wait for readiness**:

   ```sh
   for i in $(seq 1 60); do
     curl -fsS http://localhost:8080/readyz && break
     sleep 2
   done
   ```

   Expected: a readiness JSON report containing `"status": "ok"` (Postgres +
   Redis ping OK).

4. **Seed the first-boot admin** (the README first-boot pattern; on an empty
   `users` table this first `POST /api/v1/auth/login` mints the admin):

   ```sh
   curl -fsS -X POST http://localhost:8080/api/v1/auth/login \
     -H 'Content-Type: application/json' \
     -d "{\"username\":\"${E2E_USER}\",\"password\":\"${E2E_PASSWORD}\"}"
   ```

   Expected: `200` with a JSON body containing `token` (an opaque
   `ma_`-prefixed secret — shown once, never stored or pasted), `user`,
   `role`, and `token_prefix`.

5. **The browser pass** — open **http://localhost:8080** in a real browser:

   - `GET /` renders the login page (the SPA is served same-origin — 200,
     not a 404);
   - sign in with `E2E_USER` / `E2E_PASSWORD` → the **dashboard** renders
     (live counts and queue cards, not an error card);
   - visit one page per kind — `/movies`, `/tv`, `/music`, `/books` — each
     renders its real header without unhandled JavaScript errors.

   Automated equivalent (headless Chromium, the same checks — the spec the
   `ui-e2e` CI job runs against the PR-local image):

   ```sh
   cd ui
   npm ci
   npx playwright install chromium   # once per machine
   E2E_BASE_URL=http://localhost:8080 E2E_USER="$E2E_USER" E2E_PASSWORD="$E2E_PASSWORD" \
     npx playwright test e2e/container-ui.spec.ts
   ```

   Expected: `5 passed` (sign-in + dashboard, one per-kind page).

   Machine-checkable surface probes (the script the CI `smoke` job runs):

   ```sh
   BASE_URL=http://localhost:8080 scripts/ui-smoke.sh
   ```

   Expected: `ok:` lines for `GET /`, `GET /movies`, the hashed asset(s),
   `GET /api/v1/auth/me → 401`, and `GET /api/v1/events → 401`, ending in
   `=== ui-smoke: all probes passed ===`.

6. **Teardown** (zero residue):

   ```sh
   docker rm -f mediarr-verify mediarr-verify-pg mediarr-verify-redis
   docker volume rm mediarr-verify-downloads mediarr-verify-media
   docker network remove mediarr-verify
   ```

The existing kind/Helm steps below and the cosign verification are unchanged
and still part of acceptance.

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
| v0.2.0 | 2026-11-06 | [42161888254](https://github.com/brandenk514/Mediarr/actions/runs/42161888254) — success, 6m53s | [42162630428](https://github.com/brandenk514/Mediarr/actions/runs/42162630428) — success, 16m26s | success — **five-app drop-in parity + Books metadata module**: Sonarr P1–P7 (#46–#52), Radarr R1–R6 (#53–#58), Lidarr L1–L5 (#60–#64), Prowlarr PR1–PR6 (#65–#71), Readarr B1–B6 (#72–#77), movies grid server-side sort (#70), Books metadata MT1–MT5 (#79–#81). Multi-arch build, Trivy, SBOM, cosign sign + verify; consumer-side `cosign verify --key docs/cosign.pub` re-run locally against ghcr with the pinned cosign v3.1.3 for both `v0.2.0` and `:latest` (claims validated, tlog presence confirmed — the signature lives in the `sha256-<digest>` tag the pinned cosign reads; an unpinned local devel-build cosign reports "no signatures found" on this layout); `:latest` promoted and verified digest-identical to the v0.2.0 manifest list; [GitHub release](https://github.com/brandenk514/Mediarr/releases/tag/v0.2.0) with generated notes. All 45 roadmap items `Done`, milestone "v0.2.0 Release" closed, phase branches deleted (local + remote). |
