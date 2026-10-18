#!/usr/bin/env bash
# shellcheck disable=SC2154
# Numbered per-cell pins for scripts/test-verify-ifa-fault-injection.sh --
# cells 10 and 11 (duplicate-delivery, delta-retract) and the candidate-adjacent
# kill/reclaim/graph-write cells 2/3/4/5/6/12, plus the generic-cell and
# shared-lock pins that sit with them.
#
# Extracted so that mirror stays under the repository's 500-line cap, which it
# had reached exactly (499/500) -- the same split, for the same reason, as the
# ~20 case modules it already sources. `fail` and every *_lib path variable come
# from the parent, and every require_* helper here is defined in
# test-ifa-fault-injection-assertions.sh, so this module MUST be sourced after
# that one.
# ifa_fault_cell_call_order_ok FILE FUNC NEEDLE... succeeds when, in the body of
# FUNC, the first code line (comments skipped) containing each needle appears in
# the order given. A needle that is missing fails the check.
ifa_fault_cell_call_order_ok() {
	local file="$1" func="$2" line stripped in_func=0 n=0 i prev=0
	shift 2
	local -a needles=("$@") found=()
	for i in "${!needles[@]}"; do found[i]=0; done
	while IFS= read -r line || [[ -n "${line}" ]]; do
		n=$((n + 1))
		if (( ! in_func )); then
			[[ "${line}" == "${func}() {" ]] && in_func=1
			continue
		fi
		[[ "${line}" == "}" ]] && break
		stripped="${line#"${line%%[![:space:]]*}"}"
		[[ "${stripped}" == "#"* ]] && continue
		for i in "${!needles[@]}"; do
			if (( found[i] == 0 )) && [[ "${line}" == *"${needles[i]}"* ]]; then
				found[i]=${n}
			fi
		done
	done <"${file}"
	for i in "${!needles[@]}"; do
		(( found[i] > prev )) || return 1
		prev=${found[i]}
	done
}

# ifa_fault_seed_moved_cell_call FILE CALL BEFORE prints FILE with the
# multi-line call that starts with CALL moved to just above the first line that
# contains BEFORE. With BEFORE empty the call is removed. A call spans its first
# line plus the continuation lines that end the previous line with a backslash.
# It is the seeded violation for the order pin.
ifa_fault_seed_moved_cell_call() {
	awk -v call="$2" -v before="$3" '
		{ l[NR] = $0 }
		END {
			for (i = 1; i <= NR; i++) {
				if (!c && index(l[i], "\t" call " ") == 1) c = i
			}
			if (!c) exit 1
			e = c
			while (l[e] ~ /\\$/) e++
			for (i = 1; i <= NR; i++) if (!t && before != "" && i > e && index(l[i], before)) t = i
			for (i = 1; i <= c; i++) if (!t && before != "" && index(l[i], before)) t = i
			if (before != "" && !t) exit 1
			for (i = 1; i <= NR; i++) {
				if (i == t) for (j = c; j <= e; j++) print l[j]
				if (i >= c && i <= e) continue
				print l[i]
			}
		}' "$1"
}

