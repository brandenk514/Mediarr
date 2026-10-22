#!/usr/bin/env bash
#
# push-gate.sh — the wall in front of the AI (article §4).
#
# Wired up as a Claude Code PreToolUse hook in .claude/settings.json: any
# Bash command containing `git push` is intercepted, the CI preflight runs,
# and the push is BLOCKED (exit 2) if any gate fails. The failure output is
# handed straight back to the agent, which is expected to fix the gates and
# retry. This makes the checks a wall, not an instruction: the agent cannot
# skip them even if a long session makes it forget the ask.
set -u

# The hook receives the tool call as JSON on stdin
# ({"tool_name": "Bash", "tool_input": {"command": ...}}).
input="$(cat 2>/dev/null || true)"
case "$input" in
  *"git push"*) : ;;   # looks like a push — gate it
  *) exit 0 ;;         # not a push — allow
esac

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT"

echo "=== push-gate: running CI preflight before allowing git push ==="
if docker info >/dev/null 2>&1; then
  scripts/ci-preflight.sh --full </dev/null
else
  scripts/ci-preflight.sh </dev/null
fi
status=$?

if [ "$status" -ne 0 ]; then
  cat >&2 <<'EOF'
BLOCKED: this `git push` would fail in CI. The preflight gates above failed.
Fix each failure, re-run `scripts/ci-preflight.sh` until it passes, then
push again.
EOF
  exit 2
fi
exit 0
