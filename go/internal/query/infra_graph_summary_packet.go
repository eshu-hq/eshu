// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	graphSummaryDefaultHotLimit = 10
	graphSummaryMaxHotLimit     = 100
	// graphSummaryNeedsRepoNote explains why hot-entity ranking and relationship
	// counts are omitted from the ecosystem-wide packet.
	graphSummaryNeedsRepoNote = "hot-entity ranking and key-relationship counts require a repo_id scope; only bounded ecosystem-wide label counts are returned without one"
)

type graphSummaryPacketRequest struct {
	RepoID string `json:"repo_id"`
	Limit  *int   `json:"limit"`
}

func (req graphSummaryPacketRequest) repoID() string {
	return strings.TrimSpace(req.RepoID)
}

// hotLimit clamps the requested hot-entity limit into the bounded range. The
// hot-entity Cypher always carries a LIMIT so the degree query can never return
// an unbounded result set.
func (req graphSummaryPacketRequest) hotLimit() int {
	if req.Limit == nil {
		return graphSummaryDefaultHotLimit
	}
	switch {
	case *req.Limit < 1:
		return 1
	case *req.Limit > graphSummaryMaxHotLimit:
		return graphSummaryMaxHotLimit
	default:
		return *req.Limit
	}
}

// getGraphSummaryPacket returns a bounded, summary-first graph packet for a
// resolved scope.
//
// POST /api/v0/ecosystem/graph-summary
//
// With repo_id the packet is repo-scoped: hot entities ranked by call degree via
// the proven repo-anchored hub-function shape (bounded by LIMIT), per-type
// repo-anchored relationship counts, and a repo-anchored ecosystem map. Without
// repo_id only the bounded per-label ecosystem counts (identical to
// getEcosystemOverview) plus a note are returned; no whole-graph hot-entity scan
// is ever run. Every count is a single bounded label/repo-anchored scan and the
// hot-entity list is always LIMIT-bounded, so the tool stays within the bounded
// MCP/API read contract.
func (h *InfraHandler) getGraphSummaryPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryGraphSummaryPacket,
		"POST /api/v0/ecosystem/graph-summary",
		graphSummaryPacketCapability,
	)
	defer span.End()

	var req graphSummaryPacketRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), graphSummaryPacketCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"graph summary packet requires an authoritative platform graph",
			ErrorCodeUnsupportedCapability,
			graphSummaryPacketCapability,
			h.profile(),
			requiredProfile(graphSummaryPacketCapability),
		)
		return
	}

	if h == nil || h.Neo4j == nil {
		WriteError(w, http.StatusServiceUnavailable, "graph summary packet is unavailable")
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())

	if req.repoID() == "" {
		data, err := h.graphSummaryEcosystemPacket(r.Context(), access)
		if err != nil {
			if WriteGraphReadError(w, r, err, graphSummaryPacketCapability) {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteSuccess(w, r, http.StatusOK, data, ecosystemOverviewTruth(h.profile(), access))
		return
	}

	// Access scoping (#5167 Group B): the repo-scoped branch runs hot-entity
	// ranking, relationship counts, and the repo ecosystem map anchored
	// entirely on the caller-supplied repo_id with no grant check of its own.
	// A scoped caller whose repo_id is outside its granted repositories/
	// ingestion scopes -- or who holds no grants at all -- gets not_found, the
	// same no-existence-disclosure contract scopedIncidentContextRoute and
	// scopedInfraRelationshipsRoute use for an out-of-grant seed.
	if access.scoped() && !access.allowsRepositoryID(req.repoID()) {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	data, err := h.graphSummaryRepoPacket(r.Context(), req)
	if err != nil {
		if WriteGraphReadError(w, r, err, graphSummaryPacketCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, data, h.graphSummaryEnvelope())
}

func (h *InfraHandler) graphSummaryEnvelope() *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		graphSummaryPacketCapability,
		TruthBasisHybrid,
		"resolved from bounded per-label, per-type, and degree-centrality summary counters",
	)
}

// graphSummaryEcosystemPacket returns the bounded ecosystem-wide label counts
// (the same single-label count shapes as getEcosystemOverview, restricted to
// access's grant per runEcosystemOverviewCounts) plus a note that a repo_id
// scope is required for hot entities and relationship counts.
func (h *InfraHandler) graphSummaryEcosystemPacket(ctx context.Context, access repositoryAccessFilter) (map[string]any, error) {
	ecosystem, err := runEcosystemOverviewCounts(ctx, h.Neo4j, access)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"scope":         "ecosystem",
		"ecosystem_map": ecosystem,
		"note":          graphSummaryNeedsRepoNote,
	}, nil
}

