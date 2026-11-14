// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// readRepositoryContextEvidenceDetail reads the evidence_detail query
// parameter of the repository context route and validates it before any read
// runs. An unknown value writes a 400 invalid_argument envelope and reports
// false. An absent value is full, so HTTP callers keep today's shape and the
// MCP get_repo_context tool opts into handles at dispatch.
func readRepositoryContextEvidenceDetail(w http.ResponseWriter, r *http.Request) (string, bool) {
	detail := r.URL.Query().Get("evidence_detail")
	if err := querycontract.ValidateContextEvidenceDetail(detail); err != nil {
		querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeInvalidArgument,
			Message:    err.Error(),
			Capability: "platform_impact.context_overview",
		})
		return "", false
	}
	return detail, true
}

// writeRepositoryContextResponse shapes the built context response per
// evidenceDetail, appends the shaping omissions to a fresh truth envelope,
// and writes it. Shaping runs after the grant filter so a scoped caller never
// learns hidden counts, and before the evidence boundaries, which stay nil
// for this tool. The evidence_detail stage event carries the effective mode
// and the omission count so an operator can see the shaping happened.
func (h *Handler) writeRepositoryContextResponse(
	w http.ResponseWriter,
	r *http.Request,
	repoID string,
	response map[string]any,
	evidenceDetail string,
) {
	timer := startRepositoryQueryStage(r.Context(), h.Logger, "repository_context", repoID, "evidence_detail")
	omissions := querycontract.ApplyRepoContextEvidenceDetail(response, evidenceDetail)
	// Log the request's own mode: full leaves the body untouched, so the
	// response no longer carries an evidence_detail key to read back.
	mode := evidenceDetail
	if mode == "" {
		mode = querycontract.ContextEvidenceDetailFull
	}
	timer.Done(
		r.Context(),
		slog.String("evidence_detail", mode),
		slog.Int("omission_count", len(omissions)),
	)
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.context_overview",
		querycontract.TruthBasisHybrid,
		"resolved from repository context and platform evidence",
	)
	truth.Omissions = append(truth.Omissions, omissions...)
	evidence.AttachEvidenceBoundaries(response, "get_repo_context")

	querycontract.WriteSuccess(w, r, http.StatusOK, response, truth)
}
