#!/usr/bin/env bash
#
# test-agent-hooks.sh — behavioural mirror for .claude/hooks/.
#
# verify-agent-canon.sh proves every skill is *reachable* from the nudge table.
# It cannot prove the arms route to the right skill, and both mis-routes found
# when this table moved into the repo were ones a reachability check passes:
# security-scan.yml and verify-telemetry-coverage.sh each matched a broad arm
# sitting above the specific one.
#
# Two properties are asserted that a text-only check would miss:
#   - the EXIT CODE. skill-nudge blocks (2) rather than suggesting (0). A test
#     that only grepped the message would pass against the advisory version
#     this replaced, which is the whole behaviour under change.
#   - arm ORDER. bash `case` takes the first match, so a broad arm added above
#     a narrow one silently reintroduces the bug. The control cases below fail
#     if someone repairs a mis-route by widening a narrow arm instead of
#     ordering it.
#
# WHERE THE CONTENTION CASES ACTUALLY RUN. Three cases exercise guard-live-gate
# against a real bound port or a live gate process, and they skip when either is
# already present rather than fight a real backend. Under `make pre-pr` the gate
# runner is alive the whole time this mirror executes, so all three skip on
# every local run and only ever assert in CI, where verify-agent-hygiene.yml
# invokes the mirror with no runner as its parent. A green local run therefore
# does NOT mean the guard's detection was exercised. Skips are counted and
# reported separately for that reason: a summary that folded them into the pass
# count would read identically whether they ran or not.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
hooks_dir="$repo_root/.claude/hooks"
passed=0
failed=0
skipped=0
sid=0

# Every marker this suite writes is namespaced by pid so a concurrent run, or a
# developer's real session, cannot collide with it. Cleared on the way out.
run_tag="t$$"
cleanup() {
  rm -f "/tmp/claude-skill-loaded-${run_tag}"* \
        "/tmp/claude-skill-override-${run_tag}"* \
        "/tmp/claude-skill-payload-${run_tag}"* 2>/dev/null
  # The port listener is reaped here as well as at each case's end. It holds
  # its socket for an hour, so a run that dies between start_port_listener and
  # stop_port_listener -- a signal, an external kill, a harness timeout --
  # leaves 15432 or 7687 bound. The next run would then take the skip branch,
  # print "a gate or 15432 is already in use", assert nothing, and still report
  # success: the same silent-skip shape the listener rewrite exists to remove.
  # A stray hold on 7687 is worse than cosmetic, since that is the Bolt port
  # the live gate contends on.
  stop_port_listener
}
trap cleanup EXIT

# --- binding a port for the contention cases ---------------------------------
#
# These two cases need a port to be LISTENING; they do not need it to speak
# HTTP. They used `python3 -m http.server <port> --bind 127.0.0.1`, which stops
# working the moment the interpreter on PATH does not reach its own bind. On
# this repo's macOS toolchain (Homebrew CPython 3.14.7) it never binds: no
# listener appears within a 5s lsof poll and nothing is written to stderr.
# /usr/bin/python3 (3.9.6) binds the same port in ~300ms and a raw
# socket.bind under 3.14 binds in ~200ms, so it is http.server specifically.
#
# The consequence was the worst shape a mirror can have. The port stayed free,
# guard-live-gate correctly found nothing to contend with and exited 0, and the
# mirror reported "bound 15432 should block(2)" — blaming the hook for the
# listener's failure. A raw socket removes the dependency, and the wait loop
# below removes the fixed sleep that hid it.
#
# start_port_listener <port> — binds 127.0.0.1:<port>, holds it until killed,
# and sets port_listener_pid. Backgrounded from THIS shell, not a command
# substitution, so the pid stays a child this script can `wait` on.
port_listener_pid=""
start_port_listener() {
  python3 -c 'import socket, sys, time
sock = socket.socket()
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(("127.0.0.1", int(sys.argv[1])))
sock.listen(1)
while True:
    time.sleep(3600)
' "$1" >/dev/null 2>&1 &
  port_listener_pid=$!
}