// graphSummaryRepoPacket assembles the repo-scoped packet from bounded,
// repo-anchored reads: hot entities (degree-bounded LIMIT), per-type
// relationship counts, and the repo ecosystem map.
func (h *InfraHandler) graphSummaryRepoPacket(ctx context.Context, req graphSummaryPacketRequest) (map[string]any, error) {
	repoID := req.repoID()
	params := map[string]any{"repo_id": repoID}

	hot, truncated, err := h.graphSummaryHotEntities(ctx, repoID, req.hotLimit())
	if err != nil {
		return nil, err
	}

	relationships, err := h.graphSummaryRelationshipCounts(ctx, params)
	if err != nil {
		return nil, err
	}

	ecosystem, err := h.graphSummaryRepoEcosystemMap(ctx, params)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"scope":                  "repository",
		"repo_id":                repoID,
		"hot_entities":           hot,
		"hot_entities_truncated": truncated,
		"key_relationships":      relationships,
		"ecosystem_map":          ecosystem,
	}, nil
}

// graphSummaryHotEntities runs the repo-anchored hub-function degree query and
// returns at most limit rows. It probes limit+1 to set the truncation flag
// deterministically without a second scan.
func (h *InfraHandler) graphSummaryHotEntities(ctx context.Context, repoID string, limit int) ([]map[string]any, bool, error) {
	rows, err := h.Neo4j.Run(ctx, graphSummaryHotEntitiesCypher, map[string]any{
		"repo_id": repoID,
		"limit":   limit + 1,
	})
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	hot := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		hot = append(hot, map[string]any{
			"function_id":    StringVal(row, "function_id"),
			"function_name":  StringVal(row, "function_name"),
			"file_path":      StringVal(row, "file_path"),
			"incoming_calls": IntVal(row, "incoming_calls"),
			"outgoing_calls": IntVal(row, "outgoing_calls"),
			"total_degree":   IntVal(row, "total_degree"),
		})
	}
	return hot, truncated, nil
}

// graphSummaryRelationshipCounts counts each code relationship type with its own
// bounded, repo-anchored count query, mirroring the per-label portability rule.
func (h *InfraHandler) graphSummaryRelationshipCounts(ctx context.Context, params map[string]any) (map[string]any, error) {
	counts := make(map[string]any, len(graphSummaryRelationshipCounts))
	for _, entry := range graphSummaryRelationshipCounts {
		row, err := h.Neo4j.RunSingle(ctx, entry.cypher, params)
		if err != nil {
			return nil, err
		}
		counts[entry.relType] = IntVal(row, "count")
	}
	return counts, nil
}

// graphSummaryRepoEcosystemMap returns the repo-anchored structural counts using
// the same narrow count shapes proven by repository_context_counts.go.
func (h *InfraHandler) graphSummaryRepoEcosystemMap(ctx context.Context, params map[string]any) (map[string]any, error) {
	ecosystem := make(map[string]any, len(graphSummaryRepoEcosystemCounts)+1)
	for _, entry := range graphSummaryRepoEcosystemCounts {
		row, err := h.Neo4j.RunSingle(ctx, entry.cypher, params)
		if err != nil {
			return nil, err
		}
		ecosystem[entry.field] = IntVal(row, "count")
	}
	languages, err := h.graphSummaryRepoLanguages(ctx, params)
	if err != nil {
		return nil, err
	}
	ecosystem["languages"] = languages
	return ecosystem, nil
}

// graphSummaryRepoLanguages returns the bounded set of distinct languages in the
// repo, reusing the repo-anchored file-language shape from the story summary.
func (h *InfraHandler) graphSummaryRepoLanguages(ctx context.Context, params map[string]any) ([]string, error) {
	rows, err := h.Neo4j.Run(ctx, graphSummaryRepoLanguagesCypher, params)
	if err != nil {
		return nil, err
	}
	languages := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		language := StringVal(row, "language")
		if language == "" {
			continue
		}
		if _, ok := seen[language]; ok {
			continue
		}
		seen[language] = struct{}{}
		languages = append(languages, language)
	}
	return languages, nil
}
