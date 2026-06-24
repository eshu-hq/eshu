// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
)

const (
	// relationshipEdgesDefaultLimit is the default page size for the per-verb
	// concrete edge slice.
	relationshipEdgesDefaultLimit = 50
	// relationshipEdgesMaxLimit clamps the requested edge page size so a single
	// verb drill-down can never request an unbounded slice.
	relationshipEdgesMaxLimit = 200
)

// relationshipVerbTile is one entry in the relationships catalog: a typed-edge
// verb with its layer, whole-graph edge count, and evidence/source label.
type relationshipVerbTile struct {
	Verb     string `json:"verb"`
	Layer    string `json:"layer"`
	Count    int    `json:"count"`
	Evidence string `json:"evidence"`
	Detail   string `json:"detail"`
}

// relationshipEdge is one concrete typed edge with its endpoints and evidence.
type relationshipEdge struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
	Evidence   string `json:"evidence,omitempty"`
}

type relationshipEdgesRequest struct {
	Verb  string `json:"verb"`
	Limit *int   `json:"limit"`
}

// limit clamps the requested edge page size into the bounded range. The edge
// Cypher always carries a LIMIT, so the slice can never be unbounded.
func (req relationshipEdgesRequest) limit() int {
	if req.Limit == nil {
		return relationshipEdgesDefaultLimit
	}
	switch {
	case *req.Limit < 1:
		return 1
	case *req.Limit > relationshipEdgesMaxLimit:
		return relationshipEdgesMaxLimit
	default:
		return *req.Limit
	}
}

// getRelationshipsCatalog returns the fixed typed-edge verb catalog with a
// bounded, source-label-anchored whole-graph count per verb.
//
// POST /api/v0/relationships/catalog
//
// Each verb is counted with its own single bounded query anchored on the verb's
// source-node label, mirroring the per-label portability rule in
// infra_ecosystem_overview.go. No whole-graph unanchored relationship scan is
// ever run, so the catalog stays within the bounded read contract.
func (h *InfraHandler) getRelationshipsCatalog(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), relationshipsCatalogCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"relationships catalog requires an authoritative platform graph",
			ErrorCodeUnsupportedCapability,
			relationshipsCatalogCapability,
			h.profile(),
			requiredProfile(relationshipsCatalogCapability),
		)
		return
	}
	if h == nil || h.Neo4j == nil {
		WriteError(w, http.StatusServiceUnavailable, "relationships catalog is unavailable")
		return
	}

	tiles, err := h.relationshipVerbTiles(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	totalEdges := 0
	layers := make(map[string]struct{}, 6)
	for _, tile := range tiles {
		totalEdges += tile.Count
		layers[tile.Layer] = struct{}{}
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"verbs":       tiles,
		"verb_count":  len(tiles),
		"total_edges": totalEdges,
		"layer_count": len(layers),
	}, BuildTruthEnvelope(
		h.profile(),
		relationshipsCatalogCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from per-verb source-anchored relationship counts",
	))
}

// relationshipVerbTiles runs one bounded, source-anchored count per catalog
// verb and returns the verb tiles in catalog order.
func (h *InfraHandler) relationshipVerbTiles(ctx context.Context) ([]relationshipVerbTile, error) {
	tiles := make([]relationshipVerbTile, 0, len(relationshipVerbCatalog))
	for _, entry := range relationshipVerbCatalog {
		row, err := h.Neo4j.RunSingle(ctx, relationshipCountCypher(entry), nil)
		if err != nil {
			return nil, err
		}
		tiles = append(tiles, relationshipVerbTile{
			Verb:     entry.verb,
			Layer:    entry.layer,
			Count:    IntVal(row, "count"),
			Evidence: entry.evidence,
			Detail:   entry.detail,
		})
	}
	return tiles, nil
}

// getRelationshipEdges returns a bounded slice of concrete edges for one verb,
// each with its source and target endpoints plus evidence.
//
// POST /api/v0/relationships/edges
//
// The verb must be one of the fixed catalog verbs; the edge query is anchored on
// that verb's source label and always carries a LIMIT, so the slice is bounded.
// The handler over-fetches limit+1 to set a truncated flag without a second scan.
func (h *InfraHandler) getRelationshipEdges(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), relationshipsCatalogCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"relationship edges require an authoritative platform graph",
			ErrorCodeUnsupportedCapability,
			relationshipsCatalogCapability,
			h.profile(),
			requiredProfile(relationshipsCatalogCapability),
		)
		return
	}

	var req relationshipEdgesRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	verb := strings.ToUpper(strings.TrimSpace(req.Verb))
	if verb == "" {
		WriteError(w, http.StatusBadRequest, "verb is required")
		return
	}
	entry, ok := relationshipVerbByName[verb]
	if !ok {
		WriteError(w, http.StatusBadRequest, "unknown relationship verb")
		return
	}
	if h == nil || h.Neo4j == nil {
		WriteError(w, http.StatusServiceUnavailable, "relationship edges are unavailable")
		return
	}

	limit := req.limit()
	edges, truncated, err := h.relationshipEdges(r.Context(), entry, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"verb":      entry.verb,
		"layer":     entry.layer,
		"evidence":  entry.evidence,
		"detail":    entry.detail,
		"edges":     edges,
		"truncated": truncated,
		"limit":     limit,
	}, BuildTruthEnvelope(
		h.profile(),
		relationshipsCatalogCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from a bounded source-anchored typed-edge slice",
	))
}

// relationshipEdges runs the source-anchored edge slice for a verb. It probes
// limit+1 rows to set the truncation flag deterministically without a second
// scan, the established pattern from graphSummaryHotEntities.
func (h *InfraHandler) relationshipEdges(ctx context.Context, entry relationshipVerbEntry, limit int) ([]relationshipEdge, bool, error) {
	rows, err := h.Neo4j.Run(ctx, relationshipEdgesCypher(entry), map[string]any{"limit": limit + 1})
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	edges := make([]relationshipEdge, 0, len(rows))
	for _, row := range rows {
		edges = append(edges, relationshipEdge{
			SourceID:   StringVal(row, "source_id"),
			SourceName: StringVal(row, "source_name"),
			TargetID:   StringVal(row, "target_id"),
			TargetName: StringVal(row, "target_name"),
			Evidence:   StringVal(row, "evidence"),
		})
	}
	return edges, truncated, nil
}
