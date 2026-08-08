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
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-golden-corpus-gate.sh must stay under 500 lines"
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

# #5837 structural fix: committed negative tests for the matcher's own
# exactly-one-non-comment-home invariant and the bracket-placement guard
# built on it. The guard above has been evaded three times across review
# rounds, each time only caught by a human or reviewer running a scratch
# probe -- nothing committed asserted it. Extracted to its own lib chunk
# (golden-corpus-phase-timing-cases.sh is already at 460 lines) with the same
# gutted-or-early-return sentinel the other extracted case chunks use.
matcher_guard_cases_lib="${repo_root}/scripts/lib/golden-corpus-matcher-guard-cases.sh"
[[ -f "${matcher_guard_cases_lib}" ]] || fail "missing matcher guard cases lib: ${matcher_guard_cases_lib}"
bash -n "${matcher_guard_cases_lib}" || fail "golden-corpus-matcher-guard-cases.sh has a syntax error"
# shellcheck source=scripts/lib/golden-corpus-matcher-guard-cases.sh
. "${matcher_guard_cases_lib}"
[[ "${matcher_guard_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-matcher-guard-cases.sh did not run to completion (gutted, or returned early)"

# The workflow's on.pull_request.paths filter must trigger this gate on every
# source dir whose changes can alter emitted facts, graph/content projection,
# or query/MCP truth the gate asserts (#5596, widened by #5538). Assertions
# extracted to a sourced lib chunk (golden-corpus-mirror-workflow-paths.sh) to
# keep this mirror under the repo's 500-line file rule as the count grows; see
# that lib's header for the full rationale per group of paths.
workflow_paths_lib="${repo_root}/scripts/lib/golden-corpus-mirror-workflow-paths.sh"
[[ -f "${workflow_paths_lib}" ]] || fail "missing workflow paths lib: ${workflow_paths_lib}"
bash -n "${workflow_paths_lib}" || fail "golden-corpus-mirror-workflow-paths.sh has a syntax error"
# shellcheck source=scripts/lib/golden-corpus-mirror-workflow-paths.sh
. "${workflow_paths_lib}"
# The workflow is only one of THREE lists that must carry these paths. Assert the
# other two as well, or "the gap cannot reopen" is true for a third of the gap.
# Scoped to each gate's OWN trigger block: a file-wide grep passes while the path
# is present in either gate, so deleting it from one silently un-selects that
# gate and the local half of the gap reopens.
for gate_id in golden-corpus-mirror golden-corpus-gate; do
	for gate_path in 'scripts/lib/golden-corpus-*.sh' 'scripts/lib/live-gate-lock.sh' 'tests/fixtures/ecosystems/**'; do
		require_in_region "ci-gates gate ${gate_id} trigger ${gate_path}" "${ci_gates}" \
			"/^  - id: ${gate_id}\$/,/^    local:/" "- \"${gate_path}\""
	done
done
# Anchored to the golden-corpus selector line, not the whole file: the fragment
# could otherwise be moved onto an unrelated selector and still pass. The
# leading `^(?!\s*#)` keeps a commented-out selector from standing in for the
# live one, and require_matches pins it to exactly one site.
require_matches "the pre-pr golden-corpus selector must match the golden-corpus libs and the mutex" \
	"${prepr}" \
	"^(?!\s*#)[^\n]*run_or_defer golden-corpus \\\\\n[^\n]*scripts/lib/\(golden-corpus-\.\+\|live-gate-lock\)"
require_matches "the pre-pr golden-corpus selector must match static ecosystem corpus inputs" \
	"${prepr}" \
	"^(?!\s*#)[^\n]*run_or_defer golden-corpus \\\\\n[^\n]*tests/fixtures/ecosystems/"

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
# The fixture inventory is the source of truth for the corpus size. Keep the
# snapshot's machine-readable count and its operator-facing Repository note in
# lockstep so adding a fixture cannot leave stale proof metadata behind.
# shellcheck source=scripts/lib/golden-corpus-fixtures.sh
. "${fixture_lib}"
fixture_count="${#corpus_fixtures[@]}"
snapshot_fixture_count="$(jq -er '.corpus_composition.git_repos' "${snapshot}")" \
	|| fail "B-12 corpus_composition.git_repos must be an integer"
[[ "${snapshot_fixture_count}" -eq "${fixture_count}" ]] \
	|| fail "B-12 declares ${snapshot_fixture_count} repos but the fixture inventory stages ${fixture_count}"
repository_note="$(jq -er '.graph.node_counts.Repository.note' "${snapshot}")" \
	|| fail "B-12 Repository note must be a string"
rg --fixed-strings --quiet -- "${fixture_count} corpus repos" <<<"${repository_note}" \
	|| fail "B-12 Repository note must name the ${fixture_count}-repo fixture inventory"

suppression_lib="${repo_root}/scripts/lib/golden-corpus-vulnerability-suppression.sh"
[[ -f "${suppression_lib}" ]] || fail "missing suppression proof lib: ${suppression_lib}"
bash -n "${suppression_lib}" || fail "golden-corpus-vulnerability-suppression.sh has a syntax error"
captured_suppression_count_query=""
pg() {
	captured_suppression_count_query="$1"
	printf '0 0\n'
}
# shellcheck source=scripts/lib/golden-corpus-vulnerability-suppression.sh
. "${suppression_lib}"
golden_suppression_counts >/dev/null
rg --fixed-strings --quiet -- "stage='projector'" <<<"${captured_suppression_count_query}" \
	|| fail "suppression mutation count must isolate projector work from reducer fanout"
rg --fixed-strings --quiet -- "golden_suppression_verify_producer_truth()" "${suppression_lib}" \
	|| fail "suppression proof must keep its orchestration in the sourced helper"
rg --fixed-strings --quiet -- "golden_suppression_wait_for_expiry" "${suppression_lib}" \
	|| fail "suppression proof must wait for the authored future expiry"
rg --fixed-strings --quiet -- "suppression_perf drain_state=" "${suppression_lib}" \
	|| fail "suppression proof must report reducer/projector drain wall time and terminal queue counts"
rg --fixed-strings --quiet -- "golden_suppression_remove_malformed_fixture" "${suppression_lib}" \
	|| fail "suppression proof must remove the deliberately malformed source fact before API generation rewrite"
rg --fixed-strings --quiet -- "'justification', 'external_unknown'" "${suppression_lib}" \
	|| fail "suppression proof must inject the malformed enum directly at the storage/reducer seam"
rg --fixed-strings --quiet -- "golden_suppression_setup_body" "${suppression_lib}" \
	|| fail "suppression proof must establish the operator scope through the live producer before hostile storage injection"
rg --fixed-strings --quiet -- "active_generation_id" "${suppression_lib}" \
	|| fail "suppression proof must bind hostile injection to the producer-created active generation"
rg --fixed-strings --quiet -- "UPDATE fact_work_items AS work" "${suppression_lib}" \
	|| fail "suppression proof must reopen the producer-created reducer work instead of adding a supersedable duplicate"
rg --fixed-strings --quiet -- '.arguments.generation_id = $generation' "${suppression_lib}" \
	|| fail "suppression runtime snapshot must bind MCP quarantine readback to the producer-created generation"
jq -e '
	.query_shapes.mcp.list_reducer_input_invalid_facts
	| .minimum_results == 1
	  and .arguments.scope_id == "operator:vulnerability_suppressions"
	  and .arguments.generation_id == "__runtime_operator_generation__"
	  and (.result_item_required_fields | index("missing_field") != null)
	  and .required_json_values["items[].missing_field"] == "justification"
	  and .required_json_values["items[].generation_id"] == "__runtime_operator_generation__"
' "${snapshot}" >/dev/null ||
	fail "B-12 must non-vacuously assert the malformed suppression quarantine"
if rg --fixed-strings --quiet -- "golden_suppression_expired_body" "${suppression_lib}"; then
	fail "suppression proof must not create a second already-expired mutation"
fi
runtime_snapshot_test_dir="$(mktemp -d -t golden-suppression-snapshot.XXXXXX)"
log_dir="${runtime_snapshot_test_dir}"
golden_suppression_active_body="${runtime_snapshot_test_dir}/active.json"
jq -n '{
	suppression_id: "golden-CVE-2026-00010",
	justification: "ignored",
	authored_at: "2026-07-27T12:00:00Z",
	expires_at: "2099-07-27T12:00:00Z",
	scope: {cve_id: "CVE-2026-00010"}
}' >"${golden_suppression_active_body}"
runtime_snapshot_generation="suppression_runtime_test_generation"
golden_suppression_prepare_runtime_snapshot "${runtime_snapshot_generation}"
runtime_snapshot_body="$(
	jq -c '.query_shapes.http["POST /api/v0/supply-chain/impact/suppressions"].request_body' \
		"${golden_suppression_runtime_snapshot}"
)"
expected_runtime_snapshot_body="$(jq -c . "${golden_suppression_active_body}")"
[[ "${runtime_snapshot_body}" == "${expected_runtime_snapshot_body}" ]] ||
	fail "runtime snapshot must replay the exact dynamically authored suppression body"
