// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"strings"
)

const (
	// CallGraphMetricsEdgeScanLimit bounds the single indexed CALLS pass:
	// one extra row past it is read so the caller can tell an exact scope
	// from an overflowed one.
	CallGraphMetricsEdgeScanLimit = 50000
	// callGraphMetricsEdgeScanLimit is the pre-move spelling of
	// CallGraphMetricsEdgeScanLimit. The queryplan-pinned
	// CallGraphMetricsEdgesCypher body below names the bare const, and its
	// source_sha256 digest covers the declaration text, so the alias keeps
	// the body byte-identical instead of re-freezing the digest.
	callGraphMetricsEdgeScanLimit = CallGraphMetricsEdgeScanLimit
)

// CallGraphMetricsEdgesCypher builds the single indexed edge pass behind the
// hub-function and recursive-function metrics, and behind the graph-summary
// packet's hot-entity ranking.
//
// Every caller runs this exact text. Both of its routes are bound to the
// caller's grant before the read -- call-graph metrics by its mandatory,
// selector-resolved repo_id, graph-summary by the not-found it answers for an
// out-of-grant repo_id -- so a grant predicate here would be redundant by
// construction and would give this hot read a second shape with no plan behind
// it. One text is what keeps the queryplan manifest's cypher_sha256 for
// QP-CALL-GRAPH-HUBS and QP-CALL-GRAPH-RECURSIVE, plan claim included,
// describing what production emits.
func CallGraphMetricsEdgesCypher(repoID string) (string, map[string]any) {
	return `MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})
RETURN source.uid AS source_uid,
       coalesce(source.id, source.uid) AS source_id,
       source.relative_path AS source_path,
       source.language AS source_language,
       source.name AS source_name,
       source.start_line AS source_start_line,
       source.end_line AS source_end_line,
       target.uid AS target_uid,
       coalesce(target.id, target.uid) AS target_id,
       target.relative_path AS target_path,
       target.language AS target_language,
       target.name AS target_name,
       target.start_line AS target_start_line,
       target.end_line AS target_end_line
LIMIT $edge_scan_limit`, map[string]any{
			"edge_scan_limit": callGraphMetricsEdgeScanLimit + 1,
			"repo_id":         strings.TrimSpace(repoID),
		}
}
