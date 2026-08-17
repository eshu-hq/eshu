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

# run_ifa_fault_injection_documentation_registry_cases holds the
# documentation_edges (#5994) static require()/rg pins against
# ${documentation_lib} and ${documentation_cells_lib}, moved out of the
# top-level test-verify-ifa-fault-injection.sh (which was sourcing this file
# already, just for the hermetic cases above) to keep that script under the
# repository's 500-line cap -- mirroring how deployable_unit_edges's own
# static pins already live in test-ifa-fault-injection-deployable-unit-cases.sh
# rather than the top-level script. Wrapped in a function (same reason
# run_ifa_fault_injection_deployable_unit_cases is) so ${documentation_lib}/
# ${documentation_cells_lib}/${script}/${fail} resolve at CALL time from the
# parent's scope, not at source time.
run_ifa_fault_injection_documentation_registry_cases() {
	# documentation_edges (#5994): every cell drives the family via
	# cell_baseline's drive_all_cassettes, baseline exact-asserts it, and two
	# dedicated cells prove queue reclaim and graph-write retry against the
	# documentation_materialization domain rather than an unrelated row that
	# happened to run first. The family also proves the SqlTable target case
	# (batchCanonicalDocumentationEntityEdgeCypher's MATCH label alternation,
	# TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel).
	require "documentation cassette path" "testdata/cassettes/documentation/ifa-documentation-family.json"
	require "documentation expected-edge set path" "go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"
	require "documentation cassette existence guard" "documentation cassette not found"
	require "documentation expected-edge set existence guard" "documentation expected-edge set not found"
	require "documentation DOCUMENTS MERGE operation_match anchor" 'documentation_edge_operation_match="MERGE (section)-[rel:DOCUMENTS]->(target)"'
	require_driver "documentation drive in every cell" "ifa_documentation_drive"
	require_cells "documentation exact assertion in baseline" "ifa_documentation_assert"
	require_cells "documentation fault-free retry baseline" '"documentation_materialization"'
	require "documentation kill-reclaim cell invocation" "cell_killworker_documentation"
	require "documentation graph-write cell invocation" "cell_failgraphwrite_documentation"
	require_documentation_lib "documentation drive command" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
	require_documentation_lib "documentation exact assertion domain" "-domain documentation_edges"
	require_documentation_lib "documentation non-vacuity framing" "three-edge exact set"
	require_documentation_cells "claimed row targets documentation materialization" '"documentation_materialization"'
	require_documentation_cells "kill cell proves a retry above baseline" "ifa_fault_assert_retried_above"
	require_documentation_cells "graph-write cell selects queue-retry" '"queue-retry"'
	require_documentation_cells "graph-write cell targets durable documentation marker" "ifa_fault_assert_once_fault_marker"
	# #6149 precedent (deployable_unit_edges): documentationEdgeMaterializationHandler
	# never writes shared_projection_intents (no IntentWriter field, only
	# EdgeWriter.WriteEdges), so a count against that table was always zero and
	# the guard could never fail. Repointed to the live graph: a fresh stack
	# must dump zero DOCUMENTS edges, counted via `eshu-ifa graph-dump` + jq,
	# mirroring ifa_deployable_unit_require_fresh_intents's graph-dump-plus-jq
	# shape.
	require_documentation_cells "graph-write cell fresh-stack guard dumps the graph, not a Postgres query" '"${bin_dir}/eshu-ifa" graph-dump -out "${ifa_documentation_fresh_stack_dump_path}"'
	require_documentation_cells "graph-write cell fresh-stack guard counts DOCUMENTS edges via jq" 'select(.type == "DOCUMENTS")'
	require_documentation_cells "graph-write cell fresh-stack guard uses its own distinctly-named global dump path, not local" 'ifa_documentation_fresh_stack_dump_path="$(mktemp)"'
	rg --quiet --fixed-strings -- 'local dump_path' "${documentation_cells_lib}" \
		&& fail "documentation fresh-stack guard's graph-dump scratch path must not be declared local in ${documentation_cells_lib} -- its RETURN trap references it after the local binding would already be torn down (#6149 precedent)"
	require_documentation_cells "documentation precondition preserves the dump's exact exit code" 'return "${dump_rc}"'
	require_documentation_cells "documentation precondition preserves the jq count's exact exit code" 'return "${count_rc}"'
	require_documentation_cells "documentation precondition distinguishes empty output" "edge count came back empty"
	require_documentation_cells "documentation precondition rejects non-numeric output" "edge count %q is non-numeric"
	require_documentation_cells "documentation precondition reports stale edges" "survived fresh_stack"
	require_documentation_cells "documentation precondition renamed off the old intents-based name" "ifa_documentation_require_fresh_documents_edges() {"
	rg --quiet --fixed-strings -- 'SELECT count(*) FROM shared_projection_intents' "${documentation_cells_lib}" \
		&& fail "ifa_documentation_require_fresh_documents_edges must not query shared_projection_intents any more (vacuous for this family -- documentationEdgeMaterializationHandler never writes it) in ${documentation_cells_lib}"
	rg --quiet --fixed-strings -- 'ifa_documentation_require_fresh_intents() {' "${documentation_cells_lib}" \
		&& fail "the old ifa_documentation_require_fresh_intents name must not survive as a callable function definition in ${documentation_cells_lib} (the name may still appear in prose explaining the rename)"
	require_documentation_cells "both cells exact-assert three edges" "ifa_documentation_assert"
	# #6149 follow-up item 9: the fail-graph-write cell used to also print a
	# documentation_edges shared_projection_intents "intent window" that
	# always read total=0 (the same vacuous table this family never writes --
	# see item 1/#6149's ifa_documentation_require_fresh_documents_edges). A
	# zero that cannot change reads as a finding during a failure
	# investigation. Removed, not repointed at the graph:
	# ifa_documentation_assert's own diff already covers what a graph-based
	# count here would report, strictly less precisely.
	rg --quiet --fixed-strings -- 'documentation_edges intent window' "${documentation_cells_lib}" \
		&& fail "the always-zero documentation_edges intent-window diagnostic must not survive (vacuous for this family -- see #6149 item 9) in ${documentation_cells_lib}"
	rg --quiet --fixed-strings -- "FROM shared_projection_intents WHERE projection_domain = 'documentation_edges'" "${documentation_cells_lib}" \
		&& fail "the fail-graph-write cell must not query shared_projection_intents for documentation_edges any more (vacuous for this family) in ${documentation_cells_lib}"
	# Explicit return 0: this function's last statement is `rg ... && fail`,
	# a negative assertion whose PASSING outcome is rg finding nothing (exit
	# 1). At top level that is harmless under set -e (a cmd1 of a && list is
	# exempt), but as a FUNCTION's final statement its exit status becomes
	# the function's own return value -- and a bare `run_ifa_fault_injection_
	# documentation_registry_cases` call site is NOT itself part of a &&/||
	# list, so set -e sees an ordinary nonzero-returning command and aborts
	# the whole script silently, with no fail() message, even though every
	# check actually passed. Confirmed live: without this line the script
	# exited 1 with zero output the moment this function was wrapped around
	# these checks. Every check above still fails loudly and immediately via
	# fail()'s own `exit 1` when it actually finds a real violation; this
	# return only fires once nothing did.
	return 0
}