runtime_snapshot_quarantine_generation="$(
	jq -r '.query_shapes.mcp.list_reducer_input_invalid_facts.arguments.generation_id' \
		"${golden_suppression_runtime_snapshot}"
)"
[[ "${runtime_snapshot_quarantine_generation}" == "${runtime_snapshot_generation}" ]] ||
	fail "runtime snapshot must bind quarantine readback to the producer-created generation"
rm -rf "${runtime_snapshot_test_dir}"
rg --fixed-strings --quiet -- "golden_suppression_verify_producer_truth" "${script}" \
	|| fail "golden gate must execute the suppression producer proof helper"
rg --fixed-strings --quiet -- '${golden_suppression_runtime_snapshot}' "${script}" \
	|| fail "final query gate must use the runtime snapshot with the exact suppression body"

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
require "collector settle lib source" "golden-corpus-collector-settle.sh"
require_invocation "collector settle invocation" "wait_for_collector_settle"
cleanup_lib="${repo_root}/scripts/lib/golden-corpus-cleanup.sh"
[[ -f "${cleanup_lib}" ]] || fail "missing cleanup lib: ${cleanup_lib}"
bash -n "${cleanup_lib}" || fail "cleanup lib has a syntax error"
host_helpers_lib="${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"
[[ -f "${host_helpers_lib}" ]] || fail "missing host helpers lib: ${host_helpers_lib}"
bash -n "${host_helpers_lib}" || fail "host helpers lib has a syntax error"
stage_lib="${repo_root}/scripts/lib/golden-corpus-stage.sh"
[[ -f "${stage_lib}" ]] || fail "missing corpus staging lib: ${stage_lib}"
bash -n "${stage_lib}" || fail "corpus staging lib has a syntax error"
# shellcheck source=scripts/lib/golden-corpus-stage-cases.sh
. "${repo_root}/scripts/lib/golden-corpus-stage-cases.sh"
[[ "${stage_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-stage-cases.sh did not run to completion (gutted, or returned early)"
maintenance_lib="${repo_root}/scripts/lib/golden-corpus-maintenance-drains.sh"
[[ -f "${maintenance_lib}" ]] || fail "missing maintenance drains lib: ${maintenance_lib}"
bash -n "${maintenance_lib}" || fail "maintenance drains lib has a syntax error"
collector_settle_lib="${repo_root}/scripts/lib/golden-corpus-collector-settle.sh"
[[ -f "${collector_settle_lib}" ]] || fail "missing collector settle lib: ${collector_settle_lib}"
bash -n "${collector_settle_lib}" || fail "collector settle lib has a syntax error"
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
# Every Compose operation must carry the same explicit, uniquely overridable
# project name. Host binaries connect over private ports, while `-p` prevents
# containers and volumes from colliding with another worktree's gate.
require "unique Compose project default" 'GATE_COMPOSE_PROJECT:=eshu-golden-corpus-$$'
require "shared Compose args" 'compose_args=(-p "${GATE_COMPOSE_PROJECT}" -f "${compose_file}")'
for compose_case in \
	"${repo_root}/scripts/lib/golden-corpus-readiness.sh:4" \
	"${cleanup_lib}:1" "${host_helpers_lib}:1" \
	"${repo_root}/scripts/lib/golden-corpus-dead-code-fixtures.sh:1"; do
	compose_owner="${compose_case%:*}"
	expected_count="${compose_case##*:}"
	actual_count="$(rg --count --fixed-strings 'docker compose "${compose_args[@]}"' "${compose_owner}" || true)"
	[[ "${actual_count:-0}" -eq "${expected_count}" ]] ||
		fail "$(basename "${compose_owner}") must carry explicit Compose project on all ${expected_count} calls; found ${actual_count:-0}"
done
# A collector that no-ops must not let the gate pass: liveness + facts-landed
# checks, the fixed-sleep regression guard, and a functional exercise of
# wait_for_collector_settle against a mocked pg()/die() all live in the
# extracted cases lib chunk (same reason the B-11 phase-timing cases and the
# matcher-guard cases are extracted, to keep this mirror test under the
# 500-line cap). The sentinel catches a gutted or early-returning chunk.
# shellcheck source=scripts/lib/golden-corpus-collector-settle-cases.sh
. "${repo_root}/scripts/lib/golden-corpus-collector-settle-cases.sh"
[[ "${collector_settle_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-collector-settle-cases.sh did not run to completion (gutted, or returned early)"

# Pipeline stage, snapshot, timing, and exact demotion-command cases live in a
# sourced chunk so this mirror retains room below the 500-line cap. Its negative
# probes prove the demotion command cannot gain a fail-open suffix or weaken its
# package, test regex, or timeout while the mirror remains green.
# shellcheck source=scripts/lib/golden-corpus-pipeline-cases.sh
. "${repo_root}/scripts/lib/golden-corpus-pipeline-cases.sh"
[[ "${pipeline_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-pipeline-cases.sh did not run to completion (gutted, or returned early)"

# B-11 per-phase timing cases (#5837): the graph_query phase must exclude
# assertion work bracketed inside its own window. Extracted to a lib chunk to
# keep this mirror test under the 500-line cap; the sentinel below catches a
# gutted or early-returning chunk, the same guard the lock cases use.
# shellcheck source=scripts/lib/golden-corpus-phase-timing-cases.sh
. "${repo_root}/scripts/lib/golden-corpus-phase-timing-cases.sh"
[[ "${phase_timing_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-phase-timing-cases.sh did not run to completion (gutted, or returned early)"

# #5813: the "wait for backends" loop must gate Postgres readiness on a
# HOST-side TCP connect, not only the in-container pg_isready socket probe —
# see scripts/lib/golden-corpus-readiness.sh for why (initdb's temporary,
# socket-only server races a host TCP connect).
readiness_lib="${repo_root}/scripts/lib/golden-corpus-readiness.sh"
[[ -f "${readiness_lib}" ]] || fail "missing host-TCP readiness lib: ${readiness_lib}"
bash -n "${readiness_lib}" || fail "golden-corpus-readiness.sh has a syntax error"
require "readiness lib sourced" "golden-corpus-readiness.sh"
require_in "host TCP probe function definition" "${readiness_lib}" "host_tcp_port_open()"
require_invocation "backend startup invocation" "start_golden_corpus_backends"
require_in "wait loop calls host TCP probe" "${readiness_lib}" "host_tcp_port_open 127.0.0.1"
# pg_isready staying present is REQUIRED, not banned: an earlier iteration
# replaced it outright with an in-container `psql -c 'select 1'`, which still
# never exercised the HOST-side TCP path real consumers use, so it did not close
# the race. #5813's host-TCP probe is the evidenced fix, alongside pg_isready.
require_in "wait loop keeps in-container pg_isready" "${readiness_lib}" "pg_isready -U eshu -d eshu"
# The two checks must be ANDed in the SAME condition (belt and braces), not one
# replacing the other — a loosened wait loop that keeps only one of them
# reopens exactly the race this fix closes.
require_matches "Postgres readiness must require BOTH pg_isready AND host_tcp_port_open in one condition" \
	"${readiness_lib}" \
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
