#!/usr/bin/env bash
# release-notes.sh — generate GitHub release notes for a tag (#48).
#
# The notes are built from the milestone's *closed issues*: every issue
# number referenced by a commit subject in the tag's range (since the
# previous tag, if one exists) is fetched from the repo and rendered in
# issue-number order, followed by the full commit list.
#
# Usage:
#   GITHUB_REPO=owner/repo bash release-notes.sh vX.Y.Z
#
# In CI (GitHub Actions) GITHUB_REPO is set automatically and GH_TOKEN is
# GITHUB_TOKEN. Locally, set GITHUB_REPO and rely on `gh auth` (or export
# GH_TOKEN from a file — not typed into a shell).

set -euo pipefail

TAG="${1:?usage: release-notes.sh <tag>}"
REPO="${GITHUB_REPO:?GITHUB_REPO must be set (e.g. brandenk514/Mediarr)}"
# --- config ------------------------------------------------------------------
# In CI, GH_TOKEN is provided by the workflow (GITHUB_TOKEN). Locally the
# script falls back to whatever `gh auth` already has — pass GH_TOKEN only
# when you want an explicit token (never through an interactive shell: the
# Hermes terminal redacts secret-looking strings, which breaks the call).
if [ -n "${GH_TOKEN:-}" ]; then
  export GH_TOKEN
else
  gh auth status >/dev/null 2>&1 || {
    echo "error: no GH_TOKEN and gh is not authenticated (gh auth login)" >&2
    exit 1
  }
fi

# --- tag range -------------------------------------------------------------
if ! git rev-parse --quiet --verify "${TAG}^{commit}" >/dev/null; then
  echo "error: tag $TAG not found (fetch full history: git fetch --tags)" >&2
  exit 1
fi

PREV="$(git describe --tags --abbrev=0 "${TAG}^" 2>/dev/null || true)"
if [ -n "$PREV" ]; then
  RANGE="${PREV}..${TAG}"
  RANGE_LABEL="${PREV} → ${TAG}"
else
  RANGE="${TAG}"
  RANGE_LABEL="initial release (${TAG})"
fi

# --- collect referenced issues ---------------------------------------------
# Issue numbers come from commit subjects ("feat(x): ... (#46)"), the same
# convention every M-milestone merge uses. Dedupe + numeric order.
mapfile -t ISSUES < <(
  git log --pretty=%s "$RANGE" \
    | grep -oE '#[0-9]+' \
    | tr -d '#' \
    | sort -n \
    | uniq
)

# --- render ------------------------------------------------------------------
echo "# ${TAG}"
echo
echo "_${RANGE_LABEL} — $(date -u +%Y-%m-%d) · commit \`$(git rev-parse --short "${TAG}^{commit}")\`_"
echo
echo "## Closed issues"
echo
if [ "${#ISSUES[@]}" -eq 0 ]; then
  echo "_No issue references in this range._"
else
  # One batched API call for all issues; join locally. (Per-issue calls in a
  # loop hit rate limits and make notes generation flaky.)
  gh api "repos/${REPO}/issues?state=all&per_page=100" \
    --jq '.[] | [.number, .title, .state] | @tsv' > /tmp/mediarr-issues.tsv
  TAB=$'	'
  for n in "${ISSUES[@]}"; do
    line="$(awk -F'	' -v n="$n" '$1 == n { print $2 "	" $3; exit }' /tmp/mediarr-issues.tsv)"
    if [ -z "$line" ]; then
      echo "* [#${n}](https://github.com/${REPO}/issues/${n}) — _(title unavailable)_"
      continue
    fi
    TITLE="${line%%"$TAB"*}"
    STATE="${line##*"$TAB"}"
    LINK="* [#${n}](https://github.com/${REPO}/issues/${n}) — ${TITLE}"
    if [ "$STATE" != "closed" ]; then
      LINK="${LINK} _(still ${STATE} — verify before closing the milestone)_"
    fi
    echo "$LINK"
  done
fi
echo
echo "## Commits"
echo
echo '```'
git log --pretty=format:'%h %s' "$RANGE"
echo
echo '```'
