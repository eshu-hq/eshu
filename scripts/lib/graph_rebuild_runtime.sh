#!/usr/bin/env bash
# Runtime helpers for scripts/verify-graph-rebuild-from-facts.sh.

# wait_for_interrupt_point waits until a rebuild has produced graph output
# while work remains active, then records the node-presence sentinel in
# INTERRUPT_NODES and the active-work count in REMAINING. graph_scalar and
# queue_active_count are supplied by the caller.
# The bounded wait fails rather than claiming restart convergence from a kill
# that happened before projection began or after it already finished.
wait_for_interrupt_point() {
	local timeout_seconds="$1" attempt sentinel work
	if [[ ! "${timeout_seconds}" =~ ^[1-9][0-9]*$ ]]; then
		echo "Interrupt wait timeout must be a positive integer (got ${timeout_seconds})." >&2
		return 1
	fi

	for ((attempt = 1; attempt <= timeout_seconds; attempt++)); do
		# Do not use a whole-graph count or a computed numeric projection here.
		# NornicDB v1.3.2 has returned an empty result for both shapes while label
		# identity queries returned thousands of nodes. A bounded label probe is
		# sufficient: interruption needs to prove that projection started, not
		# count the partial graph exactly.
		sentinel="$(graph_scalar 'MATCH (n) RETURN labels(n)[0] AS present LIMIT 1')"
		work="$(queue_active_count)"
		if [[ -n "${sentinel}" && "${sentinel}" != "0" && "${work}" =~ ^[0-9]+$ ]] \
			&& ((work > 0)); then
			INTERRUPT_NODES="${sentinel}"
			REMAINING="${work}"
			return 0
		fi
		sleep 1
	done

	echo "Timed out waiting for an in-progress rebuild checkpoint " \
		"(last sample: node_sentinel=${sentinel:-unknown}, active_work=${work:-unknown})." >&2
	return 1
}

# enqueue_rebuild_then_start_workers preserves the recovery transaction's
# in-flight reducer fence. request_rebuild must run while projection workers are
# stopped; only an accepted durable enqueue may release them.
enqueue_rebuild_then_start_workers() {
	local idempotency_key="$1" enqueued
	enqueued="$(request_rebuild "${idempotency_key}")" || return $?
	start_services || return $?
	printf '%s\n' "${enqueued}"
}

# assert_identity_snapshot_sane refuses snapshots whose identities are empty,
# null, or only concatenation separators. Such keys can compare equal while
# proving nothing. The patterns are anchored so legitimate names such as
# null_resource.network_placeholder remain valid.
assert_identity_snapshot_sane() {
	local out_dir="$1"
	local nodes_total edges_total nodes_bad edges_bad

	nodes_total="$(wc -l <"$out_dir/nodes.txt" | tr -d ' ')"
	edges_total="$(wc -l <"$out_dir/edges.txt" | tr -d ' ')"
	if [[ "$nodes_total" == "0" ]]; then
		echo "Refusing to compare: node identity snapshot is empty." >&2
		return 1
	fi
	if [[ "$edges_total" == "0" ]]; then
		echo "Refusing to compare: edge identity snapshot is empty." >&2
		return 1
	fi

	nodes_bad="$(rg -c '^\s*$|^null$|\|null$|^[A-Za-z_][A-Za-z0-9_]*\|+$' "$out_dir/nodes.txt" 2>/dev/null || true)"
	edges_bad="$(rg -c '^\s*$|^null$|^\|{6}|\|{6}$' "$out_dir/edges.txt" 2>/dev/null || true)"
	if [[ -n "$nodes_bad" && "$nodes_bad" != "0" ]] || [[ -n "$edges_bad" && "$edges_bad" != "0" ]]; then
		echo "Refusing to compare: ${nodes_bad:-0} node and ${edges_bad:-0} edge identity lines are" \
			"blank, null, or nothing but separators." >&2
		return 1
	fi
	echo "Identity snapshot checked: $nodes_total node and $edges_total edge keys, all distinguishing."
}

# graph_pairs runs a two-column key/count query and emits key=count lines.
graph_pairs() {
	local statement="$1" prefix="$2"
	curl -fsS -H 'Content-Type: application/json' \
		-d "$(jq -nc --arg s "$statement" '{statements:[{statement:$s}]}')" \
		"${GRAPH_BASE}/db/nornic/tx/commit" \
		| jq -r --arg p "$prefix" '.results[0].data[] | "\($p)\(.row[0])=\(.row[1])"'
}

