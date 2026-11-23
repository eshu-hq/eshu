// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
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

// writeImpactAggregateReadModelUnavailable answers the 503
// backend_unavailable envelope for a winners-eligible count or inventory
// call the maintainer cannot serve exactly (#11169): the winners watermark
// is absent, older than the freshness window, or its probe failed. The text
// names the stale model (not the missing deployment the shared writer
// describes) so an operator can tell a lagging maintainer from an unwired
// route. It carries Retry-After for one freshness window (120 s): the caller
// retries after the maintainer resweeps, which the watermark age on the next
// fresh answer discloses.
func (h *Handler) writeImpactAggregateReadModelUnavailable(w http.ResponseWriter, r *http.Request, cause error) {
	retryAfter := int(supplyChainImpactWinnersFreshnessWindow / time.Second)
	message := "supply-chain impact aggregates are unavailable: the winners read model is not fresh; retry after the maintainer resweeps"
	if errors.Is(cause, impact.ErrFindingSupportsUnavailable) {
		// A granted call is answered from the supports table (#10880 PR-B2a):
		// name that model and retry on the backfill poll, not the winners window.
		retryAfter = supplyChainImpactSupportsRetryAfterSeconds
		message = "supply-chain impact aggregates are unavailable: the supports read model is not ready; retry after the supports backfill completes"
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	querycontract.WriteContractError(
		w,
		r,
		http.StatusServiceUnavailable,
		message,
		querycontract.ErrorCodeBackendUnavailable,
		ImpactAggregateCapability,
		h.profile(),
		querycontract.RequiredProfile(ImpactAggregateCapability),
	)
}
