// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file holds the over-limit chunk query runners (#7981, #10020). They
// lived in generation_retention_row_counts_sql.go until #8300 split that
// file over the 500-line cap: the page reader sizes the batch's deletes,
// and the prune counter scans one chunk prune into its destinations. The
// deletes run in committed #10020 predrain batches before the prune
// transaction opens; no page ever drains under the narrow lock.

// queryChunkPage reads one oldest-first #7981 over-limit fact page, a bounded
// read sizing the batch's deletes like the counts above. The page's deletes
// run in committed #10020 predrain batches before the prune transaction
// opens; no page ever drains under the narrow lock.
func queryChunkPage(
	ctx context.Context,
	tx db.Transaction,
	candidate generationRetentionCandidate,
	chunkRows int,
) (retentionChunkPage, error) {
	rows, err := tx.QueryContext(ctx, generationRetentionChunkPageQuery,
		candidate.scopeID, candidate.generationID, chunkRows)
	if err != nil {
		return retentionChunkPage{}, fmt.Errorf("generation retention: page chunk facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var page retentionChunkPage
	for rows.Next() {
		var id, kind, repoID, path, entityID string
		var tombstone bool
		if err := rows.Scan(&id, &kind, &tombstone, &repoID, &path, &entityID); err != nil {
			return retentionChunkPage{}, fmt.Errorf("generation retention: scan chunk page: %w", err)
		}
		page.ids = append(page.ids, id)
		switch {
		case !tombstone && kind == "file" && repoID != "" && path != "":
			page.fileRepos = append(page.fileRepos, repoID)
			page.filePaths = append(page.filePaths, path)
		case !tombstone && kind == "content_entity" && repoID != "" && entityID != "":
			page.entityRepos = append(page.entityRepos, repoID)
			page.entityIDs = append(page.entityIDs, entityID)
		}
	}
	if err := rows.Err(); err != nil {
		return retentionChunkPage{}, fmt.Errorf("generation retention: page chunk facts: %w", err)
	}
	return page, nil
}

// queryChunkPruneCounts runs one chunk content prune and scans its one
// count row into the given destinations.
func queryChunkPruneCounts(
	ctx context.Context,
	tx db.Transaction,
	statement string,
	keysA, keysB []string,
	counts ...*int64,
) error {
	rows, err := tx.QueryContext(ctx, statement, keysA, keysB)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	values := make([]any, len(counts))
	for i, count := range counts {
		values[i] = count
	}
	if !rows.Next() {
		return fmt.Errorf("generation retention: chunk prune returned no count row")
	}
	if err := rows.Scan(values...); err != nil {
		return fmt.Errorf("generation retention: scan chunk prune counts: %w", err)
	}
	return rows.Err()
}

// Progressive counting window (#10525 PR-2): bounded oldest-first batch
// sizing for retention, next to the chunk-target diversion for counts above.

// progressivePlanningWindowOwnFactsCap bounds the initial planning-count
// window (#10525 PR-2): the maximal oldest-first prefix whose cumulative
// prescreen own-facts stay within one default batch budget. Own facts are a
// subset of the rows outside the ledger and every count arm is non-negative,
// so the prefix estimates at most one batch; the extension loop below still
// admits a batch the estimate undershoots.
const progressivePlanningWindowOwnFactsCap = int64(defaultGenerationRetentionRowLimit)

// progressivePlanningWindow returns the end offset of the initial counting
// window over countable (oldest first): the maximal prefix whose cumulative
// prescreen own-facts stay within the cap, always at least one generation so
// an over-budget head still makes progress. Generations missing from ownFacts
// count as zero, as in prescreenOwnFacts.
func progressivePlanningWindow(countable []generationRetentionCandidate, ownFacts map[string]int64) int {
	if len(countable) == 0 {
		return 0
	}
	end := 1
	var cumulative int64
	for i, candidate := range countable {
		cumulative += ownFacts[candidate.generationID]
		if cumulative > progressivePlanningWindowOwnFactsCap {
			return max(i, 1)
		}
		end = i + 1
	}
	return end
}

// progressivePlanningCount plans the batch with a bounded window of counts
// (#10525 PR-2, arbiter verdict A): the planning count is a batch-choosing
// heuristic, and the re-check on the final window plus the re-lock's
// mandatory recount stay the prune authorities.
//
// It counts the initial window, then doubles it (capped at the countable
// set) and recounts the whole prefix, discarding the narrower counts: key
// attribution changes with the counted set, so a narrower count must never
// be reused for a wider window. It extends while the fit selects nothing
// with candidates left (liveness: an over-limit head never stalls the tail)
// or while nothing is skipped with the total under the limit (room left),
// and stops on a skip, on the limit, or on exhaustion. Every window count
// runs unlocked, where selectPrunableCandidates calls this; ScopeLockHold
// still spans only the re-lock onward.
//
// The intermediate fits are heuristic only and never tally: only the final
// recheckRetentionBatch, unchanged, tallies the final window's skips.
func (s GenerationRetentionStore) progressivePlanningCount(
	ctx context.Context,
	tx db.Transaction,
	countable []generationRetentionCandidate,
	ownFacts map[string]int64,
	rowLimit int,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, error) {
	end := progressivePlanningWindow(countable, ownFacts)
	for {
		window := countable[:end]
		phaseStart := time.Now()
		_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(window), retentionGenerationIDs(window))
		result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
		if err != nil {
			return nil, nil, nil, err
		}
		selected, selectedRows, _, skipped := selectCandidatesWithinRowLimit(window, eventRowCounts, rowLimit)
		if end >= len(countable) || (len(selected) > 0 &&
			(len(skipped) > 0 || generationRetentionRowsTotal(selectedRows) >= int64(rowLimit))) {
			return s.recheckRetentionBatch(ctx, tx, window, eventRowCounts, rowLimit, result)
		}
		end = min(end*2, len(countable))
	}
}
