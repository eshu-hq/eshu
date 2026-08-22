#!/bin/bash
# guard-live-gate.sh — PreToolUse hook for Bash. Blocks a second live gate from
# starting while one is already in flight.
#
# The live lanes bind fixed host ports and saturate the machine. Starting one
# while another gate or a heavy build is running produces a RED that is
# starvation, not a defect, and the run it kills is expensive: a real session
# lost a 936s run this way, drew two wrong conclusions from the failure, then
# passed the identical diff in 451s once the machine was quiet.
#
# Detect by the gate binary and the bound port, never by
# `pgrep -f "make pre-pr"` — that pattern has failed to match a live run and
# read as "the process died".
#
# Concurrency is allowed when it is deliberate. verify-golden-corpus-gate.sh
# defaults to Postgres 15432 / Bolt 7687 / HTTP 7474, and the repo defines
# alternate sets (15532, 15635, 15636) precisely so two gates can run at once.
# A command carrying its own ESHU_POSTGRES_PORT is opting into that and is not
# blocked.
#
# Override a single call: prefix the command with CLAUDE_HOOK_ALLOW=1.
set -u

INPUT=$(cat)
if ! command -v rg >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
  echo "BLOCKED: the Eshu agent hooks require rg and python3 on PATH. Install them and retry." >&2
  exit 2
fi

CMD=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("command",""))' 2>/dev/null)
[ -z "${CMD:-}" ] && exit 0
case "$CMD" in *CLAUDE_HOOK_ALLOW=1*) exit 0;; esac

printf '%s' "$CMD" | rg -q 'make (pre-pr|pre-pr-full)|verify-golden-corpus-gate' 2>/dev/null || exit 0

# An explicit port override means the caller is running a deliberately parallel
# stack. Let it through.
printf '%s' "$CMD" | rg -q 'ESHU_POSTGRES_PORT=' 2>/dev/null && exit 0

BUSY=""
pgrep -x ci-gates >/dev/null 2>&1 && BUSY="a ci-gates run is already in flight"
if [ -z "$BUSY" ] && command -v lsof >/dev/null 2>&1; then
  lsof -nP -iTCP:15432 -sTCP:LISTEN >/dev/null 2>&1 \
    && BUSY="port 15432 is already bound (a live backend is up)"
fi
[ -z "$BUSY" ] && exit 0

echo "BLOCKED: $BUSY. A second live gate now would contend for the same fixed ports and CPU, and any failure it produced would not be attributable to your diff. Wait for the running gate to finish, or run this one on an alternate port set (ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575). One-off override: prefix CLAUDE_HOOK_ALLOW=1." >&2
exit 2
