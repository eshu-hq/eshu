// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

const (
	defaultGenerationRetentionMinSuperseded = 24
	defaultGenerationRetentionMaxAge        = 7 * 24 * time.Hour
	defaultGenerationRetentionBatchLimit    = 100
	defaultGenerationRetentionRowLimit      = 100000
	defaultGenerationRetentionPolicyScope   = "global"
	defaultGenerationRetentionPolicyRev     = "global-default-v1"
)

// generationRetentionWorkMemStatement raises work_mem for the retention
// transaction only. #6809 set it for the grouped prunes, which aggregated one
// pass over every live fact of a kind and sorted it on disk at the default 4MB
// (docs/internal/evidence/6809-retention-content-prune-plan.md). Since #7279 the
// statements aggregate only the batch's candidate keys (one DISTINCT or GROUP
// BY per kind, bounded by BatchGenerationLimit generations, about 300k facts on
// the first ops-qa backlog), and 64MB keeps those aggregates in memory.
//
// work_mem is a per-plan-node allowance, not a per-statement budget: each sort or
// hash node may use up to 64MB (a hash node up to twice that,
// hash_mem_multiplier), and one statement can hold several such nodes. SET
// LOCAL reverts at commit or rollback, so it never touches the pooled session or
// the server setting.
const generationRetentionWorkMemStatement = "SET LOCAL work_mem = '64MB'"

// GenerationRetentionPolicy bounds automated cleanup of superseded source-local
// generations. The active generation and the newest superseded generations
// inside the count or age window are never candidates.
type GenerationRetentionPolicy struct {
	MinSupersededGenerations int
	MaxSupersededAge         time.Duration
	BatchGenerationLimit     int
	BatchRowLimit            int
	PolicyScope              string
	PolicyRevision           string
}

// DefaultGenerationRetentionPolicy returns the ADR #2248 default retention
// window: keep the active generation plus the last 24 superseded generations or
// any superseded generation newer than seven days, whichever keeps more.
func DefaultGenerationRetentionPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: defaultGenerationRetentionMinSuperseded,
		MaxSupersededAge:         defaultGenerationRetentionMaxAge,
		BatchGenerationLimit:     defaultGenerationRetentionBatchLimit,
		BatchRowLimit:            defaultGenerationRetentionRowLimit,
		PolicyScope:              defaultGenerationRetentionPolicyScope,
		PolicyRevision:           defaultGenerationRetentionPolicyRev,
	}
}

func (p GenerationRetentionPolicy) normalize() GenerationRetentionPolicy {
	defaults := DefaultGenerationRetentionPolicy()
	if p.MinSupersededGenerations < 0 {
		p.MinSupersededGenerations = defaults.MinSupersededGenerations
	}
	if p.MaxSupersededAge <= 0 {
		p.MaxSupersededAge = defaults.MaxSupersededAge
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
	return p
}

// GenerationRetentionResult reports the work a cleanup transaction completed.
// RowsPruned is keyed by bounded table/data-class names, never raw scope or
// generation identifiers. Each event's row_counts charges a content row shared
// by several pruned generations to the newest of them, so the events sum per
// table to the deletes (recounted after a row-limit skip).
//
// PhaseDurations is keyed by the closed GenerationRetentionPhase* set.
// ScopeLockHold is the time from the candidate statement, which locks the
// batch's ingestion_scopes and scope_generations rows, to the end of the
// commit that releases them: the window a concurrent fact insert into one of
// those scopes waits out (#7279).
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
	// LockedScopeRows is the scope rows held by the pruned batch, not the
	// selection's full lock set: the batch's distinct scopes, 1 for a batch
	// of one narrowed after its selection (arbiter ruling arb-7127-3d-c). In
	// the general path the candidate query can also hold further scope rows
	// it locked while choosing, up to BatchGenerationLimit, which this
	// figure does not count.
	LockedScopeRows int
}

// GenerationRetentionStore prunes superseded source-local generation history in
// bounded transactions while preserving active reads and changed-since truth.
type GenerationRetentionStore struct {
	database db.ExecQueryer
	Now      func() time.Time
	// beforeTargetedLock, when set by a test, runs between the selection's
	// savepoint rollback and the targeted re-lock of an over-limit batch of
	// one, where another session can take the candidate.
	beforeTargetedLock func()
}

