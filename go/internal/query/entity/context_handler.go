// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// context_handler.go holds GetEntityContext, extracted from handler.go to
// keep that file under the 500-line cap (issue #7006's shared-deadline fix
// grew the handler past it).

package entity

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/graph/rows"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// GetEntityContext retrieves the context for a specific entity. Exported so the staying graph-read-error tests keep driving the handler; see #6060.
func (h *Handler) GetEntityContext(w http.ResponseWriter, r *http.Request) {
	entityID := querycontract.PathParam(r, "entity_id")
	if entityID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	// Scoped mode used to add an `AND EXISTS { MATCH ... WHERE <grant> }`
	// block here to bound e to the caller's granted repositories. On the
	// pinned NornicDB v1.3.3 image that multi-line `AND EXISTS {...}` group
	// is unreliable: it can silently drop the WHOLE WHERE, including the
	// unrelated `e.id = $entity_id` anchor, so a scoped caller's request for
	// one entity could read back an arbitrary DIFFERENT entity (#6786). The
	// grant is now decided in Go instead, from the single-line-WHERE
	// OPTIONAL MATCH in entityContextCypher (already proven safe) plus the id/repo_id checks
	// after RunSingle: the row-id equality check just below guards
	// e.id = $entity_id even if a future backend regresses that anchor, and
	// the access.AllowsRepositoryID check after hydration (unchanged) is
	// what actually fails a scoped, ungranted read closed to not-found.
	//
	// The bare, unlabeled `MATCH (e)` anchor scans every node in the graph
	// -- the classic all-node-scan shape
	// (docs/public/reference/cypher-performance.md, "unlabeled anchor"),
	// proven live on ops-qa's NornicDB deployment (issue #7006) to blow the
	// 10s bounded-read deadline.
	//
	// A single `MATCH (e:A|B|C)` label DISJUNCTION is not a safe fix: on the
	// pinned NornicDB build `MATCH (n:A|B) WHERE n.id = $id` (and the inline
	// map form) silently returns ZERO rows for an id a single-label MATCH
	// resolves (issue #7006, live-proven for a code-entity and an
	// infra-entity id). A many-branch `CALL{UNION}` resolver did not return
	// within 15s for 28 branches on the same deployment.
	//
	// So the anchor is a fast path plus an exact fallback. First, one
	// single-label `MATCH (e:<Label>)` per EntityContextAnchorLabels entry,
	// most-common-first, stopping at the first row. Then, only if every fast-path label misses,
	// the pre-#7006 unlabeled `MATCH (e)`: the old read matched an id on ANY
	// label that carries an id property (schema, writers and fixtures create
	// well over a hundred), and no short label list can reproduce that answer.
	// A hit on a fast-path label never pays the whole-graph scan; an id on
	// another label, or a genuine miss, pays exactly what the pre-#7006 read
	// paid. Every anchor-loop read shares one bounded deadline (below); the
	// repo-identity hydration read after the loop is a separate bounded read.
	//
	// The file/repo enrichment is the pre-#7006 statement unchanged: repo_id
	// and repo_name come from the Repository that REPO_CONTAINS the entity's
	// File, with the scoped grant on that Repository. A #7006 revision read
	// coalesce(e.repo_id, f.repo_id) and dropped repo_name instead; that lost
	// both columns wherever the nodes carry no repo_id property (the
	// live-backend answer-truth fixtures on NornicDB and Neo4j) and, for a
	// scoped caller, turned an in-grant entity into a 404. The two-hop
	// OPTIONAL MATCH was observed slow on ops-qa's retired NornicDB
	// deployment; answer truth wins over that observation, and the shared
	// deadline still caps the request.
	params := access.GraphParams(map[string]any{"entity_id": entityID})
	var row map[string]any
	var err error
	if h.Neo4j != nil {
		// #7006 review (P1): the per-label loop must share ONE deadline
		// across every candidate, not let each RunSingle claim its own
		// fresh Neo4jReader.runRead window -- production's raw request
		// context carries no deadline of its own for this route, so an
		// unbounded loop could pay up to (len(EntityContextAnchorLabels)+1) x
		// the single-read budget (~160s for 16 anchors at 10s each) instead
		// of the one bounded-read budget the pre-fix single-statement
		// handler had.
		// #7006 review (F6): the telemetry query_name is "entity.context",
		// distinct from the "code_search.fuzzy_symbol" capability string
		// WriteGraphReadError/CapabilityUnsupported use below -- that
		// string is a registered capability (specs/capability-matrix.v1.yaml)
		// this route happens to share with fuzzy symbol search, not a
		// telemetry name, and changing it would break capability-matrix
		// matching. Reusing it for query_name made an entity-context
		// deadline warning indistinguishable from a fuzzy-symbol-search one.
		ctx, cancel := querycontract.WithBoundedGraphReadDeadline(
			querycontract.WithGraphQueryName(r.Context(), "entity.context"),
		)
		defer cancel()
		labelsAttempted := 0
		for _, anchor := range entityContextAnchors() {
			labelsAttempted++
			row, err = h.Neo4j.RunSingle(ctx, entityContextCypher(anchor, access), params)
			if err != nil || row != nil {
				break
			}
		}
		if err != nil {
			// #7006 review F1: Neo4jReader.runRead's graphReadResult now
			// classifies a spent WithBoundedGraphReadDeadline budget as the
			// graph-read policy's own deadline and returns the wrapped
			// querycontract.ErrGraphReadDeadline sentinel directly, so this
			// translation is normally a no-op against the real reader. It
			// stays as a defensive fallback: testutil/graph.FakeGraphReader
			// (used by this package's unit tests) and any other GraphQuery
			// implementation that bypasses Neo4jReader can still return a
			// raw context.DeadlineExceeded, and that must never fall through
			// to a generic 500 or -- worse -- be treated as a silent
			// not-found.
			if errors.Is(err, context.DeadlineExceeded) {
				err = querycontract.ErrGraphReadDeadline
			}
			if h.Logger != nil {
				failureClass := "graph_read_error"
				if errors.Is(err, querycontract.ErrGraphReadDeadline) {
					failureClass = "deadline"
				}
				h.Logger.WarnContext(r.Context(),
					"entity context anchor loop ended with an error before resolving",
					"labels_tried", labelsAttempted,
					"labels_total", len(EntityContextAnchorLabels)+1,
					telemetry.LogKeyFailureClass, failureClass,
				)
			}
			if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
	}

	// Defense-in-depth guard against a backend that stops honoring
	// `e.id = $entity_id` (the exact failure #6786 proved on NornicDB
	// v1.3.3): a row whose id does not match the requested entity is treated
	// as no row at all, the same not-found path a genuinely absent entity
	// takes, rather than trusted as an answer to this request.
	if row != nil {
		if gotID := querycontract.StringVal(row, "id"); gotID != entityID {
			// #6786 review follow-up (F5): this guard firing means the
			// backend returned a DIFFERENT node than the one anchored on --
			// backend anchor drift, not ordinary authorization, and an
			// operator needs to see it. Log outside the `if h.Logger != nil`
			// gate would panic on a nil Handler in tests that construct one
			// without a logger; every other Logger use in this package
			// checks the same way (context_content.go).
			if h.Logger != nil {
				h.Logger.WarnContext(r.Context(),
					"entity context graph row id did not match the requested entity id",
					"requested_entity_id", entityID,
					"returned_entity_id", gotID,
					"reason", "backend_anchor_mismatch",
				)
			}
			h.recordScopedGrantDenied(r.Context(), "entity_context", "backend_anchor_mismatch")
			row = nil
		}
	}

	if row == nil {
		response, fallbackErr := h.getEntityContextFromContent(r.Context(), entityID)
		if fallbackErr != nil {
			if errors.Is(fallbackErr, errContentRelationshipBuilderNotConfigured) {
				querycontract.WriteError(w, http.StatusServiceUnavailable, fallbackErr.Error())
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", fallbackErr))
			return
		}
		if response == nil {
			querycontract.WriteError(w, http.StatusNotFound, "entity not found")
			return
		}
		response["result_limits"] = contextResultLimits(response, entityID)
		response["partial_reasons"] = querycontract.ContextPartialReasons(response)
		querycontract.WriteSuccess(w, r, http.StatusOK, response, contextTruthEnvelope(h.profile()))
		return
	}

	response := map[string]any{
		"id":            querycontract.StringVal(row, "id"),
		"labels":        querycontract.StringSliceVal(row, "labels"),
		"name":          querycontract.StringVal(row, "name"),
		"file_path":     querycontract.StringVal(row, "file_path"),
		"repo_id":       querycontract.StringVal(row, "repo_id"),
		"repo_name":     querycontract.StringVal(row, "repo_name"),
		"language":      querycontract.StringVal(row, "language"),
		"start_line":    querycontract.IntVal(row, "start_line"),
		"end_line":      querycontract.IntVal(row, "end_line"),
		"relationships": extractRelationships(row),
	}
	if metadata := taxonomy.GraphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	// The repo-identity hydration below runs after the anchor loop, so it is
	// deliberately NOT on the loop's shared bounded window: a loop that spent
	// nearly all of that budget on misses would otherwise starve a read that
	// only fires for a Workload/WorkloadInstance row missing repo identity.
	// It gets its own single bounded read (Neo4jReader.runRead wraps every
	// read in the configured read timeout) but the same
	// "entity.context" query name, so its slow-read and deadline telemetry is
	// attributed to this route instead of "unnamed". Worst case for the route
	// is therefore the anchor loop budget plus one more bounded read.
	hydrationCtx := querycontract.WithGraphQueryName(r.Context(), "entity.context")
	if _, err := hydrateResolvedEntityRepoIdentity(hydrationCtx, h.Neo4j, h.Content, []map[string]any{response}); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	if access.Scoped() && !access.AllowsRepositoryID(querycontract.StringVal(response, "repo_id")) {
		h.recordScopedGrantDenied(r.Context(), "entity_context", "grant_denied")
		querycontract.WriteError(w, http.StatusNotFound, "entity not found")
		return
	}
	enriched, err := h.EnrichEntityResultsWithContentMetadata(r.Context(), []map[string]any{response}, querycontract.StringVal(response, "repo_id"), querycontract.StringVal(row, "name"), 1)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entity context: %v", err))
		return
	}
	response = enriched[0]
	attachSemanticSummary(response)

	response["result_limits"] = contextResultLimits(response, entityID)
	response["partial_reasons"] = querycontract.ContextPartialReasons(response)
	querycontract.WriteSuccess(w, r, http.StatusOK, response, contextTruthEnvelope(h.profile()))
}

// entityContextAnchors returns GetEntityContext's anchor clauses in try
// order: one single-label clause per EntityContextAnchorLabels entry, then
// the unlabeled pre-#7006 clause as the exact-answer fallback.
//
// A label whose schema carries a uid uniqueness constraint
// (graph.HasUIDUniquenessConstraint) anchors on
// `e.uid = $entity_id AND e.id = $entity_id` (issue #7089). Neo4j indexes
// uid, not id, on those labels, so the plain `e.id = $entity_id` read was a
// NodeByLabelScan over every node of the label (about 1.08M db hits for the
// first, Function, read on ops-qa), while the uid equality plans as a
// NodeUniqueIndexSeek (under 110 db hits). The id
// equality stays in the predicate, so a clause matches only nodes the old
// `e.id = $entity_id` read matched: a File carries a uid but no id and must
// still not match. Canonical code entities are written with id == uid, so
// the uid seek drops no node the id read found; a node on one of these
// labels whose id differs from its uid would still resolve through the
// unlabeled fallback, which keeps the plain id anchor. Labels with no uid
// constraint (Repository, Workload and WorkloadInstance are id-constrained;
// Directory is keyed by path) keep `e.id = $entity_id`.
func entityContextAnchors() []string {
	anchors := make([]string, 0, len(EntityContextAnchorLabels)+1)
	for _, label := range EntityContextAnchorLabels {
		if graph.HasUIDUniquenessConstraint(label) {
			anchors = append(anchors, "(e:"+label+") WHERE e.uid = $entity_id AND e.id = $entity_id")
			continue
		}
		anchors = append(anchors, "(e:"+label+") WHERE e.id = $entity_id")
	}
	return append(anchors, "(e) WHERE e.id = $entity_id")
}

// entityContextCypher renders GetEntityContext's read for one anchor clause
// from entityContextAnchors. Everything after the anchor is the pre-#7006
// statement: file/repo enrichment through the Repository that REPO_CONTAINS
// the entity's File, with the scoped grant applied to that Repository.
func entityContextCypher(anchor string, access querycontract.RepositoryAccessFilter) string {
	cypher := `
		MATCH ` + anchor + `
	`
	cypher += `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
	`
	if access.Scoped() {
		cypher += `
		WHERE ` + access.GraphCondition("r") + `
	`
	}
	cypher += `
		OPTIONAL MATCH (e)-[rel]->(target)
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + rows.GraphSemanticMetadataProjection() + `
		       ,r.id as repo_id, r.name as repo_name,
		       collect(DISTINCT {type: type(rel), target_name: target.name, target_id: target.id}) as relationships
	`
	return cypher
}
