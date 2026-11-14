// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/statements"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the retention policy: bounds, defaults, the result shape,
// and the shared candidate-read helper. The search index pre-delete glue the
// policy bounds lives in generation_retention_search_predrain.go and the
// pre-delete loop itself in the storage/postgres/search/index/retention
// leaf, because this directory is over the dirgate cap, so a new file here
// needs a Dirgate-Growth trailer.

const (
	// defaultGenerationRetentionSearchIndexPredeleteRowCap bounds the rows one
	// pass pre-deletes (#7427). At the measured 20,000-row batch cost (23 ms,
	// 1.08 MB of WAL) it is about 100 batches, a few seconds and about 108 MB
	// of WAL per pass, and it covers the largest measured generation (1.8M
	// terms plus 180k documents) in one pass. A larger generation drains over
	// several passes; nothing is admitted to the prune until it drains.
	defaultGenerationRetentionSearchIndexPredeleteRowCap int64 = 2_000_000
	// defaultGenerationRetentionSearchIndexPredeleteDeadline is a quarter of
	// the reducer's default one-hour retention poll interval. The retention
	// runner passes a quarter of its configured interval; this default
	// covers direct store callers.
	defaultGenerationRetentionSearchIndexPredeleteDeadline = 15 * time.Minute
	// defaultGenerationRetentionChunkPredeleteRowCap bounds the fact and
	// content rows one pass drains from over-limit generations in committed
	// pre-delete batches before the prune transaction (#10020). Five default
	// row-limit pages make progress on the largest backlog shape without
	// letting one pass's pre-delete dominate the poll interval; a larger
	// generation drains over several passes and is never chunk-pruned under
	// the scope lock.
	defaultGenerationRetentionChunkPredeleteRowCap int64 = 500_000
	// defaultGenerationRetentionChunkPredeleteDeadline is a quarter of the
	// reducer's default one-hour retention poll interval, matching the
	// search index pre-delete's share. The retention runner passes a
	// quarter of its configured interval; this default covers direct
	// store callers.
	defaultGenerationRetentionChunkPredeleteDeadline = 15 * time.Minute
)

// GenerationRetentionPolicy bounds automated cleanup of superseded source-local
// generations. The active generation and the newest superseded generations
// inside the count or age window are never candidates, except past the hard
// history ceiling (#7585), which never frees active, live-work, or
// dependency-pinned generations. Generations the reconcile probe still needs
// (uncovered writers, #7472) are never candidates at any age. Neither is the
// newest activated full generation of a delta-active scope nor the delta
// chain after it (#7802): they are the only record of unchanged files, kept
// at any age.
type GenerationRetentionPolicy struct {
	MinSupersededGenerations int
	MaxSupersededAge         time.Duration
	// HardMaxSupersededAge: superseded history older than this (from the
	// original superseded_at) is eligible regardless of rank. Unset resolves
	// to DefaultGenerationRetentionHardMaxAge(MaxSupersededAge).
	// EvaluateGenerationRetention reports expired-but-blocked history with
	// its reason and age instead of calling it compliant.
	HardMaxSupersededAge time.Duration
	BatchGenerationLimit int
	BatchRowLimit        int
	PolicyScope          string
	PolicyRevision       string
	// SearchIndexPredeleteRowCap bounds the eshu_search_index_terms and
	// eshu_search_index_documents rows one pass deletes in its committed
	// pre-delete batches before the prune transaction (#7427). The first
	// batch of a pass always runs. Unset resolves to 2,000,000. The retention
	// runner passes the same cap to the orphan sweep (#8281 review).
	SearchIndexPredeleteRowCap int64
	// SearchIndexPredeleteDeadline bounds the pre-delete's wall time per pass;
	// no batch starts after it. Unset resolves to 15 minutes; the retention
	// runner sets a quarter of its poll interval.
	SearchIndexPredeleteDeadline time.Duration
	// ChunkPredeleteRowCap bounds the fact and content rows one pass drains
	// in committed pre-delete batches before the prune transaction
	// (#10020): over-limit chunk targets and normal-batch generations
	// alike, since both hold fact volume the prune must not cover. The
	// first batch of a pass always runs, so every pass makes progress on
	// its oldest undrained generation. Unset resolves to 500,000.
	ChunkPredeleteRowCap int64
	// ChunkPredeleteDeadline bounds the chunk pre-delete's wall time per
	// pass; no batch starts after it. Unset resolves to 15 minutes; the
	// retention runner sets a quarter of its poll interval.
	ChunkPredeleteDeadline time.Duration
}

