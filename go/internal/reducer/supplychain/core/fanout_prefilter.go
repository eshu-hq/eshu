// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fanOutOutcomePrefilteredPending is the counter outcome for a holder the
// queued-holder prefilter dropped: its active-generation item was already
// pending or retrying, so the per-holder reopen would classify it
// pending_noop and write nothing (#10972).
const fanOutOutcomePrefilteredPending = "prefiltered_pending"

// dropQueuedHolders removes from the sorted holder list every holder whose
// active-generation item is already pending or retrying, with one set read,
// and reports how many it dropped. The kept holders stay in sorted order, so
// the cap slice and the continuation list that follow count only holders
// that still need a reopen statement. Claimed, running, succeeded,
// dead-letter, and item-less holders are kept and reach ReopenHolderDetailed
// unchanged.
//
// Dropping a queued holder is equivalent to the reopen's own pending_noop,
// taken at one snapshot, and it inherits that statement's stale-stamp limit: a
// pending attempt-0 item that carries a bounded fan-out stamp runs a one-hop
// pass scoped to the stamped CVE set only, and the reopen never widens a
// pending row's stamp (supply_chain_impact_fanout_reopen.go), so this read
// does not either. It does not claim that the pending holder's next claim
// loads every CVE's evidence. A holder claimed after the read is not dropped
// for being claimed: it was not queued at the read, so it stays in the list
// and the reopen statement flags it. No lock, lease, claim query, or ordering
// changes. A crash before
// UpdateFanOutState leaves the stored list as it was, and the retry reads and
// filters the same list again, which is idempotent.
//
// A failed read fails the pass for a queue retry; there is no unfiltered
// fallback, because that would be a silent behavior change under load.
func (h SupplyChainImpactHandler) dropQueuedHolders(
	ctx context.Context,
	cause string,
	ordered []string,
) ([]string, int, error) {
	if len(ordered) == 0 {
		return ordered, 0, nil
	}
	queued, err := h.FanOut.FindQueuedHolderScopes(ctx, ordered)
	if err != nil {
		h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolderPrefilter)
		return nil, 0, fanOutError{fmt.Errorf("find queued supply chain impact holders: %w", err)}
	}
	if len(queued) == 0 {
		return ordered, 0, nil
	}
	drop := make(map[string]struct{}, len(queued))
	for _, id := range queued {
		drop[id] = struct{}{}
	}
	kept := make([]string, 0, len(ordered))
	for _, id := range ordered {
		if _, queuedAlready := drop[id]; !queuedAlready {
			kept = append(kept, id)
		}
	}
	return kept, len(ordered) - len(kept), nil
}
