// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// readRepositoryStoryEvidenceDetail reads the evidence_detail query parameter
// of the repository story route and validates it before any read runs. An
// unknown value writes a 400 invalid_argument envelope and reports false. An
// absent value is full, so HTTP callers keep today's shape and the MCP
// get_repo_story tool opts into handles at dispatch.
func readRepositoryStoryEvidenceDetail(w http.ResponseWriter, r *http.Request) (string, bool) {
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

// writeRepositoryStoryResponse shapes the built story response per
// evidenceDetail, appends the shaping omissions to a fresh truth envelope,
// and writes it. Shaping runs after enrichment so every count and the answer
// metadata were computed from the full rows, and before the evidence
// boundaries, which stay nil for this tool. The evidence_detail stage event
// carries the effective mode and the omission count so an operator can see
// the shaping happened.
func (h *Handler) writeRepositoryStoryResponse(
	w http.ResponseWriter,
	r *http.Request,
	repoID string,
	response map[string]any,
	evidenceDetail string,
) {
	timer := startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "evidence_detail")
	omissions := querycontract.ApplyRepoStoryEvidenceDetail(response, evidenceDetail)
	timer.Done(
		r.Context(),
		slog.String("evidence_detail", querycontract.StringVal(response, "evidence_detail")),
		slog.Int("omission_count", len(omissions)),
	)
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.context_overview",
		querycontract.TruthBasisHybrid,
		"resolved from bounded repository story, content coverage, and platform evidence",
	)
	truth.Omissions = append(truth.Omissions, omissions...)
	evidence.AttachEvidenceBoundaries(response, "get_repo_story")

	querycontract.WriteSuccess(w, r, http.StatusOK, response, truth)
}
