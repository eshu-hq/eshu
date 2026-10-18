#!/usr/bin/env bash
#
# test-verify-agent-canon-pipes.sh — static self-guard for the agent-canon
# scripts' assertion idiom (#8278).
#
# Feeding a variable to quiet ripgrep through a pipe is racy under
# `set -o pipefail`: the quiet reader exits on first match without draining
# its stdin, the writer dies with SIGPIPE (141), and pipefail promotes the
# pipeline to failure, so every match reads as a miss. The canon suites run
# under pipefail (companions inherit it from their sourcing runner), so the
# piped form is banned in the files this guard scans. The herestring form is
# the replacement (proven 500/500 clean).
#
# Forms this guard must NOT flag (all drain stdin or use no pipe): quiet
# matches that read a FILE argument, and piped readers without quiet such as
# the -o, -n, and -c readers, or python3 and head.
#
# The banned line shapes are built from fragments below, never spelled out:
# the guard scans itself, and a literal banned line in this file (even in a
# comment or a seeded fixture) would trip it. Same precedent as the
# private-identifier lib cases, which build their tokens from fragments.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/.." && pwd)"

# Every script the agent-canon gate executes: the verifier, its test mirror
# (plus the two lib case files the mirror sources), the hook suites, and
# every companion they source. This guard itself is last: it must pass its
# own scan.
canon_scripts=(
  "scripts/verify-agent-canon.sh"
  "scripts/test-verify-agent-hygiene.sh"
  "scripts/lib/test-private-identifier-cases.sh"
  "scripts/lib/test-private-identifier-path-cases.sh"
  "scripts/test-agent-hooks.sh"
  "scripts/test-goal-continue-hook.sh"
  "scripts/test-goal-continue-hook-cases.sh"
  "scripts/test-goal-continue-hook-budget-cases.sh"
  "scripts/test-goal-refresh-hook.sh"
  "scripts/test-goal-refresh-hook-cases.sh"
  "scripts/test-goal-refresh-hook-guard-cases.sh"
  "scripts/test-goal-refresh-hook-parity-cases.sh"
  "scripts/test-goal-refresh-hook-hermetic-cases.sh"
  "scripts/test-goal-refresh-hook-atomic-consent-cases.sh"
  "scripts/test-goal-refresh-hook-oneline-consent-cases.sh"
  "scripts/test-goal-refresh-hook-skill-nudge-cases.sh"
  "scripts/test-muse-hooks.sh"
  "scripts/test-cursor-hooks.sh"
  "scripts/test-cursor-hooks-helper-cases.sh"
  "scripts/test-verify-agent-canon-pipes.sh"
)

# Fragment-built detector. stage1 finds a writer piped into the reader;
# stage2 keeps only the quiet invocations (short-flag bundle holding q, or
# the long quiet flag). Written this way the assembled pattern never appears
# as literal text in this file, so the self-scan below stays clean.
frag_writer='printf'
frag_reader='rg'
ws='[[:space:]]'
stage1_re="${frag_writer}[^|]*\\|${ws}*${frag_reader}${ws}"
quiet_re="[${ws}:]-[A-Za-z]*q[A-Za-z]*(${ws}|\$)|[${ws}:]--quiet(${ws}|\$)"

scan_hits() { # paths... -> prints file:line:content for banned lines
  local stage1_out
  stage1_out="$(rg -n --no-heading -e "$stage1_re" -- "$@" || true)"
  if [ -z "$stage1_out" ]; then
    return 0
  fi
  rg -e "$quiet_re" <<<"$stage1_out" || true
}

pass=0
fail=0
ok() { printf 'ok - %s\n' "$1"; pass=$((pass + 1)); }
no() { printf 'NOT OK - %s\n' "$1"; fail=$((fail + 1)); }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# A listed script that no longer exists is drift, not a pass.
missing=0
for rel in "${canon_scripts[@]}"; do
  if [ ! -f "$repo_root/$rel" ]; then
    no "canon scan target missing: $rel"
    missing=1
  fi
done
if [ "$missing" -eq 0 ]; then
  ok "all canon scan targets exist"
fi

# Seeded RED: the detector must fire on the banned shapes, in both the short
# and long quiet spellings. Built from fragments so this file never carries
# a literal banned line. A detector that cannot see a planted violation is
# vacuous, so this case failing means the guard below proves nothing.
bar='|'
printf '%s\n' \
  "${frag_writer} '%s' \"\$V\" ${bar} ${frag_reader} -q 'pat'" \
  "${frag_writer} '%s' \"\$V\" ${bar} ${frag_reader} --quiet 'pat'" \
  >"$tmp/seed-banned.sh"
seed_hits="$(scan_hits "$tmp/seed-banned.sh")"
seed_count="$(rg -c '' <<<"$seed_hits" || true)"
if [ "$seed_count" -eq 2 ]; then
  ok "detector flags a planted piped-to-quiet violation (short and long form)"
else
  no "detector missed a planted piped-to-quiet violation -- the guard is vacuous"
fi

# Seeded GREEN: the safe shapes must stay silent. A file-argument quiet
# match uses no pipe, and the -o reader drains stdin, so neither is racy.
printf '%s\n' \
  "${frag_reader} -q 'pat' -- \"\$file\"" \
  "${frag_writer} '%s' \"\$V\" ${bar} ${frag_reader} -o 'pat'" \
  >"$tmp/seed-safe.sh"
if [ -z "$(scan_hits "$tmp/seed-safe.sh")" ]; then
  ok "detector stays silent on the safe shapes (file argument, draining reader)"
else
  no "detector false-positives on a safe shape -- the guard would cry wolf"
fi

# The real scan: no banned line in any canon script, including this guard.
targets=()
for rel in "${canon_scripts[@]}"; do
  [ -f "$repo_root/$rel" ] && targets+=("$repo_root/$rel")
done
real_hits="$(scan_hits "${targets[@]}")"
if [ -z "$real_hits" ]; then
  ok "no piped-to-quiet assertion remains in the canon scripts"
else
  no "piped-to-quiet assertion reappeared in the canon scripts:"
  printf '%s\n' "$real_hits" >&2
fi

printf '\nagent-canon pipe-idiom guard: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
