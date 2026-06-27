#!/usr/bin/env bash
#
# test-verify-agent-hygiene.sh — test mirror for verify-agent-canon.sh and
# verify-no-ai-attribution.sh. Exercises pass and fail cases against throwaway
# fixtures so the gates' behavior is pinned, mirroring the other verify gates.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$here/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok() { printf 'ok - %s\n' "$1"; pass=$((pass + 1)); }
no() { printf 'NOT OK - %s\n' "$1"; fail=$((fail + 1)); }

canon="$repo_root/scripts/verify-agent-canon.sh"
attr="$repo_root/scripts/verify-no-ai-attribution.sh"

# --- verify-agent-canon ---
mkdir -p "$tmp/good"
printf 'shared canon\n' >"$tmp/good/AGENTS.md"
printf 'shared canon\n' >"$tmp/good/CLAUDE.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/good" "$canon" >/dev/null 2>&1; then
  ok "agent-canon passes when AGENTS.md == CLAUDE.md"
else
  no "agent-canon should pass when identical"
fi

mkdir -p "$tmp/bad"
printf 'one\n' >"$tmp/bad/AGENTS.md"
printf 'two\n' >"$tmp/bad/CLAUDE.md"
if ESHU_AGENT_CANON_REPO_ROOT="$tmp/bad" "$canon" >/dev/null 2>&1; then
  no "agent-canon should fail on drift"
else
  ok "agent-canon fails when the two files drift"
fi

# --- verify-no-ai-attribution --message ---
printf 'feat: a clean message\n' >"$tmp/msg-clean"
if "$attr" --message "$tmp/msg-clean" >/dev/null 2>&1; then
  ok "attribution passes on a clean commit message"
else
  no "attribution should pass on a clean message"
fi

printf 'feat: x\n\nCo-authored-by: Claude <noreply@anthropic.com>\n' >"$tmp/msg-coauth"
if "$attr" --message "$tmp/msg-coauth" >/dev/null 2>&1; then
  no "attribution should fail on a Co-authored-by trailer"
else
  ok "attribution fails on a Co-authored-by trailer"
fi

printf 'feat: x\n\n🤖 Generated with Claude Code\n' >"$tmp/msg-robot"
if "$attr" --message "$tmp/msg-robot" >/dev/null 2>&1; then
  no "attribution should fail on the robot-emoji footer"
else
  ok "attribution fails on the robot-emoji footer"
fi

# Regression for the Codex P2 on #3988: a normal HUMAN Co-authored-by trailer
# (no AI tool / no anthropic address) must NOT be flagged — the rule is about
# AI attribution, and the repo already has human co-author trailers.
printf 'feat: x\n\nCo-authored-by: Jane Doe <jane@example.com>\n' >"$tmp/msg-human"
if "$attr" --message "$tmp/msg-human" >/dev/null 2>&1; then
  ok "attribution passes on a human Co-authored-by trailer"
else
  no "attribution should NOT flag a human Co-authored-by trailer"
fi

printf '\nagent-hygiene test mirror: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
