// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// relationshipVerbEntry describes one typed-edge verb in the relationships
// catalog: its layer, the source-node label for bounded edge slices, and a short
// evidence/source label for the UI. Counts are whole-graph relationship-type
// aggregates and deliberately do not use sourceLabel.
//
// Edge slices and source_tool breakdowns start from a concrete source-node
// label. Counts use `MATCH ()-[r:VERB]->()` so NornicDB can use its
// relationship-type index and include edges written from every source label.
type relationshipVerbEntry struct {
	// verb is the relationship type as written in the canonical graph.
	verb string
	// layer is the fixed code-to-cloud layer the verb belongs to.
	layer string
	// sourceLabel is the label that anchors the bounded edge-slice query.
	sourceLabel string
	// sourceProperty is the indexed anchor property on sourceLabel. It is the
	// property whose schema index makes the label scan bounded; it is recorded
	// so the query-plan gate can assert the supporting index exists.
	sourceProperty string
	// evidence is the human-facing source/evidence label shown on the verb tile.
	evidence string
	// detail is a one-line description of what the edge means.
	detail string
	// carriesSourceTool is true for the Tier-2 shared verbs whose cross-repo edges
	// are stamped with a source_tool token (#3999). Only those verbs participate
	// in the label-grouped source_tool aggregate; Tier-1 self-labeling and Tier-3
	// code/structural verbs never carry source_tool, so including them would add
	// guaranteed-empty work (the count path is already budget-tuned).
	carriesSourceTool bool
	// sourceToolSourceLabel is the source label that owns edges stamped with
	// source_tool. It can differ from sourceLabel when one shared verb has more
	// than one source kind: DEPENDS_ON source-tool evidence belongs to
	// Repository-to-Repository edges, while its drill-down slice browses
	// Workload-to-Workload edges.
	sourceToolSourceLabel string
}

