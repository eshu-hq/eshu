#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2034,SC2154
# Static and focused behavioral checks for #5998's rationale live harness.
# Sourced by test-verify-ifa-fault-injection.sh to keep that verifier below
# the repository's 500-line cap.

test_ifa_rationale_counts_fail_closed() (
	# shellcheck source=scripts/lib/ifa_rationale_live.sh
	source "${rationale_lib}"
	local mode rc output

	ifa_det_pg() {
		local sql="$4"
		if [[ "${mode}" == "query-error" ]]; then
			return 7
		fi
		if [[ "${sql}" == *"shared_projection_acceptance"* ]]; then
			case "${mode}" in
			acceptance-error) return 9 ;;
			acceptance-empty) return 0 ;;
			acceptance-malformed) printf 'not-a-count\n' ;;
			acceptance-wrong) printf '2\n' ;;
			*) printf '1\n' ;;
			esac
			return 0
		fi
		case "${mode}" in
		tuple-empty) return 0 ;;
		tuple-malformed) printf '1|1|oops\n' ;;
		tuple-wrong) printf '1|1|0|4|2|2|4|0\n' ;;
		gen2-green) printf '1|1|0|1|0|1|1|0\n' ;;
		*)
			if [[ "${sql}" == *"generation_id = 'gen-ifa-rationale-family-2'"* ]]; then
				printf '1|1|0|1|0|1|1|0\n'
			else
				printf '1|1|0|4|3|1|4|0\n'
			fi
			;;
		esac
	}

	for mode in query-error tuple-empty tuple-malformed tuple-wrong \
		acceptance-error acceptance-empty acceptance-malformed acceptance-wrong; do
		rc=0
		output="$(ifa_rationale_assert_work_counts test project 1 unused compose.yaml \
			gen-ifa-rationale-family-1 '1|1|0|4|3|1|4|0' 2>&1)" || rc=$?
		[[ "${rc}" -ne 0 ]] || fail "rationale durable-count guard accepted ${mode}"
	done

	mode=green
	ifa_rationale_assert_work_counts test project 1 unused compose.yaml \
		gen-ifa-rationale-family-1 '1|1|0|4|3|1|4|0' >/dev/null \
		|| fail "rationale durable-count guard rejected the exact tuple and acceptance row"
	mode=gen2-green
	ifa_rationale_assert_work_counts test project 1 unused compose.yaml \
		gen-ifa-rationale-family-2 '1|1|0|1|0|1|1|0' >/dev/null \
		|| fail "rationale durable-count guard rejected the exact delta tuple and acceptance row"
	rc=0
	ifa_rationale_assert_work_counts test project 1 unused compose.yaml \
		gen-ifa-rationale-family-2 '1|1|0|4|3|1|4|0' >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "rationale durable-count guard accepted the baseline tuple for the delta generation"
)

test_ifa_rationale_delta_survivor_fails_closed() (
	# shellcheck source=scripts/lib/ifa_rationale_live.sh
	source "${rationale_lib}"
	local scratch expected dump rc
	scratch="$(mktemp -d -t ifa-rationale-survivor.XXXXXX)"
	expected="${scratch}/expected.json"
	dump="${scratch}/dump.json"
	trap 'rm -rf -- "${scratch}"' EXIT

	printf '%s\n' '{"generation_id":"gen-ifa-rationale-family-2","edges":[{}],"required_nodes":[{"labels":["Function"],"props":{"uid":"content-entity:e_763200c9adc3","generation_id":"gen-ifa-rationale-family-2"}}]}' >"${expected}"
	for payload in \
		'not-json' \
		'{"nodes":[]}' \
		'{"nodes":[{"labels":["Function"],"props":{"uid":"content-entity:e_763200c9adc3","generation_id":"gen-ifa-rationale-family-1"}}],"edges":[]}' \
		'{"nodes":[{"labels":["Function"],"props":{"uid":"content-entity:e_763200c9adc3","generation_id":"gen-ifa-rationale-family-2"}},{"labels":["Function"],"props":{"uid":"content-entity:e_763200c9adc3","generation_id":"gen-ifa-rationale-family-2"}}],"edges":[]}'; do
		printf '%s\n' "${payload}" >"${dump}"
		rc=0
		ifa_rationale_query_delta_survivor_node test "${dump}" "${expected}" >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] || fail "rationale delta survivor query accepted malformed, missing, stale, or duplicate graph truth: ${payload}"
	done
	printf '%s\n' '{"nodes":[{"labels":["Function"],"props":{"uid":"content-entity:e_763200c9adc3","generation_id":"gen-ifa-rationale-family-2"}}],"edges":[]}' >"${dump}"
	ifa_rationale_query_delta_survivor_node test "${dump}" "${expected}" >/dev/null \
		|| fail "rationale delta survivor query rejected the exact delta-generation node"
	printf '%s\n' '{"generation_id":"gen-ifa-rationale-family-2","edges":[],"required_nodes":[]}' >"${expected}"
	rc=0
	ifa_rationale_query_delta_survivor_node test "${dump}" "${expected}" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "rationale delta survivor query accepted an omitted edge/node oracle"
)