// NewGenerationRetentionStore constructs a Postgres-backed retention cleanup
// store. The supplied database must support transactions when cleanup runs.
func NewGenerationRetentionStore(database db.ExecQueryer) GenerationRetentionStore {
	return GenerationRetentionStore{database: database}
}

// PruneSupersededGenerations deletes one bounded batch of superseded
// generations outside the retained window. It records safe retention events
// before deletion and removes generation-owned rows through FK cascades.
//
// Before it locks anything it checks that both retention key indexes are
// valid; if either is not, it refuses the cycle with an error wrapping
// ErrGenerationRetentionKeyIndexUnavailable and changes nothing (#7279).
func (s GenerationRetentionStore) PruneSupersededGenerations(
	ctx context.Context,
	policy GenerationRetentionPolicy,
) (GenerationRetentionResult, error) {
	if s.database == nil {
		return GenerationRetentionResult{}, errors.New("generation retention database is required")
	}
	beginner, ok := s.database.(db.Beginner)
	if !ok {
		return GenerationRetentionResult{}, errors.New("generation retention database must support Begin")
	}

	policy = policy.normalize()
	start := time.Now()
	now := s.now()
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, generationRetentionWorkMemStatement); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: set transaction work_mem: %w", err)
	}

	result := GenerationRetentionResult{
		RowsPruned:     make(map[string]int64),
		Skipped:        make(map[string]int),
		PhaseDurations: make(map[string]time.Duration),
	}
	phaseStart := time.Now()
	if err := checkGenerationRetentionKeyIndexes(ctx, tx); err != nil {
		return GenerationRetentionResult{}, err
	}
	result.PhaseDurations[GenerationRetentionPhaseKeyIndexCheck] = time.Since(phaseStart)

	// The selection runs inside a savepoint so an over-limit batch of one can
	// give its other locks back (arbiter ruling arb-7127-3d-c).
	if _, err := tx.ExecContext(ctx, generationRetentionSavepointStatement); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: savepoint: %w", err)
	}
	// The candidate statement takes the scope and generation row locks; they
	// are held until the commit below, and ScopeLockHold measures that window.
	lockStart := time.Now()
	candidates, rowCounts, eventRowCounts, err := s.selectPrunableCandidates(ctx, tx, now, policy, &result)
	if err != nil {
		return GenerationRetentionResult{}, err
	}
	candidates, rowCounts, eventRowCounts, err = s.narrowOverLimitBatch(ctx, tx, now, policy, candidates, rowCounts, eventRowCounts, &result)
	if err != nil {
		return GenerationRetentionResult{}, err
	}
	if len(candidates) > 0 {
		if err := s.pruneCandidates(ctx, tx, candidates, rowCounts, eventRowCounts, policy, now, &result); err != nil {
			return GenerationRetentionResult{}, err
		}
	}

	phaseStart = time.Now()
	if err := tx.Commit(); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: commit: %w", err)
	}
	committed = true
	result.PhaseDurations[GenerationRetentionPhaseCommit] = time.Since(phaseStart)
	result.ScopeLockHold = time.Since(lockStart)
	result.Duration = time.Since(start)
	return result, nil
}

