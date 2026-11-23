// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fanOutOutcomePrefilteredPending is the counter outcome for a holder the
// queued-holder prefilter dropped: every active-generation item it has is
// pending or retrying, and each one's next pass already covers this change
// (#10972), so a reopen statement would add nothing.
const fanOutOutcomePrefilteredPending = "prefiltered_pending"

// dropQueuedHolders removes from the sorted holder list every holder whose
// queued pass already covers stampOut, with one set read, and reports how many
// it dropped. A pass covers the change when it runs full (owed marker, claim
// attempt above 1, or a stamp with no bounded set) or when its stamp set names
// every id of stampOut. The kept holders stay in sorted order, so the cap
// slice and the continuation list that follow count only holders that still
// need a reopen statement. Claimed, running, succeeded, dead-letter, and
// item-less holders are kept, and so is a queued holder stamped for another
// change: the reopen widens its stamp (stamp_widened).
//
// A holder that was not queued, or not covered, at the read reaches the
// reopen. A covered holder claimed after the read is dropped, and its pass
// covers the change: the read ran after this pass's writer commit, so the
// claim loads the committed source facts, and its stamp only grows, so the
// pass it runs is full or contains stampOut. No lock, lease, claim query, or
// ordering changes. A crash before UpdateFanOutState leaves the stored list
// as it was, and the retry reads and filters the same list again, which is
// idempotent.
//
// A failed read fails the pass for a queue retry; there is no unfiltered
// fallback, because that would be a silent behavior change under load. The
// read's duration is recorded with kind holder_prefilter.
func (h SupplyChainImpactHandler) dropQueuedHolders(
	ctx context.Context,
	cause string,
	ordered []string,
	stampOut []string,
) ([]string, int, error) {
	if len(ordered) == 0 {
		return ordered, 0, nil
	}
	started := time.Now()
	queued, err := h.FanOut.FindQueuedHolderScopes(ctx, ordered, stampOut)
	h.observeFanOutRead(ctx, telemetry.FanOutReadKindHolderPrefilter, time.Since(started), err)
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