test_ifa_rationale_delta_collateral_is_symmetric() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_collateral_nodes.sh
	source "${collateral_nodes_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_delivery_cells.sh
	source "${delivery_cells_lib}"
	local scratch baseline changed hostile rc wrong_generation edge_count compare_baseline compare_changed
	scratch="$(mktemp -d -t ifa-rationale-collateral.XXXXXX)"
	trap 'rm -rf -- "${scratch}"' EXIT
	baseline="${scratch}/baseline.dump"
	changed="${scratch}/changed.dump"

	write_rationale_delta_dump() {
		local output="$1" generation="$2" local_edge_count="$3" mutation="${4:-none}"
		local repo file charge invoice survivor_rationale retired_why retired_note foreign_from foreign_to
		local repo_hash file_hash charge_hash invoice_hash survivor_hash retired_why_hash retired_note_hash
		local foreign_from_hash foreign_to_hash
		repo="$(printf '{"labels":["Repository"],"props":{"generation_id":"%s","repo_id":"repository:r_f781caa5"}}' "${generation}")"
		file="$(printf '{"labels":["File"],"props":{"generation_id":"%s","language":"python","path":"/repo-rationale/services/payments/charge.py","relative_path":"services/payments/charge.py","repo_id":"repository:r_f781caa5","uid":"repository:r_f781caa5:services/payments/charge.py"}}' "${generation}")"
		charge="$(printf '{"labels":["Function"],"props":{"generation_id":"%s","name":"charge","relative_path":"services/payments/charge.py","repo_id":"repository:r_f781caa5","uid":"content-entity:e_763200c9adc3"}}' "${generation}")"
		invoice='{"labels":["Function"],"props":{"generation_id":"gen-ifa-rationale-family-1","name":"issue_invoice","relative_path":"services/billing/invoice.py","repo_id":"repository:r_f781caa5","uid":"content-entity:e_2dc98238d686"}}'
		survivor_rationale='{"labels":["Rationale"],"props":{"repo_id":"repository:r_f781caa5","uid":"rationale:survivor"}}'
		retired_why='{"labels":["Rationale"],"props":{"repo_id":"repository:r_f781caa5","uid":"rationale:retired-why"}}'
		retired_note='{"labels":["Rationale"],"props":{"repo_id":"repository:r_f781caa5","uid":"rationale:retired-note"}}'
		foreign_from='{"labels":["Rationale"],"props":{"repo_id":"repository:foreign","uid":"rationale:foreign"}}'
		foreign_to='{"labels":["Workload"],"props":{"uid":"workload-unrelated"}}'
		[[ "${mutation}" != "file-property" ]] || file="$(printf '%s\n' "${file}" | jq '.props.language = "CORRUPTED"')"
		[[ "${mutation}" != "charge-property" ]] || charge="$(printf '%s\n' "${charge}" | jq '.props.name = "CORRUPTED"')"
		repo_hash="$(printf '%s\n' "${repo}" | jq -S . | ifa_fault_sha256_stdin)"
		file_hash="$(printf '%s\n' "${file}" | jq -S . | ifa_fault_sha256_stdin)"
		charge_hash="$(printf '%s\n' "${charge}" | jq -S . | ifa_fault_sha256_stdin)"
		invoice_hash="$(printf '%s\n' "${invoice}" | jq -S . | ifa_fault_sha256_stdin)"
		survivor_hash="$(printf '%s\n' "${survivor_rationale}" | jq -S . | ifa_fault_sha256_stdin)"
		retired_why_hash="$(printf '%s\n' "${retired_why}" | jq -S . | ifa_fault_sha256_stdin)"
		retired_note_hash="$(printf '%s\n' "${retired_note}" | jq -S . | ifa_fault_sha256_stdin)"
		foreign_from_hash="$(printf '%s\n' "${foreign_from}" | jq -S . | ifa_fault_sha256_stdin)"
		foreign_to_hash="$(printf '%s\n' "${foreign_to}" | jq -S . | ifa_fault_sha256_stdin)"
		jq -n --argjson repo "${repo}" --argjson file "${file}" --argjson charge "${charge}" --argjson invoice "${invoice}" \
			--argjson survivor_rationale "${survivor_rationale}" --argjson retired_why "${retired_why}" \
			--argjson retired_note "${retired_note}" \
			--argjson foreign_from "${foreign_from}" --argjson foreign_to "${foreign_to}" \
			--arg repo_hash "${repo_hash}" --arg file_hash "${file_hash}" --arg charge_hash "${charge_hash}" --arg invoice_hash "${invoice_hash}" \
			--arg survivor_hash "${survivor_hash}" --arg retired_why_hash "${retired_why_hash}" \
			--arg retired_note_hash "${retired_note_hash}" \
			--arg foreign_from_hash "${foreign_from_hash}" --arg foreign_to_hash "${foreign_to_hash}" \
			--arg generation "${generation}" --arg mutation "${mutation}" --argjson local_edge_count "${local_edge_count}" '
			def owned_props:
				if $mutation == "missing-props" then null else
					{generation_id:$generation,evidence_source:"projector/canonical"}
					| if $mutation == "missing-generation" then del(.generation_id)
				  elif $mutation == "wrong-evidence" then .evidence_source = "collector/hostile"
				  else . end
				end;
			{
				nodes: [$repo, $file, $charge, $invoice, $survivor_rationale, $retired_why, $retired_note, $foreign_from, $foreign_to],
				edges: ([
					{type:"REPO_CONTAINS",from:$repo_hash,to:$file_hash,props:owned_props},
					{type:"CONTAINS",from:$file_hash,to:$charge_hash,props:owned_props},
					{type:"EXPLAINS",from:$survivor_hash,to:$invoice_hash,props:{evidence_source:"reducer/rationale"}},
					{type:(if $mutation == "foreign-topology" then "DEPENDS_ON" else "EXPLAINS" end),from:$foreign_from_hash,to:$foreign_to_hash,props:{evidence_source:"reducer/rationale",evidence:(if $mutation == "foreign-property" then "CORRUPTED" else "stable" end)}}
				]
				+ (if $local_edge_count == 3 then [
					{type:"EXPLAINS",from:$retired_why_hash,to:$charge_hash,props:{evidence_source:"reducer/rationale"}},
					{type:"EXPLAINS",from:$retired_note_hash,to:$charge_hash,props:{evidence_source:"reducer/rationale"}}
				] else [] end))
			}
		' >"${output}"
	}

	write_rationale_delta_dump "${baseline}" gen-ifa-rationale-family-1 3
	write_rationale_delta_dump "${changed}" gen-ifa-rationale-family-2 1
	ifa_fault_compare_collateral_edges \
		"${baseline}" "${changed}" "${scratch}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
		repository:r_f781caa5 services/payments/charge.py /repo-rationale/services/payments/charge.py \
		gen-ifa-rationale-family-1 gen-ifa-rationale-family-2 \
		|| fail "valid rationale File+Charge generation transition did not normalize symmetrically"
	for side in baseline changed; do
		hostile="${scratch}/wrong-${side}-generation.dump"
		wrong_generation=gen-1
		edge_count=3
		if [[ "${side}" == "changed" ]]; then
			wrong_generation=gen-2
			edge_count=1
		fi
		write_rationale_delta_dump "${hostile}" "${wrong_generation}" "${edge_count}"
		rc=0
		if [[ "${side}" == "baseline" ]]; then
			ifa_fault_compare_collateral_edges \
				"${hostile}" "${changed}" "${scratch}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
				repository:r_f781caa5 services/payments/charge.py /repo-rationale/services/payments/charge.py \
				gen-ifa-rationale-family-1 gen-ifa-rationale-family-2 >/dev/null 2>&1 || rc=$?
		else
			ifa_fault_compare_collateral_edges \
				"${baseline}" "${hostile}" "${scratch}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
				repository:r_f781caa5 services/payments/charge.py /repo-rationale/services/payments/charge.py \
				gen-ifa-rationale-family-1 gen-ifa-rationale-family-2 >/dev/null 2>&1 || rc=$?
		fi
		[[ "${rc}" -eq 2 ]] || fail "rationale collateral accepted SQL ${side} generation namespace; got status ${rc}, want 2"
	done
	for side in baseline changed; do
		for mutation in missing-props missing-generation wrong-evidence; do
			hostile="${scratch}/${side}-${mutation}.dump"
			compare_baseline="${baseline}"
			compare_changed="${changed}"
			if [[ "${side}" == "baseline" ]]; then
				write_rationale_delta_dump "${hostile}" gen-ifa-rationale-family-1 3 "${mutation}"
				compare_baseline="${hostile}"
			else
				write_rationale_delta_dump "${hostile}" gen-ifa-rationale-family-2 1 "${mutation}"
				compare_changed="${hostile}"
			fi
			rc=0
			ifa_fault_compare_collateral_edges \
				"${compare_baseline}" "${compare_changed}" \
				"${scratch}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
				repository:r_f781caa5 services/payments/charge.py /repo-rationale/services/payments/charge.py \
				gen-ifa-rationale-family-1 gen-ifa-rationale-family-2 >/dev/null 2>&1 || rc=$?
			[[ "${rc}" -eq 2 ]] || fail "${side} owned containment ${mutation} returned ${rc}, want fail-closed status 2"
		done
	done
	for mutation in file-property charge-property foreign-property foreign-topology; do
		hostile="${scratch}/${mutation}.dump"
		write_rationale_delta_dump "${hostile}" gen-ifa-rationale-family-2 1 "${mutation}"
		rc=0
		ifa_fault_compare_collateral_edges \
			"${baseline}" "${hostile}" "${scratch}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
			repository:r_f781caa5 services/payments/charge.py /repo-rationale/services/payments/charge.py \
			gen-ifa-rationale-family-1 gen-ifa-rationale-family-2 \
			>/dev/null 2>&1 || rc=$?
		[[ "${rc}" -eq 1 ]] || fail "rationale collateral ${mutation} returned ${rc}, want exact-diff status 1"
	done
	for mutation in foreign-property foreign-topology; do
		hostile="${scratch}/baseline-${mutation}.dump"
		write_rationale_delta_dump "${hostile}" gen-ifa-rationale-family-1 3 "${mutation}"
		rc=0
		ifa_fault_compare_collateral_edges \
			"${hostile}" "${changed}" "${scratch}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
			repository:r_f781caa5 services/payments/charge.py /repo-rationale/services/payments/charge.py \
			gen-ifa-rationale-family-1 gen-ifa-rationale-family-2 \
			>/dev/null 2>&1 || rc=$?
		[[ "${rc}" -eq 1 ]] || fail "baseline rationale collateral ${mutation} returned ${rc}, want exact-diff status 1"
	done
)