// DefaultGenerationRetentionPolicy returns the ADR #2248 default retention
// window: keep the active generation plus the last 24 superseded generations or
// any superseded generation newer than seven days, whichever keeps more,
// bounded by the 90-day hard history ceiling (#7585).
func DefaultGenerationRetentionPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations:     defaultGenerationRetentionMinSuperseded,
		MaxSupersededAge:             defaultGenerationRetentionMaxAge,
		HardMaxSupersededAge:         defaultGenerationRetentionHardMaxAge,
		BatchGenerationLimit:         defaultGenerationRetentionBatchLimit,
		BatchRowLimit:                defaultGenerationRetentionRowLimit,
		PolicyScope:                  defaultGenerationRetentionPolicyScope,
		PolicyRevision:               defaultGenerationRetentionPolicyRev,
		SearchIndexPredeleteRowCap:   defaultGenerationRetentionSearchIndexPredeleteRowCap,
		SearchIndexPredeleteDeadline: defaultGenerationRetentionSearchIndexPredeleteDeadline,
		ChunkPredeleteRowCap:         defaultGenerationRetentionChunkPredeleteRowCap,
		ChunkPredeleteDeadline:       defaultGenerationRetentionChunkPredeleteDeadline,
	}
}

// DefaultGenerationRetentionHardMaxAge returns the hard history ceiling for a
// policy that sets none: 90 days, or maxSupersededAge when that is longer
// (#7611). An explicit ceiling below the soft window is still rejected.
func DefaultGenerationRetentionHardMaxAge(maxSupersededAge time.Duration) time.Duration {
	return max(defaultGenerationRetentionHardMaxAge, maxSupersededAge)
}

func (p GenerationRetentionPolicy) normalize() GenerationRetentionPolicy {
	defaults := DefaultGenerationRetentionPolicy()
	if p.MinSupersededGenerations < 0 {
		p.MinSupersededGenerations = defaults.MinSupersededGenerations
	}
	if p.MaxSupersededAge <= 0 {
		p.MaxSupersededAge = defaults.MaxSupersededAge
	}
	if p.HardMaxSupersededAge <= 0 {
		p.HardMaxSupersededAge = DefaultGenerationRetentionHardMaxAge(p.MaxSupersededAge)
	}
	if p.BatchGenerationLimit <= 0 {
		p.BatchGenerationLimit = defaults.BatchGenerationLimit
	}
	if p.BatchRowLimit <= 0 {
		p.BatchRowLimit = defaults.BatchRowLimit
	}
	if p.PolicyScope == "" {
		p.PolicyScope = defaults.PolicyScope
	}
	if p.PolicyRevision == "" {
		p.PolicyRevision = defaults.PolicyRevision
	}
	if p.SearchIndexPredeleteRowCap <= 0 {
		p.SearchIndexPredeleteRowCap = defaults.SearchIndexPredeleteRowCap
	}
	if p.SearchIndexPredeleteDeadline <= 0 {
		p.SearchIndexPredeleteDeadline = defaults.SearchIndexPredeleteDeadline
	}
	if p.ChunkPredeleteRowCap <= 0 {
		p.ChunkPredeleteRowCap = defaults.ChunkPredeleteRowCap
	}
	if p.ChunkPredeleteDeadline <= 0 {
		p.ChunkPredeleteDeadline = defaults.ChunkPredeleteDeadline
	}
	return p
}

