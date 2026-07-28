#!/usr/bin/env bash
# Static structural test for verify-golden-corpus-gate.sh. The verifier itself
# needs Docker + a built toolchain to run end to end (exercised by the
# golden-corpus-gate CI workflow), so this mirror validates the contract that
# cannot silently drift: the script parses, sets strict mode, drives every
# pipeline stage and drain, honours the B-13 shared_projection_intents gate, and
# leaks no private data.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-golden-corpus-gate.sh"
fixture_lib="${repo_root}/scripts/lib/golden-corpus-fixtures.sh"
workflow="${repo_root}/.github/workflows/golden-corpus-gate.yml"
snapshot="${repo_root}/testdata/golden/e2e-20repo-snapshot.json"
sql_drop_fixture="${repo_root}/tests/fixtures/ecosystems/sql_comprehensive/migrations/V2__drop_legacy_tables.sql"
ci_gates="${repo_root}/specs/ci-gates.v1.yaml"
prepr="${repo_root}/scripts/dev/pre-pr.sh"

fail() { printf 'test-verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-golden-corpus-gate.sh must be executable"
[[ -f "${fixture_lib}" ]] || fail "missing ${fixture_lib}"
[[ -f "${workflow}" ]] || fail "missing ${workflow}"
[[ -f "${snapshot}" ]] || fail "missing ${snapshot}"
[[ -f "${sql_drop_fixture}" ]] || fail "missing ${sql_drop_fixture}"
[[ -f "${ci_gates}" ]] || fail "missing ${ci_gates}"
[[ -f "${prepr}" ]] || fail "missing ${prepr}"

# The exactly-one-non-comment-home invariant, and everything that enforces it
# (set_comment_prefix, count_homes, require_in_text, require_in,
# require_in_region, require_matches), lives in the sourced matcher lib. Its
# header states what the comment rule does and does not handle; read it before
# adding an assertion. A syntax error there does not reliably abort a sourced
# caller, so it is parsed first.
matcher_lib="${repo_root}/scripts/lib/golden-corpus-mirror-matcher.sh"
[[ -f "${matcher_lib}" ]] || fail "missing matcher lib: ${matcher_lib}"
bash -n "${matcher_lib}" || fail "golden-corpus-mirror-matcher.sh has a syntax error"
# shellcheck source=scripts/lib/golden-corpus-mirror-matcher.sh
. "${matcher_lib}"

# #5596: the workflow's on.pull_request.paths filter must trigger this gate on
# every source dir whose changes can alter emitted facts or fact contracts the
# gate asserts. A dir missing here means a PR that touches only that dir ships
# a fact-emission change unverified by the golden corpus (a false-green).
# Anchored off comment lines for the same reason every other assertion is: a
# YAML `#` comment naming a path would otherwise satisfy the assertion while the
# real filter entry is gone.
require_workflow_path() {
	local label="$1" path_glob="$2"
	require_in "golden-corpus-gate.yml paths filter (${label})" "${workflow}" "- '${path_glob}'"
}
require_workflow_path "collector fact emission"        "go/internal/collector/**"
require_workflow_path "parser fact emission"           "go/internal/parser/**"
require_workflow_path "projector graph writes"         "go/internal/projector/**"
require_workflow_path "reducer graph writes"           "go/internal/reducer/**"
require_workflow_path "query response shapes"          "go/internal/query/**"
require_workflow_path "storage layer"                  "go/internal/storage/**"
require_workflow_path "relationship resolution (#5596)" "go/internal/relationships/**"
require_workflow_path "fact-kind schemas (#5596)"       "sdk/go/factschema/**"
# The fact-emitting command packages (service wiring that assembles the
# collectors/ingester/reducer/projector) must trigger the gate too — a change
# under go/cmd/collector-aws-cloud/service.go can alter emitted facts as much
# as go/internal/collector (#5686 review).
require_workflow_path "collector command wiring"       "go/cmd/collector-**"
require_workflow_path "bootstrap-index fact seeding"   "go/cmd/bootstrap-index/**"
require_workflow_path "ingester fact emission"         "go/cmd/ingester/**"
require_workflow_path "projector runtime"              "go/cmd/projector/**"
require_workflow_path "reducer runtime"                "go/cmd/reducer/**"
require_workflow_path "api query surface"              "go/cmd/api/**"
# The orchestrator sources these; an edit to the mutex or a fixture/timing lib
# changes what the gate does, so each must trigger it. Without this the lock
# itself was in no trigger list at all - its only test would never have run.
require_workflow_path "golden-corpus libs"             "scripts/lib/golden-corpus-*.sh"
require_workflow_path "live gate mutex"                "scripts/lib/live-gate-lock.sh"
# The workflow is only one of THREE lists that must carry these paths. Assert the
# other two as well, or "the gap cannot reopen" is true for a third of the gap.
# Scoped to each gate's OWN trigger block: a file-wide grep passes while the path
# is present in either gate, so deleting it from one silently un-selects that
# gate and the local half of the gap reopens.
for gate_id in golden-corpus-mirror golden-corpus-gate; do
	for lib_path in 'scripts/lib/golden-corpus-*.sh' 'scripts/lib/live-gate-lock.sh'; do
		require_in_region "ci-gates gate ${gate_id} trigger ${lib_path}" "${ci_gates}" \
			"/^  - id: ${gate_id}\$/,/^    local:/" "- \"${lib_path}\""
	done
done
# Anchored to the golden-corpus selector line, not the whole file: the fragment
# could otherwise be moved onto an unrelated selector and still pass. The
# leading `^(?!\s*#)` keeps a commented-out selector from standing in for the
# live one, and require_matches pins it to exactly one site.
require_matches "the pre-pr golden-corpus selector must match the golden-corpus libs and the mutex" \
	"${prepr}" \
	"^(?!\s*#)[^\n]*run_or_defer golden-corpus \\\\\n[^\n]*scripts/lib/\(golden-corpus-\.\+\|live-gate-lock\)"

# #5817 P2 review: verify-golden-corpus-gate.sh sources FOUR
# scripts/lib/golden-corpus-*.sh helper libs (fixtures, phase-timings,
# dead-code-fixtures, readiness). Before this, the paths filter had no
# scripts/lib/** entry at all, so a PR editing only one of those libs (e.g. a
# bash cleanup that inverts host_tcp_port_open's return value) would never
# trigger this gate -- a false green. Assert the glob entry is present, and
# that every golden-corpus lib file actually on disk still matches its naming
# convention, so a future rename or an added lib outside that convention
# reopens the gap loudly here instead of silently in CI. This mirror's own
# matcher lib takes the same golden-corpus-*.sh name deliberately: it inherits
# every trigger list below without a new path entry to keep in sync, at the cost
# of counting toward this floor.
require_workflow_path "golden-corpus lib helpers (#5817)" "scripts/lib/golden-corpus-*.sh"
shopt -s nullglob
golden_corpus_libs=("${repo_root}"/scripts/lib/golden-corpus-*.sh)
shopt -u nullglob
[[ "${#golden_corpus_libs[@]}" -ge 4 ]] \
	|| fail "expected at least 4 scripts/lib/golden-corpus-*.sh files (fixtures, phase-timings, dead-code-fixtures, readiness), found ${#golden_corpus_libs[@]}: ${golden_corpus_libs[*]-none}"

# Parses under bash -n.
bash -n "${script}" || fail "verify-golden-corpus-gate.sh has a syntax error"
# The lock cases live in sourced chunks. A syntax error in a sourced file does
# NOT reliably abort the caller, so without this the whole mutex suite can be
# silently skipped and the run still reports pass.
for sourced_case_lib in \
	"${repo_root}/scripts/lib/golden-corpus-lock-cases.sh" \
	"${repo_root}/scripts/lib/golden-corpus-lock-parse-cases.sh" \
	"${repo_root}/scripts/lib/golden-corpus-lock-race-cases.sh"; do
	[[ -f "${sourced_case_lib}" ]] || fail "missing lock case lib: ${sourced_case_lib}"
	bash -n "${sourced_case_lib}" || fail "$(basename "${sourced_case_lib}") has a syntax error"
done
bash -n "${fixture_lib}" || fail "golden-corpus-fixtures.sh has a syntax error"

# Skipping comment lines is load-bearing, not cosmetic: the orchestrator and its
# lib chunks name their own helpers, sourced libs and gate flags in the prose
# above each call site, so a whole-file fixed-string match is satisfied by that
# prose alone and deleting the real line goes undetected. That false-green was
# found five separate times over the #5426 review rounds. Four of the five at
# least failed loudly at runtime; the fifth did not -- deleting the
# `emit_phase_timings_and_flags` call would have left BOTH gates green with B-11
# phase-timing capture and baseline regression detection silently off, because
# the consumer splices `${phase_flags[@]+...}` and tolerates the array never
# being set under `set -u`.
#
# require: the invariant against the gate orchestrator (a .sh target, so `#`).
require() {
	require_in "$1" "${script}" "$2"
}

# require_region: the invariant against one block of the gate orchestrator.
require_region() {
	require_in_region "$1" "${script}" "$2" "$3"
}

# require_invocation asserts a helper from an extracted lib chunk is actually
# CALLED, not merely named. It is stricter than require: anchoring to a whole
# unindented line also rejects a mention inside a string, an assignment, or a
# nested conditional that never runs at top level. Use it for every bare helper
# call; use require for flags, lib sources, and message text that cannot be
# whole-line anchored. Exactly-one-home applies here too — two unindented calls
# to the same helper means deleting one leaves the assertion green.
require_invocation() {
	local label="$1" fn="$2" homes
	homes="$(rg --count -- "^${fn}\$" "${script}" || true)"
	case "${homes:-0}" in
		0) fail "missing ${label}: unindented call to ${fn}()" ;;
		1) return 0 ;;
	esac
	fail "ambiguous ${label}: ${homes} unindented calls to ${fn}(); the invariant wants exactly one"
}

