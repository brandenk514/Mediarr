# Security Policy

Mediarr stores a lot of sensitive data — indexer API keys, download-client
credentials, your media metadata. Keep the service on a trusted network
(see the warning at the top of the Quick start in
[README.md](README.md)).

## Supported versions

| Version          | Supported |
|------------------|-----------|
| v0.1.0 (latest)  | Yes       |
| Older than v0.1.0 | No        |

Only the **latest stable release** receives security fixes. Older releases
are not backported — if a vulnerability is confirmed and you are running an
older version, the fix is to upgrade. Commits on `main` between releases are
pre-release and carry no separate security commitment beyond what the next
release ships.

## Reporting a vulnerability

**Do not report security vulnerabilities in public issues, discussions, or
pull requests.** Details posted publicly cannot be treated as confidential.

Use **GitHub private vulnerability reporting**:

1. On this repository, open **Settings → Security** (or the "Report a
   vulnerability" button on the repo's Security page) and start a report.
2. Fill in the draft: what you found, how to reproduce it, the impact, and
   any known mitigation.
3. The report is visible only to the maintainer until a security advisory is
   published — nothing you send is exposed through the issue tracker.

If the private reporting form is unavailable when you need it, contact
`@brandenk514` first **without any sensitive details** to arrange a private
channel, and share the report only through that channel.

Where possible, include:

- the Mediarr version (from `GET /api/v1/version` or the image tag)
- the affected endpoint or code path
- a minimal reproduction (a request, a config, or a short trace)
- the impact you observed or believe

## Response SLA

| Commitment                                         | Timeframe                        |
|----------------------------------------------------|----------------------------------|
| Acknowledgment of a valid report                   | within **3 business days**       |
| Status updates (triage, plan, fix progress)        | at least every **7 days** until resolved |
| Resolution target: a fix, or a documented "won't fix / not a vulnerability" explanation | ideally within 30 days of triage |

These are targets, not guarantees. If one is missed, it will be said in the
report.

## Disclosure process

1. Triage the report against a local build.
2. Develop a fix (with a regression test where practical).
3. Ship the fix as a patch release.
4. Publish a GitHub Security Advisory describing the vulnerability, the
   affected versions, and the fixed version, and notify the reporter.

## Scope

**In scope** (owned by this repository):

- the `mediarr` Go service and its REST/WebSocket API
- the web UI (`ui/`)
- the container artifacts: `deploy/Dockerfile`, compose files, Helm chart
- the release and CI tooling in this repository

**Out of scope** (report to the upstream project):

- third-party indexers, metadata providers (TMDB/TVDB/MusicBrainz/
  OpenLibrary), and download clients (qBittorrent, SABnzbd, …) — bugs in
  those products belong to them
- bugs in dependencies — these flow in automatically via Dependabot and
  `govulncheck`; if you find one we missed, mention it in your private
  report
- your own deployment configuration (port exposure, key management, proxy
  setup, …)
