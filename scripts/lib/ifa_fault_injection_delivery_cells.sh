#!/usr/bin/env bash
# shellcheck disable=SC2034  # The reducer_/projector_pid locals are filled
# indirectly by ifa_det_start_bg via printf -v, so shellcheck sees the
# declaration but not the write.
# shellcheck disable=SC2154  # This file is sourced by
# scripts/verify-ifa-fault-injection.sh and reads globals it owns (bin_dir,
# log_dir, work_dir, use_compose, compose_file, wall_times, digests, and the
# sql_* cassette/expected-set paths). Linting it standalone would otherwise
# bury a genuinely new SC2154 in ~20 expected ones.
#
# The two delivery-shaped fault cells from issue #5544, split from #5351's
# original design. Both live here rather than in ifa_fault_injection_cells.sh
# to keep every library under the repo's 500-line cap
# (.agents/skills/generator-script-discipline), matching the split that
# ifa_fault_injection_sql_cells.sh already established for #5555's cells.
#
# NOTE ON CELL NUMBERING: #5544 calls these "cell 6" and "cell 7", numbering
# them after the five original cells. #5555 independently numbered its SQL
# cells 6 and 7 in scripts/verify-ifa-fault-injection.sh's header. The numbers
# collide across issues, so these functions are named for what they do rather
# than where they sit in a sequence -- a positional name would be wrong from
# the day another cell lands.
#
# Like its sibling libraries this is a plain function library, not a script
# (no `set -euo pipefail`; see ifa_fault_injection_driver.sh's identical note).

# ifa_fault_sha256_stdin prints the lowercase SHA-256 of stdin on both the
# macOS development host and the Ubuntu CI runner.
ifa_fault_sha256_stdin() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 | awk '{print $1}'
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum | awk '{print $1}'
	else
		return 1
	fi
}