# Strict mode and self-cleanup.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
# cleanup() and the host-binary helpers (build_bin/start_bg/pg) are extracted to
# sourced lib chunks to keep this orchestrator under the 500-line cap; the
# orchestrator must still source both.
require "cleanup lib source" "golden-corpus-cleanup.sh"
require "host helpers lib source" "golden-corpus-host-helpers.sh"
require "corpus staging lib source" "golden-corpus-stage.sh"
require_invocation "corpus staging invocation" "stage_minimal_corpus"
require "maintenance drains lib source" "golden-corpus-maintenance-drains.sh"
require_invocation "maintenance drains invocation" "run_maintenance_drain_cycles"
cleanup_lib="${repo_root}/scripts/lib/golden-corpus-cleanup.sh"
[[ -f "${cleanup_lib}" ]] || fail "missing cleanup lib: ${cleanup_lib}"
bash -n "${cleanup_lib}" || fail "cleanup lib has a syntax error"
host_helpers_lib="${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"
[[ -f "${host_helpers_lib}" ]] || fail "missing host helpers lib: ${host_helpers_lib}"
bash -n "${host_helpers_lib}" || fail "host helpers lib has a syntax error"
stage_lib="${repo_root}/scripts/lib/golden-corpus-stage.sh"
[[ -f "${stage_lib}" ]] || fail "missing corpus staging lib: ${stage_lib}"
bash -n "${stage_lib}" || fail "corpus staging lib has a syntax error"
maintenance_lib="${repo_root}/scripts/lib/golden-corpus-maintenance-drains.sh"
[[ -f "${maintenance_lib}" ]] || fail "missing maintenance drains lib: ${maintenance_lib}"
bash -n "${maintenance_lib}" || fail "maintenance drains lib has a syntax error"
# The convergence margin the three maintenance cycles buy (#5426) is the whole
# reason the loop is not two passes; a silent drop back to two would sit exactly
# on the boundary again. The loop lives in the extracted lib chunk now.
require_in "three-cycle maintenance loop" "${maintenance_lib}" 'for maintenance_pass in 1 2 3; do'
# Background pids must be recorded in the PARENT shell (printf -v), or the cleanup
# trap reaps nothing on a failure path and leaks host processes. The helper lives
# in the extracted lib chunk now, not the orchestrator body. That lib's own header
# explains the printf -v choice in prose, so this MUST skip comment lines.
require_in "parent-shell pid capture" "${host_helpers_lib}" "printf -v"
# Failure must surface the host-binary logs before the work dir is removed. That
# logic lives in the extracted cleanup lib chunk now, not the orchestrator body.
require_in "failure log dump" "${cleanup_lib}" "host binary logs (failure)"
# A collector that no-ops must not let the gate pass: liveness + facts-landed.
require "collector liveness check" "exited during settle"
# Anchored on the arithmetic guard, not on its message: the phrase "credentialed
# collector source" also appears in the success printf one line below, so a
# needle on the phrase has two homes and deleting the guard leaves it green.
require "cassette facts landed check" "(( collector_sources < GATE_MIN_COLLECTOR_SOURCES ))"