# snapshot_counts records readable totals. Identity sets remain authoritative.
snapshot_counts() {
	local out_file="$1"
	{
		echo "total_nodes=$(graph_scalar 'MATCH (n) RETURN count(n) AS c')"
		echo "total_rels=$(graph_scalar 'MATCH ()-[r]->() RETURN count(r) AS c')"
		graph_pairs 'MATCH (n) UNWIND labels(n) AS l RETURN l AS k, count(*) AS c ORDER BY l' 'label:'
		graph_pairs 'MATCH ()-[r]->() RETURN type(r) AS k, count(*) AS c ORDER BY k' 'rel:'
	} >"$out_file"
}

# node_identity_expr builds the union identity shared by node and edge scans.
node_identity_expr() {
	local v="$1"
	printf "coalesce(%s.uid, %s.id, %s.ref, %s.locator, '') + '|' + coalesce(%s.name,'') + '|' + coalesce(%s.path,'') + '|' + coalesce(%s.repo_id,'') + '|' + coalesce(%s.lang,'')" \
		"$v" "$v" "$v" "$v" "$v" "$v" "$v" "$v"
}

# graph_lines runs a single-column query and emits one raw line per row.
graph_lines() {
	curl -fsS -H 'Content-Type: application/json' \
		-d "$(jq -nc --arg s "$1" '{statements:[{statement:$s}]}')" \
		"${GRAPH_BASE}/db/nornic/tx/commit" \
		| jq -r '.results[0].data[] | .row[0] // ""'
}

# snapshot_sets writes every node and edge identity, preserving duplicates so
# multiplicity remains part of the comparison. Nodes are queried one label at a
# time because this backend returns null when an UNWIND-produced label is
# concatenated, and bare labels are validated before interpolation.
snapshot_sets() {
	local out_dir="$1"
	mkdir -p "$out_dir"

	local node_expr edge_expr label
	node_expr="$(node_identity_expr n)"
	: >"$out_dir/nodes.txt"
	while IFS= read -r label; do
		[[ -n "$label" ]] || continue
		if [[ ! "$label" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
			echo "Refusing to compare: node label '$label' needs quoting, which this backend does not support." >&2
			return 1
		fi
		graph_lines "MATCH (n:${label}) RETURN ${node_expr} AS k ORDER BY k" \
			| sed "s|^|${label}\||" >>"$out_dir/nodes.txt"
	done < <(graph_lines 'MATCH (n) UNWIND labels(n) AS l RETURN l AS k, count(*) AS c ORDER BY l')
	sort -o "$out_dir/nodes.txt" "$out_dir/nodes.txt"

	edge_expr="$(node_identity_expr a) + '||' + type(r) + '||' + $(node_identity_expr b)"
	graph_lines "MATCH (a)-[r]->(b) RETURN ${edge_expr} AS k ORDER BY k" \
		| sort >"$out_dir/edges.txt"

	assert_identity_snapshot_sane "$out_dir"
}

# compare_sets reports the bidirectional identity difference. Callers invoke it
# in an OR-list, so every command that could make a false empty diff is checked.
compare_sets() {
	local before="$1" after="$2" label="$3" failed=0 kind
	for kind in nodes edges; do
		if ! comm -23 "$before/$kind.txt" "$after/$kind.txt" >"$TMP_DIR/$kind.missing" ||
			! comm -13 "$before/$kind.txt" "$after/$kind.txt" >"$TMP_DIR/$kind.extra"; then
			echo "$label: could not diff $kind identities; treating as a failure rather than a match" >&2
			return 1
		fi
		local missing extra
		missing="$(wc -l <"$TMP_DIR/$kind.missing" | tr -d ' ')"
		extra="$(wc -l <"$TMP_DIR/$kind.extra" | tr -d ' ')"
		if [[ "$missing" == "0" && "$extra" == "0" ]]; then
			echo "$label: $kind set difference 0/0."
			continue
		fi
		failed=1
		echo "$label: $kind differ from the pre-wipe snapshot: $missing missing, $extra extra" >&2
		echo "  missing (in pre-wipe, absent after rebuild), first 25:" >&2
		sed -n '1,25p' "$TMP_DIR/$kind.missing" | sed 's/^/    /' >&2
		echo "  extra (absent before, present after rebuild), first 25:" >&2
		sed -n '1,25p' "$TMP_DIR/$kind.extra" | sed 's/^/    /' >&2
	done
	return "$failed"
}
