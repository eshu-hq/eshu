// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the retention policy and, since #7427, the search index
// pre-delete hooks the policy bounds. The pre-delete loop itself lives in the
// storage/postgres/search/index/retention leaf; only the glue that needs
// root-private retention state stays here, because this directory is at its
// dirgate pin (scripts/lib/dirgate-grandfather.tsv) and takes no new file.

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
	HardMaxSupersededAge time.Duration
	BatchGenerationLimit int
	BatchRowLimit        int
	PolicyScope          string
	PolicyRevision       string
	// SearchIndexPredeleteRowCap bounds the eshu_search_index_terms and
	// eshu_search_index_documents rows one pass deletes in its committed
	// pre-delete batches before the prune transaction (#7427). The first
	// batch of a pass always runs. Unset resolves to 2,000,000.
	SearchIndexPredeleteRowCap int64
	// SearchIndexPredeleteDeadline bounds the pre-delete's wall time per pass;
	// no batch starts after it. Unset resolves to 15 minutes; the retention
	// runner sets a quarter of its poll interval.
	SearchIndexPredeleteDeadline time.Duration
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
	return p
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
	// RowsOverLimit is the batch's counted rows minus BatchRowLimit when the
	// batch is one generation over the limit (admitted alone because only its
	// changed-since ledger rows push it over), else 0. It comes from the
	// pre-count, so a link that commits after the count and names the
	// generation on its prior side is deleted uncounted: it can understate.
	RowsOverLimit int64
	// LockedScopeRows is the distinct scopes of the pruned batch. Since
	// every pass releases the selection's locks before planning and
	// re-locks only the selected set (#7334), this is usually the pass's
	// full lock set; when the under-lock refit skips re-locked members,
	// those stay held through the commit but uncounted here.
	LockedScopeRows    int
	SearchIndexBatches int // #7427 pre-delete batch transactions this pass
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
	// afterChunkTargetLocked runs once the chunked re-lock holds the
	// over-limit chunk target's scope and generation rows and before its
	// page delete, so a test can start writers that must wait on those row
	// locks (#8206).
	afterChunkTargetLocked func()
}