# Drives every pipeline stage end to end.
require "bootstrap stage" "eshu-bootstrap-index"
require "cassette replay" "-mode=cassette"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
# `eshu-api` also names the /readyz failure message, and `eshu-golden-corpus-gate`
# names BOTH gate invocations; both are pinned to the one line that must exist.
# The two gate invocations themselves are covered by the -phase assertions below.
# shellcheck disable=SC2016  # the needle is the literal orchestrator source line
require "api for query truth" 'start_bg api api_pid "${bin_dir}/eshu-api"'
require "gate binary built" "build_bin golden-corpus-gate"
require "corpus fixture inventory source" "golden-corpus-fixtures.sh"
require_in "SQL relationship corpus fixture" "${fixture_lib}" $'\tsql_comprehensive'
# The next two targets are a .sql fixture and a .json snapshot, which is why the
# matcher derives the comment marker per target instead of assuming `#`. `.sql`
# comments with `--`, and line 1 of the DROP fixture IS such a comment, so a
# `#`-only matcher passed with the real DDL commented out (reproduced: mirror
# printed `pass`, rc=0). JSON (RFC 8259) genuinely has no comment syntax, so
# every line there is a real home.
require_in "direct comma-separated SQL DROP migration fixture" "${sql_drop_fixture}" \
	'DROP TABLE IF EXISTS public.users, public.orgs;'
