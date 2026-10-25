// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository/readmodel"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const readTimeout = 10 * time.Second

// listRoute is the route literal in one named place. Mount spells it out
// anyway: the #5584 route-serves-data registry gate matches the literal
// mux.HandleFunc registration, so keep the two identical.
const listRoute = "POST /api/v0/runtime-declarations/query"

// Fixed bodies for a failed read, one per step. The backend error is recorded
// on the request span, never written to the client (#7674).
const (
	pageReadFailedMessage     = "runtime declaration graph read failed"
	excludedReadFailedMessage = "runtime declaration comparison exclusion read failed"
)

// Handler serves the runtime declaration read surface: the declared runtime
// versions the runtime_declaration_projection reducer domain writes as
// Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration (#8816). It only reads the
// graph.
type Handler struct {
	Neo4j       querycontract.GraphQuery
	Profile     querycontract.QueryProfile
	Instruments *telemetry.Instruments
}

// Mount registers the list route. The literal is spelled out instead of
// listRoute so the #5584 route-serves-data registry gate sees the
// mux.HandleFunc("<route>", h.<method>) registration it verifies.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/runtime-declarations/query", h.listDeclarations)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

// listDeclarations serves the filtered, ordered, keyset-paged list. The order
// is the graph key (repo_id, relative_path, line_number, runtime, order_index),
// so a page is stable across calls and a cursor resumes strictly after the last
// row. A version comparison (major_below, version_below) returns only the
// declarations it can place and reports the rest in partial_reasons.
func (h *Handler) listDeclarations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryRuntimeDeclarations, listRoute, Capability)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), Capability) {
		querycontract.WriteContractError(
			w, r, http.StatusNotImplemented,
			"runtime declarations require authoritative graph mode",
			querycontract.ErrorCodeUnsupportedCapability, Capability,
			h.profile(), querycontract.RequiredProfile(Capability),
		)
		return
	}
	req, err := parseRequest(w, r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		// A scoped caller with no grants sees nothing. Answer the empty page
		// without reading the graph so it cannot probe for rows.
		h.writePage(w, r, req, nil, false, "", excludedSummary{BySpecKind: map[string]int{}}, nil,
			querycontract.TruthBasisNoBackendRead, querycontract.ReasonEmptyGrantNoBackendRead)
		return
	}
	if h.Neo4j == nil {
		querycontract.WriteContractError(
			w, r, http.StatusServiceUnavailable,
			"runtime declarations require the authoritative graph",
			querycontract.ErrorCodeBackendUnavailable, Capability,
			h.profile(), querycontract.RequiredProfile(Capability),
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()

	rows, err := h.readPage(ctx, req, access)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, Capability) {
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, pageReadFailedMessage)
		return
	}
	truncated := len(rows) > req.limit
	if truncated {
		rows = rows[:req.limit]
	}

	excluded := excludedSummary{BySpecKind: map[string]int{}}
	if req.compares() {
		if excluded, err = h.readExcluded(ctx, req, access); err != nil {
			if querycontract.WriteGraphReadError(w, r, err, Capability) {
				return
			}
			tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, excludedReadFailedMessage)
			return
		}
		h.recordExcluded(ctx, excluded)
	}

	var degraded []string
	names, err := h.readNames(ctx, rows)
	if err != nil {
		if tracing.ClientCanceled(ctx, err) {
			tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, pageReadFailedMessage)
			return
		}
		degraded = append(degraded, reasonNamesUnavailable)
	}
	for i := range rows {
		rows[i].RepoName = names[rows[i].RepoID]
	}

	next := ""
	if truncated && len(rows) > 0 {
		if next, err = encodeCursor(rows[len(rows)-1].cursorKey()); err != nil {
			tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, pageReadFailedMessage)
			return
		}
	}
	h.writePage(w, r, req, rows, truncated, next, excluded, degraded,
		querycontract.TruthBasisAuthoritativeGraph,
		"resolved from the RuntimeDeclaration graph nodes the runtime_declaration_projection reducer domain writes; aliases, unresolved specs and ranges are listed but excluded from version comparisons and named in partial_reasons")
}

// readPage runs the page statement and maps its rows. It reads limit+1 rows so
// the caller can tell a full last page from a truncated one.
func (h *Handler) readPage(ctx context.Context, req request, access querycontract.RepositoryAccessFilter) ([]Row, error) {
	stmt := pageStatement(req, access, req.limit+1)
	started := time.Now()
	raw, err := h.Neo4j.Run(ctx, stmt.cypher, stmt.params)
	h.recordRead(ctx, telemetry.RuntimeDeclarationOperationList, started, err)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(raw))
	for _, row := range raw {
		rows = append(rows, rowFromGraph(row))
	}
	return rows, nil
}

// readExcluded runs the exclusion count for a version comparison.
func (h *Handler) readExcluded(ctx context.Context, req request, access querycontract.RepositoryAccessFilter) (excludedSummary, error) {
	stmt := excludedStatement(req, access)
	started := time.Now()
	raw, err := h.Neo4j.Run(ctx, stmt.cypher, stmt.params)
	h.recordRead(ctx, telemetry.RuntimeDeclarationOperationExcludedCount, started, err)
	if err != nil {
		return excludedSummary{}, err
	}
	return summarizeExcluded(raw), nil
}

// readNames reads the display name of each distinct repository on the page. A
// failure is returned for the caller to degrade on, not to hide.
func (h *Handler) readNames(ctx context.Context, rows []Row) (map[string]string, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.RepoID]; ok {
			continue
		}
		seen[row.RepoID] = struct{}{}
		ids = append(ids, row.RepoID)
	}
	started := time.Now()
	names, err := readmodel.QueryRepositoryNamesByID(ctx, h.Neo4j, ids)
	h.recordRead(ctx, telemetry.RuntimeDeclarationOperationRepositoryNames, started, err)
	return names, err
}

// writePage writes the success body. declarations is always a list, never null.
func (h *Handler) writePage(
	w http.ResponseWriter,
	r *http.Request,
	req request,
	rows []Row,
	truncated bool,
	nextCursor string,
	excluded excludedSummary,
	degraded []string,
	basis querycontract.TruthBasis,
	reason string,
) {
	if rows == nil {
		rows = []Row{}
	}
	reasons := append(excluded.partialReasons(), degraded...)
	if reasons == nil {
		reasons = []string{}
	}
	body := map[string]any{
		"declarations":             rows,
		"count":                    len(rows),
		"limit":                    req.limit,
		"truncated":                truncated,
		"filters":                  req.echo(),
		"excluded_from_comparison": excluded,
		"partial_reasons":          reasons,
	}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body,
		querycontract.BuildTruthEnvelope(h.profile(), Capability, basis, reason))
}