# wait_for_bind <port> — 0 once the port is listening, 2 when lsof is missing,
# 1 after ~3s of it never arriving. A bounded poll rather than a fixed sleep, so
# a listener that is merely slow does not fail the case and one that never
# arrives is reported as itself.
#
# The lsof arm is separate on purpose. Without it this function fails
# identically whether the bind failed or the tool that observes binds is absent,
# and the caller's message asserts the first -- misattributing a tooling gap as
# a bind failure, which is one level up from the misdiagnosis this whole
# rewrite removes. lsof is /usr/sbin/lsof on macOS, which is not on every
# harness's PATH.
wait_for_bind() {
  local port="$1" i
  command -v lsof >/dev/null 2>&1 || return 2
  for ((i = 0; i < 30; i++)); do
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  return 1
}

# stop_port_listener — kill and reap whatever start_port_listener left running.
stop_port_listener() {
  [ -n "$port_listener_pid" ] || return 0
  kill "$port_listener_pid" 2>/dev/null
  wait "$port_listener_pid" 2>/dev/null
  port_listener_pid=""
}

payload() { printf '{"session_id":"%s","tool_input":{"file_path":"%s"}}' "$1" "$2"; }

# blocks <repo-relative path> <expected skill id>
# Paths resolve against the real checkout on purpose: the hook refuses to act
# outside an Eshu tree, so a synthetic /r/... path would make every case pass by
# exiting early rather than by routing correctly.
blocks() {
  sid=$((sid + 1))
  local path="$repo_root/$1" want="$2" out rc
  out=$(payload "${run_tag}${sid}" "$path" | bash "$hooks_dir/skill-nudge.sh" 2>&1)
  rc=$?
  if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq -- "$want"; then
    printf 'ok - %s blocks, naming %s\n' "$1" "$want"
    passed=$((passed + 1))
  else
    printf 'FAIL - %s should block(2) naming %s; exit=%s out=%s\n' \
      "$1" "$want" "$rc" "${out:-<empty>}" >&2
    failed=$((failed + 1))
  fi
}

# quiet <repo-relative path> — no arm claims it, so it must pass silently.
quiet() {
  sid=$((sid + 1))
  local path="$repo_root/$1" out rc
  out=$(payload "${run_tag}${sid}" "$path" | bash "$hooks_dir/skill-nudge.sh" 2>&1)
  rc=$?
  if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
    printf 'ok - %s passes silently\n' "$1"
    passed=$((passed + 1))
  else
    printf 'FAIL - %s should pass silently; exit=%s out=%s\n' "$1" "$rc" "$out" >&2
    failed=$((failed + 1))
  fi
}

# --- routing: each governed surface blocks, naming the right skill -----------
# Regressions: these two routed to generator-script-discipline before the
# narrow arms were ordered above the broad workflow/verifier arms.
blocks '.github/workflows/security-scan.yml' 'eshu-security-scan-gates'
blocks 'scripts/verify-telemetry-coverage.sh' 'telemetry-coverage-discipline'

# Controls: the broad arms must still catch what they legitimately own. These
# flip to FAIL if a future mis-route is "fixed" by widening a narrow arm.
blocks 'scripts/verify-openapi.sh' 'generator-script-discipline'
blocks '.github/workflows/test.yml' 'generator-script-discipline'

blocks 'go/internal/telemetry/instruments.go' 'telemetry-coverage-discipline'
blocks 'go/internal/queue/claim.go' 'concurrency-deadlock-rigor'
blocks 'go/go.mod' 'eshu-security-scan-gates'
blocks 'docs/internal/remote-validation/x.md' 'eshu-performance-rigor'
blocks 'go/internal/storage/postgres/q.sql' 'eshu-postgres-rigor'
blocks 'go/internal/reducer/run.go' 'eshu-correlation-truth'
blocks 'go/internal/query/handler.go' 'eshu-mcp-call-rigor'