// selectPrunableCandidates runs the general candidate query exactly once per
// pass (arbiter ruling arb-7334.md section 3): the query already returns the
// full eligible set across every scope, oldest-first, up to
// BatchGenerationLimit, so there is nothing left to discover by excluding a
// skip and re-querying. The re-check loop over a shrinking or growing recount
// (arbiter ruling arb-7127-3d-b, generationRetentionRecheckLimit) is
// unrelated and stays: it re-evaluates the same already-fetched candidates
// after a recount, it does not requery scope_generations.
func (s GenerationRetentionStore) selectPrunableCandidates(
	ctx context.Context,
	tx db.Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, error) {
	phaseStart := time.Now()
	candidates, err := s.selectCandidates(ctx, tx, now, policy)
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(candidates) == 0 {
		return nil, nil, nil, nil
	}

	phaseStart = time.Now()
	_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(candidates), retentionGenerationIDs(candidates))
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, err
	}
	selected, rowCounts, selectedEventRowCounts, skipped := selectCandidatesWithinRowLimit(
		candidates, eventRowCounts, policy.BatchRowLimit)
	for rechecks := 0; len(skipped) > 0; rechecks++ {
		for _, generationID := range skipped {
			result.Skipped[rowLimitSkipReason(eventRowCounts[generationID], policy.BatchRowLimit)]++
		}
		if len(selected) == 0 {
			break
		}
		if rechecks == generationRetentionRecheckLimit {
			// Keep the first member only; its rows outside the ledger only
			// shrink on a recount, so one recount settles it.
			for _, dropped := range selected[1:] {
				result.Skipped[rowLimitSkipReason(eventRowCounts[dropped.generationID], policy.BatchRowLimit)]++
			}
			selected = selected[:1]
		}
		// Skipped generations stay on disk. Their facts protect keys the
		// count charged to a selected one (#6809), and a changed-since link
		// shared with one is still deleted with the selected generation, so
		// its charge moves there (#7127). The recount can shrink or grow;
		// re-check the limit, at most generationRetentionRecheckLimit times.
		phaseStart = time.Now()
		_, eventRowCounts, _, err = s.countRows(ctx, tx, retentionScopeIDs(selected), retentionGenerationIDs(selected))
		result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
		if err != nil {
			return nil, nil, nil, err
		}
		selected, rowCounts, selectedEventRowCounts, skipped = selectCandidatesWithinRowLimit(
			selected, eventRowCounts, policy.BatchRowLimit)
		if rechecks == generationRetentionRecheckLimit {
			break
		}
	}
	return selected, rowCounts, selectedEventRowCounts, nil
}

type generationRetentionCandidate struct {
	scopeID      string
	generationID string
	scopeKind    string
	supersededAt time.Time
	observedAt   time.Time
}

