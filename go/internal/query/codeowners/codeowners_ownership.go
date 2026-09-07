// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	codeownersOwnershipCapability   = "codeowners.ownership.list"
	codeownersOwnershipMaxLimit     = 200
	codeownersOwnershipDefaultLimit = 50
	codeownersOwnershipReadTimeout  = 10 * time.Second
	// codeownersOwnershipNoCursor is the "no keyset cursor" sentinel for
	// after_order_index (order_index is always >= 0), mirroring
	// CodeownersOwnershipCyphers' own doc comment.
	codeownersOwnershipNoCursor = -1
)

// Handler exposes a bounded, graph-backed read of one repository's Phase 3
// DECLARES_CODEOWNER edges (issue #5419 Phase 4), plus a
// manifest-vs-codeowners effective_owner resolved via
// resolveEffectiveRepositoryOwner. It never writes to the graph or the
// service-catalog correlation store; both are read-only dependencies.
//
// It moved out of root package query (#6060 lane A L2); root keeps the
// CodeownersOwnershipHandler compatibility alias (family_codeowners_shim.go)
// so cmd/api, cmd/mcp-server, and staying tests construct it unchanged.
type Handler struct {
	Neo4j        querycontract.GraphQuery
	Correlations querycontract.ServiceCatalogCorrelationStore
	Profile      querycontract.QueryProfile
	Instruments  *telemetry.Instruments
}

// CodeownersOwnershipRow is one CODEOWNERS rule-to-owner declaration: a
// single DECLARES_CODEOWNER edge from the requested repository to a
// CodeownerTeam.
type CodeownersOwnershipRow struct {
	Pattern    string `json:"pattern"`
	SourcePath string `json:"source_path"`
	OrderIndex int    `json:"order_index"`
	OwnerRef   string `json:"owner_ref"`
}

// Mount registers the codeowners ownership route.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/codeowners/ownership", h.ListOwnership)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

// ListOwnership serves GET /api/v0/codeowners/ownership. It is exported
// because the staying cross-family graph-read sweep
// (graph_read_error_entity_service_test.go, which must stay in root package
// query under #6060) invokes the route method directly; Mount is the
// production path.
func (h *Handler) ListOwnership(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeownersOwnership,
		"GET /api/v0/codeowners/ownership",
		codeownersOwnershipCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), codeownersOwnershipCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"codeowners ownership requires authoritative graph mode",
			querycontract.ErrorCodeUnsupportedCapability,
			codeownersOwnershipCapability,
			h.profile(),
			querycontract.RequiredProfile(codeownersOwnershipCapability),
		)
		return
	}

	repoID := querycontract.QueryParam(r, "repository_id")
	if repoID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "repository_id is required")
		return
	}
	limit, ok := codeownersOwnershipLimit(w, r)
	if !ok {
		return
	}
	afterOrderIndex, afterPattern, afterRef, ok := codeownersOwnershipCursor(w, r)
	if !ok {
		return
	}

	// Resolve the caller's scoped grant before touching either read path (the
	// DECLARES_CODEOWNER graph and the service-catalog correlation store used
	// by resolveEffectiveRepositoryOwner). A scoped caller not granted
	// repository_id gets the bounded empty page below rather than the repo's
	// real ownership/effective_owner -- otherwise a caller granted only repo-a
	// could pass ?repository_id=repo-b and read repo-b's CODEOWNERS ownership
	// and manifest owner (cross-tenant leak).
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Scoped() && !access.AllowsRepositoryID(repoID) {
		h.writeEmptyCodeownersOwnership(w, r, repoID, limit)
		return
	}

	if h.Neo4j == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"codeowners ownership requires the authoritative graph",
			querycontract.ErrorCodeBackendUnavailable,
			codeownersOwnershipCapability,
			h.profile(),
			querycontract.RequiredProfile(codeownersOwnershipCapability),
		)
		return
	}

	queryCtx, cancel := context.WithTimeout(r.Context(), codeownersOwnershipReadTimeout)
	defer cancel()

	rows, err := loadCodeownersOwnershipRows(
		queryCtx,
		h.Neo4j,
		repoID,
		afterOrderIndex,
		afterPattern,
		afterRef,
		limit+1,
	)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, codeownersOwnershipCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]CodeownersOwnershipRow, 0, len(rows))
	var lastOrderIndex int
	var lastPattern, lastRef string
	for _, row := range rows {
		results = append(results, CodeownersOwnershipRow{
			Pattern:    querycontract.StringVal(row, "pattern"),
			SourcePath: querycontract.StringVal(row, "source_path"),
			OrderIndex: querycontract.IntVal(row, "order_index"),
			OwnerRef:   querycontract.StringVal(row, "owner_ref"),
		})
		lastOrderIndex = querycontract.IntVal(row, "order_index")
		lastPattern = querycontract.StringVal(row, "pattern")
		lastRef = querycontract.StringVal(row, "owner_ref")
	}

	effectiveOwner, err := resolveEffectiveRepositoryOwner(queryCtx, h.Neo4j, h.Correlations, repoID)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, codeownersOwnershipCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	body := map[string]any{
		"ownership":       results,
		"repository_id":   repoID,
		"count":           len(results),
		"limit":           limit,
		"truncated":       truncated,
		"effective_owner": effectiveOwner,
	}
	if truncated {
		body["next_cursor"] = map[string]any{
			"after_order_index": lastOrderIndex,
			"after_pattern":     lastPattern,
			"after_ref":         lastRef,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		codeownersOwnershipCapability,
		querycontract.TruthBasisAuthoritativeGraph,
		"resolved from the Phase 3 DECLARES_CODEOWNER graph edges for the requested repository, with effective_owner resolved against the reducer's service-catalog correlation store when present",
	))
}