// generationRetentionTargetedCandidateQuery re-locks the provisionally
// selected set after the selection's savepoint rollback (#7334 fix 1): the
// candidate query's predicates for the given (scope, generation) pairs, in
// deterministic (scope_id, generation_id) order so two overlapping passes
// lock rows in the same sequence, FOR UPDATE OF generation, scope SKIP
// LOCKED, so the delete holds only the rows it is about to prune. Rank >
// $2 is written as "at least $2 newer superseded generations", which is
// what ROW_NUMBER over (superseded_at, generation_id) DESC means. It is its
// own statement so the general candidate query's plan is untouched. A
// missing row means another session took, re-activated or started work on
// that candidate after the rollback; the pass silently drops it and prunes
// the locked subset. It lives beside the policy it re-checks because the
// package's file-count ledger forbids a new file and the other retention
// files sit at the 500-line cap.
//
// The re-lock also re-checks uncovered-writer coverage at lock time (#7903):
// coverage is horizon-relative, so a full that activated after the
// selection can flip a covered candidate to uncovered before the re-lock,
// and pruning it would delete the reconcile probe's evidence before the
// heal. provisional carries the #7472 uncovered definition over the
// scope-bounded horizon, the locked leg excludes flipped members without
// locking them, and a lock-free UNION ALL leg reports them so the store
// counts them as skipped instead of vanishing them. The single statement
// classifies and locks under one snapshot, so a member is either locked
// (covered) or reported (uncovered), never both. Residual: a full that
// commits after this statement's snapshot still flips a locked member
// before the prune commits; closing that would serialize activation
// against retention, which the repo forbids.
//
// The re-lock re-checks delta-base protection the same way (#7802): a
// delta that activated after the selection can make a selected candidate
// the protected base (or chain) of a newly delta-active scope before the
// re-lock. delta_base is the newest activated full per delta-active scope,
// as in the candidate query; a newer full that is pending or failed never
// displaces it. Every table in delta_base (generation, scope and active) is
// bounded by the batch's scope ids ($4), so the planner reads the batch's
// scopes through the primary keys and scope_generations_scope_generation_idx
// and its cost follows the batch, not the delta-generation count.
// provisional joins that definition, the locked leg excludes flipped
// members without locking them, and a second lock-free UNION ALL leg
// reports them so the store counts them as skipped delta_base rows. A member that flipped both
// ways reports only on the uncovered leg, matching the candidate query.
// Same residual as above.
//
// The lock re-checks the #7981 same-SHA exemption in the same terms as the
// general candidate query (non-empty SHA, same scope, same is_delta, same
// COALESCE freshness hint, soft floor, active-or-newer witness, uncovered
// still excluded), so the two stay in lockstep: a member eligible at
// selection is still eligible here unless its row changed under it.
//
// Output columns match the general candidate query: scope id, generation
// id, scope kind, superseded at, observed at, uncovered, source commit SHA,
// is_delta, freshness hint, delta_protected.
//
// $1 soft cutoff, $2 minimum newer generations, $3 excluded ids, $4 scope
// ids, $5 generation ids, $6 hard-ceiling cutoff (#7585).
const generationRetentionTargetedCandidateQuery = `-- retention: targeted candidate lock
WITH last_activated_full_write AS MATERIALIZED (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        generation.projection_write_started_at AS full_write_started_at
    FROM scope_generations AS generation
    WHERE generation.scope_id = ANY($4::text[])
      AND generation.is_delta = false
      AND generation.activated_at IS NOT NULL
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
),
delta_base AS (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        generation.generation_id AS base_generation_id,
        generation.ingested_at AS base_ingested_at
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
    JOIN scope_generations AS active ON active.generation_id = scope.active_generation_id
    WHERE generation.scope_id = ANY($4::text[])
      AND scope.scope_id = ANY($4::text[])
      AND active.scope_id = ANY($4::text[])
      AND active.is_delta = true
      AND generation.is_delta = false
      AND generation.activated_at IS NOT NULL
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
),
provisional AS (
    SELECT generation.scope_id, generation.generation_id, scope.scope_kind,
        generation.superseded_at, generation.observed_at,
        COALESCE(generation.source_commit_sha, '') AS source_commit_sha,
        generation.is_delta,
        COALESCE(generation.freshness_hint, '') AS freshness_hint,
        (generation.projection_write_started_at IS NOT NULL
         AND generation.activated_at IS NULL
         AND generation.projection_write_started_at > COALESCE(last_full.full_write_started_at, '-infinity'::timestamptz)) AS uncovered,
        (base.base_generation_id IS NOT NULL
         AND (generation.generation_id = base.base_generation_id
              OR (generation.ingested_at, generation.generation_id) > (base.base_ingested_at, base.base_generation_id))) AS delta_protected
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
    LEFT JOIN last_activated_full_write AS last_full ON last_full.scope_id = generation.scope_id
    LEFT JOIN delta_base AS base ON base.scope_id = generation.scope_id
    WHERE generation.scope_id = ANY($4::text[])
      AND generation.generation_id = ANY($5::text[])
      AND generation.generation_id <> ALL($3::text[])
),
locked_members AS (
    SELECT provisional.scope_id, provisional.generation_id, provisional.scope_kind,
        provisional.superseded_at, provisional.observed_at,
        provisional.source_commit_sha, provisional.is_delta, provisional.freshness_hint
    FROM provisional
    JOIN scope_generations AS generation ON generation.generation_id = provisional.generation_id
    JOIN ingestion_scopes AS scope ON scope.scope_id = provisional.scope_id
    WHERE NOT provisional.uncovered
      AND NOT provisional.delta_protected
      AND generation.status = 'superseded'
      AND generation.superseded_at IS NOT NULL
      AND generation.generation_id IS DISTINCT FROM scope.active_generation_id
      AND ((generation.superseded_at < $1 AND ((
          SELECT count(*)
          FROM scope_generations AS newer
          WHERE newer.scope_id = generation.scope_id
            AND newer.status = 'superseded'
            AND newer.generation_id <> ALL($3::text[])
            AND newer.superseded_at IS NOT NULL
            AND (newer.superseded_at, newer.generation_id) > (generation.superseded_at, generation.generation_id)
      ) >= $2 OR (
             COALESCE(generation.source_commit_sha, '') <> ''
             AND EXISTS (
                 SELECT 1 FROM scope_generations AS newer
                 WHERE newer.scope_id = generation.scope_id
                   AND newer.generation_id <> generation.generation_id
                   AND newer.source_commit_sha = generation.source_commit_sha
                   AND newer.is_delta = generation.is_delta
                   AND COALESCE(newer.freshness_hint, '') = COALESCE(generation.freshness_hint, '')
                   AND (newer.generation_id = scope.active_generation_id
                        OR (newer.status = 'superseded' AND newer.superseded_at IS NOT NULL
                            AND (newer.superseded_at, newer.generation_id) > (generation.superseded_at, generation.generation_id)))
             )))) OR generation.superseded_at < $6)
      AND NOT EXISTS (
          SELECT 1
          FROM fact_work_items AS work
          WHERE work.generation_id = generation.generation_id
            AND work.status IN ('claimed', 'running', 'retrying')
      )
    ORDER BY generation.scope_id, generation.generation_id
    FOR UPDATE OF generation, scope SKIP LOCKED
)
SELECT locked.scope_id, locked.generation_id, locked.scope_kind, locked.superseded_at, locked.observed_at, false AS uncovered,
    locked.source_commit_sha, locked.is_delta, locked.freshness_hint, false AS delta_protected
FROM locked_members AS locked
UNION ALL
SELECT provisional.scope_id, provisional.generation_id, provisional.scope_kind, provisional.superseded_at, provisional.observed_at, true AS uncovered,
    provisional.source_commit_sha, provisional.is_delta, provisional.freshness_hint, false AS delta_protected
FROM provisional
WHERE provisional.uncovered
UNION ALL
SELECT provisional.scope_id, provisional.generation_id, provisional.scope_kind, provisional.superseded_at, provisional.observed_at, false AS uncovered,
    provisional.source_commit_sha, provisional.is_delta, provisional.freshness_hint, true AS delta_protected
FROM provisional
WHERE provisional.delta_protected
  AND NOT provisional.uncovered
`

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

	searchIndexUndrainedSkippedReason = "search_index_undrained"
	searchIndexLockBusySkippedReason  = "advisory_lock_busy"
)

