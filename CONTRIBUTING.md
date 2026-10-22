# Contributing to Mediarr

Thanks for your interest! Mediarr is a single-binary Go service plus a
React/TypeScript UI, and contributions of any size are welcome. Before you
start:

- Read the [code of conduct](CODE_OF_CONDUCT.md) — the short version: be
  decent.
- **Security issues do not go here.** If you think you've found a
  vulnerability, read [SECURITY.md](SECURITY.md) and use the private
  reporting channel. Never post vulnerable code or secrets in a public PR or
  issue.
- AI-assisted contributions are welcome, with one rule: disclose the
  assistance in the PR template and take responsibility for what it produced.

## Prerequisites

- Go 1.27+
- Node 22+ (for UI work)
- Docker (integration tests, `make dev`, load tests)

## Dev loop

```sh
make dev          # backend + Postgres + Redis via docker compose
# in another terminal:
cd ui && npm ci && npm run dev   # Vite on :5173, proxies /api → :8080
```

If you run the backend manually, `make key` + `export
MEDIARR_ENCRYPTION_KEY=<64-hex>` first — the service refuses to boot without
it.

The single most useful command is:

```sh
make preflight
```

It runs the same gates CI runs: gofmt, go vet, golangci-lint, unit tests +
the 60% coverage floor, UI install/audit/lint/build/test, and govulncheck
(`make preflight-full` adds the Docker-dependent integration and e2e gates).
**A change that doesn't pass preflight is not ready to push** — the git
pre-push hook and CI both refuse it. If a gate fails, fix the code; never
weaken, skip, or delete a gate to make a push pass.

## Working agreements

- **Bug fixes start with a failing test.** Write the test that reproduces
  the bug, watch it fail, then fix it and watch it pass.
- **Don't edit tests to match broken behavior.** A test file changing in the
  same commit as the code it covers needs an explanation in the PR.
- **Never add a test that cannot fail** (e.g. mocking the very thing under
  test and asserting on the mock's return value).
- **Validation happens at the API boundary.** Handlers do not trust clients;
  domain code trusts its inputs.
- **Time is UTC** everywhere; convert at the edge.
- **No shell execution in the import path.** Use Go's `os/exec`, not `sh -c`.
- **Dependencies are verified before they are added.** When adding a Go or
  npm package, confirm it exists: check the registry page, who publishes it,
  how old it is, who else uses it. Autocomplete and AI tools invent plausible
  package names, and attackers register those names waiting to be picked up
  (slopsquatting). Routine bumps come from Dependabot — review the PR, don't
  hand-edit lockfiles.

## Code style

- **Go:** `gofmt` + `golangci-lint` (gosec, errcheck, govet, unused, and
  friends — see `.golangci.yml`). Accepted findings get a comment + a tracked
  issue, never a silent exclusion.
- **UI:** TypeScript, `npm run lint` (oxlint), vitest.
- **Commits:** `type(scope): summary` — e.g. `fix(auth): reject tokens with
  embedded whitespace`, `docs(config): document REDIS_URL defaults`. Small,
  focused commits; one concern per commit.

## Opening a PR

1. Branch from `main` (`fix/…`, `feat/…`, `docs/…`).
2. Fill in the [PR template](.github/PULL_REQUEST_TEMPLATE.md) honestly —
   especially the "How it was tested" and "AI assistance" sections.
3. CI must be fully green: lint, unit + integration tests, coverage floor,
   UI gates, govulncheck, Semgrep, Trivy.
4. Expect a review of the *why*, not just the diff. Breaking changes to the
   API, config, schema, or image must be called out explicitly.

Reviewers look for:

- the test that proves the fix (or the coverage of the new behavior)
- no secrets, no `.env`, no local config in the diff
- migrations that are up/down tested and non-destructive
- behavior changes documented where users see them (README, `docs/`)

## Where things live

| What | Where |
|---|---|
| Design intent + milestones | `docs/PLAN.md` |
| As-built architecture, routes, data model | `docs/architecture.md` |
| Environment variables + defaults | `docs/config.md` |
| Metrics + observability wiring | `docs/observability.md` |
| Load-test methodology | `docs/loadtest.md` |
| Release process | `RELEASE.md` |
| AI agent rules (gates, tests, dependencies) | `AGENTS.md` |

## License

Mediarr is licensed under **GPL v3**. By contributing, you agree that your
contributions are licensed under the same terms. Keep dependencies permissive
or GPL — no proprietary components.
