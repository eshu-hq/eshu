// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	codeownersOwnershipCapability   = "codeowners.ownership.list"
	codeownersOwnershipMaxLimit     = 200
	codeownersOwnershipDefaultLimit = 50
	codeownersOwnershipReadTimeout  = 10 * time.Second
	// codeownersOwnershipNoCursor is the "no keyset cursor" sentinel for
	// after_order_index (order_index is always >= 0), mirroring
	// codeownersOwnershipCypher's own doc comment.
	codeownersOwnershipNoCursor = -1
)

// CodeownersOwnershipHandler exposes a bounded, graph-backed read of one
// repository's Phase 3 DECLARES_CODEOWNER edges (issue #5419 Phase 4), plus a
// manifest-vs-codeowners effective_owner resolved via
// resolveEffectiveRepositoryOwner. It never writes to the graph or the
// service-catalog correlation store; both are read-only dependencies.
// listOwnership resolves the request's repository_id (a canonical
// Repository.id, a human slug, or another repository_selector.go alias) to
// the canonical Repository.id via resolveRepositorySelectorForRequestWithAccess
// before touching either read path (issue #5606), since DECLARES_CODEOWNER
// edges and access grants are both keyed by canonical id.
type CodeownersOwnershipHandler struct {
	Neo4j        GraphQuery
	Correlations ServiceCatalogCorrelationStore
	Profile      QueryProfile
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
func (h *CodeownersOwnershipHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/codeowners/ownership", h.listOwnership)
}

func (h *CodeownersOwnershipHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *CodeownersOwnershipHandler) listOwnership(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeownersOwnership,
		"GET /api/v0/codeowners/ownership",
		codeownersOwnershipCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), codeownersOwnershipCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"codeowners ownership requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			codeownersOwnershipCapability,
			h.profile(),
			requiredProfile(codeownersOwnershipCapability),
		)
		return
	}

	repoSelector := QueryParam(r, "repository_id")
	if repoSelector == "" {
		WriteError(w, http.StatusBadRequest, "repository_id is required")
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

	if h.Neo4j == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"codeowners ownership requires the authoritative graph",
			ErrorCodeBackendUnavailable,
			codeownersOwnershipCapability,
			h.profile(),
			requiredProfile(codeownersOwnershipCapability),
		)
		return
	}

	// Resolve repository_id -- a canonical Repository.id, a human slug
	// (Repository.name), or any other repository_selector.go alias -- to the
	// canonical Repository.id BEFORE evaluating the caller's scoped grant or
	// touching either read path. DECLARES_CODEOWNER edges and access grants
	// are both keyed by canonical id, so passing a slug straight into the
	// Cypher $repo_id anchor (the pre-#5606-fix behavior) matched nothing.
	// Resolution here is deliberately access-agnostic (allScopes: true
	// overrides the caller's real grant for this lookup only, mirroring
	// resolveSupplyChainRepositorySelector): existence/ambiguity resolution
	// must stay independent of authorization so the empty-page check below
	// can run against the correct canonical id for every caller, granted or
	// not.
	repoID, ok := resolveRepositorySelectorForRequestWithAccess(
		w, r, h.Neo4j, nil, repoSelector, repositoryAccessFilter{allScopes: true},
	)
	if !ok {
		return
	}

	// Cross-tenant leak guard (issue #5419 Phase 4b): check the caller's
	// scoped grant against the just-resolved CANONICAL id, not the raw
	// selector -- grants are canonical-id-keyed, so checking the raw slug
	// here would incorrectly deny a caller who legitimately owns the
	// repository. A scoped caller not granted repoID gets the bounded empty
	// page below rather than the repo's real ownership/effective_owner --
	// otherwise a caller granted only repo-a could pass
	// ?repository_id=repo-b (or repo-b's slug) and read repo-b's CODEOWNERS
	// ownership and manifest owner (cross-tenant leak).
	access := repositoryAccessFilterFromContext(r.Context())
	if access.scoped() && !access.allowsRepositoryID(repoID) {
		h.writeEmptyCodeownersOwnership(w, r, repoID, limit)
		return
	}

	queryCtx, cancel := context.WithTimeout(r.Context(), codeownersOwnershipReadTimeout)
	defer cancel()

	cypher, params := codeownersOwnershipCypher(repoID, afterOrderIndex, afterPattern, afterRef, limit+1)
	rows, err := h.Neo4j.Run(queryCtx, cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
			Pattern:    StringVal(row, "pattern"),
			SourcePath: StringVal(row, "source_path"),
			OrderIndex: IntVal(row, "order_index"),
			OwnerRef:   StringVal(row, "owner_ref"),
		})
		lastOrderIndex = IntVal(row, "order_index")
		lastPattern = StringVal(row, "pattern")
		lastRef = StringVal(row, "owner_ref")
	}

	effectiveOwner, err := resolveEffectiveRepositoryOwner(queryCtx, h.Neo4j, h.Correlations, repoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		codeownersOwnershipCapability,
		TruthBasisAuthoritativeGraph,
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
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", codeownersOwnershipMaxLimit))
		return 0, false
	}
	return limit, true
}

// codeownersOwnershipCursor resolves the three-part keyset cursor. All three
// components must be provided together (or none); after_order_index defaults
// to the codeownersOwnershipNoCursor sentinel.
func codeownersOwnershipCursor(w http.ResponseWriter, r *http.Request) (afterOrderIndex int, afterPattern, afterRef string, ok bool) {
	rawOrderIndex := strings.TrimSpace(QueryParam(r, "after_order_index"))
	afterPattern = QueryParam(r, "after_pattern")
	afterRef = QueryParam(r, "after_ref")

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
		WriteError(w, http.StatusBadRequest, "after_order_index, after_pattern, and after_ref must be provided together")
		return 0, "", "", false
	}
	parsed, err := strconv.Atoi(rawOrderIndex)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "after_order_index must be a non-negative integer")
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
func (h *CodeownersOwnershipHandler) writeEmptyCodeownersOwnership(
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		codeownersOwnershipCapability,
		TruthBasisAuthoritativeGraph,
		"scoped token grants do not include the requested repository; ownership and effective_owner are withheld without reading the DECLARES_CODEOWNER graph or the service-catalog correlation store",
	))
}