// generationRetentionSearchIndexReadMarker tags the pre-delete's candidate
// read and its work_mem setting, so pg_stat_statements and the store's test
// doubles tell it apart from the prune transaction's own selection.
const generationRetentionSearchIndexReadMarker = "-- retention: search index pre-delete candidate read"

// generationRetentionSearchIndexReadQuery is the shipped selection with its
// two FOR UPDATE ... SKIP LOCKED clauses weakened to FOR NO KEY UPDATE ...
// SKIP LOCKED, run in its own short transaction that rolls back before the
// first pre-delete batch (#7427). Like the selection it skips, never waits
// for, a scope or generation another retention pass or an Ack holds FOR
// UPDATE. Unlike the selection it does not conflict with the FOR KEY SHARE a
// concurrent fact insert's foreign-key check takes, so it never blocks
// ingestion. That also means it is not guaranteed to pick the set the
// prune's selection picks: while inserts are in flight the read can spend
// BatchGenerationLimit slots on scopes the prune's FOR UPDATE then skips,
// and the prune's replacement scopes are then undrained and skipped as
// search_index_undrained until a later pass (a throughput cost; a generation
// is never pruned undrained). Its row locks end at the rollback, so no
// retention row lock spans a batch. A test pins the derivation.
var generationRetentionSearchIndexReadQuery = searchIndexCandidateReadQuery(generationRetentionCandidateQuery)

// generationRetentionSearchIndexReadWorkMemStatement is the selection's
// work_mem setting, tagged so it is told apart from the prune transaction's.
const generationRetentionSearchIndexReadWorkMemStatement = generationRetentionWorkMemStatement + " " + generationRetentionSearchIndexReadMarker

