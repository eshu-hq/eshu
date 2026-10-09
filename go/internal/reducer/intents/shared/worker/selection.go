// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

const maxSharedSelectionScanLimit = 10_000

// PartitionBatchResult holds the result of selecting one partition batch.
type PartitionBatchResult struct {
	LatestRows  []sharedintent.Row
	BlockedRows []sharedintent.Row
	// TerminalRows are phase-ready rows that are complete with no edge — the
	// handles_route #2809 terminal-no-endpoint set. They are retracted (to clear
	// any stale edge whose endpoint vanished) and marked complete, but never
	// written and never deferred, so a route-only repo cannot stall the backlog.
	TerminalRows []sharedintent.Row
	StaleIDs     []string
	StaleCount   int
	// SupersededGenerationCount is the subset of StaleIDs drained because the
	// intent's generation is superseded (#7121), as opposed to an acceptance
	// mismatch. StaleCount is the total.
	SupersededGenerationCount int
	SupersededIDs             []string
	BlockedCount              int
	TerminalCount             int
	// IndexedSelection is true when candidates were read through the indexed
	// partition predicate rather than the in-memory domain scan. It is a bounded
	// operator signal for diagnosing which selection path a domain used.
	IndexedSelection bool
	// UnhashedFallbackRows counts legacy partition-matched rows from the unhashed
	// lane that were selected into this cycle's candidate batch (after limit
	// truncation). A non-zero value during steady state means pre-hash rows are
	// still draining for the domain.
	UnhashedFallbackRows int
	// SelectionRounds counts the widen passes this selection ran,
	// including the final one (#7724 telemetry: per-visit widen rounds).
	// Every pass issues one candidate query; the acceptance prefetch runs
	// fresh on every pass while the readiness prefetch serves cached
	// answers for keys an earlier pass already resolved.
	SelectionRounds int
	// PrefetchStats accumulates this selection's prefetch behavior:
	// per-kind keys submitted, queries issued, rows returned, readiness
	// cache hits, and store durations (#7724 telemetry: per-visit
	// prefetch keys/queries/rows/cache-hits + durations). Acceptance
	// CacheHits is always zero: acceptance is never cached.
	PrefetchStats sharedintent.PrefetchStats
}

// LatestIntentsByRepoAndPartition deduplicates intents to the most recent per
// bounded acceptance key and partition, matching the Python
// _latest_intents_by_repo_and_partition function.
//
// The surviving rows are ordered refresh-first (is_refresh_intent DESC, then
// created_at ASC, intent_id ASC), the SAME primary ordering the indexed
// candidate SQL and appendUnhashedSharedCandidates emit (#3474). The dedup runs
// AFTER candidate selection but BEFORE SelectPartitionBatch truncates the ready
// set to the batch limit, so a created_at-only sort here re-buries a per-repo
// refresh intent behind older per-edge upsert rows that were enqueued first. The
// refresh then falls past the batch window and is never selected, while its
// fenced per-edge rows defer forever behind a repo-wide retract that never
// commits (#3451). Mirroring the refresh-first primary key guarantees the
// refresh leads the surviving set regardless of its created_at relative to the
// head upsert edges.
func LatestIntentsByRepoAndPartition(intents []sharedintent.Row) ([]sharedintent.Row, []string) {
	if len(intents) == 0 {
		return nil, nil
	}

	sorted := make([]sharedintent.Row, len(intents))
	copy(sorted, intents)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := sharedintent.IsRepoRefreshRow(sorted[i]), sharedintent.IsRepoRefreshRow(sorted[j])
		if ri != rj {
			return ri // refresh rows sort first (true > false)
		}
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
		}
		return sorted[i].IntentID < sorted[j].IntentID
	})

	type repoPartitionKey struct {
		scopeID          string
		acceptanceUnitID string
		sourceRunID      string
		repositoryID     string
		partitionKey     string
	}

	latestByKey := make(map[repoPartitionKey]sharedintent.Row)
	order := make([]repoPartitionKey, 0)
	var supersededIDs []string

	for _, intent := range sorted {
		k := repoPartitionKey{
			scopeID:      intent.ScopeID,
			sourceRunID:  intent.SourceRunID,
			repositoryID: intent.RepositoryID,
			partitionKey: intent.PartitionKey,
		}
		if acceptanceKey, ok := intent.AcceptanceKey(); ok {
			k.scopeID = acceptanceKey.ScopeID
			k.acceptanceUnitID = acceptanceKey.AcceptanceUnitID
			k.sourceRunID = acceptanceKey.SourceRunID
		}
		if prev, ok := latestByKey[k]; ok {
			supersededIDs = append(supersededIDs, prev.IntentID)
		} else {
			order = append(order, k)
		}
		latestByKey[k] = intent
	}

	result := make([]sharedintent.Row, 0, len(order))
	for _, k := range order {
		result = append(result, latestByKey[k])
	}

	return result, supersededIDs
}

