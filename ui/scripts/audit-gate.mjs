#!/usr/bin/env node
// Dependency audit gate for the UI tree.
//
// Runs `npm audit` and fails the build on any high/critical advisory that
// is not allowlisted below. This is the same gate the CI workflow and
// scripts/ci-preflight.sh run; keep the three in sync.
//
// An advisory that genuinely doesn't apply can be allowlisted here with a
// comment explaining why, plus the condition under which the entry must be
// removed. The gate fails closed: if npm audit output cannot be parsed or
// reports an error, the gate fails.

import { spawnSync } from 'node:child_process'

const ALLOWLIST = {
  // picomatch ReDoS through single-branch repeated extglobs (CWE-1333),
  // GHSA-fc5q-39xc-vjpf, advisory range <= 4.0.7 (every release to date).
  // Dev-only exposure: picomatch is reached solely through tinyglobby for
  // glob matching inside vite/vitest build tooling. The container image
  // ships built static assets with no node_modules, and no
  // attacker-controlled glob pattern reaches these tools in CI, dev, or
  // build. No fixed picomatch release exists (4.0.7 is latest as of
  // 2026-11-17); npm's only suggested remediation is a breaking vite
  // downgrade to 6.2.7.
  // REMOVE this entry when picomatch publishes a patched release and the
  // lockfile picks it up.
  'GHSA-fc5q-39xc-vjpf': 'dev-only build-tooling ReDoS; no fixed picomatch release exists',
}

const GATING_SEVERITIES = new Set(['high', 'critical'])

const ghsaId = (url) =>
  typeof url === 'string' ? (url.match(/GHSA(?:-[a-z0-9]+){3}/i)?.[0] ?? null) : null

const res = spawnSync('npm', ['audit', '--json'], {
  encoding: 'utf8',
  stdio: ['ignore', 'pipe', 'inherit'],
})
if (res.error) {
  console.error(`audit-gate: failed to run npm audit: ${res.error.message}`)
  process.exit(1)
}

let report
try {
  report = JSON.parse(res.stdout)
} catch {
  console.error('audit-gate: could not parse npm audit output; failing closed')
  process.exit(1)
}
if (report.error) {
  console.error(
    `audit-gate: npm audit reported an error: ${report.error.summary ?? ''} ${report.error.detail ?? ''}`,
  )
  process.exit(1)
}

const vulns = report.vulnerabilities ?? {}

// A vulnerable package is covered when its severity is below the gate, or
// every advisory in its chain is allowlisted. Transitive entries carry
// package-name refs into their parents, so resolve with a fixpoint pass.
const covered = new Set()
let changed = true
while (changed) {
  changed = false
  for (const [name, v] of Object.entries(vulns)) {
    if (covered.has(name)) continue
    const advisories = (v.via ?? []).filter((x) => typeof x === 'object' && x.url)
    const parents = (v.via ?? []).filter((x) => typeof x === 'string')
    const ok =
      !GATING_SEVERITIES.has(v.severity) ||
      (advisories.every((a) => ALLOWLIST[ghsaId(a.url)]) && parents.every((p) => covered.has(p)))
    if (ok) {
      covered.add(name)
      changed = true
    }
  }
}

const offenders = Object.entries(vulns).filter(
  ([name, v]) => GATING_SEVERITIES.has(v.severity) && !covered.has(name),
)

if (offenders.length > 0) {
  console.error('audit-gate: FAIL — high/critical advisories with no allowlist coverage:')
  for (const [name, v] of offenders) {
    console.error(`  ${name} (${v.severity})`)
    for (const x of v.via ?? []) {
      if (typeof x === 'object') {
        console.error(`    ${x.title} [${x.url}] vulnerable range: ${x.range}`)
      } else {
        console.error(`    via ${x}`)
      }
    }
  }
  process.exit(1)
}

const allowlistedGating = Object.entries(vulns).filter(
  ([name, v]) => GATING_SEVERITIES.has(v.severity) && covered.has(name),
)
for (const [name] of allowlistedGating) {
  console.log(`audit-gate: ${name} covered by allowlist`)
}

// Stale allowlist hygiene: warn (do not fail) about entries that no longer
// match anything so they get pruned instead of rotting.
const seen = new Set()
for (const v of Object.values(vulns)) {
  for (const x of v.via ?? []) {
    const g = typeof x === 'object' ? ghsaId(x.url) : null
    if (g) seen.add(g)
  }
}
const stale = Object.keys(ALLOWLIST).filter((g) => !seen.has(g))
if (stale.length > 0) {
  console.warn(`audit-gate: note: allowlisted advisories no longer present (consider removing): ${stale.join(', ')}`)
}

console.log(
  `audit-gate: ok — vulnerable packages: ${Object.keys(vulns).length}, allowlisted high/critical: ${allowlistedGating.length}`,
)
