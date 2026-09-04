// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/exposure"
)

// exposurePathCapability gates the trace-exposure-path tool on authoritative
// graph mode: the bounded CALLS traversal requires the canonical call graph.
const exposurePathCapability = "code_to_cloud.trace_exposure_path"

// exposurePathDefaultDepth and exposurePathMaxDepth bound the CALLS traversal so
// the walk is always anchored and bounded (a Level 1 non-goal is unbounded
// traversal).
const (
	exposurePathDefaultDepth = 5
	exposurePathMaxDepth     = 10
	// exposurePathResultLimit bounds the number of returned paths.
	exposurePathResultLimit = 25
)

// exposurePathRequest is the trace-exposure-path request body.
type exposurePathRequest struct {
	// Source is the handler entity name (resolved against the content store).
	Source string `json:"source"`
	// SourceEntityID is the handler entity id (preferred when known).
	SourceEntityID string `json:"source_entity_id"`
	// RepoID scopes the source resolution by exact name.
	RepoID string `json:"repo_id"`
	// MaxDepth bounds the CALLS traversal (clamped to [1, exposurePathMaxDepth]).
	MaxDepth int `json:"max_depth"`
}

// traceExposurePath traces bounded reachability from an internet-exposed handler
// source, through CALLS edges and (when materialized) the code-to-cloud bridge
// edges, to a cloud sink from the curated catalog. It returns the path with the
// conservative truth-state vocabulary, always labeled derived (Level 1
// reachability is symbol-level, not value-flow). It never fabricates a path: when
// the bridge edges are not materialized, the cloud-sink segment is reported
// unresolved with an honest reason.
//
// POST /api/v0/impact/trace-exposure-path
func (h *ImpactHandler) traceExposurePath(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), exposurePathCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"exposure-path tracing requires authoritative graph mode",
			"unsupported_capability",
			exposurePathCapability,
			h.profile(),
			requiredProfile(exposurePathCapability),
		)
		return
	}

	var req exposurePathRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Source) == "" && strings.TrimSpace(req.SourceEntityID) == "" {
		WriteError(w, http.StatusBadRequest, "source or source_entity_id is required")
		return
	}
	req.MaxDepth = clampExposureDepth(req.MaxDepth)

	source, spec, classified, reason, err := h.resolveExposureSource(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A function that is not a taint source has no exposure path to trace.
	if !classified {
		h.writeExposureFinding(w, r, exposure.ExposureFinding{
			Source:     source,
			TruthLabel: exposure.TruthLabelDerived,
			State:      exposure.TraversalUnresolved,
			Coverage:   exposure.Coverage{MaxDepth: req.MaxDepth, UnresolvedReason: reason},
		})
		return
	}

	// Internet reachability is only provable once a Function-[:HANDLES_ROUTE]->
	// Endpoint edge ties the handler to an endpoint reachable from 0.0.0.0/0.
	// That bridge is not materialized yet (#2721 Stage 2), so reachability is
	// unproven and the source is ranked network_reachable, never over-claimed as
	// internet_exposed.
	const internetReachable = false
	rank := exposure.RankSourceExposure(spec, internetReachable)

	candidates, truncated, err := h.exposurePathCandidates(r.Context(), source, req.MaxDepth)
	if err != nil {
		if WriteGraphReadError(w, r, err, exposurePathCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	unresolvedReason := ""
	if len(candidates) == 0 {
		unresolvedReason = "no reachable cloud sink found; either no graph-backed sink edge is reachable within the bounded CALLS traversal, or the remaining code-to-cloud bridge edges are not materialized for this source"
	}

	finding := exposure.BuildExposureFinding(exposure.ExposureFindingInput{
		Source:           source,
		SourceKind:       spec.Kind,
		ExposureRank:     rank,
		SinkSpecsByKind:  graphBackedSinkSpecsByKind(),
		Candidates:       candidates,
		MaxDepth:         req.MaxDepth,
		Truncated:        truncated,
		UnresolvedReason: unresolvedReason,
	})
	h.writeExposureFinding(w, r, finding)
}

// clampExposureDepth clamps the requested traversal depth into the bounded range.
func clampExposureDepth(depth int) int {
	if depth <= 0 {
		return exposurePathDefaultDepth
	}
	if depth > exposurePathMaxDepth {
		return exposurePathMaxDepth
	}
	return depth
}

// resolveExposureSource resolves the source handler entity and classifies it as a
// taint source from its content-store dead_code_root_kinds (kept off the graph by
// design; see storage/cypher canonical node writer). It returns the source node,
// the classified source spec, whether classification succeeded, and an honest
// reason when the entity is missing or not a taint source.
func (h *ImpactHandler) resolveExposureSource(ctx context.Context, req exposurePathRequest) (exposure.PathNode, exposure.SourceSpec, bool, string, error) {
	entity, err := h.resolveExposureSourceEntity(ctx, req)
	if err != nil {
		return exposure.PathNode{}, exposure.SourceSpec{}, false, "", err
	}
	if entity == nil {
		return exposure.PathNode{}, exposure.SourceSpec{}, false, "source handler not found in the content store", nil
	}

	node := exposure.PathNode{EntityID: entity.EntityID, Name: entity.EntityName, Labels: []string{"Function"}}
	spec, ok := exposure.ClassifySource(deadCodeRootKindsFromMetadata(entity.Metadata))
	if !ok {
		return node, exposure.SourceSpec{}, false, "source is not a taint source (no untrusted-input handler/root classification); only handlers, consumers, and CLI commands are sources", nil
	}
	return node, spec, true, "", nil
}

// resolveExposureSourceEntity loads the source entity by id, or resolves it by
// exact name within the repo (rejecting ambiguous names).
func (h *ImpactHandler) resolveExposureSourceEntity(ctx context.Context, req exposurePathRequest) (*EntityContent, error) {
	if h.Content == nil {
		return nil, nil
	}
	if id := strings.TrimSpace(req.SourceEntityID); id != "" {
		return h.Content.GetEntityContent(ctx, id)
	}
	candidates, err := resolveExactGraphEntityCandidates(ctx, h.Content, req.RepoID, req.Source)
	if err != nil {
		return nil, err
	}
	return selectExactGraphEntityCandidate(req.RepoID, req.Source, candidates)
}

// exposurePathCandidates runs the bounded CALLS traversal from the source handler
// and recognizes cloud sinks among the reached nodes via the catalog. It returns
// the structural reachability candidates and whether the bound truncated the
// walk. The raw nodes(path) projection works on both Neo4j and NornicDB.
func (h *ImpactHandler) exposurePathCandidates(ctx context.Context, source exposure.PathNode, maxDepth int) ([]exposure.PathCandidate, bool, error) {
	if h.Neo4j == nil || strings.TrimSpace(source.EntityID) == "" {
		return nil, false, nil
	}
	cypher := buildExposurePathCypher(maxDepth)
	params := map[string]any{
		"source_entity_id": source.EntityID,
		"sink_rels":        graphBackedSinkRelationships(),
		"limit":            exposurePathResultLimit,
	}
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, false, err
	}
	candidates := make([]exposure.PathCandidate, 0, len(rows))
	for _, row := range rows {
		if candidate, ok := exposurePathCandidateFromRow(row); ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, len(rows) >= exposurePathResultLimit, nil
}

// buildExposurePathCypher builds the bounded exposure-path traversal. It walks
// CALLS*0..maxDepth from the anchored source to a reached node, then matches a
// single catalog sink edge out of the reached node. When the code-to-cloud bridge
// edges materialize, the reached node will carry those edges and this query will
// resolve real paths; until then it returns no rows (an honest empty result). The
// raw nodes(path) projection is used because NornicDB's inline list projection
// returns null today and Neo4j handles raw nodes(path) equally.
func buildExposurePathCypher(maxDepth int) string {
	var b strings.Builder
	b.WriteString("\n\t\tMATCH (src)\n")
	b.WriteString("\t\tWHERE coalesce(src.id, src.uid) = $source_entity_id\n")
	b.WriteString("\t\tMATCH path = (src)-[:CALLS*0..")
	fmt.Fprint(&b, maxDepth)
	b.WriteString("]->(reached)\n")
	b.WriteString("\t\tMATCH (reached)-[sinkRel]->(sinkNode)\n")
	b.WriteString("\t\tWHERE type(sinkRel) IN $sink_rels\n")
	b.WriteString("\t\tRETURN nodes(path) AS chain,\n")
	b.WriteString("\t\t       type(sinkRel) AS sink_rel,\n")
	b.WriteString("\t\t       sinkNode AS sink_node,\n")
	b.WriteString("\t\t       labels(sinkNode) AS sink_labels,\n")
	b.WriteString("\t\t       length(path) AS depth\n")
	b.WriteString("\t\tLIMIT $limit\n\t")
	return b.String()
}