test_ifa_killed_child_is_joined_and_untracked() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	local victim_pid survivor_pid unowned_pid exited_pid
	sleep 30 &
	victim_pid=$!
	sleep 30 &
	survivor_pid=$!
	sleep 30 &
	unowned_pid=$!
	trap 'kill "${victim_pid}" "${survivor_pid}" "${unowned_pid}" >/dev/null 2>&1 || true; wait >/dev/null 2>&1 || true' EXIT
	bg_pids=("${victim_pid}" "${survivor_pid}")
	if ifa_det_stop_join_untrack_bg_pid "${unowned_pid}" KILL >/dev/null 2>&1; then
		fail "stop/join/untrack helper accepted a child outside the caller ownership set"
	fi
	kill -0 "${unowned_pid}" >/dev/null 2>&1 \
		|| fail "stop/join/untrack helper signaled an unowned child"
	ifa_det_stop_join_untrack_bg_pid "${victim_pid}" KILL \
		|| fail "stop/join/untrack helper rejected an owned live child"
	if kill -0 "${victim_pid}" >/dev/null 2>&1; then
		fail "stop/join/untrack helper left the killed child alive"
	fi
	[[ " ${bg_pids[*]} " != *" ${victim_pid} "* ]] \
		|| fail "stop/join/untrack helper retained the joined child PID"
	[[ " ${bg_pids[*]} " == *" ${survivor_pid} "* ]] \
		|| fail "stop/join/untrack helper removed a different owned child"

	(exit 7) &
	exited_pid=$!
	sleep 0.05
	bg_pids=("${exited_pid}" "${survivor_pid}")
	if ifa_det_stop_join_untrack_bg_pid "${exited_pid}" KILL >/dev/null 2>&1; then
		fail "stop/join/untrack helper accepted an already-exited child as a KILL injection"
	fi
	[[ " ${bg_pids[*]} " == *" ${exited_pid} "* ]] \
		|| fail "failed KILL precheck discarded ownership of an already-exited child"
	ifa_det_stop_join_untrack_bg_pid "${exited_pid}" TERM \
		|| fail "TERM cleanup rejected an already-exited owned child"
	[[ " ${bg_pids[*]} " != *" ${exited_pid} "* ]] \
		|| fail "TERM cleanup retained an already-exited joined child"
	[[ " ${bg_pids[*]} " == *" ${survivor_pid} "* ]] \
		|| fail "stop/join/untrack helper removed a live sibling while joining an exited child"
)

