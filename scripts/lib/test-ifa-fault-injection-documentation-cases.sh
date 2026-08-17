#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the documentation_edges fault-injection
# cells (#5994). Split from test-ifa-fault-injection-review-cases.sh to stay
# under the repository's 500-line cap; mirrors that file's two code_calls
# cases (test_ifa_code_call_fresh_stack_intent_guard_is_typed_and_fail_closed,
# test_ifa_fault_released_lock_holder_is_not_torn_down_twice) exactly, one
# family later, because documentation_edges adopted the same
# fresh-intents-guard / intent-lock shape cell_killworker_code_calls
# established. Sourced by scripts/test-verify-ifa-fault-injection.sh
# alongside test-ifa-fault-injection-review-cases.sh, so the top-level static
# verifier stays below the repository's 500-line cap; both files share that
# script's ${documentation_cells_lib}/${det_lib}/${driver_lib} globals and
# `fail` helper.

test_ifa_documentation_fresh_stack_edge_guard_is_typed_and_fail_closed() (
	source "${documentation_cells_lib}"
	declare -F ifa_documentation_require_fresh_documents_edges >/dev/null \
		|| fail "documentation cells do not expose ifa_documentation_require_fresh_documents_edges"
	# The precondition used to be ifa_documentation_require_fresh_intents,
	# querying shared_projection_intents WHERE projection_domain =
	# 'documentation_edges' -- vacuous, because documentationEdgeMaterializationHandler
	# never writes that table (no IntentWriter field, only
	# EdgeWriter.WriteEdges), so the count was always zero and the guard could
	# never fail. Runtime check, not just a grep: the old name must not exist
	# as a callable function after sourcing the cells lib.
	declare -F ifa_documentation_require_fresh_intents >/dev/null \
		&& fail "the old intents-based guard (ifa_documentation_require_fresh_intents) must not survive -- it queried a table this family's handler never writes"

	local case_dir
	case_dir="$(mktemp -d -t ifa-fault-documentation-fresh-edges.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	mkdir -p "${case_dir}/bin"

	# write_dump_stub replaces the stub eshu-ifa binary so `graph-dump` exits
	# with the given code. The guard under test never reads the dump file's
	# content -- jq is stubbed separately below -- so the stub does not need
	# to write real graph JSON.
	write_dump_stub() {
		local dump_rc="$1"
		cat >"${case_dir}/bin/eshu-ifa" <<STUB
#!/usr/bin/env bash
exit ${dump_rc}
STUB
		chmod +x "${case_dir}/bin/eshu-ifa"
	}

	local jq_output jq_rc output rc
	jq() {
		printf '%s' "${jq_output}"
		return "${jq_rc}"
	}

	# graph-dump FAILED: exact exit code preserved, not collapsed to a bare 1
	# -- the family idiom (an explicit rc capture: `if out="$(...)"; then
	# rc=0; else rc=$?; fi`, matching ifa_documentation_release_intent_lock
	# and the retry-count snapshot in cell_killworker_documentation) applies
	# to the dump step exactly as it applied to the old Postgres query.
	write_dump_stub 9
	rc=0
	output="$(ifa_documentation_require_fresh_documents_edges test "${case_dir}/bin" 2>&1)" || rc=$?
	[[ "${rc}" -eq 9 ]] || fail "failed graph-dump returned ${rc}, want original exit 9"
	[[ "${output}" == *"graph-dump FAILED (exit 9)"* ]] \
		|| fail "failed graph-dump did not preserve exit 9 distinctly: ${output}"
	[[ "${output}" != *"survived fresh_stack"* ]] \
		|| fail "failed graph-dump was misreported as stale edges: ${output}"

	# jq count FAILED (dump itself succeeded): same rc-preservation idiom.
	write_dump_stub 0
	jq_output="ignored"
	jq_rc=7
	rc=0
	output="$(ifa_documentation_require_fresh_documents_edges test "${case_dir}/bin" 2>&1)" || rc=$?
	[[ "${rc}" -eq 7 ]] || fail "failed jq DOCUMENTS-edge count returned ${rc}, want original exit 7"
	[[ "${output}" == *"could not count DOCUMENTS edges in the graph dump (exit 7)"* ]] \
		|| fail "failed jq DOCUMENTS-edge count did not preserve exit 7 distinctly: ${output}"

	# empty jq output: rejected as unknown, not zero.
	jq_output=""
	jq_rc=0
	rc=0
	output="$(ifa_documentation_require_fresh_documents_edges test "${case_dir}/bin" 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "empty DOCUMENTS-edge count was accepted"
	[[ "${output}" == *"edge count came back empty"* ]] \
		|| fail "empty DOCUMENTS-edge count was not diagnosed distinctly: ${output}"

	# non-numeric jq output: rejected as unknown, not zero.
	jq_output="not-a-count"
	jq_rc=0
	rc=0
	output="$(ifa_documentation_require_fresh_documents_edges test "${case_dir}/bin" 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "non-numeric DOCUMENTS-edge count was accepted"
	[[ "${output}" == *"is non-numeric"* ]] \
		|| fail "non-numeric DOCUMENTS-edge count was not diagnosed distinctly: ${output}"

	# stale, non-zero edge count: rejected as a genuine leak, distinct from
	# the three "unknown" cases above.
	jq_output=$' 3\n'
	jq_rc=0
	rc=0
	output="$(ifa_documentation_require_fresh_documents_edges test "${case_dir}/bin" 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "stale DOCUMENTS edges were accepted"
	[[ "${output}" == *"3 DOCUMENTS edge(s) survived fresh_stack"* ]] \
		|| fail "stale DOCUMENTS edges were not reported as stale: ${output}"

	# genuine zero: the only input that continues.
	jq_output=$' 0\n'
	jq_rc=0
	ifa_documentation_require_fresh_documents_edges test "${case_dir}/bin" >/dev/null \
		|| fail "zero DOCUMENTS edges did not continue"
)

test_ifa_documentation_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${documentation_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-fault-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41003
	survivor_pid=41004
	bg_pids=("${holder_pid}" "${survivor_pid}")
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() { return 0; }
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_documentation_release_intent_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined documentation lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined documentation lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)
