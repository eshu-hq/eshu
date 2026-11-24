// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
)

// findQueuedSupplyChainImpactHoldersQuery is the #10972 holder prefilter: one
// set read over a holder list that returns the holders the fan-out may skip,
// because the pass each one already has queued covers this change. A holder is
// returned only when every supply_chain_impact item at its ACTIVE generation
// in a state the reopen reads is pending or retrying AND that row's next pass
// covers the change (fanOutQueuedRowCoversSQL): it runs full (owed marker,
// claim attempt above 1, or an unbounded stamp), or its stamp set already
// names every id of the change. A queued holder stamped for another change is
// NOT returned: it reaches the reopen, which widens its stamp. A holder with a
// claimed, running, succeeded, or dead-letter item, with an item in an unread
// state only (failed, superseded), or with no item at the active generation is
// not returned either. The matched status set and the active-generation join
// are the reopen's own (reopenSupplyChainImpactMatchedStatuses).
//
// A scope's active generation can hold more than one reducer item for the
// domain; bool_and over the matched rows returns a holder only when every
// matched row is covered. The row condition is never NULL (bool_and ignores
// NULL rows, which would drop a holder whose other row is uncovered). The read
// takes no row lock and writes nothing. It never scans fact_work_items
// sequentially: the planner reads it through fact_work_items_scope_generation_idx
// (a per-scope probe, short lists) or fact_work_items_stage_domain_status_idx
// (one range read of the domain's matched-status rows with the list as a
// filter, long lists), so the queue read is bounded by the holder list or by
// those matched rows, not by the queue table. Two costs sit outside that
// bound: the join reads ingestion_scopes by a Seq Scan into its hash (15,408
// rows on QA), and the aggregate evaluates the cover rule over each matched
// row's payload, which is most of the 1.26 s the QA reading measured
// (HOLDER_FANOUT_PREFILTER_EVIDENCE.md). Arguments: $1 the holder scope ids, $2
// the change's stamp ids as a jsonb array, $3 whether they are bounded (see
// fanOutChangeArgs). The external test package reads it through
// supply_chain_impact_fanout_queued_export_test.go for the differential and
// plan proofs, so no test hand-copies it.
var findQueuedSupplyChainImpactHoldersQuery = `
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
HAVING bool_and(` + fanOutQueuedRowCoversSQL("$2::jsonb", "$3::boolean") + `)
ORDER BY work.scope_id
`

// FindQueuedHolderScopes implements supplychaincore.SupplyChainImpactFanOut: it
// returns, sorted, the holders whose active-generation items are all queued
// with a next pass that covers stampOut, the canonical stamp set the fan-out
// writes (#10972). An empty or sentinel stampOut is an unbounded change, so
// only holders whose next pass runs full are returned. It runs as one
// read-only transaction under the fan-out read timeout and fails with
// supplychaincore.ErrFanOutReadTimeout when the timeout fires.
func (s SupplyChainImpactFanOutStore) FindQueuedHolderScopes(ctx context.Context, holders, stampOut []string) ([]string, error) {
	if len(holders) == 0 {
		return nil, nil
	}
	cves, bounded, err := fanOutChangeArgs(stampOut)
	if err != nil {
		return nil, err
	}
	return s.readScopeIDs(ctx, findQueuedSupplyChainImpactHoldersQuery, holders, cves, bounded)
}