// weakenedCandidateReadQuery derives a pre-lock candidate read from a shipped
// locking query: the given marker comment, then the query with each FOR
// UPDATE row lock replaced by FOR NO KEY UPDATE, so the read skips (never
// waits on) rows another retention pass or an Ack holds, and does not
// conflict with the FOR KEY SHARE a concurrent fact insert's foreign-key
// check takes. Its row locks end at the read's rollback, so no retention
// row lock spans a pre-delete batch.
func weakenedCandidateReadQuery(marker, selection string) string {
	return marker + "\n" + strings.ReplaceAll(selection, "FOR UPDATE OF ", "FOR NO KEY UPDATE OF ")
}

// GenerationRetentionResult reports the work a cleanup transaction completed.
// RowsPruned is keyed by bounded table/data-class names, never raw scope or
// generation identifiers. Each event's row_counts charges a content row shared
// by several pruned generations to the newest of them, so the events sum per
// table to the deletes (recounted after a row-limit skip).
//
// PhaseDurations is keyed by the closed GenerationRetentionPhase* set.
// ScopeLockHold is the time from the targeted re-lock of the provisionally
// selected set, which locks only the batch's ingestion_scopes and
// scope_generations rows, to the end of the commit that releases them: the
// window a concurrent fact insert into one of those scopes waits out. The
// candidate selection, pre-screen and row counts run before it, unlocked
// (#7334). It is zero when the pass locks nothing.
type GenerationRetentionResult struct {
	GenerationsPruned int
	RowsPruned        map[string]int64
	Skipped           map[string]int
	OldestEligibleAge time.Duration
	Duration          time.Duration
	PhaseDurations    map[string]time.Duration
	ScopeLockHold     time.Duration
	// LedgerRowsPruned and LedgerRowsCounted are the changed-since ledger's
	// deleted rows and the batch's pre-count of them, by ledger table
	// (#7127). They differ only when a link committed between the two.
	LedgerRowsPruned  map[string]int64
	LedgerRowsCounted map[string]int64
	// FactKeyRowsPruned and FactKeyRowsCounted are the whole-generation key
	// sidecar delete's rows and the batch's pre-count of them, by table
	// (#7420; #10512: same (scope_id, generation_id) envelope, orphans
	// included). They differ only when keys were written between them.
	FactKeyRowsPruned  map[string]int64
	FactKeyRowsCounted map[string]int64
	// RowsOverLimit is the batch's counted rows minus BatchRowLimit when the
	// batch is one generation over the limit (admitted alone because only its
	// changed-since ledger rows push it over), else 0. The recount runs after
	// the #10020 predrain deleted the generation's facts, so a generation
	// that was not over the limit on its own facts at the predrain read adds
	// that read's fact count back: recount-plus-carry equals the pre-drain
	// total. It comes from the pre-count, so a link that commits after the
	// count and names the generation on its prior side is deleted uncounted:
	// it can understate.
	RowsOverLimit int64
	// LockedScopeRows is the distinct scopes of the pruned batch. Since
	// every pass releases the selection's locks before planning and
	// re-locks only the selected set (#7334), this is usually the pass's
	// full lock set; when the under-lock refit skips re-locked members,
	// those stay held through the commit but uncounted here.
	LockedScopeRows    int
	SearchIndexBatches int // #7427 pre-delete batches committed this pass
	// ChunkPredeleteBatches is the #10020 chunk pre-delete batch
	// transactions the pass committed before its prune transaction opened.
	ChunkPredeleteBatches int
	// PredrainedOverLimitGenerations counts the drain members that exceeded
	// the row limit on own facts at the predrain read and saw at least one
	// committed batch this pass. It tells Gate 3 whether the pass moved
	// chunk-shaped volume pre-Begin (nonzero) or only normal-batch volume
	// (zero with ChunkPredeleteBatches > 0), since a fully drained
	// over-limit generation prunes whole with no other trace.
	PredrainedOverLimitGenerations int
}

