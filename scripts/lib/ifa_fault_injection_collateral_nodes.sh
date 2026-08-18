#!/usr/bin/env bash
# shellcheck disable=SC2154
# Full-node collateral comparison for the delta-retract fault cell. This file
# is sourced by verify-ifa-fault-injection.sh; strict mode and the portable
# ifa_fault_sha256_stdin helper are owned by the parent libraries.

# ifa_fault_merge_disjoint_maps combines repository-attributed graphdump hash
# maps while failing closed if a node is ambiguously owned by both fixtures.
ifa_fault_merge_disjoint_maps() {
	local left="$1" right="$2" output="$3"
	jq -e -s '
		if length != 2 or any(.[]; type != "object") then
			error("collateral maps must be exactly two objects")
		else
			(.[0] | keys) as $left_keys
			| (.[1] | keys) as $right_keys
			| ($left_keys - ($left_keys - $right_keys)) as $overlap
			| if ($overlap | length) > 0 then
				error("collateral repository maps overlap")
			else .[0] * .[1]
			end
		end
	' "${left}" "${right}" >"${output}" || return 2
}

# ifa_fault_write_collateral_nodes writes a sorted full-node multiset. Nodes in
# the combined mutable SQL+rationale identity map keep their typed stable key
# plus every label and property after validating the side-specific generation.
# The one additional fixture-owned transition is the SqlIndex retarget already
# proved by the exact SQL-v2 edge assertion; code-call nodes remain byte-exact,
# as do all other unmapped nodes.
ifa_fault_write_collateral_nodes() {
	local graph_dump="$1" repo_identities="$2" output="$3" comparison_side="$4"
	local rationale_repo_id="${5:-}" rationale_baseline_generation_id="${6:-}"
	local rationale_delta_generation_id="${7:-}"
	local node_records="${output}.nodes.jsonl" normalized_rows="${output}.rows.jsonl"
	if [[ -n "${rationale_repo_id}" ]] \
		&& [[ -z "${rationale_baseline_generation_id}" || -z "${rationale_delta_generation_id}" ]]; then
		return 2
	fi
	jq -e '
		if type != "object" then
			error("node identity map must be an object")
		elif all(.[]; type == "object") then
			true
		else
			error("node identity map values must be objects")
		end
	' \
		"${repo_identities}" >/dev/null || return 2
	jq -c --arg comparison_side "${comparison_side}" '
		if ($comparison_side != "baseline" and $comparison_side != "changed") then
			error("collateral comparison side must be baseline or changed")
		elif ((.nodes | type) != "array") or ((.edges | type) != "array") then
			error("graph dump must contain nodes and edges arrays")
		else
			.nodes[]
		end
	' "${graph_dump}" >"${node_records}" || return 2
	: >"${normalized_rows}"
	local node_record node_hash node_identity normalized
	while IFS= read -r node_record; do
		node_hash="$(printf '%s\n' "${node_record}" | jq -S . | ifa_fault_sha256_stdin)" \
			|| return 2
		node_identity="$(jq -c --arg hash "${node_hash}" '
			if type != "object" then
				error("node identity map must be an object")
			elif has($hash) then
				.[$hash]
			else
				null
			end
		' "${repo_identities}")" || return 2
		if [[ "${node_identity}" == "null" ]]; then
			jq -nc --argjson node "${node_record}" \
				'{mapped: false, node: $node}' >>"${normalized_rows}" || return 2
			continue
		fi
		normalized="$(printf '%s\n' "${node_record}" | jq -c \
			--arg comparison_side "${comparison_side}" \
			--arg rationale_repo_id "${rationale_repo_id}" \
			--arg rationale_baseline_generation_id "${rationale_baseline_generation_id}" \
			--arg rationale_delta_generation_id "${rationale_delta_generation_id}" \
			--argjson identity "${node_identity}" '
			if (($identity | type) != "object")
				or (($identity.labels? | type) != "array")
				or ((($identity.owner_repo_id? // "") | length) == 0)
				or (
					((($identity.uid? // "") | length) == 0)
					and ((($identity.path? // "") | length) == 0)
					and ((($identity.repo_id? // "") | length) == 0)
				)
				or ((.labels | type) != "array")
				or ((.props | type) != "object") then
				error("mapped node identity, labels, and props must be typed")
			else
				(if $rationale_repo_id != "" and $identity.owner_repo_id == $rationale_repo_id then
					if $comparison_side == "baseline" then
						$rationale_baseline_generation_id
					else
						$rationale_delta_generation_id
					end
				else
					if $comparison_side == "baseline" then "gen-1" else "gen-2" end
				end) as $expected_generation
				| if .props.generation_id? != $expected_generation then
					error("mapped node has unexpected generation_id")
				else
					.props.generation_id = "<generation-provenance>"
				end
				| if (
					((.labels | index("SqlIndex")) != null)
					and ($identity.uid? == "content-entity:sql-idx-users-email")
				) then
					(if $comparison_side == "baseline" then "public.users" else "public.orders" end) as $expected_table
					| if .props.table_name? != $expected_table then
						error("mapped SqlIndex has unexpected table_name")
					else
						.props.table_name = "<sql-index-retarget>"
					end
				else
					.
				end
				| {mapped: true, identity: $identity, node: .}
			end
		')" || return 2
		printf '%s\n' "${normalized}" >>"${normalized_rows}"
	done <"${node_records}"
	jq -s '
		sort as $rows
		| [$rows[] | select(.mapped) | (.identity | tojson)] as $mapped_keys
		| if ($mapped_keys | length) != ($mapped_keys | unique | length) then
			error("ambiguous mapped node identity")
		else
			$rows
		end
	' "${normalized_rows}" >"${output}" || return 2
}

# ifa_fault_compare_collateral_nodes returns 0 for normalized full-node parity,
# 1 for graph-truth difference, and 2 for jq/hash/diff failure.
ifa_fault_compare_collateral_nodes() {
	local baseline_dump="$1" changed_dump="$2" baseline_identities="$3"
	local changed_identities="$4" output_dir="$5"
	local rationale_repo_id="${6:-}" rationale_baseline_generation_id="${7:-}"
	local rationale_delta_generation_id="${8:-}"
	local baseline_nodes="${output_dir}/baseline-collateral-nodes.json"
	local changed_nodes="${output_dir}/changed-collateral-nodes.json"
	local diff_output="${output_dir}/collateral-nodes.diff"
	ifa_fault_write_collateral_nodes \
		"${baseline_dump}" "${baseline_identities}" "${baseline_nodes}" baseline \
		"${rationale_repo_id}" "${rationale_baseline_generation_id}" "${rationale_delta_generation_id}" || return 2
	ifa_fault_write_collateral_nodes \
		"${changed_dump}" "${changed_identities}" "${changed_nodes}" changed \
		"${rationale_repo_id}" "${rationale_baseline_generation_id}" "${rationale_delta_generation_id}" || return 2
	local diff_rc=0
	diff -u "${baseline_nodes}" "${changed_nodes}" >"${diff_output}" || diff_rc=$?
	[[ "${diff_rc}" -le 1 ]] || return 2
	return "${diff_rc}"
}
