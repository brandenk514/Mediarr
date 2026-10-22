# AGENTS.md

Rules for AI agents (and humans) working in this repo.

## Gates

- `make preflight` (i.e. `scripts/ci-preflight.sh`) runs the CI gates locally
  in one pass: gofmt, go vet, unit tests + the 60% coverage floor, UI
  install/audit/lint/build/test, semgrep, govulncheck. `make preflight-full`
  adds the Docker-dependent integration and e2e gates.
- A push must pass the preflight. The `pre-push` git hook (install with
  `make hooks`) and the Claude Code PreToolUse hook
  (`.claude/settings.json` -> `scripts/push-gate.sh`) enforce this: they run
  the preflight and refuse the push if any gate fails. The gates are a wall,
  not an instruction — do not bypass them (`git push --no-verify`) to make a
  red push "work".
- If a gate fails, fix the code and re-run the preflight until it passes.
  Never weaken, skip, or delete a gate to make it pass.

## Tests

- When a bug is reported, first write a test that reproduces it and FAILS.
  Then fix the bug and watch the test pass.
- If a change makes a test fail, fix the code — do not edit the test to match
  the broken behavior. A test file changing in the same commit as the code it
  covers needs an explanation.
- Never add a test that cannot fail (e.g. mocking the very thing under test
  and asserting on the mock's return value).

## Dependencies

- When adding a package (Go or npm), verify it actually exists: check the
  registry page, who publishes it, how old it is, who else uses it. Models
  sometimes invent plausible package names, and attackers register those
  names waiting to be picked up (slopsquatting).
- Routine version bumps come from Dependabot as PRs; review them through
  normal CI instead of hand-editing lockfiles.
