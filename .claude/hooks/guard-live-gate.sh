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
# Order is deliberate and load-bearing. Everything that could refuse a call is
# below the two scope checks, so a machine missing an interpreter, or a command
# this hook does not guard, or a repository that is not Eshu, is never touched.
# An earlier revision hard-failed on a missing `rg`/`python3` before any of
# those checks and would have locked the Bash tool out of every repository on a
# machine without them.
#
# Override a single call: prefix the command with CLAUDE_HOOK_ALLOW=1.
set -u

INPUT=$(cat)

# Degrade, never fail closed, on a missing interpreter — matching the sibling
# hooks. Without python3 the payload cannot be read, and a hook that cannot
# read its input has no basis to refuse anything.
command -v python3 >/dev/null 2>&1 || exit 0

CMD=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("command",""))' 2>/dev/null)
[ -z "${CMD:-}" ] && exit 0
case "$CMD" in *CLAUDE_HOOK_ALLOW=1*) exit 0;; esac

# Is this even a command this hook guards? Matched with bash globbing rather
# than rg, so the hook carries no dependency beyond python3.
case "$CMD" in
  *"make pre-pr"*|*"make pre-pr-full"*|*verify-golden-corpus-gate*) ;;
  *) exit 0;;
esac

# Only guard inside an Eshu checkout. At user level this hook sees every Bash
# call on the machine, and another project's `make pre-pr` is none of its
# business. Port 15432 and the ci-gates binary are Eshu's.
CWD=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("cwd",""))' 2>/dev/null)
[ -n "${CWD:-}" ] || CWD="$PWD"
d="$CWD"
in_eshu=1
while [ -n "$d" ] && [ "$d" != "/" ] && [ "$d" != "." ]; do
  if [ -e "$d/.agents/skills/eshu-code-review" ]; then in_eshu=0; break; fi
  d="$(dirname "$d")"
done
[ "$in_eshu" -eq 0 ] || exit 0

# Two kinds of signal, and each is waived only by the thing that invalidates it.
#
# `pgrep -x ci-gates` catches the FAST-GATE phase, and only that phase. The live
# lane is scripts/verify-golden-corpus-gate.sh, invoked directly by
# scripts/dev/pre-pr.sh; it contains no ci-gates reference and spawns no such
# process. So while the live lane runs -- the phase that actually binds these
# ports -- this check matches nothing. An earlier revision claimed the process
# check covered the ports the hook could not see. It does not, and the port
# probes below are what make that claim true instead.
#
# Each default port is probed unless the command overrides THAT variable. A
# caller who moved Postgres has not moved Bolt, so the Bolt probe still applies
# and a second gate started with only ESHU_POSTGRES_PORT is still caught. Per
# port rather than all-or-nothing, because a blanket waiver on one override is
# how the residual collision got in.
#
# Prometheus 19090 and Ask 19191 are deliberately omitted: they are mock
# providers rather than the contended backend, and every probe costs an lsof on
# a hook that runs on every Bash call.
BUSY=""
pgrep -x ci-gates >/dev/null 2>&1 && BUSY="a ci-gates run is already in flight"

if [ -z "$BUSY" ] && command -v lsof >/dev/null 2>&1; then
  for pair in \
    "ESHU_POSTGRES_PORT 15432" \
    "NEO4J_BOLT_PORT 7687" \
    "NEO4J_HTTP_PORT 7474" \
    "GATE_API_PORT 18080" \
    "GATE_MCP_PORT 18091"
  do
    var="${pair%% *}"
    port="${pair##* }"
    case "$CMD" in
      *"$var"=*) continue;;
    esac
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      BUSY="port $port is already bound ($var not overridden, so a live stack is up)"
      break
    fi
  done
fi
[ -z "$BUSY" ] && exit 0

echo "BLOCKED: $BUSY. A second live gate now would contend for the same fixed ports and CPU, and any failure it produced would not be attributable to your diff. Wait for the running gate to finish, or move every probed port: ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 NEO4J_HTTP_PORT=7575 GATE_API_PORT=18081 GATE_MCP_PORT=18092 (all five -- each probe is waived only by its own variable, so moving a subset still trips the rest). One-off override: prefix CLAUDE_HOOK_ALLOW=1." >&2
exit 2