require_in "SQL DROP required correlation in B-12 snapshot" "${snapshot}" '"id": "rc-163"'

# Asserts all four B-7 buckets. The snapshot flag is passed to BOTH gate
# invocations, so it is asserted once per invocation region rather than once
# file-wide — a file-wide needle has two homes and dropping it from either phase
# would go unnoticed.
require "drains phase" "-phase=drains"
require "graph+query+timing phase" "-phase=graph,query,timing"
require_region "snapshot contract (drains phase)" \
	"/-phase=drains/,/-drain-timeout=/" "-snapshot=testdata/golden/e2e-20repo-snapshot.json"
require_region "snapshot contract (graph,query,timing phase)" \
	"/-phase=graph,query,timing/,/-elapsed-seconds=/" "-snapshot=testdata/golden/e2e-20repo-snapshot.json"
require "timing budget" "-budget-multiplier"
# #4596: the blocking-correlation set must be single-sourced from the
# snapshot's own required_correlations ids via the "all" sentinel, not a
# second, hand-maintained comma-separated id list duplicated here.
require "single-sourced required-correlations" '-required-correlations="all"'
if rg --pcre2 --quiet -- '-required-correlations="rc-[0-9]+,rc-' "${script}"; then
	fail "-required-correlations reverted to a hand-maintained comma-separated id list (#4596 regression)"
fi
# B-11 (#3804) macro per-phase wall-clock: the orchestrator sources the timing
# helper lib and invokes it; the emission + gate wiring live in that lib chunk
# (extracted to keep this orchestrator under the 500-line cap).
require "phase-timing lib source" "golden-corpus-phase-timings.sh"
require_invocation "phase-timing invocation" "emit_phase_timings_and_flags"
require "passes phase flags to gate" "phase_flags"
require "cross-repo dead-code fixture source" "golden-corpus-dead-code-fixtures.sh"
require_invocation "cross-repo dead-code fixture invocation" "seed_cross_repo_dead_code_fixture"