// searchIndexCandidateReadQuery derives the pre-delete's candidate read from
// the shipped selection: the marker comment, then the query with each
// FOR UPDATE row lock replaced by FOR NO KEY UPDATE.
func searchIndexCandidateReadQuery(selection string) string {
	return generationRetentionSearchIndexReadMarker + "\n" + strings.ReplaceAll(selection, "FOR UPDATE OF ", "FOR NO KEY UPDATE OF ")
}

// generationRetentionSearchIndex carries one pass's pre-delete into the prune
// transaction: the drained set, and the generations the prune admitted on
// it, so the pass can log any it then left unpruned.
type generationRetentionSearchIndex struct {
	pass     *indexretention.Pass
	admitted []generationRetentionCandidate
}

// generationRetentionStoreOptions holds the store's optional collaborators.
type generationRetentionStoreOptions struct {
	logger *slog.Logger
}

// WithLogger returns the store with a logger for the per-generation search
// index pre-delete completion lines (#7427); nil discards them.
func (s GenerationRetentionStore) WithLogger(logger *slog.Logger) GenerationRetentionStore {
	s.logger = logger
	return s
}

// predeleteSearchIndex runs the pass's search index pre-delete before the
// prune transaction opens (#7427): it reads the eligible candidates in a
// transaction it rolls back first, drops uncovered writers and each identity group's newest copy as the
// prune's planning does, and drains the rest oldest first in committed
// batches under the policy's row cap and deadline. It checks the retention
// key indexes first, so a cycle the prune would refuse changes nothing.
func (s GenerationRetentionStore) predeleteSearchIndex(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
) (*generationRetentionSearchIndex, error) {
	if err := checkGenerationRetentionKeyIndexes(ctx, s.database); err != nil {
		return nil, err
	}
	candidates, err := s.readSearchIndexCandidates(ctx, beginner, now, policy)
	if err != nil {
		return nil, err
	}
	covered := candidates[:0]
	for _, candidate := range candidates {
		if !candidate.uncovered {
			covered = append(covered, candidate)
		}
	}
	prunable, _ := dropNewestSameIdentityPerBatch(covered)
	generations := make([]indexretention.Generation, 0, len(prunable))
	for _, candidate := range prunable {
		generations = append(generations, indexretention.Generation{
			ScopeID: candidate.scopeID, GenerationID: candidate.generationID,
		})
	}
	pass := indexretention.NewPass(policy.SearchIndexPredeleteRowCap,
		time.Now().Add(policy.SearchIndexPredeleteDeadline), s.logger)
	if err := pass.Run(ctx, beginner, generations); err != nil {
		return nil, fmt.Errorf("generation retention: %w", err)
	}
	return &generationRetentionSearchIndex{pass: pass}, nil
}

// readSearchIndexCandidates runs the candidate read in its own transaction
// and rolls it back before returning, releasing the selection's row locks.
func (s GenerationRetentionStore) readSearchIndexCandidates(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
) ([]generationRetentionCandidate, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("generation retention: begin search index candidate read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, generationRetentionSearchIndexReadWorkMemStatement); err != nil {
		return nil, fmt.Errorf("generation retention: set search index candidate read work_mem: %w", err)
	}
	rows, err := tx.QueryContext(ctx, generationRetentionSearchIndexReadQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations,
		policy.BatchGenerationLimit, now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, fmt.Errorf("generation retention: read search index candidates: %w", err)
	}
	candidates, err := scanRetentionCandidates(rows)
	if err != nil {
		return nil, fmt.Errorf("generation retention: read search index candidates: %w", err)
	}
	sortRetentionCandidatesOldestFirst(candidates)
	return candidates, nil
}

