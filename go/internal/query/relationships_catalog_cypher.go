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
	// targetIdentityProperty overrides the default target_id/tie-breaker
	// coalesce() order when the target label's canonical identity is not
	// id/uid/name (e.g. MANAGES targets Directory, which has only path). When
	// set, the builder moves this property to the front of the target_id
	// projection and appends it to the ORDER BY tie-breaker. When empty
	// (the default for every other verb), the emitted Cypher for both
	// relationshipEdgesCypher and relationshipEdgesCypherFiltered is
	// byte-identical to the pre-#5369 shape, preserving the query-plan gate's
	// pinned cypher_sha256 for every unaffected entry.
	targetIdentityProperty string
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
	// targetAttributable is true when the edge's target endpoint (t) carries
	// tenant attribution a #5167 scoped-token grant can bind: a Repository, a
	// Workload/WorkloadInstance reachable from a Repository via
	// DEFINES/INSTANCE_OF, or a code entity carrying repo_id. It is false for
	// verbs whose target is a shared/global entity with no tenant attribution
	// (Module for IMPORTS, Platform for RUNS_ON, SqlTable for QUERIES_TABLE,
	// CloudAction for INVOKES_CLOUD_ACTION) -- relationshipEdgesCypher binds
	// the scope predicate to target t only when this is true; the source
	// endpoint s is always bound for a scoped caller regardless, which alone
	// prevents a scoped caller from ever seeing an edge whose source belongs to
	// another tenant.
	targetAttributable bool
}

