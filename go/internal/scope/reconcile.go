// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import "time"

// FullReconcileState summarizes one scope's full (non-delta) generations for
// the periodic reconciliation sweep that re-observes a scope to retract drift
// the delta path could not (#7288).
//
// The sweep needs two separate facts. The reconcile obligation is measured on
// the last full generation that actually reached the graph (activated), so a
// full generation that was superseded while still pending does not satisfy it.
// The throttle is measured on the latest full attempt of any status, so a full
// generation still in flight, or one that recently failed, holds the sweep off
// instead of stacking another full snapshot of the same commit.
type FullReconcileState struct {
	// HasProjectedFull reports whether any full generation was activated.
	HasProjectedFull bool
	// LastProjectedFullAt is the ingest time of the newest activated full
	// generation, or the time its last #7978 re-projection was acknowledged
	// when that is later. It is zero when HasProjectedFull is false.
	LastProjectedFullAt time.Time
	// LastStoredFullAt is the ingest time of the newest activated full
	// generation itself, without any re-projection Ack. A reindex watermark is
	// satisfied only by it (ruling a9-125, review r2 N1): a re-projection does
	// not re-parse, so it must never answer a reindex request.
	LastStoredFullAt time.Time
	// HasLatestFull reports whether the scope has any full generation.
	HasLatestFull bool
	// LatestFullAt is the ingest time of the newest full generation of any
	// status. It is zero when HasLatestFull is false.
	LatestFullAt time.Time
	// LatestFullStatus is the lifecycle status of that newest full generation.
	LatestFullStatus GenerationStatus
	// LatestFullProjected reports whether that newest full generation was
	// activated. A superseded generation with no activation never projected.
	LatestFullProjected bool
	// LatestFullSourceCommitSHA is that newest full generation's source
	// commit (ruling a9-126): a pending full at the remote head holds the
	// sweep while its work is live.
	LatestFullSourceCommitSHA string
	// LatestFullWorkLive reports whether that newest full generation has a
	// projector work row a worker can still run: pending, retrying, claimed,
	// or running. A missing or terminal row is not live, so the sweep forces
	// the scope instead of holding it forever (ruling a9-126).
	LatestFullWorkLive bool
	// ReconcileRequestedAt is when an unchanged reconcile last re-opened the
	// newest activated full generation's projector work instead of storing a
	// duplicate (#7978). It is zero when there is none or when that generation
	// is no longer active.
	ReconcileRequestedAt time.Time
	// ReconcileProjectedAt is when that re-projection was last acknowledged.
	// LastProjectedFullAt already folds it in. Zero when none.
	ReconcileProjectedAt time.Time
	// ReconcileDeadLetteredAt is when that re-projection was last
	// dead-lettered. The dead-letter path leaves an activated generation
	// active, so the sweep reads this to force a full that the store stores.
	// Zero when none.
	ReconcileDeadLetteredAt time.Time
	// ActiveGenerationID is the scope's active generation
	// (ingestion_scopes.active_generation_id); empty when there is none. The
	// skipped-parse path re-projects exactly this generation (ruling a9-143).
	ActiveGenerationID string
	// ActiveIsDelta reports that the active generation is a delta; a full
	// snapshot never equals it, so the parse is not skipped.
	ActiveIsDelta bool
	// ActiveCommitSHA is the active generation's source commit.
	ActiveCommitSHA string
	// ActivePreparseIdentity is the pre-parse identity stamped on the active
	// generation; empty until a parse stamps one.
	ActivePreparseIdentity string
	// ActivePreparseSkips counts the skipped-parse re-projections of the active
	// generation since its content was last parsed (the valve, ruling a9-143
	// D3).
	ActivePreparseSkips int
	// ActiveStreamDroppedPaths are the repository-relative paths the active
	// generation's fact stream dropped because it could not read them (#8185).
	// Empty when the active generation dropped nothing. The skip probe
	// re-checks each path before skipping an equal snapshot: a path that
	// opens now means the next parse would emit facts this generation lacks,
	// so the scope parses instead of skipping.
	ActiveStreamDroppedPaths []string
	// ActiveStreamDroppedTruncated reports that the active generation dropped
	// more files than ActiveStreamDroppedPaths records (#8185). Unrecorded
	// drops cannot be re-checked, so the skip probe refuses and the scope
	// parses instead of skipping.
	ActiveStreamDroppedTruncated bool
}

// ActiveReprojection asks the store to re-project the scope's active
// generation without a snapshot (ruling a9-143): the collector found its
// pre-parse identity equal at the same commit. The store fences on all three
// of GenerationID, CommitSHA, and PreparseIdentity.
type ActiveReprojection struct {
	// ScopeID is the scope.
	ScopeID string
	// GenerationID is the active generation the collector read.
	GenerationID string
	// CommitSHA is the fetched remote head, equal to the active commit.
	CommitSHA string
	// PreparseIdentity is the identity the collector computed.
	PreparseIdentity string
	// Reason is the bounded reconcile reason (interval_elapsed or
	// graph_dirty), stamped as reconcile_reason.
	Reason string
}

// ReconcileReasonRequestStale is the reconcile reason for a scope whose
// re-projection request (#7978) went unacknowledged for one interval or was
// dead-lettered. The ingestion store always stores a generation that carries
// it, even when its hint and commit equal the active generation's: today's
// heal, bounded to one per interval per scope (ruling a9-117 Q3).
const ReconcileReasonRequestStale = "reconcile_request_stale"

// Explicit reindex reasons (#7620). They come only from an operator request:
// the fleet watermark (POST /api/v0/admin/reindex) or a repository-scoped
// request. The ingestion store always stores a generation that carries one,
// even when its identity equals the active generation's (ruling a9-125): an
// explicit reindex is the remedy for a parser change the content-version gate
// did not catch.
const (
	// ReconcileReasonReindexRequested is the fleet reindex watermark reason.
	ReconcileReasonReindexRequested = "reindex_requested"
	// ReconcileReasonRepositoryReindexRequested is the repository-scoped
	// reindex reason.
	ReconcileReasonRepositoryReindexRequested = "repository_reindex_requested"
)

// UncoveredProjectionWriter is a generation that started writing the canonical
// graph and never activated, and that no later activated full generation has
// covered (#7389). Its overlay may still be in the graph, so the git collector
// forces the scope's next sync to a full snapshot instead of a delta.
type UncoveredProjectionWriter struct {
	// GenerationID identifies the generation.
	GenerationID string
	// Status is superseded or failed.
	Status GenerationStatus
	// FailureClass is the projector work row's failure_class, or empty.
	FailureClass string
	// ProjectionWriteStartedAt is its latest projection write start.
	ProjectionWriteStartedAt time.Time
}
