// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// MetricDimensionBlock labels eshu_dp_deferred_backfill_block_duration_seconds
// with the block of the deferred relationship maintenance pass that ran (#8082).
// The value set is closed: the nine DeferredBackfillBlock* constants below.
// It never carries a scope, repository, or reopen-domain identifier; the
// per-domain split of the reopen block lives only on its span and log line.
const MetricDimensionBlock = "block"

// The closed set of structured log line names a block can emit (#8082). A
// block emits exactly one of them: completed on success, failed when the pass
// failed because of it, degraded when the block failed but the pass fell back
// and continued (today only the memo gate).
const (
	// DeferredBackfillBlockLogCompleted is the line a block emits on success.
	DeferredBackfillBlockLogCompleted = "deferred_backfill_block_completed"
	// DeferredBackfillBlockLogFailed is the line a block emits when it fails the pass.
	DeferredBackfillBlockLogFailed = "deferred_backfill_block_failed"
	// DeferredBackfillBlockLogDegraded is the line a block emits when it errored
	// but the pass continued on a fallback path.
	DeferredBackfillBlockLogDegraded = "deferred_backfill_block_degraded"
)

// The closed set of deferred maintenance pass blocks. Each value is the
// MetricDimensionBlock label value, the suffix of the block's
// relationship.backfill.<block> span name, and the block= field of the block's
// log line. Add a value only with a matching row in
// docs/public/reference/telemetry/deferred-backfill-blocks.md.
const (
	// DeferredBackfillBlockCatalogLoad is the repository catalog read, row
	// iteration, and JSON decode.
	DeferredBackfillBlockCatalogLoad = "catalog_load"
	// DeferredBackfillBlockParamsBuild is the anchor-scoped query parameter build.
	DeferredBackfillBlockParamsBuild = "params_build"
	// DeferredBackfillBlockPartitionList is the active scope-generation
	// partition listing.
	DeferredBackfillBlockPartitionList = "partition_list"
	// DeferredBackfillBlockMemoGate is the partition memo lookup and skip decision.
	DeferredBackfillBlockMemoGate = "memo_gate"
	// DeferredBackfillBlockFactLoad is the per-partition fact load, merge, and
	// ArgoCD config reload.
	DeferredBackfillBlockFactLoad = "fact_load"
	// DeferredBackfillBlockDiscover is evidence discovery, dedupe, and
	// per-source-repository grouping.
	DeferredBackfillBlockDiscover = "discover"
	// DeferredBackfillBlockWriteBatches is the per-repository evidence write
	// batches.
	DeferredBackfillBlockWriteBatches = "write_batches"
	// DeferredBackfillBlockFanIn is the per-partition readiness and memo
	// publication.
	DeferredBackfillBlockFanIn = "fanin"
	// DeferredBackfillBlockReopen is one reopen domain call after the backfill
	// commits. The histogram label stays this single value for every domain.
	DeferredBackfillBlockReopen = "reopen"
)
