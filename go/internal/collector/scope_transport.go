// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ResolvedScopeTransport is one claimed work item's requested_scope_set
// entry: the per-scope query list and/or CVE id list for scope ids that
// name a shard rather than its contents (pinned virtual-slot scopes,
// eshu-hq/eshu#9432; findings-derived EPSS chunks, eshu-hq/eshu#9876). The
// zero value carries no transport, so legacy scopes resolve without touching
// the run store. Exactly one of Queries or CVEIDs is set: slot scopes carry
// queries, findings scopes carry CVE ids, never both.
type ResolvedScopeTransport struct {
	Queries []sdkcollector.ScopeQuery
	CVEIDs  []string
}

// ScopeQueryResolver delivers the run's requested_scope_set entry for one
// claimed work item: the per-scope query list or CVE id list for scope ids
// that name a shard rather than its contents. It reads the workflow run row
// by item RunID, so it observes the tenant-filtered set the coordinator
// persisted at plan time. A nil resolver means no transport: sharded claims
// keep the fail-closed behavior they have without it. A non-nil resolver
// returns the zero transport for every legacy scope id without touching the
// run store. A resolver error wrapping ErrSlotScopeTransport fails the claim
// terminal; any other resolver error fails it retryable.
type ScopeQueryResolver func(context.Context, workflow.WorkItem) (ResolvedScopeTransport, error)
