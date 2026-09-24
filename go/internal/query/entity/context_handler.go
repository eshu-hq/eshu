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
	// OPTIONAL MATCH below (already proven safe) plus the id/repo_id checks
	// after RunSingle: the row-id equality check just below guards
	// e.id = $entity_id even if a future backend regresses that anchor, and
	// the access.AllowsRepositoryID check after hydration (unchanged) is
	// what actually fails a scoped, ungranted read closed to not-found.
	//
	// The bare, unlabeled `MATCH (e)` anchor scanned every node in the graph
	// for every call regardless of scope -- the classic all-node-scan shape
	// (docs/public/reference/cypher-performance.md, "unlabeled anchor"),
	// proven live on ops-qa (issue #7006) to blow the 10s bounded-read
	// deadline on every request.
	//
	// A single `MATCH (e:A|B|C)` label DISJUNCTION looks like the fix, and is
	// the shape codequery/chain.AnchorLabelDisjunction and
	// impact/deployment.ImpactAnchorLabelDisjunction use -- but both of those only
	// ever render on the Neo4j-compat path (BuildCallChainCypher's own NornicDB
	// branch bypasses it entirely, and the impact family never uses the raw
	// disjunction in a MATCH at all, only Go-side via strings.Split for its
	// CALL{UNION} resolver below). Proven live on ops-qa (issue #7006): on
	// this NornicDB pin, `MATCH (n:A|B) WHERE n.id = $id` -- and the inline-map
	// form `MATCH (n:A|B {id: $id})` -- both silently return ZERO rows for an
	// id a single-label `MATCH (n:A) WHERE n.id = $id` resolves correctly,
	// reproduced for both a code-entity id and an infra-entity id. Shipping
	// that shape would have converted the timeout into an always-wrong
	// not-found. A many-branch `CALL{UNION}` resolver (the impact/deployment
	// package's own pattern) is also live-proven unsafe here: 28 branches did not return
	// before a 15s timeout even though the target existed and an 8-branch
	// subset resolved it in 0.67s -- badly non-linear, not just slower.
	//
	// The only shape proven both correct and bounded is a single label per
	// MATCH. EntityContextAnchorLabels tries each candidate label in turn,
	// most-common-first, stopping at the first match; a genuinely absent
	// entity pays the full label count (14 reads, each individually proven
	// sub-second live), never a whole-graph or many-branch scan.
	//
	// The file/repo enrichment also changed shape, for a second, independent
	// reason: chaining a SECOND hop onto the (already fast, single-bound-node)
	// `OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)` -- reaching for `r:Repository`
	// via `<-[:REPO_CONTAINS]-` -- is itself live-proven catastrophic on this
	// NornicDB pin: it did not return before a 10s timeout even anchored on a
	// real Function id whose File parent exists and resolves instantly alone.
	// The reverse `(f)<-[:REPO_CONTAINS]-(r)` hop is unreliable even as a
	// REQUIRED (non-optional) single hop from an indexed, bound File node: it
	// returned zero rows for a File whose containing Repository is real and
	// resolves correctly via the forward direction
	// (`(r:Repository {id:...})-[:REPO_CONTAINS]->(f:File)`). So the fix
	// drops the graph-side Repository hop entirely: `e`/`f` already carry a
	// direct `repo_id` property (the canonical writer sets it on every
	// code-entity and File node), and the handler's existing
	// hydrateResolvedEntityRepoIdentity call below already backfills
	// repo_name from the content store once repo_id is set -- no second graph
	// round trip needed. A Repository entity's own id/name backfill the same
	// way, via that function's resolvedEntityIsRepository branch.
	buildCypher := func(label string) string {
		cypher := `
		MATCH (e:` + label + `) WHERE e.id = $entity_id
	`
		cypher += `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)
		OPTIONAL MATCH (e)-[rel]->(target)
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + rows.GraphSemanticMetadataProjection() + `
		       ,coalesce(e.repo_id, f.repo_id) as repo_id,
		       collect(DISTINCT {type: type(rel), target_name: target.name, target_id: target.id}) as relationships
	`
		return cypher
	}

	params := access.GraphParams(map[string]any{"entity_id": entityID})
	var row map[string]any
	var err error
	if h.Neo4j != nil {
		// #7006 review (P1): the per-label loop must share ONE deadline
		// across every candidate, not let each RunSingle claim its own
		// fresh Neo4jReader.runRead window -- production's raw request
		// context carries no deadline of its own for this route, so an
		// unbounded loop could pay up to len(EntityContextAnchorLabels) x
		// the single-read budget (~140s for 14 labels at 10s each) instead
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
		for _, label := range EntityContextAnchorLabels {
			labelsAttempted++
			row, err = h.Neo4j.RunSingle(ctx, buildCypher(label), params)
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
			// stays as a defensive fallback: querytestutil.FakeGraphReader
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
					"labels_total", len(EntityContextAnchorLabels),
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
	if _, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, []map[string]any{response}); err != nil {
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