// admit keeps, inside the prune transaction and before the re-lock, only the
// candidates (and chunk target) the pre-delete drained and whose
// per-generation advisory lock this transaction takes; the rest count as
// skipped and stay for a later pass. It also folds the pre-delete's phase
// times and batch count into the result.
func (si *generationRetentionSearchIndex) admit(
	ctx context.Context,
	tx db.Transaction,
	candidates []generationRetentionCandidate,
	chunkTarget *generationRetentionCandidate,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, *generationRetentionCandidate, error) {
	_, _, batches, keyset, sweep := si.pass.Totals()
	if si.pass.Ran() {
		result.PhaseDurations[GenerationRetentionPhasePredeleteSearchIndex] += keyset
		result.PhaseDurations[GenerationRetentionPhaseSweepSearchIndex] += sweep
	}
	result.SearchIndexBatches = batches
	drained := func(candidate generationRetentionCandidate) bool {
		if si.pass.Drained(candidate.generationID) {
			return true
		}
		result.Skipped[searchIndexUndrainedSkippedReason]++
		return false
	}
	var ids []string
	kept := make([]generationRetentionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if drained(candidate) {
			kept = append(kept, candidate)
			ids = append(ids, candidate.generationID)
		}
	}
	if chunkTarget != nil {
		if drained(*chunkTarget) {
			ids = append(ids, chunkTarget.generationID)
		} else {
			chunkTarget = nil
		}
	}
	phaseStart := time.Now()
	locked, err := indexretention.TryLockGenerations(ctx, tx, ids)
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, fmt.Errorf("generation retention: %w", err)
	}
	admitted := kept[:0]
	for _, candidate := range kept {
		if locked[candidate.generationID] {
			admitted = append(admitted, candidate)
		} else {
			result.Skipped[searchIndexLockBusySkippedReason]++
		}
	}
	if chunkTarget != nil && !locked[chunkTarget.generationID] {
		result.Skipped[searchIndexLockBusySkippedReason]++
		chunkTarget = nil
	}
	si.admitted = append(si.admitted[:0], admitted...)
	if chunkTarget != nil {
		si.admitted = append(si.admitted, *chunkTarget)
	}
	return admitted, chunkTarget, nil
}

// eventCounts returns the per-generation event row counts with each
// generation's pre-deleted search index rows added: the recount under the
// lock runs after the pre-delete and sees those tables empty, so without
// this a pruned generation's retention event would record 0 rows the
// counter (finish) still adds. It copies every inner map it changes; the
// batch's row-limit totals were already computed from the recount.
func (si *generationRetentionSearchIndex) eventCounts(perGeneration map[string]map[string]int64) map[string]map[string]int64 {
	out := make(map[string]map[string]int64, len(perGeneration))
	for generationID, tables := range perGeneration {
		out[generationID] = tables
	}
	for _, candidate := range si.admitted {
		report, ok := si.pass.Report(candidate.generationID)
		if !ok || report.Rows() == 0 {
			continue
		}
		tables := make(map[string]int64, len(out[candidate.generationID])+2)
		for table, rows := range out[candidate.generationID] {
			tables[table] = rows
		}
		tables["eshu_search_index_terms"] += report.TermsDeleted
		tables["eshu_search_index_documents"] += report.DocumentsDeleted
		out[candidate.generationID] = tables
	}
	return out
}

// finish adds the pre-delete's rows to RowsPruned once pruneCandidates has
// rebuilt it from the batch counts (the recount sees the drained tables
// empty). Rows of generations the prune removed also sit in their events
// (eventCounts); rows pre-deleted for a generation this pass did not prune
// appear only on the counter, because no event is written for it. It logs every admitted generation the prune did not then remove:
// a re-lock miss, a row-limit refit or a chunk drain. Its search index rows
// are already gone, which is harmless for a terminal superseded generation;
// a later pass prunes the rest.
func (si *generationRetentionSearchIndex) finish(
	ctx context.Context,
	logger *slog.Logger,
	result *GenerationRetentionResult,
	pruned []generationRetentionCandidate,
) {
	terms, documents, _, _, _ := si.pass.Totals()
	for table, rows := range map[string]int64{"eshu_search_index_terms": terms, "eshu_search_index_documents": documents} {
		if rows > 0 {
			result.RowsPruned[table] += rows
		}
	}
	if logger == nil {
		return
	}
	prunedIDs := make(map[string]bool, len(pruned))
	for _, candidate := range pruned {
		prunedIDs[candidate.generationID] = true
	}
	for _, candidate := range si.admitted {
		if prunedIDs[candidate.generationID] {
			continue
		}
		logger.InfoContext(ctx, "generation retention search index pre-deleted generation left for a later pass",
			slog.String(telemetry.LogKeyScopeID, candidate.scopeID),
			slog.String(telemetry.LogKeyGenerationID, candidate.generationID),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