run_ifa_fault_injection_cell_pins_cases() {
	# Cell 10 (duplicate-delivery, #5544) -- numbered as the gate header numbers
	# it, not by #5544's own 'cell 6' wording, which collides with #5555's cells.
	# The redelivery must actually reset rows.
	# Without the >0 assertion the second drain is a no-op and every downstream
	# digest comparison passes vacuously -- the inert-gate defect #5974 records.
	require_delivery_cells "duplicate-delivery redelivers via the shared helper" "ifa_fault_redeliver_succeeded"
	require_delivery_cells "duplicate-delivery asserts the redelivery was non-vacuous" '[[ -n "${reset_count}" && "${reset_count}" -gt 0 ]]'
	# EXACTLY TWO, and the second is the point: the cell drains, redelivers, then
	# drains AGAIN. Both calls are the same line, so a -ge 1 pin stayed green with
	# the post-redelivery drain -- the half that proves redelivery converges --
	# deleted (#6161).
	require_delivery_cells_count "duplicate-delivery drains before AND after redelivery" "run_drain_gate duplicatedelivery" 2
	require_delivery_cells "duplicate-delivery proves idempotency against the baseline" "assert_matches_baseline duplicatedelivery"
	require_lib "redelivery clears the lease, not only the status" "lease_owner = NULL"
	require_lib "redelivery makes the row visible again" "visible_at = now()"
	require_lib "redelivery counts what it actually wrote (CTE, not a second SELECT)" "SELECT count(*) FROM redelivered;"

	# Cell 11 (delta-retract, #5544; #5544 calls it 'cell 7'): shares the
	# determinism gate's helper so the
	# two gates cannot drift on what a correctly-landed delta means, and asserts
	# SQL and rationale generation 1 landed BEFORE driving generation 2 -- otherwise "the retract
	# removed it" and "it never arrived" look identical.
	# Match the CALL, not the bare helper name: this file's own comment names
	# ifa_det_run_sql_delta_live, so a bare-name needle stays green when the
	# invocation is deleted. Proven by seeding exactly that deletion -- the
	# bare-name form passed, this argument-shaped form fails.
	require_delivery_cells_multiline "delta-retract drives gen 2 through the shared helper" $'ifa_det_run_sql_delta_live \\\n\t\t1 "${bin_dir}" "${sql_delta_cassette}"'

	require_delivery_cells_multiline "delta-retract reasserts the unaffected code-call family exactly" $'ifa_code_call_assert "deltaretract" "${bin_dir}" "${code_call_expected_edges}"'
	# Bind the CALL, not the name: the function's own definition satisfied the old
	# needle, so the sole call site could be replaced with `true` and the
	# collateral-edge comparison silently stopped running (#6161).
	require_delivery_cells_multiline "delta-retract compares collateral graph truth outside exactly asserted families" $'ifa_fault_compare_collateral_edges \\\n\t\t"${work_dir}/graph-baseline.dump"'
	require_delivery_cells "delta-retract asserts generation 1 landed first" "generation-1 SQL edge set did not match before the delta was driven"
	require_delivery_cells "delta-retract collateral success names every exact family" "outside exact SQL/code-call/rationale assertions"
	require_catalog "delta-retract overview names the combined generation-2 drive" "generation-2 SQL and rationale cassettes"
	require_catalog "delta-retract overview names the rationale exact proof" "rationale exact-one edge record, Charge survivor, and durable lifecycle"
	[[ "$(_ifa_count_code_matches 'scripts/lib/ifa_sql_delta_live.sh' "${sources_lib}")" -ge 1 ]] \
		|| fail "gate source inventory omits the shared delta-live helper"
	require_fixture "gate defines the delta expected-edge set" "sql_delta_expected_edges="
	if rg --fixed-strings --quiet -- "ifa_fault_compare_non_sql_edges" "${delivery_cells_lib}"; then
		fail "delta-retract must not compare whole non-SQL graph-dump endpoint hashes: SQL generation updates legitimately replace SQL-owned CONTAINS/REPO_CONTAINS hashes; assert unaffected covered families exactly instead"
	fi
	# Cell 11 CHANGES the graph on purpose (gen 2 adds and retracts edges), so a
	# baseline-digest comparison would fail correctly and invite the wrong fix.
	# Its exactness assertion is the expected-v2 set, which names the edges.
	if rg --fixed-strings --quiet -- "assert_matches_baseline deltaretract" "${delivery_cells_lib}"; then
		fail "cell_deltaretract must NOT compare to the baseline digest: generation 2 intentionally changes the graph, so its proof is the expected-v2 edge set, not digest equality"
	fi

	require "fail-terminal explicitly excluded with rationale" "fail-terminal (a thirty-fourth possible cell) is deliberately NOT included"

	# Candidate-adjacent kill/reclaim cells (generic, SQL, code-call,
	# documentation, and rationale):
	# real kill -9 + a fresh process, not the hermetic-only faultreplay kind.
	# Killworker still waits for a live claim. Expirelease uses an exact
	# held-handler handshake instead of an observation-only count (#7939).
	require_cells_count "claimed-row wait before generic kill" "ifa_fault_wait_for_claimed" 1
	require_cells "kill, join, and untrack the live reducer" 'ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL'
	require_cells "fresh reducer process after kill" "reducer-killworker-after"
	require_sql_cells "SQL-targeted claimed-row wait before kill" "ifa_fault_wait_for_claimed"
	require_sql_cells "SQL-targeted claimed-wait scoped to the SQL domain, not any item" '"sql_relationship_materialization")"'
	require_sql_cells "SQL-targeted kill, join, and untrack the live reducer" 'ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL'

	# Cell 3 (expire-lease-mid-handler): direct SQL forced expiry, no kill.
	# The SQL itself (claimed/running rows, ack lock order) lives in
	# ifa_fault_expire_held_claim and is proven by the barrier cases.
	require_cells "forced lease expiry through the lock-ordered helper" "ifa_fault_expire_held_claim"
	require_cells "claim-bound held handler" "ifa_fault_wait_for_held_claim"
	require_cells "same-item replacement while original held" "ifa_fault_wait_for_held_replacement"
	require_cells "identity-bound release" "ifa_fault_release_held_claim"
	require_cells "expire-lease proves the expiry caused a re-claim" "ifa_fault_assert_reclaimed_above"
	require_cells "expire-lease captures the pre-expiry re-claim baseline" 'reclaimed_before="$(ifa_fault_count_reclaimed'
	require_cells "stale ACK fenced in the real reducer after the drain" "ifa_fault_assert_stale_ack_fenced"
	# Presence is not placement: the log observation only means anything after
	# the drain gate (a fenced ACK changes no queue row, so the log is settled by
	# then), and the terminal-row read follows it, both before the reclaim-count
	# assertion. Prove the order check on the real cell, then on seeded copies
	# that move or drop each call (RED/GREEN pair).
	require_cells "terminal row of the held item read after the drain" "ifa_fault_assert_held_item_terminal"
	local order_args=(cell_expirelease "run_drain_gate expirelease" "ifa_fault_assert_stale_ack_fenced" "ifa_fault_assert_held_item_terminal" 'reclaimed_after="$(ifa_fault_assert_reclaimed_above')
	ifa_fault_cell_call_order_ok "${cells_lib}" "${order_args[@]}" \
		|| fail "cell_expirelease must call ifa_fault_assert_stale_ack_fenced then ifa_fault_assert_held_item_terminal after run_drain_gate and before the reclaim-count assertion"
	local seeded_dir seeded_call seeded_before seeded_label
	seeded_dir="$(mktemp -d)" || fail "could not create a scratch dir for the seeded cell-order violation"
	for seeded_call in ifa_fault_assert_stale_ack_fenced ifa_fault_assert_held_item_terminal; do
		for seeded_before in "run_drain_gate expirelease" "assert_no_dead_letters expirelease" ""; do
			seeded_label="${seeded_call} moved before '${seeded_before:-<removed>}'"
			ifa_fault_seed_moved_cell_call "${cells_lib}" "${seeded_call}" "${seeded_before}" >"${seeded_dir}/cells.sh" \
				|| { rm -rf "${seeded_dir}"; fail "could not seed: ${seeded_label}"; }
			if ifa_fault_cell_call_order_ok "${seeded_dir}/cells.sh" "${order_args[@]}"; then
				rm -rf "${seeded_dir}"
				fail "the cell-order pin accepted: ${seeded_label}"
			fi
		done
	done
	rm -rf "${seeded_dir}"

	# Cell 4 (fail-graph-write-once-then-succeed): queue-retry lane, CloudResource
	# MERGE anchor, ESHU_IFA_FAULT_SCRIPT wiring, and a durable non-vacuity retry
	# check (Postgres attempt_count, not the reducer log -- see the helper doc for
	# why the log grep raced the buffered-stderr flush in CI).
	require_cells "once-then-succeed script writer" "ifa_fault_write_once_script"
	require_anchors_lib "CloudResource MERGE operation_match anchor" 'cloud_resource_operation_match="MERGE (r:CloudResource"'
	require_cells "queue-retry lane selected" '"queue-retry"'
	require_cells "ESHU_IFA_FAULT_SCRIPT env wiring" "ESHU_IFA_FAULT_SCRIPT=\${fault_once_script}"
	require_cells "non-vacuity retry check for cell 4 (baseline differential)" "ifa_fault_assert_retried_above"
	# Bind the assignment FROM THE COUNTER. The `${baseline_retried:-0}` default on
	# the next line matched the old needle too, so the real count could be renamed
	# away, leaving the baseline at 0 and the later `-gt baseline` retry proof
	# vacuously true (#6161).
	require_cells "fault-free baseline retry snapshot in cell 1" 'baseline_retried="$(ifa_fault_count_retried'
	require_lib "durable retry-signal query" "SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1"
	require_lib "baseline-differential assert helper" "ifa_fault_assert_retried_above"
	# Anchored on the emitted `"kind":` line, not the bare string. Both kind names
	# also appear in this lib's PROSE, and require_framing is a whole-file match, so
	# the bare needle was satisfied by comments -- renaming the real emission left
	# the mirror green. With the prefix each occurs exactly once, on the heredoc
	# line that actually writes the script.
	require_framing "once-script JSON kind" "\"kind\": \"fail-graph-write-once-then-succeed\"" "${fault_lib}"

	# Cell 5 (restart-backend-between-phase-groups): sentinel-driven backend
	# restart, --no-compose skip, and a non-vacuity fired check.
	require_cells "restart script writer" "ifa_fault_write_restart_script"
	require_cells "sentinel suffix matches Go wiring" '.restart-sentinel"'
	require_cells "sentinel watcher invocation" "ifa_fault_watch_restart_sentinel"
	require_cells "no-compose skips cell 5" "SKIPPED (--no-compose cannot restart a backend it does not own)"
	require_cells "non-vacuity fired check for cell 5" '"${restart_fired}" == "fired"'
	require_framing "restart script JSON kind" "\"kind\": \"restart-backend-between-phase-groups\"" "${fault_lib}"
	require_lib "nornicdb restart command" "bash scripts/ci/compose.sh -p \"\${compose_project}\" -f \"\${compose_file}\" -- restart nornicdb"

	# Cell 6 (kill-worker-after-claim-sql, #5555): see the Cell 2 checks above --
	# require_sql_cells asserts the SQL-scoped variant exists distinctly.

	# Cell 12 (fail-graph-write-once-then-succeed-sql, #5555/#5974): SQL edge MERGE
	# anchor (not CloudResource), queue-retry lane, and a fired-fault proof read
	# from the marker the fault decorator writes at injection time. It does NOT
	# read the reducer log: fact_work_items attempt_count does not exist for this
	# domain's async graph writes, and the log route raced the logger's flush,
	# which is what made this cell inert in CI while passing locally (#5974).
	require_anchors_lib "SQL edge MERGE operation_match anchor" 'sql_edge_operation_match="MERGE (source)-[rel:QUERIES_TABLE]->(target)"'
	if rg --pcre2 --quiet -- 'sql_edge_operation_match="[^"]*CloudResource' "${anchors_lib}"; then
		fail "sql_edge_operation_match must not be anchored to CloudResource -- that is issue #5555's exact complaint"
	fi
	require_sql_cells "SQL-targeted once-then-succeed script writer" "ifa_fault_write_once_script"
	require_sql_cells "SQL-targeted queue-retry lane selected" '"queue-retry"'
	require_sql_cells "SQL-targeted ESHU_IFA_FAULT_SCRIPT env wiring" "ESHU_IFA_FAULT_SCRIPT=\${fault_once_script_sql}"
	require_sql_cells "SQL fired-fault proof reads the durable marker" "ifa_fault_assert_once_fault_marker"
	require_framing "SQL fired-fault proof non-vacuity framing" "non-vacuity" "${sql_cells_lib}"
	require_lib "once-fired marker function signature" 'ifa_fault_assert_once_fault_marker() {'
	require_framing "marker assertion names the retired log route and why" "races the logger's flush" "${fault_lib}"
	# The log-poll helper is retired, not merely unused: leaving a mechanism that
	# is known to go inert in CI invites the next cell to reach for it.
	if rg --fixed-strings --quiet -- "ifa_fault_assert_sql_graph_write_fired" "${fault_lib}" "${sql_cells_lib}" "${script}"; then
		fail "the retired log-poll fired-fault helper is still referenced; #5974 replaced it with ifa_fault_assert_once_fault_marker"
	fi
	require_lib "count_retried generalized with a domain arg" 'domain="${5:-gcp_resource_materialization}"'
	require_lib "assert_retried_above generalized with a domain arg" 'domain="${7:-gcp_resource_materialization}"'
	require_lib "wait_for_claimed generalized with a domain arg" 'domain="${6:-}"'

	# code_calls (#5991): every cell drives the family, baseline exact-asserts it,
	# and two dedicated cells prove queue reclaim and graph-write retry against the
	# code-call domains rather than an unrelated row that happened to run first.
	require_fixture "code-call cassette path" "testdata/cassettes/codecalls/ifa-code-call-family.json"
	require_fixture "code-call expected-edge set path" "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"
	require_fixture "code-call cassette existence guard" "code-call cassette not found"
	require_fixture "code-call expected-edge set existence guard" "code-call expected-edge set not found"
	# The anchor moved into the registry when code_calls migrated onto the generic
	# cell: the gate variable this used to pin had no readers left, so the pin was
	# guarding a dead string -- change the real anchor and it stayed green. Pin the
	# live source instead.
	rg --fixed-strings --quiet -- 'IFA_FAMILY_ANCHOR[code_calls]="MERGE (source)-[rel:CALLS]->(target)"' \
		"${repo_root}/scripts/lib/ifa_family_registry/rows/02_code_calls.sh" \
		|| fail "code_calls registry row does not carry the CALLS MERGE anchor the fail-graph-write cell targets"
	require_driver "code-call drive in every cell" "ifa_code_call_drive"
	require_cells "code-call exact assertion in baseline" "ifa_code_call_assert"
	require_cells "code-call fault-free retry baseline" '"code_call_materialization"'
	require_code "code-call kill-reclaim cell invocation" "cell_killworker_code_calls"
	require_code "code-call graph-write cell invocation" "cell_failgraphwrite_code_calls"
	require_code_call_lib "code-call drive command" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
	require_code_call_lib "code-call exact assertion domain" "-domain code_calls"
	# cell_killworker_code_calls/cell_failgraphwrite_code_calls now delegate to
	# the generic, registry-driven dispatcher (scripts/lib/ifa_fault_generic_cells.sh)
	# instead of hand-writing the kill/reclaim/drain/assert and fail-graph-write
	# skeleton per family, per that file's own WIRING header. The five literal-
	# text pins this replaces moved with the behavior, not away -- they now check
	# the mechanism's new, family-agnostic home instead of the (now one-line)
	# per-family cell body. The anchored per-family delegation proof (both
	# code_calls and rationale_edges) and the registry-row wait_key/retry-
	# baseline bindings live in scripts/lib/test-ifa-fault-injection-shard-cases.sh
	# instead of here or in test-ifa-fault-injection-rationale-cases.sh, to keep
	# every file under the line cap without a bare, prose-satisfiable needle
	# anywhere -- see that module for why a bare `cell_killworker_family
	# code_calls` substring check is not anchored enough on its own. The
	# generic_cells_lib's own `projection_domain` intent-window diagnostic IS
	# restored (the old bespoke cell had it; the generic dispatcher now does too,
	# generically, driven by the family argument every cell already passes).
	[[ "$(_ifa_count_code_matches 'scripts/lib/ifa_fault_generic_cells.sh' "${sources_lib}")" -ge 1 ]] \
		|| fail "generic-cell dispatcher is not sourced by the gate source inventory"
	require_generic_cells "generic kill cell proves a retry above baseline" "ifa_fault_assert_retried_above"
	require_generic_cells "generic graph-write cell selects queue-retry" '"queue-retry"'
	require_generic_cells "generic graph-write cell targets the durable once-fired marker" "ifa_fault_assert_once_fault_marker"
	require_generic_cells "generic graph-write cell asserts edges by the registry's per-family domain" 'assert-edges -domain "${family}" -expected "${expected}"'
	require_lib "shared-domain precondition preserves query failure" "precondition query FAILED (exit"
	require_lib "shared-domain precondition distinguishes empty output" "returned empty output"
	require_lib "shared-domain precondition rejects non-numeric output" "returned non-numeric output"
	require_lib "shared-domain precondition reports stale intents" "survived fresh_stack"
	require_lib "shared lock preserves code-call log naming" 'log_namespace="${namespace//_/-}"'
}