test_ifa_stop_join_failures_preserve_ownership() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	local event_log rc
	event_log="$(mktemp)"
	trap 'rm -f "${event_log}"' EXIT

	bg_pids=(41001)
	kill() {
		if [[ "$1" == "-0" ]]; then
			return 0
		fi
		printf 'signal\n' >>"${event_log}"
		return 1
	}
	# shellcheck disable=SC2329 # invoked indirectly by the helper under test
	wait() {
		printf 'wait\n' >>"${event_log}"
		return 0
	}
	rc=0
	ifa_det_stop_join_untrack_bg_pid 41001 KILL >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "stop/join/untrack helper accepted failed required signal delivery"
	[[ " ${bg_pids[*]} " == *" 41001 "* ]] \
		|| fail "stop/join/untrack helper discarded ownership after failed signal delivery"
	[[ "$(<"${event_log}")" == "signal" ]] \
		|| fail "stop/join/untrack helper waited after failed signal delivery"

	: >"${event_log}"
	bg_pids=(41002)
	kill() {
		[[ "$1" != "-0" ]]
	}
	# shellcheck disable=SC2329 # invoked indirectly by the helper under test
	wait() {
		printf 'wait\n' >>"${event_log}"
		return 127
	}
	rc=0
	ifa_det_stop_join_untrack_bg_pid 41002 TERM >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "stop/join/untrack helper accepted wait status 127 for a non-child"
	[[ " ${bg_pids[*]} " == *" 41002 "* ]] \
		|| fail "stop/join/untrack helper discarded ownership after wait status 127"

	: >"${event_log}"
	bg_pids=(41003)
	kill() {
		[[ "$1" != "-0" ]]
	}
	# shellcheck disable=SC2329 # invoked indirectly by the helper under test
	wait() {
		printf 'wait\n' >>"${event_log}"
		return 9
	}
	ifa_det_untrack_bg_pid() {
		printf 'untrack\n' >>"${event_log}"
		bg_pids=()
	}
	ifa_det_stop_join_untrack_bg_pid 41003 TERM \
		|| fail "stop/join/untrack helper rejected a conclusively joined exited child"
	[[ "$(<"${event_log}")" == $'wait\nuntrack' ]] \
		|| fail "stop/join/untrack helper did not join before untracking"
)

