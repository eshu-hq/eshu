// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// supplyChainImpactSupportsRetryAfterSeconds is the Retry-After a supports-refused
// call carries: one backfill idle poll (30 s), the soonest the readiness marker or
// an emptied old-writer fence can change.
const supplyChainImpactSupportsRetryAfterSeconds = 30

// writeImpactFindingsSupportsUnavailable answers the 503 backend_unavailable
// envelope for a granted list the supports table cannot serve exactly (#10880
// PR-B2a): the backfill marker is incomplete or behind this binary's contract
// version, the old-writer fence is not empty, or the readiness probe failed. The
// text names the supports model so an operator can tell it from a lagging winners
// maintainer. There is no fallback read.
func (h *Handler) writeImpactFindingsSupportsUnavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", strconv.Itoa(supplyChainImpactSupportsRetryAfterSeconds))
	querycontract.WriteContractError(
		w,
		r,
		http.StatusServiceUnavailable,
		"supply-chain impact findings are unavailable: the supports read model is not ready; retry after the supports backfill completes",
		querycontract.ErrorCodeBackendUnavailable,
		ImpactFindingsCapability,
		h.profile(),
		querycontract.RequiredProfile(ImpactFindingsCapability),
	)
}