// FilterAuthoritativeIntents splits intents into active (matching accepted
// generation) and stale (mismatching generation) sets, matching the Python
// _filter_authoritative_intents function.
func FilterAuthoritativeIntents(
	intents []sharedintent.Row,
	acceptedGen sharedintent.AcceptedGenerationLookup,
) (active []sharedintent.Row, staleIDs []string) {
	for _, intent := range intents {
		key, ok := intent.AcceptanceKey()
		if !ok {
			continue
		}

		accepted, ok := acceptedGen(key)
		if !ok {
			continue
		}
		if intent.GenerationID != accepted {
			staleIDs = append(staleIDs, intent.IntentID)
			continue
		}
		active = append(active, intent)
	}
	return active, staleIDs
}

// SelectPartitionBatch selects one accepted partition batch, matching the
// Python _select_partition_batch function. It scans pending intents, filters
// by partition, checks authoritative generation state, and deduplicates to
// latest per repo/partition pair.
func SelectPartitionBatch(
	ctx context.Context,
	reader IntentReader,
	domain string,
	partitionID, partitionCount int,
	batchLimit int,
	acceptedGen sharedintent.AcceptedGenerationLookup,
	prefetch sharedintent.AcceptedGenerationPrefetch,
	readinessLookup gpphase.ReadinessLookup,
	readinessPrefetch gpphase.ReadinessPrefetch,
	endpointPresence gpphase.EndpointPresenceLookup,
) (PartitionBatchResult, error) {
	if batchLimit < 1 {
		batchLimit = 1
	}

	// Indexed candidate readers (Postgres) return only this partition's pending
	// rows, so the scan never dilutes across partitions and cannot starve at the
	// scan cap. Readers without the candidate interface keep the in-memory
	// domain scan with its widen-and-cap behavior unchanged.
	_, indexed := reader.(PartitionCandidateReader)

	// Prefetch telemetry rides on the context so the frozen prefetch
	// signatures stay unchanged (#7724 1A). Cancellation still flows:
	// the wrapper only adds the stats value.
	var prefetchStats sharedintent.PrefetchStats
	ctx = sharedintent.ContextWithPrefetchStats(ctx, &prefetchStats)

	// Cross-round readiness cache (#7724 1B): each widen round re-scans a
	// superset of the previous window, so rounds share most of their
	// readiness keys. The cache serves the overlap and queries only the
	// delta. Acceptance is deliberately NOT cached: it advances on
	// generation activation, and a stale cached answer can complete a
	// live row as stale. The #7121 drain below receives the raw prefetch
	// for the same reason: its re-read must be fresh.
	roundReadinessPrefetch := readinessPrefetch
	if readinessPrefetch != nil {
		roundReadinessPrefetch = newRoundReadinessCache(readinessPrefetch).prefetch
	}

	scanLimit := batchLimit * max(partitionCount, 1) * 2
	if scanLimit > maxSharedSelectionScanLimit {
		scanLimit = maxSharedSelectionScanLimit
	}

	rounds := 0
	for {
		if err := ctx.Err(); err != nil {
			return PartitionBatchResult{}, err
		}
		rounds++

		partitionRows, loadedCount, unhashedFallback, err := loadPartitionRows(
			ctx, reader, domain, partitionID, partitionCount, scanLimit, indexed,
		)
		if err != nil {
			return PartitionBatchResult{}, err
		}

		seenAll := loadedCount < scanLimit
		if len(partitionRows) == 0 {
			if seenAll {
				return PartitionBatchResult{IndexedSelection: indexed, SelectionRounds: rounds, PrefetchStats: prefetchStats}, nil
			}
			if scanLimit >= maxSharedSelectionScanLimit {
				if indexed {
					return PartitionBatchResult{IndexedSelection: indexed, SelectionRounds: rounds, PrefetchStats: prefetchStats}, nil
				}
				return PartitionBatchResult{}, scanCapError(domain, partitionID, partitionCount)
			}
			scanLimit = widenScanLimit(scanLimit)
			continue
		}

		lookup := acceptedGen
		if prefetch != nil {
			resolvedLookup, err := prefetch(ctx, partitionRows)
			if err != nil {
				return PartitionBatchResult{}, fmt.Errorf("prefetch accepted generations: %w", err)
			}
			lookup = resolvedLookup
		}

		active, mismatchIDs := FilterAuthoritativeIntents(partitionRows, lookup)
		latest, supersededIntentIDs := LatestIntentsByRepoAndPartition(active)
		readyRows, blockedRows, terminalRows, err := FilterRowsByReadiness(
			ctx,
			domain,
			latest,
			readinessLookup,
			roundReadinessPrefetch,
			endpointPresence,
		)
		if err != nil {
			return PartitionBatchResult{}, err
		}

		// Drain only the rows the readiness gate blocked, and only when their
		// scope generation is superseded with no in-flight producer (#7121): that
		// phase row never publishes. The reader omits a superseded generation
		// while its producer is still running, so an in-flight producer defers the
		// drain to a later pass instead of losing the edge. Readiness is re-read
		// for the rows about to drain, after the lookup, so a producer that
		// published between the first readiness read and the lookup keeps its row
		// (it projects). Ready and terminal rows on a superseded generation keep
		// projecting, because a delta successor would never re-emit their edge.
		// The lookup is one bounded round trip over the blocked rows' generation
		// ids and is skipped when nothing is blocked; the re-check adds one more
		// only when there are rows to drain. It receives the RAW readiness
		// prefetch, bypassing the cross-round cache above: serving it from
		// cache would reintroduce the publish-between-reads race this
		// re-read exists to close (#7724 1B, non-negotiable).
		drain, err := drainSupersededBlockedRows(
			ctx, reader, domain, blockedRows,
			readinessLookup, readinessPrefetch, endpointPresence,
		)
		if err != nil {
			return PartitionBatchResult{}, err
		}
		blockedRows = drain.Blocked
		generationSupersededIDs := drain.DrainedIDs
		readyRows = append(readyRows, drain.Ready...)
		terminalRows = append(terminalRows, drain.Terminal...)
		staleIDs := make([]string, 0, len(mismatchIDs)+len(generationSupersededIDs))
		staleIDs = append(staleIDs, mismatchIDs...)
		staleIDs = append(staleIDs, generationSupersededIDs...)

		// Terminal rows are complete with no edge; draining them promptly (rather
		// than widening the scan in search of more ready rows) is what keeps a
		// route-only backlog from stalling, so they count toward returning a batch.
		// Blocked rows drained as superseded are progress for the same reason: a
		// window full of orphans must not widen the scan toward the cap.
		if len(readyRows) >= batchLimit || len(terminalRows) > 0 || len(generationSupersededIDs) > 0 || seenAll {
			if len(readyRows) > batchLimit {
				readyRows = readyRows[:batchLimit]
			}
			return PartitionBatchResult{
				LatestRows:                readyRows,
				BlockedRows:               blockedRows,
				TerminalRows:              terminalRows,
				StaleIDs:                  staleIDs,
				StaleCount:                len(staleIDs),
				SupersededGenerationCount: len(generationSupersededIDs),
				SupersededIDs:             supersededIntentIDs,
				BlockedCount:              len(blockedRows),
				TerminalCount:             len(terminalRows),
				IndexedSelection:          indexed,
				UnhashedFallbackRows:      unhashedFallback,
				SelectionRounds:           rounds,
				PrefetchStats:             prefetchStats,
			}, nil
		}

		if scanLimit >= maxSharedSelectionScanLimit {
			if indexed {
				return PartitionBatchResult{
					LatestRows:                readyRows,
					BlockedRows:               blockedRows,
					TerminalRows:              terminalRows,
					StaleIDs:                  staleIDs,
					StaleCount:                len(staleIDs),
					SupersededGenerationCount: len(generationSupersededIDs),
					SupersededIDs:             supersededIntentIDs,
					BlockedCount:              len(blockedRows),
					TerminalCount:             len(terminalRows),
					IndexedSelection:          indexed,
					UnhashedFallbackRows:      unhashedFallback,
					SelectionRounds:           rounds,
					PrefetchStats:             prefetchStats,
				}, nil
			}
			return PartitionBatchResult{}, scanCapError(domain, partitionID, partitionCount)
		}
		scanLimit = widenScanLimit(scanLimit)
	}
}
