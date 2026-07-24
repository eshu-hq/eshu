// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
	"time"

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
		if WriteGraphReadError(w, r, err, relationshipsCatalogCapability) {
			return
		}
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
// per catalog verb and returns the verb tiles in catalog order. Source-tool
// distributions use one label-grouped aggregate so each owning source label is
// scanned once rather than once per stamped verb.
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

	breakdowns, err := h.relationshipSourceToolBreakdowns(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tiles {
		if breakdown := breakdowns[tiles[i].Verb]; len(breakdown) > 0 {
			tiles[i].SourceTools = breakdown
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

// acquireRelationshipBreakdownSlot waits for one handler-wide source-tool
// breakdown permit and returns its release function. The surrounding queue and
// in-flight telemetry is label-free and does not change the four-slot admission
// order, cancellation behavior, or permit ownership contract.
func (h *InfraHandler) acquireRelationshipBreakdownSlot(ctx context.Context) (func(), error) {
	slots := h.relationshipBreakdownSemaphore()
	started := time.Now()
	h.adjustRelationshipBreakdownQueued(ctx, 1)
	select {
	case slots <- struct{}{}:
		h.adjustRelationshipBreakdownQueued(ctx, -1)
		h.recordRelationshipBreakdownPermitWait(ctx, time.Since(started))
		h.adjustRelationshipBreakdownInFlight(ctx, 1)
		return func() {
			h.adjustRelationshipBreakdownInFlight(ctx, -1)
			<-slots
		}, nil
	case <-ctx.Done():
		h.adjustRelationshipBreakdownQueued(ctx, -1)
		h.recordRelationshipBreakdownPermitWait(ctx, time.Since(started))
		return nil, ctx.Err()
	}
}

func (h *InfraHandler) adjustRelationshipBreakdownQueued(ctx context.Context, delta int64) {
	if h != nil && h.Instruments != nil && h.Instruments.RelationshipBreakdownQueued != nil {
		h.Instruments.RelationshipBreakdownQueued.Add(ctx, delta)
	}
}

func (h *InfraHandler) adjustRelationshipBreakdownInFlight(ctx context.Context, delta int64) {
	if h != nil && h.Instruments != nil && h.Instruments.RelationshipBreakdownInFlight != nil {
		h.Instruments.RelationshipBreakdownInFlight.Add(ctx, delta)
	}
}

func (h *InfraHandler) recordRelationshipBreakdownPermitWait(ctx context.Context, duration time.Duration) {
	if h != nil && h.Instruments != nil && h.Instruments.RelationshipBreakdownPermitWaitDuration != nil {
		h.Instruments.RelationshipBreakdownPermitWaitDuration.Record(ctx, duration.Seconds())
	}
}

type relationshipSourceToolBreakdownResult struct {
	index int
	rows  []map[string]any
	err   error
}

// relationshipSourceToolBreakdowns returns source-tool distributions keyed by
// verb. Each source-owner aggregate owns one handler-wide permit, so the two
// independent label scans overlap while all catalog requests share the same
// four-read backend cap.
func (h *InfraHandler) relationshipSourceToolBreakdowns(ctx context.Context) (map[string]map[string]int, error) {
	queries := relationshipSourceToolBreakdownCyphers()
	completed := make(chan relationshipSourceToolBreakdownResult, len(queries))
	for index, cypher := range queries {
		go func() {
			rows, err := h.relationshipSourceToolBreakdown(ctx, cypher)
			completed <- relationshipSourceToolBreakdownResult{index: index, rows: rows, err: err}
		}()
	}
	ordered := make([]relationshipSourceToolBreakdownResult, len(queries))
	for range queries {
		result := <-completed
		ordered[result.index] = result
	}
	allRows := make([]map[string]any, 0)
	for _, result := range ordered {
		if result.err != nil {
			return nil, result.err
		}
		allRows = append(allRows, result.rows...)
	}

	result := make(map[string]map[string]int, len(allRows))
	for _, row := range allRows {
		verb := StringVal(row, "verb")
		tool := StringVal(row, "source_tool")
		if verb == "" || tool == "" {
			continue
		}
		if result[verb] == nil {
			result[verb] = make(map[string]int)
		}
		result[verb][tool] = IntVal(row, "count")
	}
	return result, nil
}

func (h *InfraHandler) relationshipSourceToolBreakdown(
	ctx context.Context,
	cypher string,
) ([]map[string]any, error) {
	release, err := h.acquireRelationshipBreakdownSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return h.Neo4j.Run(ctx, cypher, nil)
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

	// Access scoping (#5167 Group B): this is a whole-graph edge scan across
	// every source label in relationshipVerbCatalog, so a scoped caller with
	// no granted repository or ingestion scope must never reach the graph
	// (empty page, no read); a granted scoped caller's edges are bound to its
	// grant on both endpoints via relationshipEdgesScopeWhereClause (source
	// always, target when entry.targetAttributable).
	access := repositoryAccessFilterFromContext(r.Context())

	limit := req.limit()
	var (
		edges     []relationshipEdge
		truncated bool
	)
	switch {
	case access.empty():
		edges = []relationshipEdge{}
	// Short-circuit a source_tool filter on a verb that never stamps it (Tier-1
	// self-labeling and Tier-3 code/structural verbs): no edge can match by this
	// package's own contract, and running the filtered slice would still scan the
	// verb's source label — e.g. the IMPORTS slice scans the large File label at
	// ~9.9s even for zero edges (docs/public/reference/cypher-performance.md). An
	// empty page is the correct, immediate answer.
	case tool != "" && !entry.carriesSourceTool:
		edges = []relationshipEdge{}
	default:
		var err error
		edges, truncated, err = h.relationshipEdges(r.Context(), entry, tool, limit, access)
		if err != nil {
			if WriteGraphReadError(w, r, err, relationshipsCatalogCapability) {
				return
			}
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
func (h *InfraHandler) relationshipEdges(
	ctx context.Context,
	entry relationshipVerbEntry,
	tool string,
	limit int,
	access repositoryAccessFilter,
) ([]relationshipEdge, bool, error) {
	var (
		cypher string
		params map[string]any
	)
	if tool != "" {
		cypher = relationshipEdgesCypherFiltered(entry, access)
		params = map[string]any{"limit": limit + 1, "source_tool": tool}
	} else {
		cypher = relationshipEdgesCypher(entry, access)
		params = map[string]any{"limit": limit + 1}
	}
	params = access.graphParams(params)
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
