// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

const (
	// relationshipEdgesDefaultLimit is the default page size for the per-verb
	// concrete edge slice.
	relationshipEdgesDefaultLimit = 50
	// relationshipEdgesMaxLimit clamps the requested edge page size so a single
	// verb drill-down can never request an unbounded slice.
	relationshipEdgesMaxLimit = 200
	// relationshipBreakdownMaxConcurrency is the retained-data-proven safe
	// overlap for the catalog's source-label scans on NornicDB. The slots belong
	// to the handler, so simultaneous HTTP requests share the same cap instead of
	// multiplying per-request fan-out.
	relationshipBreakdownMaxConcurrency = 4
)

// relationshipVerbTile is one entry in the relationships catalog: a typed-edge
// verb with its layer, whole-graph edge count, evidence/source label, and an
// optional per-source-tool breakdown for Tier-2 verbs that carry source_tool.
type relationshipVerbTile struct {
	Verb        string         `json:"verb"`
	Layer       string         `json:"layer"`
	Count       int            `json:"count"`
	Evidence    string         `json:"evidence"`
	Detail      string         `json:"detail"`
	SourceTools map[string]int `json:"source_tools,omitempty"`
}

// relationshipEdge is one concrete typed edge with its endpoints, evidence,
// and the optional source_tool property stamped by the Tier-2 resolver.
type relationshipEdge struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
	Evidence   string `json:"evidence,omitempty"`
	SourceTool string `json:"source_tool,omitempty"`
}

type relationshipEdgesRequest struct {
	Verb       string `json:"verb"`
	SourceTool string `json:"source_tool"`
	Limit      *int   `json:"limit"`
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

// getRelationshipsCatalog returns the fixed typed-edge verb catalog with one
// relationship-type-indexed whole-graph count per verb.
//
// POST /api/v0/relationships/catalog
//
// Counts use the anonymous-endpoint relationship-type aggregate so every source
// label that writes the verb is included. Source-label anchoring applies only
// to the concrete edge slices and source_tool breakdowns.
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
		"resolved from per-verb relationship-type-indexed whole-graph counts",
	))
}

// relationshipVerbTiles runs one relationship-type-indexed whole-graph count
// per catalog verb and returns the verb tiles in catalog order. It overlaps the
// independent source_tool breakdown reads only for verbs that carry stamped
// edges; each result is written back to its catalog position so scheduling
// cannot reorder the API.
func (h *InfraHandler) relationshipVerbTiles(ctx context.Context) ([]relationshipVerbTile, error) {
	tiles := make([]relationshipVerbTile, 0, len(relationshipVerbCatalog))
	for _, entry := range relationshipVerbCatalog {
		row, err := h.Neo4j.RunSingle(ctx, relationshipCountCypher(entry), nil)
		if err != nil {
			return nil, err
		}
		tile := relationshipVerbTile{
			Verb:     entry.verb,
			Layer:    entry.layer,
			Count:    IntVal(row, "count"),
			Evidence: entry.evidence,
			Detail:   entry.detail,
		}
		tiles = append(tiles, tile)
	}

	errs := make([]error, len(relationshipVerbCatalog))
	breakdownSlots := h.relationshipBreakdownSemaphore()
	var wg sync.WaitGroup
	for i, entry := range relationshipVerbCatalog {
		if !entry.carriesSourceTool {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case breakdownSlots <- struct{}{}:
				defer func() { <-breakdownSlots }()
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			breakdown, err := h.relationshipSourceToolBreakdown(ctx, entry)
			if err != nil {
				errs[i] = err
				return
			}
			if len(breakdown) > 0 {
				tiles[i].SourceTools = breakdown
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return tiles, nil
}

// relationshipBreakdownSemaphore returns the handler-wide slots shared by all
// catalog requests. Lazy initialization keeps direct handler construction in
// tests and embedded callers safe while sync.Once prevents a first-request race.
func (h *InfraHandler) relationshipBreakdownSemaphore() chan struct{} {
	h.relationshipBreakdownOnce.Do(func() {
		h.relationshipBreakdownSlots = make(chan struct{}, relationshipBreakdownMaxConcurrency)
	})
	return h.relationshipBreakdownSlots
}

// relationshipSourceToolBreakdown queries the source-label-anchored source_tool
// distribution for one stamped verb. It excludes edges that have no source_tool
// property, so the map only contains tools that have actually stamped edges for
// that verb. Its cost still scales with the selected source-label population.
func (h *InfraHandler) relationshipSourceToolBreakdown(ctx context.Context, entry relationshipVerbEntry) (map[string]int, error) {
	rows, err := h.Neo4j.Run(ctx, relationshipSourceToolBreakdownCypher(entry), nil)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		tool := StringVal(row, "source_tool")
		if tool == "" {
			continue
		}
		result[tool] = IntVal(row, "count")
	}
	return result, nil
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

	tool := strings.ToLower(strings.TrimSpace(req.SourceTool))
	if tool != "" && !sourcetool.IsValid(tool) {
		WriteError(w, http.StatusBadRequest, "unknown source_tool")
		return
	}

	limit := req.limit()
	var (
		edges     []relationshipEdge
		truncated bool
	)
	// Short-circuit a source_tool filter on a verb that never stamps it (Tier-1
	// self-labeling and Tier-3 code/structural verbs): no edge can match by this
	// package's own contract, and running the filtered slice would still scan the
	// verb's source label — e.g. the IMPORTS slice scans the large File label at
	// ~9.9s even for zero edges (docs/public/reference/cypher-performance.md). An
	// empty page is the correct, immediate answer.
	if tool != "" && !entry.carriesSourceTool {
		edges = []relationshipEdge{}
	} else {
		var err error
		edges, truncated, err = h.relationshipEdges(r.Context(), entry, tool, limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	resp := map[string]any{
		"verb":      entry.verb,
		"layer":     entry.layer,
		"evidence":  entry.evidence,
		"detail":    entry.detail,
		"edges":     edges,
		"truncated": truncated,
		"limit":     limit,
	}
	if tool != "" {
		resp["source_tool"] = tool
	}
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
		h.profile(),
		relationshipsCatalogCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from a bounded source-anchored typed-edge slice",
	))
}

// relationshipEdges runs the source-anchored edge slice for a verb. It probes
// limit+1 rows to set the truncation flag deterministically without a second
// scan, the established pattern from graphSummaryHotEntities.
//
// When tool is non-empty the filtered Cypher variant is used, which adds a
// WHERE clause on r.source_tool. When tool is empty the unfiltered path is
// used and the $source_tool param is never sent.
func (h *InfraHandler) relationshipEdges(ctx context.Context, entry relationshipVerbEntry, tool string, limit int) ([]relationshipEdge, bool, error) {
	var (
		cypher string
		params map[string]any
	)
	if tool != "" {
		cypher = relationshipEdgesCypherFiltered(entry)
		params = map[string]any{"limit": limit + 1, "source_tool": tool}
	} else {
		cypher = relationshipEdgesCypher(entry)
		params = map[string]any{"limit": limit + 1}
	}
	rows, err := h.Neo4j.Run(ctx, cypher, params)
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
			SourceTool: StringVal(row, "source_tool"),
		})
	}
	return edges, truncated, nil
}