func (s GenerationRetentionStore) selectCandidates(
	ctx context.Context,
	tx db.Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
) ([]generationRetentionCandidate, error) {
	rows, err := tx.QueryContext(
		ctx,
		generationRetentionCandidateQuery,
		now.Add(-policy.MaxSupersededAge),
		policy.MinSupersededGenerations,
		policy.BatchGenerationLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("generation retention: select candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []generationRetentionCandidate
	for rows.Next() {
		var candidate generationRetentionCandidate
		if err := rows.Scan(
			&candidate.scopeID,
			&candidate.generationID,
			&candidate.scopeKind,
			&candidate.supersededAt,
			&candidate.observedAt,
		); err != nil {
			return nil, fmt.Errorf("generation retention: scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("generation retention: select candidates: %w", err)
	}
	// The locking SELECT has no ORDER BY; the row count's attribution and the
	// row limit both rely on oldest-first, generation id breaking ties.
	slices.SortStableFunc(candidates, func(a, b generationRetentionCandidate) int {
		if c := a.supersededAt.Compare(b.supersededAt); c != 0 {
			return c
		}
		return strings.Compare(a.generationID, b.generationID)
	})
	return candidates, nil
}

// Retention cycle phases reported in GenerationRetentionResult.PhaseDurations.
// The set is closed so it can label a metric: every phase after
// GenerationRetentionPhaseKeyIndexCheck runs while the batch holds its
// ingestion_scopes and scope_generations row locks.
const (
	GenerationRetentionPhaseKeyIndexCheck         = "key_index_check"
	GenerationRetentionPhaseSelectCandidates      = "select_candidates"
	GenerationRetentionPhaseCountRows             = "count_rows"
	GenerationRetentionPhaseRecordEvents          = "record_events"
	GenerationRetentionPhaseDeleteIntents         = "delete_shared_projection_intents"
	GenerationRetentionPhasePruneFileReferences   = "prune_content_file_references"
	GenerationRetentionPhaseLockInfraRepositories = "lock_infra_repositories"
	GenerationRetentionPhasePruneEntities         = "prune_content_entities"
	GenerationRetentionPhaseDeleteInfraOrphans    = "delete_infra_orphans"
	GenerationRetentionPhasePruneFiles            = "prune_content_files"
	GenerationRetentionPhaseDeleteLedger          = "delete_changed_since_ledger"
	GenerationRetentionPhaseDeleteGenerations     = "delete_scope_generations"
	GenerationRetentionPhaseCommit                = "commit"
)

// pruneCandidates records one retention event per candidate and then deletes
// the batch's rows in dependency order, timing each statement as a phase.
func (s GenerationRetentionStore) pruneCandidates(
	ctx context.Context,
	tx db.Transaction,
	candidates []generationRetentionCandidate,
	rowCounts map[string]int64,
	eventRowCounts map[string]map[string]int64,
	policy GenerationRetentionPolicy,
	now time.Time,
	result *GenerationRetentionResult,
) error {
	result.OldestEligibleAge = oldestEligibleAge(now, candidates)
	generationIDs := retentionGenerationIDs(candidates)
	for tableName, count := range rowCounts {
		result.RowsPruned[tableName] = count
	}
	if total := generationRetentionRowsTotal(rowCounts); len(candidates) == 1 && total > int64(policy.BatchRowLimit) {
		result.RowsOverLimit = total - int64(policy.BatchRowLimit) // a batch of one, admitted alone
	}
	result.LockedScopeRows = len(slices.Compact(slices.Sorted(slices.Values(retentionScopeIDs(candidates)))))

	phaseStart := time.Now()
	for _, candidate := range candidates {
		if err := s.recordRetentionEvent(ctx, tx, candidate, policy, eventRowCounts[candidate.generationID], now); err != nil {
			return err
		}
	}
	result.PhaseDurations[GenerationRetentionPhaseRecordEvents] = time.Since(phaseStart)

	deleteWith := func(query string) func() (int64, error) {
		return func() (int64, error) { return execRowsAffected(ctx, tx, query, generationIDs) }
	}
	// Each step: the phase it is timed under, the RowsPruned key (empty for
	// none), and the call.
	steps := []struct {
		phase, table string
		run          func() (int64, error)
	}{
		{GenerationRetentionPhaseDeleteIntents, "shared_projection_intents", deleteWith(deleteSharedProjectionIntentsForGenerationsQuery)},
		{GenerationRetentionPhaseDeleteIntents, "shared_projection_unroutable_intents", deleteWith(deleteSharedProjectionUnroutableIntentsForGenerationsQuery)},
		{GenerationRetentionPhasePruneFileReferences, "content_file_references", deleteWith(pruneContentFileReferencesForGenerationsQuery)},
		// The infra read model mirrors content_entities (#6793): lock its
		// repositories before the content prune, drop its orphans after it.
		{GenerationRetentionPhaseLockInfraRepositories, "", func() (int64, error) {
			return 0, inventory.LockRepositoriesForGenerations(ctx, tx, generationIDs)
		}},
		{GenerationRetentionPhasePruneEntities, "content_entities", deleteWith(pruneContentEntitiesForGenerationsQuery)},
		{GenerationRetentionPhaseDeleteInfraOrphans, "infra_resource_entities", func() (int64, error) {
			return inventory.DeleteOrphanedRows(ctx, tx, generationIDs)
		}},
		{GenerationRetentionPhasePruneFiles, "content_files", deleteWith(pruneContentFilesForGenerationsQuery)},
		// The changed-since ledger statement finds the scopes through
		// scope_generations, so it runs before the generation rows go (#7127
		// ruling 2.8). It fills RowsPruned for the four ledger tables itself.
		{GenerationRetentionPhaseDeleteLedger, "", func() (int64, error) {
			ledgerRows, err := linksfreshnessstore.DeletePrunedGenerationRows(ctx, tx, retentionScopeIDs(candidates), generationIDs)
			if err != nil {
				return 0, err
			}
			result.LedgerRowsCounted = make(map[string]int64, len(ledgerRows))
			for tableName := range ledgerRows {
				result.LedgerRowsCounted[tableName] = rowCounts[tableName]
			}
			result.LedgerRowsPruned = ledgerRows
			maps.Copy(result.RowsPruned, ledgerRows)
			return 0, nil
		}},
		{GenerationRetentionPhaseDeleteGenerations, "scope_generations", deleteWith(deleteScopeGenerationsForRetentionQuery)},
	}
	for _, step := range steps {
		phaseStart := time.Now()
		affected, err := step.run()
		result.PhaseDurations[step.phase] += time.Since(phaseStart)
		if err != nil {
			return fmt.Errorf("generation retention: %s: %w", step.phase, err)
		}
		if step.table != "" {
			result.RowsPruned[step.table] = affected
		}
	}
	result.GenerationsPruned = int(result.RowsPruned["scope_generations"])
	return nil
}

func (s GenerationRetentionStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func retentionGenerationIDs(candidates []generationRetentionCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.generationID)
	}
	return ids
}
