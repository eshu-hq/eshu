// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
)

// findQueuedSupplyChainImpactHoldersQuery is the #10972 holder prefilter: one
// set read over a holder list that returns the holders whose
// supply_chain_impact item at the scope's ACTIVE generation is pending or
// retrying and that have no item in any other state the reopen reads. Those
// are the holders reopenSupplyChainImpactHolderQuery classifies pending_noop
// and leaves untouched, so dropping them before the per-holder loop changes no
// row. A holder with a claimed, running, succeeded, or dead-letter item, with
// an item in an unread state only (failed, superseded), or with no item at the
// active generation is not returned and still goes through the reopen. The
// matched status set and the active-generation join are the reopen's own
// (reopenSupplyChainImpactMatchedStatuses), so the two cannot drift.
//
// A scope's active generation can hold more than one reducer item for the
// domain; bool_and over the matched rows returns a holder only when every
// matched row is queued, the same strongest-outcome rule the reopen applies. The
// read takes no row lock and writes nothing. It is bounded by the holder list
// (at most the stored continuation plus one read's holders) and reads
// fact_work_items_scope_generation_idx per scope. Argument: $1 the holder scope
// ids. The external test package reads it through
// supply_chain_impact_fanout_queued_export_test.go for the differential and
// plan proofs, so no test hand-copies it.
const findQueuedSupplyChainImpactHoldersQuery = `
SELECT work.scope_id
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
 AND scope.active_generation_id = work.generation_id
WHERE work.scope_id = ANY($1::text[])
  AND work.stage = 'reducer'
  AND work.domain = 'supply_chain_impact'
  AND work.status IN ` + reopenSupplyChainImpactMatchedStatuses + `
GROUP BY work.scope_id
HAVING bool_and(work.status IN ('pending', 'retrying'))
ORDER BY work.scope_id
`

// FindQueuedHolderScopes implements supplychaincore.SupplyChainImpactFanOut: it
// returns, sorted, the holders whose active-generation item is already pending
// or retrying, so the fan-out can drop them before its cap slice (#10972). It
// runs as one read-only transaction under the fan-out read timeout and fails
// with supplychaincore.ErrFanOutReadTimeout when the timeout fires.
func (s SupplyChainImpactFanOutStore) FindQueuedHolderScopes(ctx context.Context, holders []string) ([]string, error) {
	if len(holders) == 0 {
		return nil, nil
	}
	return s.readScopeIDs(ctx, findQueuedSupplyChainImpactHoldersQuery, holders)
}
