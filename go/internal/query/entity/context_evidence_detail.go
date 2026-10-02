// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// readContextEvidenceDetail reads the evidence_detail query parameter of a
// workload or service context request and validates it before any read runs.
// An unknown value writes a 400 invalid_argument envelope and reports false.
// An absent value is full, so HTTP callers keep today's shape (#7129).
func readContextEvidenceDetail(w http.ResponseWriter, r *http.Request) (string, bool) {
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