// relationshipVerbCatalog is the fixed set of typed-edge verbs the relationships
// surface browses, spanning all six layers (code, deploy, infra, runtime,
// security, ops). Every verb is grounded in a relationship type the canonical
// graph actually writes. Every edge-slice source label carries a schema index.
// The set is intentionally fixed (not derived at query time) so each entry can
// be covered by the static query-plan regression gate.
var relationshipVerbCatalog = []relationshipVerbEntry{
	// code layer
	{verb: "CALLS", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Call graph", detail: "Function invokes another function"},
	{verb: "IMPORTS", layer: "code", sourceLabel: "File", sourceProperty: "path", evidence: "Module imports", detail: "File imports a module or symbol"},
	{verb: "INHERITS", layer: "code", sourceLabel: "Class", sourceProperty: "uid", evidence: "Type hierarchy", detail: "Class inherits from a base type"},
	{verb: "REFERENCES", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Symbol references", detail: "Symbol references another symbol"},
	{verb: "OVERRIDES", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Type hierarchy", detail: "Method overrides a base method"},
	{verb: "QUERIES_TABLE", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Data access", detail: "Function queries a database table"},
	// deploy layer
	{verb: "DEPLOYS_FROM", layer: "deploy", sourceLabel: "Repository", sourceProperty: "id", evidence: "Deployment evidence", detail: "Repository deploys from a source", carriesSourceTool: true, sourceToolSourceLabel: "Repository"},
	{verb: "INSTANCE_OF", layer: "deploy", sourceLabel: "WorkloadInstance", sourceProperty: "id", evidence: "Workload model", detail: "Instance realizes a workload definition"},
	// infra layer
	{verb: "PROVISIONS_DEPENDENCY_FOR", layer: "infra", sourceLabel: "Repository", sourceProperty: "id", evidence: "Terraform", detail: "Repository provisions infrastructure for a target", carriesSourceTool: true, sourceToolSourceLabel: "Repository"},
	{verb: "USES_MODULE", layer: "infra", sourceLabel: "Repository", sourceProperty: "id", evidence: "Terraform modules", detail: "Repository consumes a module repository", carriesSourceTool: true, sourceToolSourceLabel: "Repository"},
	{verb: "DISCOVERS_CONFIG_IN", layer: "infra", sourceLabel: "Repository", sourceProperty: "id", evidence: "Config discovery", detail: "Repository discovers configuration in a target", carriesSourceTool: true, sourceToolSourceLabel: "Repository"},
	// runtime layer
	{verb: "RUNS_ON", layer: "runtime", sourceLabel: "WorkloadInstance", sourceProperty: "id", evidence: "Runtime placement", detail: "Workload instance runs on a platform", carriesSourceTool: true, sourceToolSourceLabel: "WorkloadInstance"},
	{verb: "DEPENDS_ON", layer: "runtime", sourceLabel: "Workload", sourceProperty: "id", evidence: "Runtime dependency", detail: "Workload depends on another workload", carriesSourceTool: true, sourceToolSourceLabel: "Repository"},
	// security layer
	{verb: "INVOKES_CLOUD_ACTION", layer: "security", sourceLabel: "Function", sourceProperty: "uid", evidence: "IAM call analysis", detail: "Function invokes a cloud action"},
	// ops layer
	{verb: "READS_CONFIG_FROM", layer: "ops", sourceLabel: "Repository", sourceProperty: "id", evidence: "Config access", detail: "Repository reads configuration from a target", carriesSourceTool: true, sourceToolSourceLabel: "Repository"},
	{verb: "TAINT_FLOWS_TO", layer: "ops", sourceLabel: "Function", sourceProperty: "uid", evidence: "Taint analysis", detail: "Tainted data flows to a sink"},
}

// relationshipVerbByName indexes the catalog by verb for O(1) lookup when
// serving the per-verb edge endpoint.
var relationshipVerbByName = func() map[string]relationshipVerbEntry {
	index := make(map[string]relationshipVerbEntry, len(relationshipVerbCatalog))
	for _, entry := range relationshipVerbCatalog {
		index[entry.verb] = entry
	}
	return index
}()

// relationshipCountCypher builds the bounded count query for a verb. It is the
// bare relationship-type aggregate `MATCH ()-[r:VERB]->() RETURN count(r)`,
// which the NornicDB relationship-type index answers directly in milliseconds.
//
// # Whole-graph scope
//
// The count is intentionally whole-graph: it includes every edge of that type
// regardless of source label. This is the documented "bounded whole-graph edge
// count" the catalog contract promises. Callers and the UI must treat the tile
// count as the whole-graph population, not the source-label-scoped population
// that the companion edge-slice endpoint returns. When a relationship type is
// written by more than one source label (e.g. DEPENDS_ON is written for both
// Repository→Repository and Workload→Workload edges), the count reflects all
// source labels combined while the edge slice is anchored on the catalog entry's
// sourceLabel; the two numbers can legitimately differ.
//
// # Why not source-label anchoring
//
// A source-label anchor (`MATCH (s:Label)-[r:VERB]->()`) forces a scan of the
// entire source-label population per verb. At ~900k-edge scale the 16 sequential
// label scans exceeded 30s (profiled live: File label alone cost 8.9s for 0
// IMPORTS edges). NornicDB has no composite relationship-type+label index, so
// there is no index-served path for a scoped count. The endpoints are anonymous
// `()`, not bound unlabeled nodes `(s)`, so the shape stays within the bounded
// read contract and the query-plan gate (issue #3429).
//
// The verb is taken from the fixed catalog, never from request input, so the
// interpolation cannot inject arbitrary patterns.
func relationshipCountCypher(entry relationshipVerbEntry) string {
	return "MATCH ()-[r:" + entry.verb + "]->()\n" +
		"RETURN count(r) AS count"
}

// relationshipEdgesCypher builds the bounded, source-label-anchored edge slice
// query for a verb. It anchors on the source label, projects narrow endpoint
// identity plus optional evidence fields, orders by the indexed source-anchor
// property, and bounds the result with $limit. Callers over-fetch limit+1 to
// set a truncated flag. As with the count query, verb, label, and property come
// from the fixed catalog only.
//
// The ORDER BY targets the indexed source property (`s.<sourceProperty>`, the
// same property whose schema index anchors the scan) rather than the projected
// coalesce() expressions. An index-ordered scan lets the LIMIT short-circuit
// after the first page, instead of materializing and sorting the verb's full
// edge set on a non-indexed expression, which at ~900k-edge scale pushed the
// per-verb slice past the few-second budget. A labeled source node is kept on
// purpose: on NornicDB a bare-type edge match with bound, unlabeled endpoints is
// dramatically slower than the source-label-anchored expand.
//
// The secondary ORDER BY clause `coalesce(t.id, t.uid)` is a deterministic
// tie-breaker for rows that share the same source key (e.g. one function with
// many outgoing CALLS edges). Without it, rows tied on the primary key are
// returned in an unspecified order, so a page boundary falling inside one
// source node's outgoing edges can produce nondeterministic or repeated rows
// across requests. The tie-breaker is a coalesce expression over the target's
// two most common identity properties and does not require an index because it
// only resolves within the already-bounded first-page set.
func relationshipEdgesCypher(entry relationshipVerbEntry) string {
	return "MATCH (s:" + entry.sourceLabel + ")-[r:" + entry.verb + "]->(t)\n" +
		"RETURN coalesce(s.id, s.uid, s.name, s.path) AS source_id,\n" +
		"       coalesce(s.name, s.path, s.id, s.uid) AS source_name,\n" +
		"       coalesce(t.id, t.uid, t.name, t.path) AS target_id,\n" +
		"       coalesce(t.name, t.path, t.id, t.uid) AS target_name,\n" +
		"       r.rationale AS evidence,\n" +
		"       r.source_tool AS source_tool\n" +
		"ORDER BY s." + entry.sourceProperty + ", coalesce(t.id, t.uid)\n" +
		"LIMIT $limit"
}

// relationshipEdgesCypherFiltered is the source_tool-filtered variant of
// relationshipEdgesCypher. It inserts a WHERE clause after the MATCH line that
// binds $source_tool to r.source_tool, so the index-ordered scan and LIMIT
// short-circuit are preserved. The $source_tool param must always be provided
// by the caller; the unfiltered path must NOT call this function.
//
// The verb, label, and property are taken from the fixed catalog, never from
// request input, so the interpolation cannot inject arbitrary patterns.
func relationshipEdgesCypherFiltered(entry relationshipVerbEntry) string {
	return "MATCH (s:" + entry.sourceLabel + ")-[r:" + entry.verb + "]->(t)\n" +
		"WHERE r.source_tool = $source_tool\n" +
		"RETURN coalesce(s.id, s.uid, s.name, s.path) AS source_id,\n" +
		"       coalesce(s.name, s.path, s.id, s.uid) AS source_name,\n" +
		"       coalesce(t.id, t.uid, t.name, t.path) AS target_id,\n" +
		"       coalesce(t.name, t.path, t.id, t.uid) AS target_name,\n" +
		"       r.rationale AS evidence,\n" +
		"       r.source_tool AS source_tool\n" +
		"ORDER BY s." + entry.sourceProperty + ", coalesce(t.id, t.uid)\n" +
		"LIMIT $limit"
}

// relationshipSourceToolBreakdownCyphers builds one source_tool aggregate per
// owning source label. Keeping the two label scans independent lets the shared
// handler limiter overlap them without multiplying scans per stamped verb.
// Labels and relationship types come only from the fixed catalog.
func relationshipSourceToolBreakdownCyphers() []string {
	ownerOrder := make([]string, 0, 2)
	verbsByOwner := make(map[string][]string, 2)
	for _, entry := range relationshipVerbCatalog {
		if !entry.carriesSourceTool {
			continue
		}
		owner := entry.sourceToolSourceLabel
		if _, ok := verbsByOwner[owner]; !ok {
			ownerOrder = append(ownerOrder, owner)
		}
		verbsByOwner[owner] = append(verbsByOwner[owner], entry.verb)
	}

	queries := make([]string, 0, len(ownerOrder))
	for _, owner := range ownerOrder {
		queries = append(queries,
			"MATCH (s:"+owner+")-[r:"+strings.Join(verbsByOwner[owner], "|")+"]->()\n"+
				"WHERE r.source_tool IS NOT NULL\n"+
				"RETURN type(r) AS verb, r.source_tool AS source_tool, count(r) AS count\n"+
				"ORDER BY verb, source_tool",
		)
	}
	return queries
}