// relationshipVerbCatalog is the fixed set of typed-edge verbs the relationships
// surface browses, spanning all six layers (code, deploy, infra, runtime,
// security, ops). Every verb is grounded in a relationship type the canonical
// graph actually writes. Every edge-slice source label carries a schema index.
// The set is intentionally fixed (not derived at query time) so each entry can
// be covered by the static query-plan regression gate.
var relationshipVerbCatalog = []relationshipVerbEntry{
	// code layer
	{verb: "CALLS", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Call graph", detail: "Function invokes another function", targetAttributable: true},
	{verb: "IMPORTS", layer: "code", sourceLabel: "File", sourceProperty: "path", evidence: "Module imports", detail: "File imports a module or symbol"},
	{verb: "INHERITS", layer: "code", sourceLabel: "Class", sourceProperty: "uid", evidence: "Type hierarchy", detail: "Class inherits from a base type", targetAttributable: true},
	{verb: "REFERENCES", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Symbol references", detail: "Symbol references another symbol", targetAttributable: true},
	{verb: "OVERRIDES", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Type hierarchy", detail: "Method overrides a base method", targetAttributable: true},
	{verb: "QUERIES_TABLE", layer: "code", sourceLabel: "Function", sourceProperty: "uid", evidence: "Data access", detail: "Function queries a database table"},
	// deploy layer
	{verb: "DEPLOYS_FROM", layer: "deploy", sourceLabel: "Repository", sourceProperty: "id", evidence: "Deployment evidence", detail: "Repository deploys from a source", carriesSourceTool: true, sourceToolSourceLabel: "Repository", targetAttributable: true},
	{verb: "INSTANCE_OF", layer: "deploy", sourceLabel: "WorkloadInstance", sourceProperty: "id", evidence: "Workload model", detail: "Instance realizes a workload definition", targetAttributable: true},
	// RECONCILES_FROM (issue #5360 PR B) anchors on the source FluxKustomization;
	// its target (FluxGitRepository/FluxOCIRepository/FluxBucket) is a generic
	// canonical entity carrying repo_id like every CALLS/INHERITS/REFERENCES/
	// OVERRIDES target, so targetAttributable is true here: the #5167 scoped-grant
	// predicate can bind the target endpoint's repo_id, so binding it is the
	// more-restrictive (correct) choice. This differs from the Atlantis
	// governance verbs below precisely because their targets (Directory,
	// AtlantisWorkflow) carry no repo_id a #5167 grant could bind, which is
	// why those stay targetAttributable=false -- the distinction is repo_id
	// bindability, not the unrelated targetIdentityProperty override MANAGES
	// uses for its non-standard Directory identity key.
	//
	// Issue #5483 C1 extends the SAME RECONCILES_FROM edge type to
	// FluxHelmRelease sources (FluxHelmRepository/FluxGitRepository/
	// FluxOCIRepository/FluxBucket) and deliberately does NOT add a second
	// catalog entry: relationshipVerbByName is verb-keyed, so a second
	// "RECONCILES_FROM" entry would clobber this one (the DEPENDS_ON
	// precedent -- a shared verb with more than one source label keeps one
	// entry). This whole-graph count (`MATCH ()-[r:RECONCILES_FROM]->()`)
	// includes HelmRelease-sourced edges automatically; the sourceLabel stays
	// FluxKustomization, so the bounded list_relationship_edges SLICE below
	// stays anchored there and never returns a FluxHelmRelease-sourced edge --
	// those are honestly reachable only through get_entity_context (the
	// generic graph-projected-node edge surface), not this catalog's
	// per-verb browse. See relationships_catalog_flux_test.go for the
	// count/slice divergence proof.
	{verb: "RECONCILES_FROM", layer: "deploy", sourceLabel: "FluxKustomization", sourceProperty: "uid", evidence: "Flux GitOps", detail: "Flux Kustomization or HelmRelease reconciles from its source", targetAttributable: true},
	// infra layer
	{verb: "PROVISIONS_DEPENDENCY_FOR", layer: "infra", sourceLabel: "Repository", sourceProperty: "id", evidence: "Terraform", detail: "Repository provisions infrastructure for a target", carriesSourceTool: true, sourceToolSourceLabel: "Repository", targetAttributable: true},
	{verb: "USES_MODULE", layer: "infra", sourceLabel: "Repository", sourceProperty: "id", evidence: "Terraform modules", detail: "Repository consumes a module repository", carriesSourceTool: true, sourceToolSourceLabel: "Repository", targetAttributable: true},
	{verb: "DISCOVERS_CONFIG_IN", layer: "infra", sourceLabel: "Repository", sourceProperty: "id", evidence: "Config discovery", detail: "Repository discovers configuration in a target", carriesSourceTool: true, sourceToolSourceLabel: "Repository", targetAttributable: true},
	// Atlantis-governance verbs anchor on the source AtlantisProject; their
	// targets (Terraform directory, another AtlantisProject, a workflow) carry
	// no repo_id a #5167 grant can bind, so targetAttributable stays false and
	// the scope predicate binds the source endpoint only.
	{verb: "MANAGES", layer: "infra", sourceLabel: "AtlantisProject", sourceProperty: "uid", targetIdentityProperty: "path", evidence: "Atlantis governance", detail: "Atlantis project manages a Terraform directory"},
	{verb: "ATLANTIS_DEPENDS_ON", layer: "infra", sourceLabel: "AtlantisProject", sourceProperty: "uid", evidence: "Atlantis governance", detail: "Atlantis project applies after another project"},
	{verb: "USES_WORKFLOW", layer: "infra", sourceLabel: "AtlantisProject", sourceProperty: "uid", evidence: "Atlantis governance", detail: "Atlantis project uses a custom workflow"},
	// runtime layer
	{verb: "RUNS_ON", layer: "runtime", sourceLabel: "WorkloadInstance", sourceProperty: "id", evidence: "Runtime placement", detail: "Workload instance runs on a platform", carriesSourceTool: true, sourceToolSourceLabel: "WorkloadInstance"},
	// AWS_lambda_function_uses_image (#5450) anchors on the source CloudResource
	// (the Lambda function) by uid -- the same indexed anchor the edge writer's
	// MATCH (source:CloudResource {uid}) uses. Its target ContainerImage is a
	// cross-generation canonical node carrying no repo_id a #5167 grant could
	// bind, so targetAttributable stays false (bind the source endpoint only),
	// matching the Atlantis-governance precedent above.
	{verb: "AWS_lambda_function_uses_image", layer: "runtime", sourceLabel: "CloudResource", sourceProperty: "uid", evidence: "Lambda container image", detail: "AWS Lambda function uses a container image"},
	{verb: "DEPENDS_ON", layer: "runtime", sourceLabel: "Workload", sourceProperty: "id", evidence: "Runtime dependency", detail: "Workload depends on another workload", carriesSourceTool: true, sourceToolSourceLabel: "Repository", targetAttributable: true},
	// security layer
	{verb: "INVOKES_CLOUD_ACTION", layer: "security", sourceLabel: "Function", sourceProperty: "uid", evidence: "IAM call analysis", detail: "Function invokes a cloud action"},
	// ops layer
	{verb: "READS_CONFIG_FROM", layer: "ops", sourceLabel: "Repository", sourceProperty: "id", evidence: "Config access", detail: "Repository reads configuration from a target", carriesSourceTool: true, sourceToolSourceLabel: "Repository", targetAttributable: true},
	{verb: "TAINT_FLOWS_TO", layer: "ops", sourceLabel: "Function", sourceProperty: "uid", evidence: "Taint analysis", detail: "Tainted data flows to a sink", targetAttributable: true},
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
// The secondary ORDER BY clause is a deterministic tie-breaker for rows that
// share the same source key (e.g. one function with many outgoing CALLS
// edges). Without it, rows tied on the primary key are returned in an
// unspecified order, so a page boundary falling inside one source node's
// outgoing edges can produce nondeterministic or repeated rows across
// requests. The tie-breaker is a coalesce expression over the target's most
// common identity properties (`coalesce(t.id, t.uid)` by default) and does
// not require an index because it only resolves within the already-bounded
// first-page set. entry.targetIdentityProperty appends a verb-specific
// fallback (see targetOrderTiebreakerProperties) for target labels whose
// canonical identity is not id/uid/name, e.g. MANAGES -> Directory (path
// only). Leaving targetIdentityProperty empty keeps this function's output
// byte-identical to the pre-#5369 shape. The access filter adds the #5167
// scope WHERE clause for a scoped caller (empty for shared/admin/local).
func relationshipEdgesCypher(entry relationshipVerbEntry, access repositoryAccessFilter) string {
	return "MATCH (s:" + entry.sourceLabel + ")-[r:" + entry.verb + "]->(t)\n" +
		relationshipEdgesScopeWhereClause(entry, access) +
		"RETURN coalesce(s.id, s.uid, s.name, s.path) AS source_id,\n" +
		"       coalesce(s.name, s.path, s.id, s.uid) AS source_name,\n" +
		"       " + targetIdentityCoalesce(entry) + " AS target_id,\n" +
		"       coalesce(t.name, t.path, t.id, t.uid) AS target_name,\n" +
		"       r.rationale AS evidence,\n" +
		"       r.source_tool AS source_tool\n" +
		"ORDER BY s." + entry.sourceProperty + ", " + targetOrderTiebreaker(entry) + "\n" +
		"LIMIT $limit"
}

// targetIdentityCoalesceProperties returns the coalesce() property order for
// the target_id projection. The default order (id, uid, name, path) is
// identical for every catalog entry that leaves targetIdentityProperty unset,
// which is what keeps their emitted Cypher byte-identical to the pre-#5369
// shape. When targetIdentityProperty is set, it is moved to the front so the
// target's canonical identity resolves before the generic fallbacks (e.g.
// MANAGES needs t.path first because its Directory target has no id/uid).
func targetIdentityCoalesceProperties(entry relationshipVerbEntry) []string {
	order := []string{"id", "uid", "name", "path"}
	if entry.targetIdentityProperty == "" {
		return order
	}
	out := make([]string, 0, len(order))
	out = append(out, entry.targetIdentityProperty)
	for _, property := range order {
		if property == entry.targetIdentityProperty {
			continue
		}
		out = append(out, property)
	}
	return out
}

// targetOrderTiebreakerProperties returns the coalesce() property order for
// the ORDER BY tie-breaker. The default (id, uid) is identical for every
// catalog entry that leaves targetIdentityProperty unset. When set, the
// property is appended as an extra fallback so ties still resolve
// deterministically for target labels whose identity properties do not
// include id/uid (e.g. MANAGES -> Directory, which only has path).
func targetOrderTiebreakerProperties(entry relationshipVerbEntry) []string {
	order := []string{"id", "uid"}
	if entry.targetIdentityProperty == "" {
		return order
	}
	for _, property := range order {
		if property == entry.targetIdentityProperty {
			return order
		}
	}
	out := make([]string, 0, len(order)+1)
	out = append(out, order...)
	return append(out, entry.targetIdentityProperty)
}

func coalesceExpr(variable string, properties []string) string {
	parts := make([]string, 0, len(properties))
	for _, property := range properties {
		parts = append(parts, variable+"."+property)
	}
	return "coalesce(" + strings.Join(parts, ", ") + ")"
}

// targetIdentityCoalesce builds the target_id RETURN projection expression.
func targetIdentityCoalesce(entry relationshipVerbEntry) string {
	return coalesceExpr("t", targetIdentityCoalesceProperties(entry))
}

// targetOrderTiebreaker builds the ORDER BY tie-breaker expression.
func targetOrderTiebreaker(entry relationshipVerbEntry) string {
	return coalesceExpr("t", targetOrderTiebreakerProperties(entry))
}

// relationshipEdgesScopeWhereClause returns the #5167 access-scoping WHERE
// clause for relationshipEdgesCypher/relationshipEdgesCypherFiltered, or the
// empty string for a shared/admin/local caller (access.scoped() == false,
// unscoped Cypher stays byte-identical to the pre-#5167 query). For a scoped
// caller it always binds the source endpoint s -- entry.sourceLabel is always
// one of Repository, Workload, WorkloadInstance, or a repo_id-carrying code
// label, all covered by relationshipEndpointScopePredicate -- which alone
// ensures a scoped caller never sees an edge sourced from another tenant's
// entity. It additionally binds target t when entry.targetAttributable is
// true (see that field's doc comment for which verbs qualify).
func relationshipEdgesScopeWhereClause(entry relationshipVerbEntry, access repositoryAccessFilter) string {
	if !access.scoped() {
		return ""
	}
	scalars, _ := access.scopeGrantInlineScalars()
	clauses := []string{relationshipEndpointScopePredicate("s", scalars)}
	if entry.targetAttributable {
		clauses = append(clauses, relationshipEndpointScopePredicate("t", scalars))
	}
	return "WHERE " + strings.Join(clauses, " AND ") + "\n"
}

// relationshipEdgesCypherFiltered is the source_tool-filtered variant of
// relationshipEdgesCypher. It inserts a WHERE clause after the MATCH line that
// binds $source_tool to r.source_tool, so the index-ordered scan and LIMIT
// short-circuit are preserved. The $source_tool param must always be provided
// by the caller; the unfiltered path must NOT call this function.
//
// The verb, label, and property are taken from the fixed catalog, never from
// request input, so the interpolation cannot inject arbitrary patterns.
func relationshipEdgesCypherFiltered(entry relationshipVerbEntry, access repositoryAccessFilter) string {
	where := "WHERE r.source_tool = $source_tool"
	if access.scoped() {
		scalars, _ := access.scopeGrantInlineScalars()
		where += " AND " + relationshipEndpointScopePredicate("s", scalars)
		if entry.targetAttributable {
			where += " AND " + relationshipEndpointScopePredicate("t", scalars)
		}
	}
	return "MATCH (s:" + entry.sourceLabel + ")-[r:" + entry.verb + "]->(t)\n" +
		where + "\n" +
		"RETURN coalesce(s.id, s.uid, s.name, s.path) AS source_id,\n" +
		"       coalesce(s.name, s.path, s.id, s.uid) AS source_name,\n" +
		"       " + targetIdentityCoalesce(entry) + " AS target_id,\n" +
		"       coalesce(t.name, t.path, t.id, t.uid) AS target_name,\n" +
		"       r.rationale AS evidence,\n" +
		"       r.source_tool AS source_tool\n" +
		"ORDER BY s." + entry.sourceProperty + ", " + targetOrderTiebreaker(entry) + "\n" +
		"LIMIT $limit"
}

// relationshipEndpointScopePredicate binds a relationships/edges endpoint
// alias (source or target) to the caller's grant using
// infraResourceScopeCoreDisjuncts -- the same durable-provenance disjuncts
// infraResourceScopePredicate (infra_scope_grant.go) uses, MINUS its DEFINES
// disjunct.
//
// Why not infraResourceScopePredicate directly (#5167 F-6 W6 review, P1 "do
// not authorize relationship endpoints through shared workloads"):
// relationshipVerbCatalog's sourceLabel/target set includes bare Workload
// nodes (DEPENDS_ON's source and target, INSTANCE_OF's target). Workload
// identity is name-derived only -- projection.go builds workload:%s from the
// workload name and MERGEs on {id: $workload_id} -- so two repositories that
// define same-named workloads collapse to a single Workload node with
// last-writer-wins repo_id. A DEFINES disjunct (whether infraResourceScopePredicate's
// own or a belt-and-suspenders EXISTS clause layered on top of it, as an
// earlier version of this predicate carried) admits that shared node whenever
// ANY granted repository defines it, not only the repository whose write won
// the repo_id race. That is safe for a reachability-counting caller (the
// admitted alias only gates a further, independently-scoped hop), but this
// endpoint predicate is projected directly as the returned edge's
// source_id/source_name/target_id/target_name -- admitting a collision
// Workload via DEFINES would expose every edge attached to it, including
// edges a DIFFERENT tenant's ingestion wrote, purely because the two tenants'
// workloads share a name. Durable per-node provenance (direct repo_id/id
// ownership, USES, or DEPLOYMENT_SOURCE) is required instead; a bare Workload
// endpoint with none of those durably attributable to the caller's grant is
// correctly excluded (under-authorization is the fail-closed, acceptable
// outcome here, never a leak).
func relationshipEndpointScopePredicate(alias string, scalars []string) string {
	// Parenthesized as one atomic OR-group: callers AND-combine this with a
	// sibling predicate (source AND target) or with an unrelated condition
	// (e.g. r.source_tool = $source_tool), and Cypher's AND binds tighter than
	// OR, so an unparenthesized trailing " OR ..." would silently detach from
	// the rest of the OR-chain under AND combination.
	return "(" + strings.Join(infraResourceScopeCoreDisjuncts(alias, scalars), " OR ") + ")"
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
