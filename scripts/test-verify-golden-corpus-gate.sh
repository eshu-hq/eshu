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

fail() { printf 'test-verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-golden-corpus-gate.sh must be executable"
[[ -f "${fixture_lib}" ]] || fail "missing ${fixture_lib}"
[[ -f "${workflow}" ]] || fail "missing ${workflow}"
[[ -f "${snapshot}" ]] || fail "missing ${snapshot}"
[[ -f "${sql_drop_fixture}" ]] || fail "missing ${sql_drop_fixture}"

# #5596: the workflow's on.pull_request.paths filter must trigger this gate on
# every source dir whose changes can alter emitted facts or fact contracts the
# gate asserts. A dir missing here means a PR that touches only that dir ships
# a fact-emission change unverified by the golden corpus (a false-green).
require_workflow_path() {
	local label="$1" path_glob="$2"
	rg --fixed-strings --quiet -- "- '${path_glob}'" "${workflow}" \
		|| fail "golden-corpus-gate.yml paths filter missing ${label}: ${path_glob}"
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
	gate_block="$(sed -n "/^  - id: ${gate_id}\$/,/^    local:/p" "${repo_root}/specs/ci-gates.v1.yaml")"
	[[ -n "${gate_block}" ]] || fail "ci-gates registry has no gate ${gate_id}"
	for lib_path in 'scripts/lib/golden-corpus-*.sh' 'scripts/lib/live-gate-lock.sh'; do
		printf '%s\n' "${gate_block}" |
			rg --fixed-strings --quiet -- "- \"${lib_path}\"" ||
			fail "ci-gates gate ${gate_id} triggers omit ${lib_path}"
	done
done
# Anchored to the golden-corpus selector line, not the whole file: the fragment
# could otherwise be moved onto an unrelated selector and still pass.
rg --pcre2 --quiet --multiline \
	"run_or_defer golden-corpus \\\\\n[^\n]*scripts/lib/\(golden-corpus-\.\+\|live-gate-lock\)" \
	"${repo_root}/scripts/dev/pre-pr.sh" ||
	fail "the pre-pr golden-corpus selector no longer matches the golden-corpus libs or the mutex"

# #5817 P2 review: verify-golden-corpus-gate.sh sources FOUR
# scripts/lib/golden-corpus-*.sh helper libs (fixtures, phase-timings,
# dead-code-fixtures, readiness). Before this, the paths filter had no
# scripts/lib/** entry at all, so a PR editing only one of those libs (e.g. a
# bash cleanup that inverts host_tcp_port_open's return value) would never
# trigger this gate -- a false green. Assert the glob entry is present, and
# that every golden-corpus lib file actually on disk still matches its naming
# convention, so a future rename or an added lib outside that convention
# reopens the gap loudly here instead of silently in CI.
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
	"${repo_root}/scripts/lib/golden-corpus-lock-race-cases.sh"; do
	[[ -f "${sourced_case_lib}" ]] || fail "missing lock case lib: ${sourced_case_lib}"
	bash -n "${sourced_case_lib}" || fail "$(basename "${sourced_case_lib}") has a syntax error"
done
bash -n "${fixture_lib}" || fail "golden-corpus-fixtures.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
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
require "corpus staging invocation" "stage_minimal_corpus"
cleanup_lib="${repo_root}/scripts/lib/golden-corpus-cleanup.sh"
[[ -f "${cleanup_lib}" ]] || fail "missing cleanup lib: ${cleanup_lib}"
bash -n "${cleanup_lib}" || fail "cleanup lib has a syntax error"
host_helpers_lib="${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"
[[ -f "${host_helpers_lib}" ]] || fail "missing host helpers lib: ${host_helpers_lib}"
bash -n "${host_helpers_lib}" || fail "host helpers lib has a syntax error"
stage_lib="${repo_root}/scripts/lib/golden-corpus-stage.sh"
[[ -f "${stage_lib}" ]] || fail "missing corpus staging lib: ${stage_lib}"
bash -n "${stage_lib}" || fail "corpus staging lib has a syntax error"
# Background pids must be recorded in the PARENT shell (printf -v), or the cleanup
# trap reaps nothing on a failure path and leaks host processes. The helper lives
# in the extracted lib chunk now, not the orchestrator body.
rg --fixed-strings --quiet -- "printf -v" "${host_helpers_lib}" ||
	fail "parent-shell pid capture (printf -v) missing from ${host_helpers_lib}"
# Failure must surface the host-binary logs before the work dir is removed. That
# logic lives in the extracted cleanup lib chunk now, not the orchestrator body.
rg --fixed-strings --quiet -- "host binary logs (failure)" "${cleanup_lib}" ||
	fail "failure log dump missing from ${cleanup_lib}"
# A collector that no-ops must not let the gate pass: liveness + facts-landed.
require "collector liveness check" "exited during settle"
require "cassette facts landed check" "credentialed collector source"

# Drives every pipeline stage end to end.
require "bootstrap stage" "eshu-bootstrap-index"
require "cassette replay" "-mode=cassette"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
require "api for query truth" "eshu-api"
require "gate binary" "eshu-golden-corpus-gate"
require "corpus fixture inventory source" "golden-corpus-fixtures.sh"
rg --fixed-strings --quiet -- $'\tsql_comprehensive' "${fixture_lib}" \
	|| fail "missing SQL relationship corpus fixture in ${fixture_lib}"
rg --fixed-strings --quiet -- 'DROP TABLE IF EXISTS public.users, public.orgs;' "${sql_drop_fixture}" \
	|| fail "missing direct comma-separated SQL DROP migration fixture"
rg --fixed-strings --quiet -- '"id": "rc-163"' "${snapshot}" \
	|| fail "missing SQL DROP required correlation in B-12 snapshot"

# Asserts all four B-7 buckets.
require "drains phase" "-phase=drains"
require "graph+query+timing phase" "-phase=graph,query,timing"
require "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"
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
require "phase-timing invocation" "emit_phase_timings_and_flags"
require "passes phase flags to gate" "phase_flags"
require "cross-repo dead-code fixture source" "golden-corpus-dead-code-fixtures.sh"
require "cross-repo dead-code fixture invocation" "seed_cross_repo_dead_code_fixture"

timing_lib="${repo_root}/scripts/lib/golden-corpus-phase-timings.sh"
[[ -f "${timing_lib}" ]] || fail "missing phase-timing lib: ${timing_lib}"
bash -n "${timing_lib}" || fail "phase-timing lib has a syntax error"
dead_code_lib="${repo_root}/scripts/lib/golden-corpus-dead-code-fixtures.sh"
[[ -f "${dead_code_lib}" ]] || fail "missing dead-code fixture lib: ${dead_code_lib}"
bash -n "${dead_code_lib}" || fail "dead-code fixture lib has a syntax error"
require_lib() {
	rg --fixed-strings --quiet -- "$2" "${timing_lib}" || fail "missing $1 in phase-timing lib: $2"
}
require_lib "phase-timings emission" "phase-timings.json"
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
rg --fixed-strings --quiet -- "host_tcp_port_open()" "${readiness_lib}" \
	|| fail "missing host TCP probe function definition in ${readiness_lib}"
require "wait loop calls host TCP probe" "host_tcp_port_open 127.0.0.1"
require "wait loop keeps in-container pg_isready" "pg_isready -U eshu -d eshu"
# The two checks must be ANDed in the SAME condition (belt and braces), not one
# replacing the other — a loosened wait loop that keeps only one of them
# reopens exactly the race this fix closes.
if ! rg --pcre2 --multiline --quiet -- 'pg_isready -U eshu -d eshu >/dev/null 2>&1 && \\\s*\n\s*host_tcp_port_open 127\.0\.0\.1' "${script}"; then
	fail "Postgres readiness must require BOTH pg_isready AND host_tcp_port_open in one condition"
fi

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
# The backend wait must be a real query. pg_isready answers ~30s before the
# server will serve under load, and the gate then dies with
# `ping postgres: tls error: EOF` - which reads as a broken backend, not as a
# gate that asked too early. Reverting this is a plausible "simplification".
# Comment-aware, like the bans below: `require` would otherwise be satisfied by
# the literal sitting in a comment while the predicate itself was neutered.
rg -v '^[[:space:]]*#' "${script}" |
	rg --fixed-strings --quiet -- "psql -U eshu -d eshu -c 'select 1'" ||
	fail "backend wait is not a real query (psql -c 'select 1')"
if rg -v '^[[:space:]]*#' "${script}" | rg --fixed-strings --quiet -- 'pg_isready'; then
	fail "backend wait reverted to pg_isready: it reports ready ~30s before the server serves"
fi
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
for scanned in "${script}" \
	"${repo_root}/scripts/lib/live-gate-lock.sh" \
	"${repo_root}/scripts/lib/golden-corpus-lock-cases.sh" \
	"${repo_root}/scripts/lib/golden-corpus-lock-race-cases.sh" \
	"${cleanup_lib}" "${host_helpers_lib}" "${stage_lib}"; do
	if rg --pcre2 --quiet -- "${private_pattern}" "${scanned}"; then
		fail "$(basename "${scanned}") looks like it contains private data"
	fi
done

. "${repo_root}/scripts/lib/golden-corpus-lock-cases.sh"
[[ "${lock_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-lock-cases.sh did not run to completion (gutted, or returned early)"
[[ "${lock_race_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-lock-race-cases.sh did not run to completion (gutted, or returned early)"

printf 'test-verify-golden-corpus-gate: pass\n'
