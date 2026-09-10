// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/quality"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the HTTP handler of the code-quality family.
// Inspection requests, the bounded scan, and result shaping live in the
// quality leaf; the handler stays here because Go requires methods to
// live in their type's package.

// codeQualityCapability aliases the quality leaf's capability so the
// contract tests keep naming the pre-move spelling.
const codeQualityCapability = quality.Capability

func (h *CodeHandler) handleCodeQualityInspection(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), codeQualityCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code quality inspection requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			codeQualityCapability,
			h.profile(),
			querycontract.RequiredProfile(codeQualityCapability),
		)
		return
	}

	var req quality.Request
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Normalize()
	if !quality.SupportedCheck(req.Check) {
		WriteError(w, http.StatusBadRequest, "unsupported code quality check")
		return
	}
	if req.Offset > quality.MaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, codeQualityCapability) {
		return
	}

	rows, err := quality.Inspect(r.Context(), h.Neo4j, req, codeGrantAccessFilter(r.Context()))
	if err != nil {
		if WriteGraphReadError(w, r, err, codeQualityCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results, truncated := quality.TrimResults(quality.Rows(rows), req.Limit)
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"source":                 "graph",
		"source_backend":         "graph",
		"check":                  req.Check,
		"repo_id":                req.RepoID,
		"language":               req.Language,
		"limit":                  req.Limit,
		"offset":                 req.Offset,
		"truncated":              truncated,
		"result_key":             "entity_id",
		"thresholds":             quality.Thresholds(req),
		"results":                results,
		"recommended_next_calls": quality.NextCalls(results),
	}, BuildTruthEnvelope(h.profile(), codeQualityCapability, TruthBasisAuthoritativeGraph, "resolved from bounded graph code-quality metrics"))
}