// codeownersOwnershipLimit resolves the bounded limit parameter, defaulting
// to 50 when absent and rejecting values outside 1..200.
func codeownersOwnershipLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return codeownersOwnershipDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > codeownersOwnershipMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", codeownersOwnershipMaxLimit))
		return 0, false
	}
	return limit, true
}

// codeownersOwnershipCursor resolves the three-part keyset cursor. All three
// components must be provided together (or none); after_order_index defaults
// to the codeownersOwnershipNoCursor sentinel.
func codeownersOwnershipCursor(w http.ResponseWriter, r *http.Request) (afterOrderIndex int, afterPattern, afterRef string, ok bool) {
	rawOrderIndex := strings.TrimSpace(querycontract.QueryParam(r, "after_order_index"))
	afterPattern = querycontract.QueryParam(r, "after_pattern")
	afterRef = querycontract.QueryParam(r, "after_ref")

	present := 0
	for _, v := range []string{rawOrderIndex, afterPattern, afterRef} {
		if v != "" {
			present++
		}
	}
	if present == 0 {
		return codeownersOwnershipNoCursor, "", "", true
	}
	if present != 3 {
		querycontract.WriteError(w, http.StatusBadRequest, "after_order_index, after_pattern, and after_ref must be provided together")
		return 0, "", "", false
	}
	parsed, err := strconv.Atoi(rawOrderIndex)
	if err != nil || parsed < 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "after_order_index must be a non-negative integer")
		return 0, "", "", false
	}
	return parsed, afterPattern, afterRef, true
}

// writeEmptyCodeownersOwnership returns the bounded zero-row page used when a
// scoped caller's grant does not include repoID. It reports the same shape as
// a real "no CODEOWNERS rules" answer -- empty ownership, no next_cursor, and
// a zero-value effective_owner -- without reading the DECLARES_CODEOWNER graph
// or the service-catalog correlation store, so a scoped caller cannot
// distinguish "out of grant" from "granted but genuinely empty" and cannot use
// either read path to probe an ungranted repository's ownership.
func (h *Handler) writeEmptyCodeownersOwnership(
	w http.ResponseWriter,
	r *http.Request,
	repoID string,
	limit int,
) {
	body := map[string]any{
		"ownership":       []CodeownersOwnershipRow{},
		"repository_id":   repoID,
		"count":           0,
		"limit":           limit,
		"truncated":       false,
		"effective_owner": EffectiveRepositoryOwner{},
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		codeownersOwnershipCapability,
		querycontract.TruthBasisAuthoritativeGraph,
		"scoped token grants do not include the requested repository; ownership and effective_owner are withheld without reading the DECLARES_CODEOWNER graph or the service-catalog correlation store",
	))
}