# The *.go fallback. Nothing above claims go/cmd/ci-gates, so before it existed
# these produced no nudge at all.
blocks 'go/cmd/ci-gates/await.go' 'golang-engineering'
blocks 'go/cmd/api/main.go' 'golang-engineering'
blocks 'go/internal/parser/parse.go' 'golang-engineering'

# Placement guards. The *.go fallback must stay BELOW the specialist arms; each
# of these flips to golang-engineering if someone reorders the case.
blocks 'go/internal/collector/doc.go' 'eshu-folder-doc-keeper'
blocks 'go/internal/telemetry/contract_x.go' 'telemetry-coverage-discipline'
blocks 'go/internal/reducer/project.go' 'eshu-correlation-truth'

# --- surfaces nobody claims stay out of the way ------------------------------
quiet 'README-not-a-package.txt'
quiet 'deploy/values.yaml'

# --- the requirement is LOADING, not acknowledgement -------------------------
# The block lifts only when the skill is recorded as loaded. Without this case
# the suite would pass against a hook that blocked once and then let anything
# through, which is acknowledgement theatre rather than enforcement.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-loaded-${probe_sid}-golang-engineering"
out=$(payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - a loaded skill lifts the block\n'
  passed=$((passed + 1))
else
  printf 'FAIL - loaded skill should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# Retrying WITHOUT loading must be refused again. This is the difference
# between requiring a load and requiring a shrug.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" >/dev/null 2>&1
first=$?
payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" >/dev/null 2>&1
second=$?
if [ "$first" -eq 2 ] && [ "$second" -eq 2 ]; then
  printf 'ok - retrying without loading is refused again\n'
  passed=$((passed + 1))
else
  printf 'FAIL - unloaded retry should stay blocked; got %s then %s\n' \
    "$first" "$second" >&2
  failed=$((failed + 1))
fi

# A multi-skill arm must require ALL of them. Loading one is not enough.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-loaded-${probe_sid}-concurrency-deadlock-rigor"
out=$(payload "$probe_sid" "$repo_root/go/internal/queue/claim.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq 'eshu-postgres-rigor' \
   && ! printf '%s' "$out" | rg -Fq 'concurrency-deadlock-rigor'; then
  printf 'ok - a partially satisfied arm still blocks, naming only what is missing\n'
  passed=$((passed + 1))
else
  printf 'FAIL - partial load should block naming only the missing skill; exit=%s out=%s\n' \
    "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# The escape hatch, for a skill that genuinely cannot be loaded.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-override-${probe_sid}"
out=$(payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - the override marker lets an unloadable surface through\n'
  passed=$((passed + 1))
else
  printf 'FAIL - override should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# --- the repo guard ----------------------------------------------------------
# At user level this hook sits in front of every repository on the machine, so
# a Go file outside an Eshu checkout must be untouched. This case is also why
# the paths above resolve against the real checkout: synthetic ones would exit
# here and every assertion would pass for the wrong reason.
sid=$((sid + 1))
out=$(payload "${run_tag}${sid}" "/tmp/not-eshu/go/cmd/x/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - a Go file outside an Eshu checkout is untouched\n'
  passed=$((passed + 1))
else
  printf 'FAIL - outside-repo path should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# --- skill-loaded.sh records what was loaded ---------------------------------
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
printf '{"session_id":"%s","tool_input":{"skill":"superpowers:brainstorming"}}' "$probe_sid" \
  | bash "$hooks_dir/skill-loaded.sh" >/dev/null 2>&1
if [ -f "/tmp/claude-skill-loaded-${probe_sid}-brainstorming" ]; then
  printf 'ok - skill-loaded records a load and strips the plugin prefix\n'
  passed=$((passed + 1))
else
  printf 'FAIL - skill-loaded did not record superpowers:brainstorming\n' >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
printf '{"session_id":"%s","tool_input":{}}' "$probe_sid" \
  | bash "$hooks_dir/skill-loaded.sh" >/dev/null 2>&1
rc=$?
if [ "$rc" -eq 0 ]; then
  printf 'ok - skill-loaded never blocks, even on an unexpected payload\n'
  passed=$((passed + 1))
else
  printf 'FAIL - skill-loaded must always exit 0; got %s\n' "$rc" >&2
  failed=$((failed + 1))
fi

# --- guard-live-gate ---------------------------------------------------------
# This is the ONE pass-through case that reaches the port probes, so it is the
# only one needing a live-backend skip. Measured with Bolt bound: this case
# returns 2, while CLAUDE_HOOK_ALLOW (early exit) and `go test` (not a guarded
# command) both still return 0. Guarding those two as well would be cargo.
#
# The skip matters because a developer box with a live stack is exactly the
# machine this guard was written for -- without it the mirror red-lines during
# `make pre-pr` for the wrong reason, on the one machine most likely to run it.
sid=$((sid + 1))
probed_bound=""
# The process check belongs in the skip predicate too, not just the ports. This
# mirror is itself run by the gate runner during `make pre-pr`, so a `ci-gates`
# process can be alive while the case executes -- and the guard blocks on that
# unconditionally, by design. Verified directly: with ci-gates alive, this exact
# payload returns 2.
#
# It has not actually red-lined a run: two consecutive `make pre-pr` runs passed
# this case, so the timing currently works out. That is the point. The case was
# depending on an accident of when the runner happens to be alive, and a skip
# predicate that only knows about ports cannot see the signal most likely to
# fire here.
if pgrep -x ci-gates >/dev/null 2>&1; then
  probed_bound="ci-gates process"
else
  for p in 15432 7687 7474 18080 18091; do
    lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1 && { probed_bound="port $p"; break; }
  done
fi
if [ -n "$probed_bound" ]; then
  printf 'skip - override pass-through: %s in flight\n' "$probed_bound"
  skipped=$((skipped + 1))
else
  out=$(printf '{"tool_input":{"command":"ESHU_POSTGRES_PORT=15532 make pre-pr"}}' \
    | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
  rc=$?
  if [ "$rc" -eq 0 ]; then
    printf 'ok - explicit port override is allowed through\n'
    passed=$((passed + 1))
  else
    printf 'FAIL - port-override run should pass; exit=%s out=%s\n' "$rc" "$out" >&2
    failed=$((failed + 1))
  fi
fi

sid=$((sid + 1))
out=$(printf '{"tool_input":{"command":"CLAUDE_HOOK_ALLOW=1 make pre-pr"}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ]; then
  printf 'ok - CLAUDE_HOOK_ALLOW=1 escape hatch works\n'
  passed=$((passed + 1))
else
  printf 'FAIL - override should pass; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"tool_input":{"command":"go test ./..."}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - unrelated command is untouched\n'
  passed=$((passed + 1))
else
  printf 'FAIL - unrelated command should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"cwd":"/tmp/not-eshu","tool_input":{"command":"make pre-pr"}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - live-gate guard ignores make pre-pr outside an Eshu checkout\n'
  passed=$((passed + 1))
else
  printf 'FAIL - outside-repo make pre-pr should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# --- the guard's BLOCKING path, which is its whole reason to exist -----------
# Both detection branches are exercised hermetically. Neither needs a real
# gate: a typo in the lsof flags or pgrep -x becoming pgrep -f would silently
# disable detection, and every pass-through case above would still be green.

# pgrep branch: a process whose name is literally ci-gates. `sleep` symlinked
# under that name satisfies `pgrep -x` without running anything.
sid=$((sid + 1))
fakebin="$(mktemp -d)"
ln -s "$(command -v sleep)" "$fakebin/ci-gates" 2>/dev/null
"$fakebin/ci-gates" 30 &
fake_pid=$!
sleep 0.3
out=$(printf '{"cwd":"%s","tool_input":{"command":"make pre-pr"}}' "$repo_root" \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
kill "$fake_pid" 2>/dev/null
wait "$fake_pid" 2>/dev/null
rm -rf "$fakebin"
if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq 'ci-gates run is already in flight'; then
  printf 'ok - a running ci-gates process blocks the gate\n'
  passed=$((passed + 1))
else
  printf 'FAIL - running ci-gates should block(2); exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# A running gate blocks even with a full port override: the second run still
# contends for CPU, and an earlier revision let any override skip this check.
sid=$((sid + 1))
fakebin="$(mktemp -d)"
ln -s "$(command -v sleep)" "$fakebin/ci-gates" 2>/dev/null
"$fakebin/ci-gates" 30 &
fake_pid=$!
sleep 0.3
out=$(printf '{"cwd":"%s","tool_input":{"command":"ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 make pre-pr"}}' "$repo_root" \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
kill "$fake_pid" 2>/dev/null
wait "$fake_pid" 2>/dev/null
rm -rf "$fakebin"
if [ "$rc" -eq 2 ]; then
  printf 'ok - a port override does not waive the running-process check\n'
  passed=$((passed + 1))
else
  printf 'FAIL - override must not bypass the process check; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# The residual collision. A Postgres-only override waives the 15432 probe, but
# the live lane also binds Bolt, HTTP, API and MCP, and `pgrep -x ci-gates`
# matches nothing while that lane runs (it is driven straight from pre-pr.sh and
# spawns no ci-gates process). Before the per-port waiver, a second gate started
# this way was caught by neither signal.
sid=$((sid + 1))
if pgrep -x ci-gates >/dev/null 2>&1 || lsof -nP -iTCP:7687 -sTCP:LISTEN >/dev/null 2>&1; then
  printf 'skip - bolt-bound case: a gate or 7687 is already in use\n'
  skipped=$((skipped + 1))
else
  start_port_listener 7687
  wait_for_bind 7687
  bind_rc=$?
  if [ "$bind_rc" -ne 0 ]; then
    stop_port_listener
    if [ "$bind_rc" -eq 2 ]; then
      printf 'FAIL - lsof is not on PATH, so this case could not observe port 7687 at all; install it or add /usr/sbin (not a guard-live-gate defect)\n' >&2
    else
      printf 'FAIL - the listener never bound 7687, so this case never ran; the mirror could not set it up (not a guard-live-gate defect)\n' >&2
    fi
    failed=$((failed + 1))
  else
    out=$(printf '{"cwd":"%s","tool_input":{"command":"ESHU_POSTGRES_PORT=15532 make pre-pr"}}' "$repo_root" \
      | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
    rc=$?
    stop_port_listener
    if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq '7687'; then
      printf 'ok - a Postgres-only override still blocks when Bolt is bound\n'
      passed=$((passed + 1))
    else
      printf 'FAIL - Postgres-only override should not waive the Bolt probe; exit=%s out=%s\n' \
        "$rc" "$out" >&2
      failed=$((failed + 1))
    fi
  fi
fi

# lsof branch: bind 15432 briefly. Skipped rather than failed if something is
# already listening, since that is a real backend and not this suite's to kill.
sid=$((sid + 1))
if pgrep -x ci-gates >/dev/null 2>&1 || lsof -nP -iTCP:15432 -sTCP:LISTEN >/dev/null 2>&1; then
  printf 'skip - port-bound case: a gate or 15432 is already in use\n'
  skipped=$((skipped + 1))
else
  start_port_listener 15432
  wait_for_bind 15432
  bind_rc=$?
  if [ "$bind_rc" -ne 0 ]; then
    stop_port_listener
    if [ "$bind_rc" -eq 2 ]; then
      printf 'FAIL - lsof is not on PATH, so this case could not observe port 15432 at all; install it or add /usr/sbin (not a guard-live-gate defect)\n' >&2
    else
      printf 'FAIL - the listener never bound 15432, so this case never ran; the mirror could not set it up (not a guard-live-gate defect)\n' >&2
    fi
    failed=$((failed + 1))
  else
    out=$(printf '{"cwd":"%s","tool_input":{"command":"make pre-pr"}}' "$repo_root" \
      | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
    rc=$?
    stop_port_listener
    if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq '15432'; then
      printf 'ok - a bound port 15432 blocks the gate\n'
      passed=$((passed + 1))
    else
      printf 'FAIL - bound 15432 should block(2); exit=%s out=%s\n' "$rc" "$out" >&2
      failed=$((failed + 1))
    fi
  fi
fi

# A missing interpreter must DEGRADE, not fail closed. An earlier revision
# hard-blocked on absent rg/python3 before any scope check, so on a machine
# without them every Bash call in every repository was refused -- the exact
# interference this hook claims not to cause. PATH is emptied to simulate it.
# Build a PATH that provably lacks python3 instead of naming a directory and
# hoping. Two earlier attempts got this wrong in opposite directions:
# PATH=/nonexistent also removed `cat`, so the hook exited before reaching the
# guard and the case passed without testing it; PATH=/bin works on macOS but
# not on the Linux runner, where /bin is merged into /usr/bin and python3 is
# right there — one case then failed in CI and the other passed for the wrong
# reason. A directory holding only the binaries the hook needs before its guard
# is deterministic on both.
nopy="$(mktemp -d)"
ln -s "$(command -v cat)" "$nopy/cat" 2>/dev/null
if command -v python3 >/dev/null 2>&1 && PATH="$nopy" command -v python3 >/dev/null 2>&1; then
  printf 'FAIL - no-python3 PATH fixture still resolves python3; cases below would be vacuous\n' >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"cwd":"%s","tool_input":{"command":"make pre-pr"}}' "$repo_root" \
  | PATH="$nopy" /bin/bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - guard-live-gate degrades silently without python3\n'
  passed=$((passed + 1))
else
  printf 'FAIL - missing interpreter must not block or print; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# Same for on-compact, the last of the four to get the guard.
sid=$((sid + 1))
out=$(printf '{"cwd":"%s"}' "$repo_root" \
  | PATH="$nopy" /bin/bash "$hooks_dir/on-compact.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - on-compact degrades silently without python3\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact must not print without python3; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi
rm -rf "$nopy"

# --- settings integration ----------------------------------------------------
# Every hook this suite exercises directly must also be REGISTERED, or the
# behaviour is correct and unreachable. The blocking nudge without its recorder
# is the worst shape: the agent is refused, loads the skill, no marker is
# written, and it stays refused until it finds the override. Calling the
# recorder directly -- as the cases above do -- cannot see that.
# Assert the full (event, matcher, command) triple, not that the filename
# appears somewhere in the file. A bare substring match is satisfied by a hook
# wired to the wrong event: move skill-loaded.sh to PreToolUse/Edit and the
# recorder fires on edits instead of skill loads, so the nudge never lifts --
# unreachable behaviour, which is the failure this block claims to catch.
sid=$((sid + 1))
settings="$repo_root/.claude/settings.json"
wiring_report() { python3 -c '
import json, sys
hooks = json.load(open(sys.argv[1])).get("hooks", {})
want = [
    ("PostToolUse",  "Skill",                   "skill-loaded.sh"),
    ("PreToolUse",   "Edit|MultiEdit|Write",    "skill-nudge.sh"),
    ("PreToolUse",   "Bash",                    "guard-live-gate.sh"),
    ("SessionStart", "compact|resume",          "on-compact.sh"),
    # Stop groups carry no matcher, so the registration stores no "matcher"
    # key and group.get("matcher") is None. Comparing against None is the
    # assertion, not a placeholder for one.
    ("Stop",         None,                      "goal-continue.sh"),
    # The producer/refresher. Without this triple a dropped registration
    # would leave the goal never restated and the Stop hook never fed --
    # correct and unreachable, the same way the Stop triple was missing.
    ("UserPromptSubmit", None,                  "goal-refresh.sh"),
]
bad = []
for event, matcher, name in want:
    ok = any(
        group.get("matcher") == matcher and name in hook.get("command", "")
        for group in hooks.get(event, [])
        for hook in group.get("hooks", [])
    )
    if not ok:
        bad.append("%s[%s]->%s" % (event, matcher, name))
print(" ".join(bad))
' "$1" 2>/dev/null; }

missing_reg=$(wiring_report "$settings")
if [ -z "$missing_reg" ]; then
  printf 'ok - every hook is wired to the right event and matcher\n'
  passed=$((passed + 1))
else
  printf 'FAIL - hook wiring wrong or missing: %s\n' "$missing_reg" >&2
  failed=$((failed + 1))
fi

# ...and the check must actually catch a wrong wiring. Without this case it
# only ever runs against a correct file and would pass if it were a no-op --
# which is precisely how the substring version it replaced went unnoticed.
sid=$((sid + 1))
bad_settings="$(mktemp)"
python3 -c '
import json, sys
cfg = json.load(open(sys.argv[1]))
# Move the recorder to the wrong event: it would then fire on edits rather
# than on skill loads, and the nudge would never lift.
post = cfg["hooks"].get("PostToolUse", [])
cfg["hooks"]["PostToolUse"] = [
    g for g in post
    if not any("skill-loaded.sh" in h.get("command", "") for h in g.get("hooks", []))
]
cfg["hooks"].setdefault("PreToolUse", []).append(
    {"matcher": "Edit", "hooks": [{"type": "command", "command": "x/skill-loaded.sh"}]})
json.dump(cfg, open(sys.argv[2], "w"))
' "$settings" "$bad_settings" 2>/dev/null
caught=$(wiring_report "$bad_settings")
rm -f "$bad_settings"
if printf '%s' "$caught" | rg -Fq 'skill-loaded.sh'; then
  printf 'ok - the wiring check catches a hook moved to the wrong event\n'
  passed=$((passed + 1))
else
  printf 'FAIL - wiring check missed a misplaced hook; report was: %s\n' "${caught:-<empty>}" >&2
  failed=$((failed + 1))
fi

# --- on-compact --------------------------------------------------------------
# Compaction must invalidate loaded-skill markers. A resume keeps the session
# id while discarding skill content, so a surviving marker makes the nudge wave
# through an edit whose skill is no longer in context.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-loaded-${probe_sid}-golang-engineering"
printf '{"session_id":"%s","cwd":"%s"}' "$probe_sid" "$repo_root" \
  | bash "$hooks_dir/on-compact.sh" >/dev/null 2>&1
if [ ! -f "/tmp/claude-skill-loaded-${probe_sid}-golang-engineering" ]; then
  printf 'ok - compaction clears this session loaded-skill markers\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact left a stale loaded-skill marker\n' >&2
  failed=$((failed + 1))
fi

# ...and only its own. A concurrent session's markers are not ours to drop.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
other_sid="${run_tag}other${sid}"
touch "/tmp/claude-skill-loaded-${other_sid}-golang-engineering"
printf '{"session_id":"%s","cwd":"%s"}' "$probe_sid" "$repo_root" \
  | bash "$hooks_dir/on-compact.sh" >/dev/null 2>&1
if [ -f "/tmp/claude-skill-loaded-${other_sid}-golang-engineering" ]; then
  printf 'ok - compaction leaves another session markers alone\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact deleted a marker belonging to another session\n' >&2
  failed=$((failed + 1))
fi
rm -f "/tmp/claude-skill-loaded-${other_sid}-golang-engineering"

sid=$((sid + 1))
out=$(printf '{}' | bash "$hooks_dir/on-compact.sh" 2>/dev/null)
if printf '%s' "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if "eshu-session-lifecycle" in d["hookSpecificOutput"]["additionalContext"] else 1)' 2>/dev/null; then
  printf 'ok - on-compact emits valid JSON naming eshu-session-lifecycle\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact output bad: %s\n' "${out:-<empty>}" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"cwd":"/tmp/not-eshu"}' | bash "$hooks_dir/on-compact.sh" 2>/dev/null)
if [ -z "$out" ]; then
  printf 'ok - on-compact says nothing outside an Eshu checkout\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact should be silent outside the repo; got: %s\n' "$out" >&2
  failed=$((failed + 1))
fi

# --- CI/local parity for the hook suites ------------------------------------
# The agent-canon gate's test_command chains four scripts; the workflow must run
# the same four. They drifted once already: two goal suites were chained locally
# with no workflow step, so 40-odd cases ran in promotion and NOWHERE in
# Actions. A passing test that never executes in CI is the most expensive kind
# of green, so assert the steps rather than trusting them to stay.
hygiene_wf="$repo_root/.github/workflows/verify-agent-hygiene.yml"
for suite in \
  test-verify-agent-hygiene.sh \
  test-agent-hooks.sh \
  test-goal-continue-hook.sh \
  test-goal-refresh-hook.sh; do
  if rg -Fq -- "scripts/$suite" "$hygiene_wf" 2>/dev/null; then
    printf 'ok - verify-agent-hygiene.yml runs %s\n' "$suite"
    passed=$((passed + 1))
  else
    printf 'FAIL - verify-agent-hygiene.yml has no step for %s (chained locally, absent in CI)\n' \
      "$suite" >&2
    failed=$((failed + 1))
  fi
done

# --- the mirrors' load sentinels must be the LAST line ----------------------
# Each mirror sources a companion holding most of its cases, and the companion
# sets a sentinel the parent asserts, so a companion that stops being sourced
# fails loudly instead of reporting a clean pass over a third of its suite.
#
# That only means "the companion ran to the end" while the sentinel IS the end.
# Three commits in a row appended cases BELOW it -- including the commit that
# introduced the sentinel, and the commits whose own regression cases then sat
# in the unprotected tail. Truncating there reported 54 passed, 0 failed, exit
# 0, with five assertions silently gone.
#
# A convention that has failed three times is not a convention. This asserts
# the position, so forgetting fails here rather than in whatever the tail
# happened to contain.
# Enumerated by glob, not by a hand-written list. A third companion was added
# one commit after this check landed, and a list would have silently not
# covered it -- the same shape as the check itself: correct when written, with
# nothing that fails when the world moves. The count floor is what stops a
# rename turning the glob into a vacuous zero-iteration pass.
companion_count=0
for companion_path in "$repo_root"/scripts/test-goal-*cases*.sh; do
  [ -f "$companion_path" ] || continue
  companion="$(basename "$companion_path")"
  companion_count=$((companion_count + 1))
  last_line="$(rg -v '^[[:space:]]*(#|$)' "$companion_path" 2>/dev/null | tail -1)"
  case "$last_line" in
    *_cases_loaded=1)
      printf 'ok - %s: the load sentinel is its last line\n' "$companion"
      passed=$((passed + 1))
      ;;
    *)
      printf 'FAIL - %s: the load sentinel is NOT the last line (last: %s) -- append ABOVE it\n' \
        "$companion" "$last_line" >&2
      failed=$((failed + 1))
      ;;
  esac
done
if [ "$companion_count" -ge 5 ]; then
  printf 'ok - the companion glob found %s files to check\n' "$companion_count"
  passed=$((passed + 1))
else
  printf 'FAIL - the companion glob found only %s file(s); a rename would make this check vacuous\n' \
    "$companion_count" >&2
  failed=$((failed + 1))
fi

printf '\nagent-hooks test mirror: %s passed, %s failed, %s skipped\n' \
  "$passed" "$failed" "$skipped"
[ "$failed" -eq 0 ] || exit 1
