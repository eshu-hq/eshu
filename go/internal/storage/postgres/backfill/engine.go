// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DeferredMaintenanceRepoBatchSize bounds how many source repositories one
// deferred-maintenance write transaction locks and commits together. Each batch
// is an independent transaction that takes only its own repositories' exclusive
// locks, so a stalled or slow batch holds at most this many repository locks and
// releases them on commit before the next batch starts. The size trades
// transaction/round-trip overhead (smaller batches => more commits) against lock
// hold time and conflict surface (larger batches => longer holds, more
// repositories blocked at once). 32 keeps per-batch lock hold time small while
// amortizing transaction overhead across the corpus.
const DeferredMaintenanceRepoBatchSize = 32

// RunAllRelationshipEvidence runs a corpus-wide backward evidence discovery
// pass and publishes readiness for the active repository generations. Evidence
// discovery reads the whole committed fact corpus (cross-repo relationships need
// every repository's facts), but the writes are split into independent,
// per-repository-batch transactions so the pass never holds a fleet-wide lock.
// Each batch transaction acquires only its own repositories' exclusive
// maintenance locks (sorted, deadlock-free), re-reads those repositories' active
// generations under the lock so evidence attaches to the current generation, and
// commits to release the locks before the next batch. A stall on one batch
// therefore blocks only that batch's repositories, never unrelated commits.
//
// It additionally returns the set of (scope_id, generation_id) partitions this
// pass's Track-1 memo gate (ApplyDeferredPartitionMemoGate, via
// loadDeferredScopedFactsAcrossPartitions) SKIPPED because they were already a
// memo hit at the START of this pass, MINUS every partition the pass's backfill
// actually inserted evidence rows under (see ExcludePartitionsWithNewEvidence)
// — i.e. partitions whose backward evidence this pass provably did NOT change.
// The subtraction is load-bearing: discovery is cross-partition, so a memo-hit
// partition can still receive new evidence during the pass (issue #7636), and
// returning it unrevised would let the reopen gate skip items that never
// re-read the new rows.
//
// The root RunDeferredRelationshipMaintenance is the only caller that uses this
// return value: it is the sole set the deferred reopen gate (issue #4770) may
// treat as reopen-redundant in the SAME pass. Every other caller (the root
// BackfillAllRelationshipEvidence wrapper that bootstrap-index's
// RelationshipMaintenanceCommitter phase uses, and every direct test call)
// discards it and gets the legacy error-only contract unchanged.
func (s Store) RunAllRelationshipEvidence(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) (map[ScopeGenerationPartition]struct{}, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("ingestion store db is required")
	}
	if s.Beginner == nil {
		return nil, fmt.Errorf("transaction beginner is required for batched deferred backfill")
	}

	start := time.Now()
	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "relationship.backfill_deferred")
		defer span.End()
	}

	var catalog []relationships.CatalogEntry
	if s.LoadCatalog != nil {
		var err error
		catalog, _, err = s.LoadCatalog(ctx, s.DB)
		if err != nil {
			return nil, fmt.Errorf("load repository catalog for deferred relationship backfill: %w", err)
		}
	}

	// Scope the corpus-wide source-fact load to the content anchors of the full
	// catalog instead of streaming every committed fact (issue #3569), and
	// partition the load per (scope_id, generation_id) so it fans out across the
	// deferred-maintenance worker pool (issue #3710). The deferred backfill treats
	// every repository as an eligible relationship target, so anchors derive from
	// the whole catalog rather than an onboarding delta. The dedicated deferred
	// loader (issue #3659) splits the anchor set into non-repo_id terms ($1) and
	// repo_id terms ($2 self-exclusion) so facts that only match because their own
	// repo_id appears as an anchor are excluded at the SQL layer, while facts
	// referencing ANOTHER repo's repo_id in their content are still loaded. No
	// evidence the full-corpus load would have produced is dropped
	// (truth-equivalence: the in-memory matcher already skips self-matches on
	// entry.RepoID == sourceRepoID and re-applies boundary-safe token matching).
	// With no usable anchors no fact can resolve a catalog target, so the fact
	// load short-circuits and the pass still publishes readiness for the active
	// generations below.
	activeFacts, snapshotGenerations, skippedPartitions, err := s.LoadDeferredAnchorScopedRelationshipFacts(ctx, s.DB, catalog, instruments)
	if err != nil {
		return nil, fmt.Errorf("load anchor-scoped facts for deferred relationship backfill: %w", err)
	}

	discoveredEvidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(activeFacts, catalog),
	)

	var totalEvidence int64
	evidenceBySourceRepo := make(map[string][]relationships.EvidenceFact)
	for _, fact := range discoveredEvidence {
		if strings.TrimSpace(fact.SourceRepoID) == "" || strings.TrimSpace(fact.TargetRepoID) == "" {
			continue
		}
		evidenceBySourceRepo[fact.SourceRepoID] = append(evidenceBySourceRepo[fact.SourceRepoID], fact)
		totalEvidence++
	}

	// The catalog fingerprint (issue #3624 Track 1 / B') is derived from the
	// SAME $1/$2 query parameters BuildDeferredScopedFactQueryParams already
	// computed for the fact load above, so the memo written at commit time
	// records the exact catalog shape this pass's evidence was derived against.
	// An empty/unbuildable params (BuildDeferredScopedFactQueryParams's ok=false
	// case) still yields a stable, deterministic fingerprint via
	// DeferredCatalogFingerprint's zero-value handling, so a no-anchor catalog
	// never produces an empty or colliding fingerprint.
	fingerprintParams, _ := BuildDeferredScopedFactQueryParams(catalog)
	catalogFingerprint := DeferredCatalogFingerprint(fingerprintParams)

	readinessRows, insertedRows, err := s.writeDeferredBackfillInBatches(ctx, evidenceBySourceRepo, snapshotGenerations, catalogFingerprint, instruments)
	if err != nil {
		return nil, err
	}

	dur := time.Since(start).Seconds()
	if instruments != nil {
		instruments.DeferredBackfillDuration.Record(ctx, dur)
		instruments.DeferredBackfillEvidence.Add(ctx, totalEvidence)
	}
	log.Printf("deferred_backfill_completed evidence_facts=%d readiness_rows=%d duration_s=%.2f batch_size=%d",
		totalEvidence, readinessRows, dur, DeferredMaintenanceRepoBatchSize)

	return ExcludePartitionsWithNewEvidence(skippedPartitions, insertedRows), nil
}