timing_lib="${repo_root}/scripts/lib/golden-corpus-phase-timings.sh"
[[ -f "${timing_lib}" ]] || fail "missing phase-timing lib: ${timing_lib}"
bash -n "${timing_lib}" || fail "phase-timing lib has a syntax error"
dead_code_lib="${repo_root}/scripts/lib/golden-corpus-dead-code-fixtures.sh"
[[ -f "${dead_code_lib}" ]] || fail "missing dead-code fixture lib: ${dead_code_lib}"
bash -n "${dead_code_lib}" || fail "dead-code fixture lib has a syntax error"
# require_lib: require_in against the phase-timing lib. The non-comment
# anchoring matters here too — that lib's own header prose names
# phase-timings.json, so a whole-file match is satisfied with no emission code.
require_lib() {
	require_in "$1" "${timing_lib}" "$2"
}
# Anchored on the ASSIGNMENT, not the bare filename: the else-branch log message
# ("emitted phase-timings.json for seeding") is a second non-comment home, so a
# bare-filename needle stays green when the emission target is renamed or the
# whole emission block is replaced.
# shellcheck disable=SC2016  # the needle is the literal lib source line
require_lib "phase-timings emission" 'phase_timings_file="${log_dir}/phase-timings.json"'
require_lib "phase baseline default" "e2e-baseline.json"
require_lib "per-phase gate flag" "-phase-timings-file="

# #5813: the "wait for backends" loop must gate Postgres readiness on a
# HOST-side TCP connect, not only the in-container pg_isready socket probe —
# see scripts/lib/golden-corpus-readiness.sh for why (initdb's temporary,
# socket-only server races a host TCP connect).
readiness_lib="${repo_root}/scripts/lib/golden-corpus-readiness.sh"
[[ -f "${readiness_lib}" ]] || fail "missing host-TCP readiness lib: ${readiness_lib}"
bash -n "${readiness_lib}" || fail "golden-corpus-readiness.sh has a syntax error"
require "readiness lib sourced" "golden-corpus-readiness.sh"
require_in "host TCP probe function definition" "${readiness_lib}" "host_tcp_port_open()"
require "wait loop calls host TCP probe" "host_tcp_port_open 127.0.0.1"
# pg_isready staying present is REQUIRED, not banned: an earlier iteration
# replaced it outright with an in-container `psql -c 'select 1'`, which still
# never exercised the HOST-side TCP path real consumers use, so it did not close
# the race. #5813's host-TCP probe is the evidenced fix, alongside pg_isready.
require "wait loop keeps in-container pg_isready" "pg_isready -U eshu -d eshu"
# The two checks must be ANDed in the SAME condition (belt and braces), not one
# replacing the other — a loosened wait loop that keeps only one of them
# reopens exactly the race this fix closes.
require_matches "Postgres readiness must require BOTH pg_isready AND host_tcp_port_open in one condition" \
	"${script}" \
	'^(?!\s*#)[^\n]*pg_isready -U eshu -d eshu >/dev/null 2>&1 && \\\s*\n\s*host_tcp_port_open 127\.0\.0\.1'

# Genuinely exercise the probe logic (not just its source text) against a real
# closed port and a real open port, mirroring the executable-test bar
# scripts/test-nancy-local.sh's header sets (PR #5806 review rejected a
# text-only assertion for exactly this reason: it cannot tell a working probe
# from a broken one, e.g. an inverted condition or a dropped `exec 3<&-`).
# shellcheck source=scripts/lib/golden-corpus-readiness.sh
. "${readiness_lib}"

if host_tcp_port_open 127.0.0.1 1; then
	fail "host_tcp_port_open must fail against a closed TCP port (127.0.0.1:1)"
fi

# python3 is already an established scripts/ dependency (verify-telemetry-
# coverage.sh, verify-demo-compose-answers.sh); use it to bind an ephemeral
# loopback listener so the "open port" case is a real, currently-listening
# socket rather than a guessed port number.
listener_port_file="$(mktemp -t golden-corpus-readiness-port.XXXXXX)"
listener_pid=""
trap 'kill "${listener_pid}" >/dev/null 2>&1 || true; rm -f "${listener_port_file}"' EXIT
python3 - "${listener_port_file}" <<'PY' &
import socket
import sys
import time

s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 0))
s.listen(1)
with open(sys.argv[1], "w") as f:
	f.write(str(s.getsockname()[1]))
time.sleep(10)
PY
listener_pid=$!

listener_port=""
for _ in $(seq 1 50); do
	if [[ -s "${listener_port_file}" ]]; then
		listener_port="$(cat "${listener_port_file}")"
		break
	fi
	sleep 0.1