# ifa_fault_write_repo_node_identities maps graphdump's content-addressed node
# IDs to stable logical identities for the repository plus one explicitly
# mutable delta path and its directory ancestors. graphdump hashes the sorted,
# two-space-indented {labels,props} record including its trailing newline;
# `jq -S` emits the same byte shape. Direct intermediate files keep jq/hash
# errors observable. Nodes outside that path remain raw and exact.
ifa_fault_write_repo_node_identities() {
	local graph_dump="$1" repo_id="$2" mutable_relative_path="$3"
	local mutable_absolute_path="$4" output="$5"
	local node_records="${output}.nodes.jsonl" identity_rows="${output}.rows.jsonl"
	jq -c --arg repo_id "${repo_id}" \
		--arg mutable_relative_path "${mutable_relative_path}" \
		--arg mutable_absolute_path "${mutable_absolute_path}" '
		if ((.nodes | type) != "array") or ((.edges | type) != "array") then
			error("graph dump must contain nodes and edges arrays")
		else
			.nodes[]
			| . as $node
			| select(
				$node.props.repo_id? == $repo_id
				or $node.props.repository_id? == $repo_id
				or $node.props.id? == $repo_id
				or $node.props.uid? == $repo_id
			)
			| select(
				($node.labels | index("Repository")) != null
				or $node.props.relative_path? == $mutable_relative_path
				or $node.props.path? == $mutable_absolute_path
				or (
					(($node.labels | index("Directory")) != null)
					and (($mutable_absolute_path | startswith(($node.props.path? // "") + "/")))
				)
			)
		end
	' "${graph_dump}" >"${node_records}" || return 2
	: >"${identity_rows}"
	local node_record node_hash node_identity
	while IFS= read -r node_record; do
		node_hash="$(printf '%s\n' "${node_record}" | jq -S . | ifa_fault_sha256_stdin)" \
			|| return 2
		node_identity="$(printf '%s\n' "${node_record}" | jq -c '
			if ((.props.uid? // "") | length) > 0 then
				{labels: .labels, uid: .props.uid}
			elif ((.labels | index("Directory")) != null)
				and (((.props.path? // "") | length) > 0) then
				{labels: .labels, path: .props.path}
			elif ((.labels | index("Repository")) != null)
				and ((((.props.id? // .props.repo_id? // "")) | length) > 0) then
				{labels: .labels, repo_id: (.props.id // .props.repo_id)}
			else
				error("mutable repository node lacks a stable logical identity")
			end
		')" || return 2
		jq -nc --arg hash "${node_hash}" --argjson identity "${node_identity}" \
			'{hash: $hash, identity: $identity}' >>"${identity_rows}" || return 2
	done <"${node_records}"
	jq -s 'reduce .[] as $row ({}; .[$row.hash] = $row.identity)' \
		"${identity_rows}" >"${output}" || return 2
}

# ifa_fault_write_repo_node_membership maps every node owned by repo_id to its
# graphdump content hash. Ordinary nodes establish ownership only through
# repo_id/repository_id; id/uid fallbacks are restricted to Repository nodes so
# an unrelated node whose identifier happens to equal the repo ID cannot make
# a cross-repository edge look SQL-owned.
ifa_fault_write_repo_node_membership() {
	local graph_dump="$1" repo_id="$2" output="$3"
	local node_records="${output}.nodes.jsonl" membership_rows="${output}.rows.jsonl"
	jq -c --arg repo_id "${repo_id}" '
		if ((.nodes | type) != "array") or ((.edges | type) != "array") then
			error("graph dump must contain nodes and edges arrays")
		else
			.nodes[]
			| select(
				.props.repo_id? == $repo_id
				or .props.repository_id? == $repo_id
				or (
					((.labels | index("Repository")) != null)
					and (.props.id? == $repo_id or .props.uid? == $repo_id)
				)
			)
		end
	' "${graph_dump}" >"${node_records}" || return 2
	: >"${membership_rows}"
	local node_record node_hash
	while IFS= read -r node_record; do
		node_hash="$(printf '%s\n' "${node_record}" | jq -S . | ifa_fault_sha256_stdin)" \
			|| return 2
		jq -nc --arg hash "${node_hash}" '{hash: $hash}' >>"${membership_rows}" || return 2
	done <"${node_records}"
	jq -s 'reduce .[] as $row ({}; .[$row.hash] = true)' \
		"${membership_rows}" >"${output}" || return 2
}

# ifa_fault_write_collateral_edges writes the graph edge set outside the SQL
# and code-call families, which the cell asserts independently against exact
# expected fixtures. SQL generation 2 legitimately changes node properties,
# replacing content hashes on SQL-repository CONTAINS/REPO_CONTAINS endpoints.
# Replace only those repository-attributed hashes with stable labels+uid (or
# the Repository's repo_id). On mapped containment edges whose original
# endpoints are both members of that repository, a present projector/canonical
# generation_id must be gen-1 in the baseline. In the changed dump it must be
# gen-2 when both endpoints are mutable identities, or remain gen-1 at the
# one-mapped preserved-subtree seam. All other properties, attachment topology,
# types, and counts remain exact.
ifa_fault_write_collateral_edges() {
	local graph_dump="$1" repo_identities="$2" repo_membership="$3"
	local output="$4" comparison_side="$5"
	local sql_types='["EXECUTES","HAS_COLUMN","INDEXES","MIGRATES","QUERIES_TABLE","READS_FROM","REFERENCES_TABLE","TRIGGERS","WRITES_TO"]'
	local code_call_types='["CALLS","INSTANTIATES","REFERENCES","USES_METACLASS"]'
	jq -S --slurpfile repo_identities "${repo_identities}" \
		--slurpfile repo_membership "${repo_membership}" \
		--argjson sql_types "${sql_types}" \
		--argjson code_call_types "${code_call_types}" \
		--arg comparison_side "${comparison_side}" '
		if ($comparison_side != "baseline" and $comparison_side != "changed") then
			error("collateral comparison side must be baseline or changed")
		elif ((.nodes | type) != "array") or ((.edges | type) != "array") then
			error("graph dump must contain nodes and edges arrays")
		else
			.edges
			| map(
				. as $edge
				| select($sql_types | index($edge.type) | not)
				| select($code_call_types | index($edge.type) | not)
				| ($repo_identities[0][$edge.from] // null) as $from_identity
				| ($repo_identities[0][$edge.to] // null) as $to_identity
				| ($repo_membership[0][$edge.from] // false) as $from_repo_member
				| ($repo_membership[0][$edge.to] // false) as $to_repo_member
				| (
					($edge.type == "CONTAINS" or $edge.type == "REPO_CONTAINS")
					and ($from_identity != null or $to_identity != null)
				) as $mapped_sql_containment
				| if $mapped_sql_containment then
					$edge
					| if $from_identity != null then .from = $from_identity else . end
					| if $to_identity != null then .to = $to_identity else . end
				else
					$edge
				end
				| if (
					$mapped_sql_containment
					and $from_repo_member
					and $to_repo_member
					and ((.props | type) == "object")
					and (.props.evidence_source? == "projector/canonical")
					and (.props | has("generation_id"))
				) then
					(
						if $comparison_side == "baseline" then
							"gen-1"
						elif ($from_identity != null and $to_identity != null) then
							"gen-2"
						else
							"gen-1"
						end
					) as $expected_generation
					|
					if .props.generation_id == $expected_generation then
						.props.generation_id = "<generation-provenance>"
					else
						error("mapped projector/canonical containment edge has unexpected generation_id")
					end
				else
					.
				end
			)
			| sort
		end
	' "${graph_dump}" >"${output}" || return 2
}

# ifa_fault_compare_collateral_edges returns 0 for exact collateral parity, 1
# for a real difference, and 2 for jq/hash/diff failure. It deliberately does
# not absorb SQL or code-call exactness: their dedicated assertions run first.
ifa_fault_compare_collateral_edges() {
	local baseline_dump="$1" changed_dump="$2" output_dir="$3"
	local sql_repo_id="${4:-repo-ifa-sql-family}"
	local mutable_relative_path="${5:-db/schema.sql}"
	local mutable_absolute_path="${6:-/repo/db/schema.sql}"
	local baseline_identities="${output_dir}/baseline-sql-repo-node-identities.json"
	local changed_identities="${output_dir}/changed-sql-repo-node-identities.json"
	local baseline_membership="${output_dir}/baseline-sql-repo-node-membership.json"
	local changed_membership="${output_dir}/changed-sql-repo-node-membership.json"
	local baseline_edges="${output_dir}/baseline-collateral-edges.json"
	local changed_edges="${output_dir}/changed-collateral-edges.json"
	local diff_output="${output_dir}/collateral-edges.diff"
	ifa_fault_write_repo_node_identities \
		"${baseline_dump}" "${sql_repo_id}" \
		"${mutable_relative_path}" "${mutable_absolute_path}" \
		"${baseline_identities}" || return 2
	ifa_fault_write_repo_node_identities \
		"${changed_dump}" "${sql_repo_id}" \
		"${mutable_relative_path}" "${mutable_absolute_path}" \
		"${changed_identities}" || return 2
	ifa_fault_write_repo_node_membership \
		"${baseline_dump}" "${sql_repo_id}" "${baseline_membership}" || return 2
	ifa_fault_write_repo_node_membership \
		"${changed_dump}" "${sql_repo_id}" "${changed_membership}" || return 2
	ifa_fault_write_collateral_edges \
		"${baseline_dump}" "${baseline_identities}" "${baseline_membership}" "${baseline_edges}" \
		"baseline" || return 2
	ifa_fault_write_collateral_edges \
		"${changed_dump}" "${changed_identities}" "${changed_membership}" "${changed_edges}" \
		"changed" || return 2
	local diff_rc=0
	diff -u "${baseline_edges}" "${changed_edges}" >"${diff_output}" || diff_rc=$?
	[[ "${diff_rc}" -le 1 ]] || return 2
	return "${diff_rc}"
}

# cell_duplicatedelivery (#5544 cell 6) proves the materialization write path
# is idempotent under redelivery: the same work item delivered twice must
# converge to the same graph, not double-write edges or dead-letter.
#
# It drains once cleanly, then forces every succeeded reducer row back to
# 'pending' in SQL -- the cell_expirelease precedent, which likewise perturbs
# fact_work_items directly rather than killing a process -- and drains again.
# A queue that redelivers an already-succeeded item is the real-world case
# (at-least-once delivery, a lease that expired after the handler committed
# but before the ack landed); this reproduces it deterministically instead of
# waiting to lose that race in CI.
#
# NON-VACUITY: the redelivery UPDATE must actually match rows. If it matched
# none, the second drain would be a no-op, the digest would trivially equal
# the baseline, and the cell would pass while proving nothing -- the inert-gate
# defect #5555 and #5974 exist to remove. The reset count is asserted > 0
# before the second drain, so an UPDATE that stops matching (a status rename, a
# schema change, a stage rename) fails this cell loudly instead of greening it.
cell_duplicatedelivery() {
	local cell_start
	cell_start=$(date +%s)
	log "cell duplicate-delivery: fresh stack"
	fresh_stack duplicatedelivery
	drive_all_cassettes duplicatedelivery
	local projector_pid reducer_pid reset_count
	ifa_det_start_bg "${log_dir}" "projector-duplicatedelivery" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-duplicatedelivery" reducer_pid "${bin_dir}/eshu-reducer"

	# First drain: establish the fully-materialized, fault-free state that the
	# redelivery below has to be idempotent against.
	run_drain_gate duplicatedelivery
	assert_no_dead_letters duplicatedelivery

	log "duplicate-delivery: force every succeeded reducer row back to pending (redelivery, SQL, no kill)"
	reset_count="$(ifa_fault_redeliver_succeeded "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
		"${ESHU_POSTGRES_DSN}" "${compose_file}")" \
		|| die "duplicate-delivery: redelivery UPDATE failed"
	[[ -n "${reset_count}" && "${reset_count}" -gt 0 ]] \
		|| die "duplicate-delivery: no succeeded reducer rows were redelivered -- non-vacuous precondition failed, the second drain would be a no-op and this cell would pass while proving nothing"
	printf 'duplicate-delivery: non-vacuous: %s succeeded reducer row(s) redelivered as pending\n' "${reset_count}"

	# Second drain: the redelivered work is reprocessed end to end.
	run_drain_gate duplicatedelivery
	assert_no_dead_letters duplicatedelivery
	capture_digest duplicatedelivery

	# The absolute-set assertion matters here for the same reason it does in
	# cell_baseline: digest equality alone is satisfied by empty == empty. A
	# redelivery that dropped the SQL family entirely would still match a
	# baseline that never had it.
	log "duplicate-delivery: assert SQL relationship family materialized edges (absolute set, non-vacuity)"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}" \
		|| die "duplicate-delivery: SQL relationship family materialized edge set did not match the expected set after redelivery -- redelivery either dropped or duplicated edges; do NOT normalize this away"

	assert_matches_baseline duplicatedelivery
	teardown_cell duplicatedelivery
	wall_times[duplicatedelivery]=$(( $(date +%s) - cell_start ))
	printf 'duplicate-delivery: cell wall time: %ss\n' "${wall_times[duplicatedelivery]}"
}

# cell_deltaretract (#5544 cell 7) proves a second generation's retract lands
# correctly under this gate: generation 2 of the SQL family reuses the same
# source_run_id, retracts what generation 1 asserted, and the accumulated edge
# set must match the committed expected-v2 fixture exactly -- proving the
# retract fired AND that nothing still-valid was lost with it.
#
# The drive/drain/assert body is ifa_det_run_sql_delta_live, the same helper
# scripts/verify-ifa-determinism.sh calls, so the two gates cannot drift on
# what "the delta landed correctly" means.
#
# WHY THIS CELL DOES NOT COMPARE TO THE BASELINE DIGEST: every other cell in
# this matrix injects a fault and asserts the graph is UNCHANGED. This one
# deliberately changes the graph -- generation 2 adds and retracts edges, so
# the post-delta digest is expected to differ from the fault-free generation-1
# baseline. Calling assert_matches_baseline here would fail correctly and
# tempt exactly the wrong fix. Its exactness assertion is the expected-v2 set,
# which is strictly stronger than a digest comparison: it names the edges.
cell_deltaretract() {
	local cell_start
	cell_start=$(date +%s)
	log "cell delta-retract: fresh stack"
	fresh_stack deltaretract
	drive_all_cassettes deltaretract
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-deltaretract" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-deltaretract" reducer_pid "${bin_dir}/eshu-reducer"

	# Generation 1 must be fully materialized before the delta is driven,
	# otherwise "the delta retracted it" is indistinguishable from "generation
	# 1 never landed".
	run_drain_gate deltaretract
	assert_no_dead_letters deltaretract
	log "delta-retract: assert generation-1 SQL edges before driving the delta (precondition)"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}" \
		|| die "delta-retract: generation-1 SQL edge set did not match before the delta was driven -- the retract assertion below would be meaningless"

	ifa_det_run_sql_delta_live \
		1 "${bin_dir}" "${sql_delta_cassette}" "${sql_delta_expected_edges}" "${log_dir}" \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"${compose_file}" "${GATE_DRAIN_TIMEOUT}" \
		|| die "delta-retract: SQL delta-live proof failed -- the generation-2 retract did not converge to the expected accumulated edge set"

	assert_no_dead_letters deltaretract
	capture_digest deltaretract

	# Generation 2 intentionally updates SQL-owned canonical nodes. graph-dump
	# identifies relationship endpoints by the digest of every endpoint
	# property, so those legitimate updates replace the apparent hashes of the
	# SQL repository's CONTAINS and REPO_CONTAINS edges. A whole-graph
	# "everything except the nine SQL types" comparison therefore cannot use
	# raw endpoint hashes. Reassert code calls through their stable, hand-derived
	# edge identities, then compare the remaining graph through the scoped stable
	# endpoint mapping below. This fails on dropped, duplicated, spurious, or
	# wrongly attached edges without globally ignoring structural edge types.
	ifa_code_call_assert "deltaretract" "${bin_dir}" "${code_call_expected_edges}" \
		|| die "delta-retract: SQL generation 2 changed the code-call family's five-edge exact set"

	# The SQL-v2 and code-call assertions above own their relationship families.
	# Compare every remaining edge exactly, except that content-addressed endpoint
	# hashes on the Repository plus db/schema.sql and its Directory ancestors are
	# replaced by stable logical identities after proving ownership in each dump.
	# Preserved paths such as cmd/api/handlers.go stay raw and exact. The records
	# retain topology, type, count, generation_id presence, and every other
	# property, so attachment swaps, containment loss/addition, and out-of-scope
	# property churn still fail; no unrelated CONTAINS or REPO_CONTAINS edge is
	# ignored. Baseline canonical containment is gen-1; changed edges refresh to
	# gen-2 only when both endpoints map to the mutable SQL seam, while a
	# one-mapped preserved-subtree edge remains gen-1.
	command -v jq >/dev/null 2>&1 \
		|| die "delta-retract: jq is required for fail-closed collateral graph comparison"
	if ! command -v shasum >/dev/null 2>&1 && ! command -v sha256sum >/dev/null 2>&1; then
		die "delta-retract: shasum or sha256sum is required to attribute SQL-owned containment endpoints"
	fi
	local collateral_rc=0
	ifa_fault_compare_collateral_edges \
		"${work_dir}/graph-baseline.dump" \
		"${work_dir}/graph-deltaretract.dump" \
		"${work_dir}" \
		"repo-ifa-sql-family" \
		"db/schema.sql" \
		"/repo/db/schema.sql" || collateral_rc=$?
	if [[ "${collateral_rc}" -eq 1 ]]; then
		printf 'delta-retract: graph collateral changed outside the exact SQL and code-call families:\n' >&2
		cat "${work_dir}/collateral-edges.diff" >&2
		die "delta-retract: SQL generation 2 changed unrelated graph truth; do not widen the family allowlists"
	fi
	[[ "${collateral_rc}" -eq 0 ]] \
		|| die "delta-retract: collateral graph comparison failed (status ${collateral_rc}); refusing to report parity"
	printf 'delta-retract: collateral graph edge set unchanged outside exact SQL/code-call assertions\n'

	teardown_cell deltaretract
	wall_times[deltaretract]=$(( $(date +%s) - cell_start ))
	printf 'delta-retract: cell wall time: %ss\n' "${wall_times[deltaretract]}"
}
