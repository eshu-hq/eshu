#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the documentation_edges fault-injection
# cells (#5994). Split from test-ifa-fault-injection-review-cases.sh to stay
# under the repository's 500-line cap. The documentation handler writes its
# graph records directly and holds no shared-projection intent, so these cases
# exercise its family-specific fresh-stack guard -- repointed at the graph by
# #6157, since the old intents query was vacuous for this family -- and the
# exact fact_work_items ACK-transition barrier that gives the family its
# mid-handler-interruption cell (#5998). Sourced by
# scripts/test-verify-ifa-fault-injection.sh alongside
# test-ifa-fault-injection-review-cases.sh, so the top-level static verifier
# stays below the repository's 500-line cap; both files share that script's
# ${documentation_cells_lib}/${det_lib}/${driver_lib} globals and `fail`
# helper.

run_ifa_documentation_live_static_cases() {
	# Every cell drives the family via drive_all_cassettes, baseline exact-asserts
	# it, and two dedicated cells prove queue reclaim and graph-write retry.
	require_fixture "documentation cassette path" "testdata/cassettes/documentation/ifa-documentation-family.json"
	require_fixture "documentation expected-edge set path" "go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"
	require_fixture "documentation cassette existence guard" "documentation cassette not found"
	require_fixture "documentation expected-edge set existence guard" "documentation expected-edge set not found"
	require_code "documentation DOCUMENTS MERGE operation_match anchor" 'documentation_edge_operation_match="MERGE (section)-[rel:DOCUMENTS]->(target)"'
	require_driver "documentation drive in every cell" "ifa_documentation_drive"
	require_cells "documentation exact assertion in baseline" "ifa_documentation_assert"
	require_cells "documentation fault-free retry baseline" '"documentation_materialization"'
	# Needles carry the ifa_fault_shard_run prefix (scripts/lib/ifa_fault_shard.sh):
	# every dispatch line routes through that wrapper for --shard skip support,
	# and the check is strictly stronger for it -- an unwrapped cell would
	# silently ignore --shard and run in every shard instead of just its own.
	rg --quiet --line-regexp -- 'ifa_fault_shard_run cell_killworker_documentation' "${script}" \
		|| fail "verifier does not invoke cell_killworker_documentation via ifa_fault_shard_run on its own line -- missing entirely, or dispatched WITHOUT the wrapper"
	rg --quiet --line-regexp -- 'ifa_fault_shard_run cell_failgraphwrite_documentation' "${script}" \
		|| fail "verifier does not invoke cell_failgraphwrite_documentation via ifa_fault_shard_run on its own line -- missing entirely, or dispatched WITHOUT the wrapper"
	require_documentation_lib "documentation drive command" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
	require_documentation_lib "documentation exact assertion domain" "-domain documentation_edges"
	require_documentation_lib "documentation non-vacuity framing" "three-edge exact set"
	require_documentation_cells "claimed row targets documentation materialization" '"documentation_materialization"'
	require_documentation_cells "kill cell proves a retry above baseline" "ifa_fault_assert_retried_above"
	require_documentation_cells "graph-write cell selects queue-retry" '"queue-retry"'
	require_documentation_cells "graph-write cell targets durable documentation marker" "ifa_fault_assert_once_fault_marker"
	# Pinned against the real precondition CALL, not against prose. The previous
	# literal here ("projection_domain = 'documentation_edges'") was satisfied
	# only by a COMMENT explaining that this probe had been deleted as vacuous,
	# so rewording that comment turned the mirror red while deleting the actual
	# precondition below left it green -- a guard pinned to the explanation of a
	# thing rather than the thing.
	require_documentation_cells "graph-write cell probes fresh DOCUMENTS edges" 'ifa_documentation_require_fresh_documents_edges "fail-graph-write-once-then-succeed-documentation"'
	require_documentation_cells "both cells exact-assert three edges" "ifa_documentation_assert"
	require_documentation_cells "documentation retry baseline is captured by the shared baseline cell" "baseline_documentation_retried is captured by cell_baseline"
	require_documentation_cells "kill cell joins and untracks its owned reducer" 'ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL'
	require_documentation_cells "documentation kill cell installs its ACK barrier" "ifa_documentation_start_ack_barrier"
	require_documentation_barrier_setup "documentation ACK barrier is a before-update trigger" "BEFORE UPDATE ON public.fact_work_items"
	require_documentation_barrier_setup "documentation ACK barrier preflight requires absent artifacts" "documentation ACK barrier preflight expected 0 artifacts"
	rg -U --fixed-strings --quiet -- $'local setup_sql="BEGIN;\nCREATE FUNCTION' "${documentation_barrier_setup_lib}" \
		|| fail "documentation ACK barrier DDL does not begin an explicit transaction"
	rg -U --fixed-strings --quiet -- $'EXECUTE FUNCTION public.ifa_documentation_block_ack();\nCOMMIT;' "${documentation_barrier_setup_lib}" \
		|| fail "documentation ACK barrier DDL does not commit after trigger creation"
	require_documentation_barrier_setup "documentation ACK barrier is domain-specific" "NEW.domain = 'documentation_materialization'"
	require_documentation_barrier_setup "documentation ACK barrier blocks only success ACKs" "NEW.status = 'succeeded'"
	require_documentation_barrier_setup "documentation ACK barrier checks the prior reducer stage" "OLD.stage = 'reducer'"
	require_documentation_barrier_setup "documentation ACK barrier checks the new reducer stage" "NEW.stage = 'reducer'"
	require_documentation_barrier_setup "documentation ACK barrier checks the prior domain" "OLD.domain = 'documentation_materialization'"
	require_documentation_barrier_setup "documentation ACK barrier checks the fixture scope" "NEW.scope_id = 'scope-ifa-documentation-family'"
	require_documentation_barrier_setup "documentation ACK barrier checks the fixture generation" "NEW.generation_id = 'gen-ifa-documentation-family-1'"
	require_documentation_barrier_setup "documentation ACK barrier starts from a live lease" "OLD.lease_owner IS NOT NULL"
	require_documentation_barrier_setup "documentation ACK barrier sees ACK clear the lease" "NEW.lease_owner IS NULL"
	require_documentation_barrier_setup "documentation ACK barrier tags the waiting backend locally" "pg_catalog.set_config('application_name', '\${waiter_app_name}', true)"
	require_documentation_barrier_setup "documentation ACK barrier uses a transaction advisory lock" "pg_advisory_xact_lock(5998, 5994)"
	require_documentation_barrier_setup "documentation ACK barrier holder uses the matching session lock" "pg_advisory_lock(5998, 5994)"
	require_documentation_barrier_setup "documentation ACK barrier contention is nonblocking" "pg_try_advisory_lock(5998, 5994)"
	require_documentation_barrier_setup "documentation ACK cleanup tracks holder ownership separately" "ifa_documentation_ack_holder_owned"
	require_documentation_barrier_setup "documentation ACK cleanup tracks DDL ownership separately" "ifa_documentation_ack_ddl_owned"
	rg -U --pcre2 --quiet -- 'ifa_documentation_start_ack_holder[\s\S]*ifa_documentation_install_ack_barrier' "${documentation_barrier_setup_lib}" \
		|| fail "documentation ACK startup does not acquire its session holder before DDL preflight/setup"
	rg -U --pcre2 --quiet -- 'ifa_documentation_start_ack_holder[^\n]*\|\| return \$\?[\s\S]*ifa_documentation_install_ack_barrier' "${documentation_barrier_setup_lib}" \
		|| fail "documentation ACK startup does not short-circuit before installer DDL when holder acquisition fails"
	require_documentation_cells "documentation kill cell proves the ACK connection is blocked" "ifa_documentation_wait_for_blocked_ack"
	require_documentation_barrier "documentation ACK proof inspects an ungranted advisory lock" "NOT barrier.granted"
	require_documentation_barrier "documentation ACK proof verifies the exact two-key lock kind" "barrier.objsubid = 2"
	require_documentation_barrier "documentation ACK proof binds the waiter lock to this database" "barrier.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database())"
	require_documentation_barrier "documentation ACK proof binds the holder lock to this database" "held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database())"
	require_documentation_barrier "documentation ACK proof verifies its blocker PID" "pg_catalog.pg_blocking_pids(waiter.pid)"
	require_documentation_cells "documentation kill cell terminates the exact blocked ACK backend" "ifa_documentation_terminate_blocked_ack"
	require_documentation_cells "documentation kill cell proves the waiter and lock are gone" "ifa_documentation_wait_for_ack_backend_gone"
	require_documentation_cells "documentation kill cell snapshots the claimed row" "ifa_documentation_claim_snapshot"
	require_documentation_barrier "documentation ACK barrier cleanup drops its trigger" "DROP TRIGGER IF EXISTS ifa_documentation_ack_barrier ON public.fact_work_items"
	require_documentation_barrier "documentation ACK barrier cleanup drops its function" "DROP FUNCTION IF EXISTS public.ifa_documentation_block_ack()"
	require_documentation_barrier "documentation backend termination proves one PostgreSQL true result" '"1|true"'
	require_documentation_barrier_setup "documentation ACK barrier uses a validated run identity" "ifa_documentation_validate_ack_identity"
	require_documentation_barrier "documentation ACK cleanup always censuses tagged waiters" "ifa_documentation_census_ack_waiter"
	require_documentation_barrier "documentation ACK cleanup censuses uncaptured holders" "ifa_documentation_census_ack_holder"
	require_documentation_barrier_setup "documentation ACK preflight rejects an occupied advisory key" "documentation ACK barrier key is already in use"
	if rg --fixed-strings --quiet -- "pg_catalog.extract(" "${documentation_barrier_lib}"; then
		fail "documentation ACK barrier schema-qualifies EXTRACT special syntax"
	fi
	require_documentation_barrier "documentation claim snapshot extracts claim_until epoch" "EXTRACT(epoch FROM claim_until)"
	require_documentation_barrier "documentation claim snapshot extracts updated_at epoch" "EXTRACT(epoch FROM updated_at)"
	rg --fixed-strings --quiet -- "ifa_documentation_probe_cleanup" "${documentation_barrier_cases_lib}" \
		|| fail "documentation ACK Postgres probe has no fail-safe cleanup trap"
	rg --fixed-strings --quiet -- "trap ifa_documentation_probe_cleanup EXIT" "${documentation_barrier_cases_lib}" \
		|| fail "documentation ACK Postgres probe does not install its fail-safe cleanup trap"
	rg --fixed-strings --quiet -- "ifa_documentation_cleanup_ack_barrier probe" "${documentation_barrier_cases_lib}" \
		|| fail "documentation ACK Postgres probe cleanup does not use the production lifecycle"
	rg --fixed-strings --quiet -- "exit 97" "${documentation_barrier_cases_lib}" \
		|| fail "documentation ACK Postgres probe does not surface cleanup failure"
	rg --fixed-strings --quiet -- "ACK_POSTGRES_PROBE_PASS" "${documentation_barrier_cases_lib}" \
		|| fail "documentation ACK Postgres probe has no terminal non-vacuity marker"
	require_documentation_barrier "documentation producer shutdown signals before joining" "ifa_documentation_signal_ack_producers"
	require_documentation_barrier "documentation producer shutdown joins after signaling" "ifa_documentation_join_ack_producers"
	require_documentation_barrier "documentation ACK trigger cleanup is bounded" "SET LOCAL lock_timeout = '2s'"
	require_documentation_barrier "documentation ACK function cleanup is independently bounded" "SET LOCAL statement_timeout = '5s'"
	require_code "global cleanup invokes documentation ACK barrier cleanup" "ifa_documentation_cleanup_ack_barrier"
	require_code "global cleanup joins ACK producers before DB cleanup" "ifa_documentation_stop_ack_producers"
	require_driver "per-cell teardown invokes documentation ACK barrier cleanup" "ifa_documentation_cleanup_ack_barrier"
	require_driver "per-cell teardown joins ACK producers before DB cleanup" "ifa_documentation_stop_ack_producers"
	rg -U --pcre2 --quiet -- 'ifa_documentation_stop_ack_producers \|\| \{ failed=1; holder_safe=0; \}[\s\S]*ifa_documentation_census_ack_waiter[\s\S]*ifa_documentation_census_ack_holder[\s\S]*ifa_documentation_drop_ack_trigger[\s\S]*ifa_documentation_drop_ack_function' \
		"${documentation_barrier_lib}" \
		|| fail "ACK failure cleanup does not join producers, fence waiter, stop holder, then attempt both DDL drops"
	if rg --fixed-strings --quiet -- "LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE" "${documentation_cells_lib}"; then
		fail "documentation kill cell still blocks shared_projection_intents, which its handler never writes"
	fi
	# Cleanup owns the only IF EXISTS drop; installation must not erase leaked
	# artifacts before checking them. This count is unconditional: `rg --quiet`
	# emits no bytes and must never guard the assertion.
	[[ "$(rg --fixed-strings --count -- "DROP TRIGGER IF EXISTS ifa_documentation_ack_barrier" "${documentation_barrier_lib}")" == "1" ]] \
		|| fail "documentation ACK barrier must contain exactly one cleanup DROP TRIGGER"
	if rg --fixed-strings --quiet -- 'kill -9 "${reducer_pid_before}"' "${documentation_cells_lib}"; then
		fail "documentation kill cell bypasses the shared join/untrack ownership helper"
	fi
	rg -U --pcre2 --quiet -- 'ifa_fault_wait_for_claimed[\s\S]*ifa_documentation_wait_for_blocked_ack[\s\S]*ifa_documentation_claim_snapshot[\s\S]*ifa_documentation_assert[\s\S]*ifa_det_stop_join_untrack_bg_pid[\s\S]*ifa_documentation_terminate_blocked_ack[\s\S]*ifa_documentation_wait_for_ack_backend_gone[\s\S]*ifa_documentation_claim_snapshot[\s\S]*ifa_documentation_release_ack_barrier[\s\S]*reducer-killworkerdocumentation-after' "${documentation_cells_lib}" \
		|| fail "documentation kill cell does not prove graph+claim+blocked ACK before KILL and clean exact backends before replacement"
	# The sequence pin above proves the guards are CALLED. These two prove the
	# claimed-row guard also FAILS CLOSED, which the sequence cannot see: a
	# `|| die` softened to `|| true` would leave the call site intact and the
	# cell would then "pass" against a queue that never claimed the row at all.
	# That inert-fixture shape is not hypothetical -- the #5998 pre-adoption
	# probe hit exactly it (a fixture INSERT rejected on a NOT NULL column left
	# every observation running against an empty table while still printing
	# plausible output), and it is the same class as #5974 and the vacuous
	# intents guard #6157 repointed.
	rg -U --pcre2 --quiet -- 'ifa_fault_wait_for_claimed[^\n]*\\\n\s*\|\| die "kill-worker-after-claim-documentation: no documentation_materialization row was claimed"' "${documentation_cells_lib}" \
		|| fail "documentation kill cell's claimed-row guard must die when no documentation_materialization row was claimed -- otherwise the cell passes against an empty queue"
	require_documentation_cells "documentation kill cell reports its non-vacuity evidence" "non-vacuous: %s claimed row; ACK backend %s blocked by holder %s after exact graph write"
	require "claimed-row proof inventory includes every lease-reclaim cell" "a claimed-row proof for cells 2/3/6/7/8/9/17"
	require "once-fired proof inventory includes every graph-write cell" "a once-fired marker for cells 4/12/13/14/15/18"
	require "retry-delay inventory includes every graph-write cell" "cells 4/12/13/14/15/18's queue-retry lane"
	require "SQL graph-write anchor has its current cell number" "cell_failgraphwrite_sql (cell 12, #5555)"
	require_driver "delta exception leaves all other cells on baseline rationale truth" "The other twenty cells remain bound"
	# The literal-label pin that used to live here ("Run Ifa fault-injection
	# matrix (18 cells, fresh stack per cell)") was migrated to
	# scripts/lib/test-ifa-fault-injection-shard-cases.sh once the
	# fault-injection job was sharded across parallel runners and that step
	# label no longer exists. The replacement proves the same
	# "coverage cannot silently drop" intent behaviorally (IFA_FAULT_SHARD is
	# wired from matrix.shard into the gate's --shard flag) and adds a
	# matrix-cardinality cross-check (workflow shard count vs.
	# IFA_FAULT_SHARD_DEFAULT_N) that a single label string never could.
	# This used to extract the scan's hand-typed file list and assert six
	# documentation libs appeared in it. That check is obsolete BY UPGRADE, not
	# by deletion: the scan now derives its list from every declared *_lib via
	# compgen, so naming individual libs proves less than the derivation does.
	# The hand-typed list was also wrong -- it covered 36 of 42 libs and had
	# silently lost the file the 500-line split created.
	#
	# So assert the DERIVATION instead, which is what makes coverage total, plus
	# the fail-closed guard that stops a missing file being skipped rather than
	# reported. Both are pinned as live code, so a comment describing them
	# cannot stand in for them.
	local assertions_src static_test
	assertions_src="${repo_root}/scripts/lib/test-ifa-fault-injection-assertions.sh"
	static_test="${repo_root}/scripts/test-verify-ifa-fault-injection.sh"
	# Pin the CALL SITE as live code. The check this replaced extracted the scan
	# block FROM the mirror, so deleting the block reddened it; reading only the
	# assertions lib cannot see whether the mirror still CALLS the scan.
	# Replacing `assert_no_private_data "${script}"` with `:` removed the entire
	# private-data scan and the mirror passed. That was a trade, not an upgrade.
	[[ "$(_ifa_count_code_matches 'assert_no_private_data' "${static_test}")" -ge 1 ]] \
		|| fail "the fault mirror no longer calls assert_no_private_data -- the private-data scan is not running at all"
	# And a floor on what it scans, so a derivation that resolves to nothing reds.
	[[ "$(_ifa_count_code_matches 'the *_lib derivation has collapsed' "${assertions_src}")" -ge 1 ]] \
		|| fail "assert_no_private_data no longer asserts a scanned-file floor -- a collapsed derivation would scan one file and pass"
	[[ "$(_ifa_count_code_matches 'compgen -v | rg' "${assertions_src}")" -ge 1 ]] \
		|| fail "private-data scan no longer derives its file list from the declared *_lib vars -- a hand-typed list stops growing when the tree does"
	[[ "$(_ifa_count_code_matches 'does not exist -- a scan that skips a missing file proves nothing' "${assertions_src}")" -ge 1 ]] \
		|| fail "private-data scan no longer fails closed on a missing file"
}

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
	# rc=0; else rc=$?; fi`) applies to the dump step exactly as it applied
	# to the old Postgres query.
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
	# cell_baseline's drive_all_cassettes, baseline exact-asserts it, and one
	# dedicated cell proves graph-write retry against the
	# documentation_materialization domain rather than an unrelated row that
	# happened to run first. The family also proves the SqlTable target case
	# (batchCanonicalDocumentationEntityEdgeCypher's MATCH label alternation,
	# TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel).
	require_fixture "documentation cassette path" "testdata/cassettes/documentation/ifa-documentation-family.json"
	require_fixture "documentation expected-edge set path" "go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"
	require_fixture "documentation cassette existence guard" "documentation cassette not found"
	require_fixture "documentation expected-edge set existence guard" "documentation expected-edge set not found"
	require_code "documentation DOCUMENTS MERGE operation_match anchor" 'documentation_edge_operation_match="MERGE (section)-[rel:DOCUMENTS]->(target)"'
	require_driver "documentation drive in every cell" "ifa_documentation_drive"
	require_cells "documentation exact assertion in baseline" "ifa_documentation_assert"
	require_cells "documentation fault-free retry baseline" '"documentation_materialization"'
	require_code "documentation graph-write cell invocation" "cell_failgraphwrite_documentation"
	# #6149 follow-up item 8: DocumentationEdgeMaterializationHandler.Handle
	# has no Postgres write to lock (only two graph writes through the same
	# EdgeWriter), so this family cannot make a kill-worker cell deterministic
	# the way code_calls/deployable_unit_correlation can. A domain-scoped
	# expire-lease cell was tried as a replacement and also failed live: the
	# handler's ~7ms duration races the forced-expiry UPDATE, so the trigger
	# never lands. Both of those mechanisms (the lock helpers and the lock
	# table they targeted) must be genuinely gone, not merely renamed or
	# swapped for an equally-vacuous sibling.
	#
	# The kill-worker CELL is a different matter and is deliberately NOT
	# asserted absent here any more: #5998 reinstated it on a mechanism that
	# does not race the handler at all -- it blocks the queue ACK through a
	# BEFORE UPDATE trigger on fact_work_items held by a separate advisory-lock
	# session. What must stay gone is the intent-lock machinery below, which is
	# what was actually vacuous.
	rg --quiet --fixed-strings -- 'cell_expirelease_documentation() {' "${documentation_cells_lib}" \
		&& fail "cell_expirelease_documentation must not survive as a callable function definition in ${documentation_cells_lib} -- it does not fire the fault live (#6149 follow-up item 8 correction)"
	rg --quiet --fixed-strings -- 'ifa_documentation_start_intent_lock' "${documentation_cells_lib}" \
		&& fail "ifa_documentation_start_intent_lock must not survive in ${documentation_cells_lib} -- this family holds no lock any more (#6149 follow-up item 8)"
	rg --quiet --fixed-strings -- 'ifa_documentation_release_intent_lock' "${documentation_cells_lib}" \
		&& fail "ifa_documentation_release_intent_lock must not survive in ${documentation_cells_lib} -- this family holds no lock any more (#6149 follow-up item 8)"
	rg --quiet --fixed-strings -- 'LOCK TABLE shared_projection_intents' "${documentation_cells_lib}" \
		&& fail "no cell in ${documentation_cells_lib} may lock shared_projection_intents any more -- the handler never writes it (#6149 follow-up item 8)"
	# ifa_fault_wait_for_claimed is expected here again: cell_killworker_
	# documentation uses it to prove a genuinely claimed row before the ACK
	# barrier blocks that row's acknowledgement (#5998). It is asserted
	# PRESENT by the kill-cell sequence pin in
	# run_ifa_documentation_live_static_cases, not absent.
	# The header must state plainly HOW this family regained a deterministic
	# mid-handler fault cell, and must keep the measured fact that defeated the
	# two earlier mechanisms -- the same honesty requirement, now discharged by
	# explaining the mechanism instead of recording a gap. It must also keep
	# the scope of the pre-adoption probe, so a reader cannot mistake a
	# mechanism proof for the live-matrix proof.
	require_documentation_cells "documentation cells header states the family regained a mid-handler cell" "MID-HANDLER INTERRUPTION IS PROVEN FOR THIS FAMILY, BY AN ACK BARRIER"
	require_documentation_cells "documentation cells header names the superseded gap ruling" "superseding the gap #6149 follow-up item 8 recorded here"
	require_documentation_cells "documentation cells header keeps the measured handler duration that defeated both earlier triggers" "duration_seconds 0.0073"
	require_documentation_cells "documentation cells header names the ACK-barrier mechanism" "pg_advisory_xact_lock(5998, 5994)"
	require_documentation_cells "documentation cells header explains why handler width no longer matters" "handler width is irrelevant"
	require_documentation_cells "documentation cells header bounds what the pre-adoption probe did not cover" "not the real"
	require_documentation_lib "documentation drive command" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
	require_documentation_lib "documentation exact assertion domain" "-domain documentation_edges"
	require_documentation_lib "documentation non-vacuity framing" "three-edge exact set"
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
	require_documentation_cells "graph-write cell exact-asserts three edges" "ifa_documentation_assert"
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
