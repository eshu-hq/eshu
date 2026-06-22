package reducer

import (
	"context"
	"fmt"
	"sort"
)

// loadPartitionRows returns the pending rows for one leased partition. On the
// indexed path the rows are already partition-scoped by the candidate reader;
// on the legacy path the full domain head slice is filtered in memory. The
// returned loadedCount is the number of rows the reader returned for the current
// scan window, used to decide whether the whole pending set has been seen.
func loadPartitionRows(
	ctx context.Context,
	reader SharedIntentReader,
	domain string,
	partitionID, partitionCount, scanLimit int,
	indexed bool,
) (partitionRows []SharedProjectionIntentRow, loadedCount, unhashedFallback int, err error) {
	if indexed {
		rows, matched, atLimit, _, err := sharedPartitionCandidates(ctx, reader, domain, partitionID, partitionCount, scanLimit)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("list partition candidates: %w", err)
		}
		// When either candidate lane returned a full window, more pending rows
		// may exist in the database for this partition. Report a full loadedCount
		// so SelectPartitionBatch keeps widening instead of treating the partition
		// as drained, which would slow unhashed backlog drain during a migration.
		loadedCount := len(rows)
		if atLimit {
			loadedCount = scanLimit
		}
		return rows, loadedCount, matched, nil
	}

	pending, err := reader.ListPendingDomainIntents(ctx, domain, scanLimit)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("list pending intents: %w", err)
	}
	return RowsForPartition(pending, partitionID, partitionCount), len(pending), 0, nil
}

func widenScanLimit(scanLimit int) int {
	next := scanLimit * 2
	if next > maxSharedSelectionScanLimit {
		next = maxSharedSelectionScanLimit
	}
	return next
}

func scanCapError(domain string, partitionID, partitionCount int) error {
	return fmt.Errorf(
		"shared partition selection reached scan cap (%d) for domain %q partition %d/%d",
		maxSharedSelectionScanLimit,
		domain,
		partitionID,
		partitionCount,
	)
}

// SharedProjectionPartitionCandidateReader reads pending shared projection rows
// whose stored partition hash belongs to one leased worker partition. It lets
// the generic shared projection runner select candidates with an indexed
// partition predicate instead of scanning pending rows by domain and filtering
// partition membership in memory. It is a selection optimization only; selected
// work is still gated by acceptance generation and readiness before any graph
// write.
type SharedProjectionPartitionCandidateReader interface {
	ListPendingDomainPartitionIntents(
		ctx context.Context,
		domain string,
		partitionID int,
		partitionCount int,
		limit int,
	) ([]SharedProjectionIntentRow, error)
}

// SharedProjectionUnhashedCandidateReader reads pending legacy shared projection
// rows that were inserted before partition_hash was stored. Those rows have a
// NULL partition hash and must be partition-filtered in memory so a migration
// window cannot silently strand pre-hash work.
type SharedProjectionUnhashedCandidateReader interface {
	ListPendingDomainUnhashedIntents(
		ctx context.Context,
		domain string,
		limit int,
	) ([]SharedProjectionIntentRow, error)
}

// sharedPartitionCandidates loads pending intents for one leased partition using
// an indexed partition predicate when reader supports it. It returns the
// partition-scoped rows, the number of legacy unhashed rows that were
// partition-matched in memory, and indexed=true when the indexed path was used.
// When reader does not implement SharedProjectionPartitionCandidateReader it
// returns indexed=false and leaves selection to the legacy domain scan so
// non-Postgres readers keep working unchanged.
func sharedPartitionCandidates(
	ctx context.Context,
	reader SharedIntentReader,
	domain string,
	partitionID, partitionCount, limit int,
) (rows []SharedProjectionIntentRow, unhashedMatched int, atLimit, indexed bool, err error) {
	candidateReader, ok := reader.(SharedProjectionPartitionCandidateReader)
	if !ok {
		return nil, 0, false, false, nil
	}

	hashed, err := candidateReader.ListPendingDomainPartitionIntents(ctx, domain, partitionID, partitionCount, limit)
	if err != nil {
		return nil, 0, false, true, err
	}
	hashedAtLimit := limit > 0 && len(hashed) >= limit

	merged, matched, unhashedAtLimit, err := appendUnhashedSharedCandidates(ctx, reader, hashed, domain, partitionID, partitionCount, limit)
	if err != nil {
		return nil, 0, false, true, err
	}
	return merged, matched, hashedAtLimit || unhashedAtLimit, true, nil
}

// appendUnhashedSharedCandidates merges partition-matched legacy unhashed rows
// into the indexed candidate set, preserving created_at/intent_id ordering and
// the requested limit. Rows are deduplicated by intent id so a row re-upserted
// from the unhashed lane into the hashed lane is never counted twice. The
// returned matched count reflects only the unhashed rows that survive the limit
// truncation, and atLimit reports whether the unhashed query filled its window
// (more legacy rows may remain in the database).
func appendUnhashedSharedCandidates(
	ctx context.Context,
	reader SharedIntentReader,
	hashed []SharedProjectionIntentRow,
	domain string,
	partitionID, partitionCount, limit int,
) (merged []SharedProjectionIntentRow, matched int, atLimit bool, err error) {
	unhashedReader, ok := reader.(SharedProjectionUnhashedCandidateReader)
	if !ok {
		return hashed, 0, false, nil
	}

	legacy, err := unhashedReader.ListPendingDomainUnhashedIntents(ctx, domain, limit)
	if err != nil {
		return nil, 0, false, err
	}
	atLimit = limit > 0 && len(legacy) >= limit
	if len(legacy) == 0 {
		return hashed, 0, atLimit, nil
	}

	seen := make(map[string]struct{}, len(hashed))
	for _, row := range hashed {
		seen[row.IntentID] = struct{}{}
	}

	unhashedIDs := make(map[string]struct{})
	merged = hashed
	for _, row := range RowsForPartition(legacy, partitionID, partitionCount) {
		if _, dup := seen[row.IntentID]; dup {
			continue
		}
		seen[row.IntentID] = struct{}{}
		unhashedIDs[row.IntentID] = struct{}{}
		merged = append(merged, row)
	}
	if len(unhashedIDs) == 0 {
		return hashed, 0, atLimit, nil
	}

	// Preserve the refresh-first primary ordering across the merged set so a
	// refresh intent emitted later than its paired upsert edges is not buried
	// behind those edges after the UNION of hashed + unhashed lanes (#3474).
	// Mirror: ORDER BY is_refresh_intent DESC, created_at ASC, intent_id ASC.
	sort.SliceStable(merged, func(i, j int) bool {
		ri, rj := isRepoRefreshRow(merged[i]), isRepoRefreshRow(merged[j])
		if ri != rj {
			return ri // refresh rows sort first (true > false)
		}
		if !merged[i].CreatedAt.Equal(merged[j].CreatedAt) {
			return merged[i].CreatedAt.Before(merged[j].CreatedAt)
		}
		return merged[i].IntentID < merged[j].IntentID
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}

	// Count only the unhashed rows that survived truncation so the
	// UnhashedFallbackRows signal reflects what this cycle actually selected.
	for _, row := range merged {
		if _, ok := unhashedIDs[row.IntentID]; ok {
			matched++
		}
	}
	return merged, matched, atLimit, nil
}
