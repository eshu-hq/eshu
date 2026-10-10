// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

const (
	defaultGenerationRetentionMinSuperseded = 24
	defaultGenerationRetentionMaxAge        = 7 * 24 * time.Hour
	// defaultGenerationRetentionHardMaxAge is the #7585 hard history ceiling
	// floor; see DefaultGenerationRetentionHardMaxAge. 90 days preserves
	// observable behavior (ops-qa census found nothing older).
	defaultGenerationRetentionHardMaxAge  = 90 * 24 * time.Hour
	defaultGenerationRetentionBatchLimit  = 100
	defaultGenerationRetentionRowLimit    = 100000
	defaultGenerationRetentionPolicyScope = "global"
	defaultGenerationRetentionPolicyRev   = "global-default-v1"
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
	LockedScopeRows int
}

// GenerationRetentionStore prunes superseded source-local generation history in
// bounded transactions while preserving active reads and changed-since truth.
type GenerationRetentionStore struct {
	database db.ExecQueryer
	Now      func() time.Time
	// beforeTargetedLock, when set by a test, runs between the selection's
	// savepoint rollback and the targeted re-lock of the provisionally
	// selected set, where another session can take a candidate.
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
	// An explicit hard ceiling below the soft window promises retention the
	// ceiling denies (#7585): fail closed before locking anything.
	if policy.HardMaxSupersededAge < policy.MaxSupersededAge {
		return GenerationRetentionResult{}, fmt.Errorf(
			"generation retention: hard max superseded age (%v) must not be below max superseded age (%v)",
			policy.HardMaxSupersededAge, policy.MaxSupersededAge)
	}
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

	// The selection runs inside a savepoint so the pass can give the wide
	// selection lock set back right after selecting (#7334 fix 1): the
	// pre-screen and the row counts plan the batch unlocked, and only the
	// provisionally selected set is re-locked before the prune.
	if _, err := tx.ExecContext(ctx, generationRetentionSavepointStatement); err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("generation retention: savepoint: %w", err)
	}
	candidates, rowCounts, eventRowCounts, err := s.selectPrunableCandidates(ctx, tx, now, policy, &result)
	if err != nil {
		return GenerationRetentionResult{}, err
	}
	// ScopeLockHold spans the re-lock, its recount and the prune, and is
	// zero when the pass locks nothing.
	var narrowHoldStart time.Time
	held := false
	if len(candidates) > 0 {
		narrowHoldStart = time.Now()
		candidates, rowCounts, eventRowCounts, held, err = s.relockRetentionSelection(ctx, tx, now, policy, candidates, &result)
		if err != nil {
			return GenerationRetentionResult{}, err
		}
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
	if held {
		result.ScopeLockHold = time.Since(narrowHoldStart)
	}
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
// after a recount, it does not requery scope_generations. The selection's
// savepoint is rolled back before planning, so the pre-screen, the row count
// and the re-check loop all run unlocked; the caller re-locks the
// provisionally selected set before pruning (#7334 fix 1).
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
	// Uncovered writers (#7472) ride the candidate query's report leg so the
	// pass can count them without spending prune batch slots on them; they
	// are never locked, counted, or deleted.
	prunable := candidates[:0]
	for _, candidate := range candidates {
		if candidate.uncovered {
			result.Skipped["uncovered_writer"]++
			continue
		}
		prunable = append(prunable, candidate)
	}
	candidates = prunable
	if len(candidates) == 0 {
		return nil, nil, nil, nil
	}
	// Give the selection's locks back immediately; everything below plans
	// unlocked, and the caller re-locks only the selected set.
	if _, err := tx.ExecContext(ctx, generationRetentionRollbackSavepointStatement); err != nil {
		return nil, nil, nil, fmt.Errorf("generation retention: rollback to savepoint: %w", err)
	}
	// #7334 fix 2: generations over the limit on own facts alone can never
	// join a batch; skip them without running the heavyweight row count.
	phaseStart = time.Now()
	ownFacts, err := s.prescreenOwnFacts(ctx, tx, candidates)
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, err
	}
	rowLimit := int64(policy.BatchRowLimit)
	countable := make([]generationRetentionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if ownFacts[candidate.generationID] > rowLimit {
			result.Skipped["row_limit_own_rows"]++
			continue
		}
		countable = append(countable, candidate)
	}
	if len(countable) == 0 {
		return nil, nil, nil, nil
	}

	phaseStart = time.Now()
	_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(countable), retentionGenerationIDs(countable))
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, err
	}
	return s.recheckRetentionBatch(ctx, tx, countable, eventRowCounts, policy.BatchRowLimit, result)
}

type generationRetentionCandidate struct {
	scopeID      string
	generationID string
	scopeKind    string
	supersededAt time.Time
	observedAt   time.Time
	// uncovered marks a reported-but-not-prunable row (#7472): the
	// reconcile probe still needs it, so it is counted as skipped and
	// never locked or deleted.
	uncovered bool
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
		now.Add(-policy.HardMaxSupersededAge),
	)
	if err != nil {
		return nil, fmt.Errorf("generation retention: select candidates: %w", err)
	}
	candidates, err := scanRetentionCandidates(rows)
	if err != nil {
		return nil, fmt.Errorf("generation retention: select candidates: %w", err)
	}
	// The locking SELECT has no ORDER BY; the row count's attribution and the
	// row limit both rely on oldest-first, generation id breaking ties.
	sortRetentionCandidatesOldestFirst(candidates)
	return candidates, nil
}

// Retention cycle phases reported in GenerationRetentionResult.PhaseDurations.
// The set is closed so it can label a metric. Since #7334 the selection,
// pre-screen and planning counts run unlocked and only the targeted re-lock,
// its recount and the prune run while the batch holds its ingestion_scopes
// and scope_generations row locks; the re-lock and recount are timed under
// the existing select_candidates and count_rows phases.
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