run_ifa_rationale_live_static_cases() {
	local needle invocation_count

	for needle in \
		"scripts/lib/ifa_rationale_live.sh" \
		"scripts/lib/ifa_fault_injection_rationale_cells.sh" \
		'testdata/cassettes/rationale/ifa-rationale-family.json' \
		'testdata/cassettes/rationale/ifa-rationale-family-delta.json' \
		'go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json' \
		'go/internal/ifa/testdata/rationale/ifa-rationale-family-delta-live-expected-records.json' \
		'rationale_edge_operation_match="MERGE (rationale)-[rel:EXPLAINS]->(target)"'; do
		rg --fixed-strings --quiet -- "${needle}" "${script}" "${fixtures_lib}" \
			|| fail "fault verifier missing rationale wiring: ${needle}"
	done

	for needle in \
		'ifa_rationale_drive "${cell}"' \
		'assert_rationale_truth() {' \
		'ifa_rationale_assert "${cell}"' \
		'ifa_rationale_assert_work_counts "${cell}"'; do
		rg --fixed-strings --quiet -- "${needle}" "${driver_lib}" \
			|| fail "fault driver missing per-cell rationale contract: ${needle}"
	done

	for needle in \
		'1|1|0|4|3|1|4|0' \
		'1|1|0|1|0|1|1|0' \
		"projection_domain = 'rationale_edges'" \
		"payload->>'action' = 'upsert'" \
		"payload->>'action' = 'refresh'" \
		'shared_projection_acceptance'; do
		rg --fixed-strings --quiet -- "${needle}" "${rationale_lib}" \
			|| fail "rationale helper missing fail-closed durable assertion: ${needle}"
	done

	for needle in \
		'ifa_fault_require_fresh_domain_intents() {' \
		'ifa_fault_start_shared_intent_lock() {' \
		'ifa_fault_release_shared_intent_lock() {'; do
		rg --fixed-strings --quiet -- "${needle}" "${fault_lib}" \
			|| fail "fault common library missing generalized shared-intent helper: ${needle}"
	done
	rg --fixed-strings --quiet -- 'ifa_fault_start_shared_intent_lock "killworkercodecalls"' "${code_call_cells_lib}" \
		|| fail "code-call kill cell did not move onto the generalized shared-intent lock"
	rg --fixed-strings --quiet -- 'ifa_fault_require_fresh_domain_intents' "${code_call_cells_lib}" \
		|| fail "code-call graph-fault cell did not move onto the generalized fresh-domain guard"

	for needle in \
		'cell_killworker_rationale() {' \
		'"rationale_materialization")' \
		'"${baseline_rationale_retried}"' \
		'cell_failgraphwrite_rationale() {' \
		'rationale_edges' \
		'"queue-retry"' \
		'ifa_fault_assert_once_fault_marker'; do
		rg --fixed-strings --quiet -- "${needle}" "${rationale_cells_lib}" \
			|| fail "rationale fault cells missing targeted recovery proof: ${needle}"
	done

	# Needles carry the ifa_fault_shard_run prefix (scripts/lib/ifa_fault_shard.sh):
	# every dispatch line routes through that wrapper for --shard skip support,
	# and the check is strictly stronger for it -- an unwrapped cell would
	# silently ignore --shard and run in every shard instead of just its own.
	for needle in cell_killworker_rationale cell_failgraphwrite_rationale; do
		rg --line-regexp --quiet -- "ifa_fault_shard_run ${needle}" "${script}" \
			|| fail "fault verifier does not invoke ${needle} via ifa_fault_shard_run on its own line -- missing entirely, or dispatched WITHOUT the wrapper"
	done
	# 18 = the 15 cells this family's gate defined plus the three
	# deployable_unit-targeted cells (#5993) that landed alongside it. Still a
	# COUNT pin, not weakened to an existence check: the needle now counts
	# ifa_fault_shard_run-wrapped dispatch lines, since every one of the
	# eighteen carries that prefix.
	invocation_count="$(rg --count --line-regexp -- 'ifa_fault_shard_run cell_[a-z_]+' "${script}")"
	[[ "${invocation_count}" == "18" ]] \
		|| fail "fault verifier invokes ${invocation_count} cells via ifa_fault_shard_run, want 18"
	rg --fixed-strings --quiet -- 'assert_rationale_truth "deltaretract-post-delta"' "${delivery_cells_lib}" \
		&& fail "delta-retract incorrectly demands baseline rationale truth after driving its delta generation"
	rg --fixed-strings --quiet -- 'assert_rationale_delta_truth "deltaretract-post-delta"' "${delivery_cells_lib}" \
		|| fail "delta-retract does not assert rationale exact-one and the delta-generation Charge survivor"
	rg --fixed-strings --quiet -- '"${rationale_delta_cassette}"' "${delivery_cells_lib}" \
		|| fail "delta-retract omits the rationale delta cassette from the shared delta drive"
	rg --fixed-strings --quiet -- "ifa_rationale_delta_generation_id" "${delivery_cells_lib}" \
		|| fail "delta-retract omits the rationale delta durable-count assertion"
	rg --fixed-strings --quiet -- "ifa_rationale_delta_expected_tuple" "${delivery_cells_lib}" \
		|| fail "delta-retract omits the exact rationale delta durable tuple"
	for needle in \
		'ifa_rationale_generation_id="gen-ifa-rationale-family-1"' \
		'ifa_rationale_delta_generation_id="gen-ifa-rationale-family-2"'; do
		rg --fixed-strings --quiet -- "${needle}" "${rationale_lib}" \
			|| fail "rationale live helper does not pin globally unique fixture generation: ${needle}"
	done
	for needle in \
		'"${ifa_rationale_repository_id}"' \
		'"${ifa_rationale_delta_relative_path}"' \
		'"${ifa_rationale_delta_absolute_path}"'; do
		rg --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" \
			|| fail "delta collateral comparison does not scope its rationale allowance exactly: ${needle}"
	done
	rg --fixed-strings --quiet -- 'rationale_repo_membership' "${delivery_cells_lib}" \
		|| fail "collateral edge comparison does not scope EXPLAINS exclusion to the exact rationale repository"
	rg --fixed-strings --quiet -- 'combined_repo_identities' "${delivery_cells_lib}" \
		|| fail "collateral comparison does not normalize both SQL and rationale mutable repository identities"
	if rg --fixed-strings --quiet -- 'excluded_changed_uid' "${collateral_nodes_lib}" "${delivery_cells_lib}"; then
		fail "rationale delta collateral normalization must not exclude Charge on only one side"
	fi
	local rationale_delta_drive_line shared_delta_drain_line
	rationale_delta_drive_line="$(rg -n --fixed-strings -- 'ifa_rationale_drive "delta-n${n}"' "${delta_lib}" | cut -d: -f1 || true)"
	shared_delta_drain_line="$(rg -n --fixed-strings -- 'drain SQL + rationale delta through caller-owned running projector + reducer' "${delta_lib}" | cut -d: -f1 || true)"
	[[ "${rationale_delta_drive_line}" =~ ^[0-9]+$ && "${shared_delta_drain_line}" =~ ^[0-9]+$ \
		&& "${rationale_delta_drive_line}" -lt "${shared_delta_drain_line}" ]] \
		|| fail "rationale delta generation must be driven before the shared post-delta drain"
	local delta_function
	delta_function="$(rg -U --pcre2 --only-matching -- '(?ms)^ifa_det_run_sql_delta_live\(\) \{.*?^\}' "${delta_lib}")"
	[[ "${delta_function}" != *"ifa_det_start_bg"* ]] \
		|| fail "delta helper starts a duplicate projector/reducer pair instead of reusing caller-owned workers"
	[[ "${delta_function}" == *"caller-owned running projector + reducer"* ]] \
		|| fail "delta helper does not document its one-lifecycle worker ownership"
	for target in "${cells_lib}" "${sql_cells_lib}" "${code_call_cells_lib}" "${documentation_cells_lib}" "${rationale_cells_lib}"; do
		rg --fixed-strings --quiet -- 'ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL' "${target}" \
			|| fail "$(basename "${target}") does not join/untrack the killed candidate-adjacent reducer"
	done

	test_ifa_rationale_counts_fail_closed
	test_ifa_rationale_delta_survivor_fails_closed
	test_ifa_rationale_delta_collateral_is_symmetric
	test_ifa_killed_child_is_joined_and_untracked
	test_ifa_stop_join_failures_preserve_ownership
}