// generationRetentionTestHooks are the ordering points the live retention
// proofs use to put another session at an exact place inside one prune
// pass. The fields are unexported and only same-package tests set them;
// production wiring never does, so every hook is nil there and each call
// site is a single nil check.
type generationRetentionTestHooks struct {
	// beforeTargetedLock runs between the selection's savepoint rollback and
	// the targeted re-lock of the provisionally selected set, where another
	// session can take a candidate.
	beforeTargetedLock func()
	// beforeChunkPredrainPage runs in a predrain batch after it locks its
	// generation and before it pages that generation's facts, so a test
	// can flip eligibility (or start a concurrent writer) mid-drain and
	// prove the next batch re-checks.
	beforeChunkPredrainPage func()
	// beforePruneTransaction runs after the search and chunk pre-deletes
	// and before the prune transaction opens, so a test can change a
	// drained generation's eligibility (delta base, uncovered writer) and
	// prove the prune's own selection drops it instead of pruning rows
	// the predrain already deleted.
	beforePruneTransaction func()
}

// generationRetentionTargetedCandidateQuery is the targeted candidate
// re-lock (#7334, #7903, #7802, #7981), kept in the retention/statements
// leaf because this directory takes no new file and its retention files sit
// at the 500-line cap. See statements.TargetedCandidateLock.
const generationRetentionTargetedCandidateQuery = statements.TargetedCandidateLock

// newGenerationRetentionResult returns an empty pass result with its maps
// allocated.
func newGenerationRetentionResult() GenerationRetentionResult {
	return GenerationRetentionResult{
		RowsPruned:     make(map[string]int64),
		Skipped:        make(map[string]int),
		PhaseDurations: make(map[string]time.Duration),
	}
}

// validate fails closed on a normalized policy that promises retention it
// cannot keep: an explicit hard ceiling below the soft window (#7585).
func (p GenerationRetentionPolicy) validate() error {
	if p.HardMaxSupersededAge < p.MaxSupersededAge {
		return fmt.Errorf(
			"generation retention: hard max superseded age (%v) must not be below max superseded age (%v)",
			p.HardMaxSupersededAge, p.MaxSupersededAge)
	}
	return nil
}

// Retention phases and skip reasons of the #7427 search index pre-delete. The
// phases time the committed batches that run before the prune transaction
// opens; they sit beside the closed GenerationRetentionPhase* set above.
const (
	// GenerationRetentionPhasePredeleteSearchIndex is the forward keyset walk
	// of the pre-delete batches.
	GenerationRetentionPhasePredeleteSearchIndex = "predelete_search_index"
	// GenerationRetentionPhaseSweepSearchIndex is the from-start re-sweep
	// that confirms a table is empty and walks its dead index entries.
	GenerationRetentionPhaseSweepSearchIndex = "sweep_search_index"

	// GenerationRetentionPhasePredrainChunk is the chunk pre-delete's
	// candidate read plus its committed batches, which run before the prune
	// transaction opens (#10020). The read stays out of count_rows so Gate 3
	// keeps measuring exactly the prune's own unlocked planning; when no
	// batch commits the phase is absent (one bounded read, observable via
	// its pg_stat_statements marker). It sits beside the closed
	// GenerationRetentionPhase* set with the search index pre-delete phases
	// above.
	GenerationRetentionPhasePredrainChunk = "predrain_chunk"

	searchIndexUndrainedSkippedReason = "search_index_undrained"
	searchIndexLockBusySkippedReason  = "advisory_lock_busy"
)

// generationRetentionStoreOptions holds the store's optional collaborators.
type generationRetentionStoreOptions struct {
	logger *slog.Logger
	// instruments records the expired-generations live-signal report
	// (#7585 PR3): the expired-blocked counter, the report duration, and
	// report failures on the shared retention failure counter. Nil
	// disables report metrics; every record site nil-guards it the way
	// the retention runner's recordResult does.
	instruments *telemetry.Instruments
	// tracer starts the expired-generations report span (#7585 PR3). Nil
	// disables the span; the read itself is unaffected.
	tracer trace.Tracer
}

// WithLogger returns the store with a logger for the per-generation search
// index pre-delete completion lines (#7427); nil discards them.
func (s GenerationRetentionStore) WithLogger(logger *slog.Logger) GenerationRetentionStore {
	s.logger = logger
	return s
}

// The search index pre-delete glue lives in
// generation_retention_search_predrain.go; only the policy bounds and the
// store options stay here.