// writeDeferredBackfillInBatches commits deferred backward evidence in bounded
// per-repository batches, each in its own transaction holding only that batch's
// exclusive maintenance locks, and then publishes readiness and the partition
// memo in a separate per-partition fan-in step. It returns the number of
// readiness rows published, plus the per-partition actually-inserted evidence
// row counts the pass's memo-hit skip-set revision needs (issue #7636). Every
// active repository is published as backward-evidence-ready even when it
// discovered no new evidence, preserving the prior corpus-wide readiness
// contract; repositories whose active generation disappears between batches
// are skipped idempotently.
//
// The two phases are separate because they have different natural units. A
// batch is a contiguous slice of the repo-ID-sorted corpus, so the repositories
// of one (scope, generation) partition are routinely spread across several
// batches; readiness and the memo, by contrast, are partition-wide claims. See
// PublishDeferredBackfillPartitions for the publication contract and its
// deadlock-freedom argument.
func (s Store) writeDeferredBackfillInBatches(
	ctx context.Context,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	catalogFingerprint string,
	instruments *telemetry.Instruments,
) (int, map[ScopeGenerationPartition]int64, error) {
	repoGenerations, err := LoadActiveRepositoryGenerations(ctx, s.DB)
	if err != nil {
		return 0, nil, fmt.Errorf("load active repository generations for deferred relationship backfill: %w", err)
	}
	if len(repoGenerations) == 0 {
		return 0, nil, nil
	}

	repoIDs := make([]string, 0, len(repoGenerations))
	for repoID := range repoGenerations {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = DeferredMaintenanceRepoBatchSize
	}

	// Partition the sorted repository list into disjoint per-batch slices. Because
	// the list is sorted and the slices are contiguous and non-overlapping, no two
	// batches request the same repository advisory lock, so concurrent batches
	// cannot deadlock on lock ordering (each batch also sorts its own keys). Each
	// batch transaction holds exactly one pooled connection for its lifetime and
	// never acquires a second, so running W batches concurrently can at most block
	// on Begin when the pool is smaller than W; it cannot deadlock. At
	// ESHU_POSTGRES_MAX_OPEN_CONNS=1 the pool self-serializes the batches.
	bounds := make([][2]int, 0, (len(repoIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(repoIDs); start += batchSize {
		end := start + batchSize
		if end > len(repoIDs) {
			end = len(repoIDs)
		}
		bounds = append(bounds, [2]int{start, end})
	}

	workers := s.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(bounds) {
		workers = len(bounds)
	}

	contributions, insertedRows, err := s.runDeferredBackfillBatches(
		ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshotGenerations, instruments,
	)
	if err != nil {
		// A failed or canceled batch means at least one partition's evidence is
		// only partly committed. Publishing readiness or a memo row for ANY
		// partition now would claim completion the pass did not achieve, and the
		// memo in particular is durable: ApplyDeferredPartitionMemoGate would skip
		// that partition's fact load on every later pass until the catalog
		// fingerprint changes. Withhold the whole fan-in instead; the committed
		// evidence is content-addressed and the next pass re-derives and
		// republishes it.
		return 0, nil, err
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, fmt.Errorf("deferred backfill canceled before readiness publication: %w", err)
	}

	published, err := s.PublishDeferredBackfillPartitions(ctx, contributions, snapshotGenerations, catalogFingerprint, workers, instruments)
	if err != nil {
		return 0, nil, err
	}
	return published, insertedRows, nil
}

// runDeferredBackfillBatches executes the partitioned per-repository batches with
// a bounded worker pool and accumulates, per (scope, generation) partition, the
// repositories whose evidence committed. The batches are independent (disjoint
// repository sets, idempotent ON CONFLICT writes, per-batch transaction scope),
// so the only shared mutable state is the contribution map and the first-error
// latch, both guarded. The first failing batch cancels the remaining work through
// ctx so a partial pass stops promptly; the deferred maintenance pass is
// idempotent and re-runs converge.
//
// The returned map is the input to PublishDeferredBackfillPartitions. On error it
// is not returned at all: the caller must not publish readiness or a memo row for
// any partition once a batch has failed, because a partition's repositories are
// spread across batches and a survivor's contribution says nothing about whether
// the rest of that partition committed. The second map carries the
// per-partition actually-inserted row counts for the pass's memo-hit
// skip-set revision (issue #7636); it is likewise withheld on error.
func (s Store) runDeferredBackfillBatches(
	ctx context.Context,
	repoIDs []string,
	bounds [][2]int,
	workers int,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	instruments *telemetry.Instruments,
) (map[ScopeGenerationPartition][]string, map[ScopeGenerationPartition]int64, error) {
	return s.RunDeferredBackfillBatchesWith(
		ctx, repoIDs, bounds, workers, evidenceBySourceRepo, snapshotGenerations, instruments,
		LoadAllActiveRepositoryGenerations,
	)
}

// writeDeferredBackfillBatchWith processes one bounded batch of source repositories
// in its own transaction. It acquires the batch's exclusive maintenance locks in
// sorted order, re-reads the active generations under the lock so evidence
// attaches to the generation current at lock time, persists each repository's
// evidence, and commits to release the locks. The batch is idempotent: evidence
// inserts are content-addressed (ON CONFLICT (evidence_id) DO NOTHING), so a
// re-run converges on the same rows.
//
// The batch deliberately publishes NOTHING partition-wide. It returns the
// (scope, generation) partitions it contributed evidence to, mapped to the
// repositories it persisted under each, and the caller's fan-in step publishes
// readiness and the memo once every batch of the pass has committed. A batch is
// a contiguous slice of the repo-ID-sorted corpus, so it generally holds only
// SOME of a partition's repositories; publishing a partition-wide claim from
// here would assert completion on behalf of sibling batches that may still fail.
//
// The second return maps each partition the batch wrote evidence under to the
// number of rows it actually INSERTED (ON CONFLICT DO NOTHING skips do not
// count). The pass's reopen step uses it to revise the memo-hit skip-set:
// a partition that received new rows this pass must reopen even when the fact
// loader skipped it at pass start (issue #7636).
//
// The under-lock generation read is supplied by the caller. The whole pass supplies
// loadAllActiveRepositoryGenerations, the shipped corpus-wide read; the
// partition-scoped pass (#7584) supplies loadActiveRepositoryGenerationsForRepos,
// which returns the same rows for batchRepoIDs and nothing else. Every other
// step -- lock order, generation guard, evidence upsert, commit -- is shared.
func (s Store) writeDeferredBackfillBatchWith(
	ctx context.Context,
	batchRepoIDs []string,
	evidenceBySourceRepo map[string][]relationships.EvidenceFact,
	snapshotGenerations map[string]string,
	loadGenerations RepositoryGenerationLoader,
) (map[ScopeGenerationPartition][]string, map[ScopeGenerationPartition]int64, error) {
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin deferred backfill batch transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	lockKeys := make([]string, 0, len(batchRepoIDs))
	for _, repoID := range batchRepoIDs {
		lockKeys = append(lockKeys, lockstore.DeferredMaintenanceRepoLockKeyFromID(repoID))
	}
	if err := lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, lockKeys); err != nil {
		return nil, nil, fmt.Errorf("acquire deferred backfill batch locks: %w", err)
	}

	currentGenerations, err := loadGenerations(ctx, tx, batchRepoIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("reload active repository generations under batch lock: %w", err)
	}

	if s.NewEvidenceStore == nil {
		return nil, nil, fmt.Errorf("evidence store constructor is required for deferred backfill batch write")
	}
	relationshipStore := s.NewEvidenceStore(tx)
	// contributions records which (scope, generation) partition each repository's
	// evidence landed under, keyed by partition so the caller's fan-in publishes
	// exactly once per partition however many repositories or batches feed it.
	// This is where the old within-batch dedupe of the partition-keyed sinks
	// moved to: readiness and the memo are partition facts, not per-repo facts
	// (graph_projection_phase_state's six-column PK and
	// deferred_backfill_partition_memo's (scope_id, generation_id) PK are both
	// functions of (ScopeID, GenerationID) alone), so N repositories sharing a
	// partition must never contribute N byte-identical conflict keys to one
	// INSERT ... ON CONFLICT DO UPDATE -- Postgres rejects that with SQLSTATE
	// 21000 ("ON CONFLICT DO UPDATE command cannot affect row a second time").
	// Keying by partition here makes that shape unrepresentable rather than
	// merely filtered.
	contributions := make(map[ScopeGenerationPartition][]string, len(batchRepoIDs))
	insertedRows := make(map[ScopeGenerationPartition]int64)
	for _, repoID := range batchRepoIDs {
		repoGeneration, ok := currentGenerations[repoID]
		if !ok {
			log.Printf(
				"relationship_backfill_deferred_source_skipped=true source_repo_id=%q reason=%q",
				repoID,
				"missing_active_generation",
			)
			continue
		}
		// Generation-consistency guard (issue #3725): when a fact load ran
		// (snapshotGenerations is non-nil), skip any repository whose active
		// generation under the batch lock differs from the generation the fact
		// load queried for that scope — or whose scope was absent from the
		// snapshot. Persisting evidence and contributing a publication candidate
		// here would mark the new generation backward-evidence-committed even
		// though its facts were never loaded (the per-scope query rejected the
		// stale generation), reopening deployment mapping with missing
		// relationship evidence. The next periodic
		// RunDeferredRelationshipMaintenance pass re-snapshots and processes the
		// advanced generation. A nil snapshot (no anchors/no partitions) disables
		// the guard so the legacy publish-for-every-active-repository contract
		// holds when no fact load occurred. PublishDeferredBackfillPartitions
		// re-applies the same guard under its own locks, because the generation
		// can advance again between this commit and the fan-in.
		if snapshotGenerations != nil {
			snapshotGen, inSnapshot := snapshotGenerations[repoGeneration.ScopeID]
			if !inSnapshot || snapshotGen != repoGeneration.GenerationID {
				log.Printf(
					"relationship_backfill_deferred_source_skipped=true source_repo_id=%q reason=%q",
					repoID,
					"generation_advanced_since_snapshot",
				)
				continue
			}
		}
		partition := ScopeGenerationPartition{
			ScopeID:      repoGeneration.ScopeID,
			GenerationID: repoGeneration.GenerationID,
		}
		if repoEvidence := evidenceBySourceRepo[repoID]; len(repoEvidence) > 0 {
			inserted, err := relationshipStore.UpsertEvidenceFactsCounted(ctx, repoGeneration.GenerationID, repoEvidence)
			if err != nil {
				return nil, nil, fmt.Errorf("persist deferred relationship evidence for repo %q: %w", repoID, err)
			}
			if inserted > 0 {
				insertedRows[partition] += inserted
			}
		}
		contributions[partition] = append(contributions[partition], repoID)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit deferred backfill batch transaction: %w", err)
	}
	committed = true
	return contributions, insertedRows, nil
}

// ExcludePartitionsWithNewEvidence revises a same-pass memo-hit skip-set with
// the ground truth of what the pass's backfill actually wrote (issue #7636).
// skipped holds the partitions the fact loader skipped at pass start; a
// memo-hit partition can still receive NEW evidence during the pass when
// cross-partition discovery writes under its generation (for example a GCP
// cloud scope with no repository fact of its own). insertedRows maps each
// partition to the rows the pass's backfill actually INSERTED — ON CONFLICT
// DO NOTHING skips do not count — so a re-upsert of already-committed
// evidence never invalidates a skip. Every skipped partition with a positive
// count is excluded; the result is the set whose backward evidence the pass
// provably did NOT change.
//
// A nil skipped (gate failure upstream) stays nil: the reopen-all fallback is
// preserved, not converted into an empty skip-set. The input map is not
// mutated. Indexing a nil insertedRows is safe and excludes nothing.
func ExcludePartitionsWithNewEvidence(
	skipped map[ScopeGenerationPartition]struct{},
	insertedRows map[ScopeGenerationPartition]int64,
) map[ScopeGenerationPartition]struct{} {
	if skipped == nil {
		return nil
	}
	revised := make(map[ScopeGenerationPartition]struct{}, len(skipped))
	unskipped := 0
	for partition := range skipped {
		if insertedRows[partition] > 0 {
			unskipped++
			continue
		}
		revised[partition] = struct{}{}
	}
	log.Printf(
		"deferred_backfill_skip_set_revised skipped_before=%d unskipped_new_evidence=%d skipped_after=%d",
		len(skipped), unskipped, len(revised),
	)
	return revised
}
