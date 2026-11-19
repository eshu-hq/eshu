// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import "time"

// UnroutableReasonCodeCallShortfallWaitExceeded is the bounded unroutable
// reason for a code-call intent whose enforced CALLS shortfall park reached
// the 30-minute elapsed bound (#10947). The park releases only when the
// missing endpoint commits or a drain supersedes the generation; a deleted
// endpoint in a never-superseded generation would otherwise park one
// partition forever. Past the bound the runner dead-letters every active
// intent under this reason and completes them, so the loss is durable and
// queryable instead of a silent completion. It matches
// worker.StaleReasonCodeCallShortfallWaitExceeded so the stale-intents
// series and the unroutable-intents table share one vocabulary.
const UnroutableReasonCodeCallShortfallWaitExceeded = "code_call_shortfall_wait_exceeded"

// UnroutableRowForCodeCallShortfallWaitExceeded builds the durable
// dead-letter row for one shortfall-parked intent whose wait expired. The
// intent id joins back to shared_projection_intents and the
// scope/generation/domain triple is how an operator scopes the query; the
// reason stays the bounded UnroutableReasonCodeCallShortfallWaitExceeded so
// the counter label stays low-cardinality.
func UnroutableRowForCodeCallShortfallWaitExceeded(row Row, evidenceSource string, decidedAt time.Time) UnroutableRow {
	return UnroutableRow{
		IntentID:         row.IntentID,
		ProjectionDomain: row.ProjectionDomain,
		PartitionKey:     row.PartitionKey,
		RepositoryID:     RowRepoID(row),
		ScopeID:          row.ScopeID,
		GenerationID:     row.GenerationID,
		EvidenceSource:   evidenceSource,
		Reason:           UnroutableReasonCodeCallShortfallWaitExceeded,
		DecidedAt:        decidedAt.UTC(),
	}
}