done
[[ -n "${listener_port}" ]] || fail "test harness: python3 loopback listener never reported a port"

if ! host_tcp_port_open 127.0.0.1 "${listener_port}"; then
	fail "host_tcp_port_open must succeed against a real open TCP port (127.0.0.1:${listener_port})"
fi

kill "${listener_pid}" >/dev/null 2>&1 || true
wait "${listener_pid}" 2>/dev/null || true
rm -f "${listener_port_file}"
trap - EXIT
# The per-phase check must default to advisory on shared CI runners (hardware
# variance exceeds the band); a controlled host flips it blocking.
require_lib "per-phase advisory default" "-phase-regression-advisory"
# Minimal-corpus posture: graph-populated smoke is required. Every
# shared_projection_intents domain (incl. code_calls, #3865) must drain — no
# domain is quarantined as advisory.
require "graph-populated smoke" "-required-node-labels"
if rg --quiet --fixed-strings -- 'drain-advisory-domains="code_calls"' "${script}"; then
	fail "code_calls must no longer be quarantined as an advisory drain domain (#3865 fixed)"
fi

# Wires all nine B-10 cassette collectors.
for collector in \
	collector-kubernetes-live collector-aws-cloud collector-azure-cloud \
	collector-gcp-cloud collector-vault-live collector-oci-registry \
	collector-package-registry collector-terraform-state collector-prometheus-mimir; do
	require "collector ${collector}" "${collector}"
done

# The B-13 (#3859) drain gate lives in the gate binary; the orchestrator must run
# the drains phase against the snapshot whose shared_projection_intents bound is
# the real signal. Guard against someone reducing the drain check to a sleep.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# Premature-convergence guard: the drain must require the reducer to be observed
# populated before accepting a drained reading, or it can pass on an unreduced
# pipeline (the 0/0-before-the-reducer-runs race).
require "populated-then-drained guard" 'require-populated-domains="repo_dependency"'

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
# The 12-digit arm catches a bare cloud account id. The lock cases used to age
# a guard past its budget with `touch -h -t <stamp>`; age now comes from a
# birth epoch embedded in the guard's own payload (pid:epoch), computed at
# runtime via `date +%s`, so no `touch -h -t` stamp (and no exclusion for one)
# remains in the scanned files.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(?<![0-9])[0-9]{12}(?![0-9])|/Users/|/home/[a-z]'
# Scan every scripts/lib/golden-corpus-*.sh lib via the glob-derived
# golden_corpus_libs array (built above, and already asserted non-empty), not a
# hand-maintained list of names: a hand-maintained list silently stopped
# covering golden-corpus-maintenance-drains.sh even though it now owns the live
# gate orchestration this PR moved out of the main script, so a future secret
# added there would pass this scan. Deriving the list from the same glob the
# orchestrator's own lib-count assertion uses means an added lib is covered
# automatically, with no new path entry to keep in sync. The orchestrator
# script itself and live-gate-lock.sh are not golden-corpus-*.sh named, so they
# still need an explicit entry.
for scanned in "${script}" \
	"${repo_root}/scripts/lib/live-gate-lock.sh" \
	"${golden_corpus_libs[@]}"; do
	if rg --pcre2 --quiet -- "${private_pattern}" "${scanned}"; then
		fail "$(basename "${scanned}") looks like it contains private data"
	fi
done

# golden-corpus-lock-cases.sh is not only executable cases: it also carries one
# invariant-routed assertion of its own -- require "live gate mutex"
# 'acquire_live_gate_lock' -- so the enforced set is 77 assertions, the 76 in
# this file plus that one. A mutation sweep enumerated from this file alone
# misses it. It is genuinely enforced: mutating the orchestrator's call reports
# `missing live gate mutex` with rc=1.
. "${repo_root}/scripts/lib/golden-corpus-lock-cases.sh"
[[ "${lock_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-lock-cases.sh did not run to completion (gutted, or returned early)"
[[ "${lock_parse_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-lock-parse-cases.sh did not run to completion (gutted, or returned early)"
[[ "${lock_race_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-lock-race-cases.sh did not run to completion (gutted, or returned early)"

printf 'test-verify-golden-corpus-gate: pass\n'
