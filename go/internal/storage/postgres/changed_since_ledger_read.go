// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ledgerReadChainBudget caps the stored delta rows one ledger read
// composes. It defaults to the provisional theory-proof budget and is a var
// only so tests can plant a tiny bound; production never changes it.
var ledgerReadChainBudget = int64(linksfreshnessstore.ChainDeltaRowBudget)

// ledgerCursor is the scope cursor a ledger read validates against.
type ledgerCursor struct {
	state  string
	seq    int64
	digest int64
}

// tryLedgerRead answers the prior-to-current diff from the link ledger
// when a link chain covers the window. When the queryer opens a read-only
// repeatable-read transaction, the whole attempt runs inside one: the
// cursor, chain, deltas, tombstone probe and unchanged samples then share
// one snapshot, so a writer or retention commit landing mid-read is
// invisible instead of skewing the composed rows. Otherwise the attempt
// runs statement by statement, with the cursor recheck and the delta-row
// completeness check as best-effort guards. Either way it returns
// served=false with the fallback reason when the legacy statement should
// answer instead; the wire contract never changes, so a fallback still
// returns exact rows. A ledger error falls back too (and is logged and
// counted), never partial rows.
func (s StatusStore) tryLedgerRead(
	ctx context.Context,
	scopeID, priorGenerationID, currentGenerationID string,
	sampleLimit int,
) ([]changedsince.CategoryDelta, linksfreshnessstore.ReadOutcome, bool) {
	if beginner, ok := s.queryer.(db.ReadOnlyRepeatableReadBeginner); ok {
		tx, err := beginner.BeginReadOnlyRepeatableRead(ctx)
		if err != nil {
			return nil, linksfreshnessstore.ReadOutcome{
				Fallback: s.ledgerReadError(ctx, scopeID, currentGenerationID, err),
			}, false
		}
		defer func() { _ = tx.Rollback() }()
		snapshot := s
		snapshot.queryer = tx
		return snapshot.tryLedgerReadInSnapshot(ctx, scopeID, priorGenerationID, currentGenerationID, sampleLimit)
	}
	return s.tryLedgerReadInSnapshot(ctx, scopeID, priorGenerationID, currentGenerationID, sampleLimit)
}

// tryLedgerReadInSnapshot runs one ledger-read attempt over queryer, which
// is either a single repeatable-read snapshot or the store's own queryer.
func (s StatusStore) tryLedgerReadInSnapshot(
	ctx context.Context,
	scopeID, priorGenerationID, currentGenerationID string,
	sampleLimit int,
) ([]changedsince.CategoryDelta, linksfreshnessstore.ReadOutcome, bool) {
	unserved := func(reason linksfreshnessstore.ReadFallbackReason) ([]changedsince.CategoryDelta, linksfreshnessstore.ReadOutcome, bool) {
		return nil, linksfreshnessstore.ReadOutcome{Fallback: reason}, false
	}
	if priorGenerationID == currentGenerationID {
		return unserved(linksfreshnessstore.ReadFallbackSameGeneration)
	}
	cursor, found, err := s.readLedgerCursor(ctx, scopeID)
	if err != nil {
		return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
	}
	if !found || cursor.state == "" {
		return unserved(linksfreshnessstore.ReadFallbackCurrentUnlinked)
	}
	if cursor.digest != linksfreshnessstore.DigestVersion {
		return unserved(linksfreshnessstore.ReadFallbackDigestMismatch)
	}
	if cursor.state != currentGenerationID {
		return unserved(linksfreshnessstore.ReadFallbackCursorMoved)
	}
	links, err := s.readLedgerChain(ctx, scopeID, priorGenerationID, currentGenerationID)
	if err != nil {
		return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
	}
	if len(links) == 0 {
		linked, err := s.currentGenerationLinked(ctx, scopeID, currentGenerationID)
		if err != nil {
			return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
		}
		if !linked {
			return unserved(linksfreshnessstore.ReadFallbackCurrentUnlinked)
		}
		return unserved(linksfreshnessstore.ReadFallbackPriorOffChain)
	}
	if err := validateLedgerChain(links, priorGenerationID, currentGenerationID); err != nil {
		return unserved(linksfreshnessstore.ReadFallbackPriorOffChain)
	}
	var deltaTotal int64
	for _, link := range links {
		if link.digest != linksfreshnessstore.DigestVersion {
			return unserved(linksfreshnessstore.ReadFallbackDigestMismatch)
		}
		deltaTotal += link.deltaRows
	}
	if deltaTotal > ledgerReadChainBudget {
		return unserved(linksfreshnessstore.ReadFallbackChainTooLarge)
	}
	deltasByLink := make([][]ledgerDelta, len(links))
	for i, link := range links {
		deltas, err := s.readLedgerLinkDeltas(ctx, scopeID, link)
		if err != nil {
			return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
		}
		if int64(len(deltas)) != link.deltaRows {
			return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID,
				fmt.Errorf("changed-since ledger read: link %s->%s holds %d delta rows, want %d",
					link.priorID, link.generationID, len(deltas), link.deltaRows)))
		}
		deltasByLink[i] = deltas
	}
	touches := composeLedgerTouches(deltasByLink)
	entries, removed := composeLedgerEntries(touches)
	if len(removed) > 0 {
		probes, err := s.probeLedgerTombstones(ctx, scopeID, currentGenerationID, removed)
		if err != nil {
			return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
		}
		entries = append(entries, classifyLedgerRemoved(touches, removed, probes)...)
	}
	sortLedgerEntries(entries)
	samples, err := s.readLedgerUnchangedSamples(ctx, scopeID, links, sampleLimit)
	if err != nil {
		return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
	}
	ending, found, err := s.readLedgerCursor(ctx, scopeID)
	if err != nil {
		return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID, err))
	}
	if !found || ending != cursor {
		return unserved(linksfreshnessstore.ReadFallbackCursorMoved)
	}
	categories, ok := assembleLedgerCategories(entries, samples, links[len(links)-1], sampleLimit)
	if !ok {
		return unserved(s.ledgerReadError(ctx, scopeID, currentGenerationID,
			fmt.Errorf("changed-since ledger read: unchanged count below zero")))
	}
	return categories, linksfreshnessstore.ReadOutcome{Served: true, Links: len(links)}, true
}

// validateLedgerChain checks the walked links connect the requested prior
// to the current generation with one row per step.
func validateLedgerChain(links []ledgerLink, priorGenerationID, currentGenerationID string) error {
	if len(links) == 0 || len(links) > linksfreshnessstore.ChainMaxLinks {
		return fmt.Errorf("chain length %d", len(links))
	}
	previous := priorGenerationID
	for _, link := range links {
		if link.priorID != previous {
			return fmt.Errorf("chain gap at %s", link.generationID)
		}
		previous = link.generationID
	}
	if previous != currentGenerationID {
		return fmt.Errorf("chain ends at %s, want %s", previous, currentGenerationID)
	}
	return nil
}

// readLedgerCursor reads the scope's ledger cursor: the generation its key
// state describes, the activation sequence, and the digest version.
func (s StatusStore) readLedgerCursor(ctx context.Context, scopeID string) (ledgerCursor, bool, error) {
	rows, err := s.queryer.QueryContext(ctx, linksfreshnessstore.ReadCursorQuery, scopeID)
	if err != nil {
		return ledgerCursor{}, false, fmt.Errorf("read ledger cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return ledgerCursor{}, false, rows.Err()
	}
	var cursor ledgerCursor
	var state string
	if err := rows.Scan(&state, &cursor.seq, &cursor.digest); err != nil {
		return ledgerCursor{}, false, fmt.Errorf("read ledger cursor: %w", err)
	}
	cursor.state = state
	return cursor, true, rows.Err()
}

// readLedgerChain walks the link chain from the prior toward the current
// generation, in chain order.
func (s StatusStore) readLedgerChain(ctx context.Context, scopeID, priorGenerationID, currentGenerationID string) ([]ledgerLink, error) {
	rows, err := s.queryer.QueryContext(ctx, linksfreshnessstore.ReadChainQuery, scopeID, priorGenerationID, currentGenerationID)
	if err != nil {
		return nil, fmt.Errorf("read ledger chain: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var links []ledgerLink
	for rows.Next() {
		var link ledgerLink
		var kind string
		var files, entities, facts int64
		if err := rows.Scan(&link.generationID, &link.priorID, &kind,
			&link.digest, &link.deltaRows, &files, &entities, &facts); err != nil {
			return nil, fmt.Errorf("read ledger chain: %w", err)
		}
		link.kind = kind
		link.liveKeys = map[changedsince.Category]int{
			changedsince.CategoryFiles:           int(files),
			changedsince.CategoryContentEntities: int(entities),
			changedsince.CategoryFacts:           int(facts),
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// currentGenerationLinked reports whether the current generation has any
// link row, to tell an unlinked current generation from a broken chain.
func (s StatusStore) currentGenerationLinked(ctx context.Context, scopeID, currentGenerationID string) (bool, error) {
	rows, err := s.queryer.QueryContext(ctx, linksfreshnessstore.ReadCurrentLinkExistsQuery, scopeID, currentGenerationID)
	if err != nil {
		return false, fmt.Errorf("read current link: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := rows.Next()
	return found, rows.Err()
}

// readLedgerLinkDeltas reads every stored delta row of one link by its full
// three-column key.
func (s StatusStore) readLedgerLinkDeltas(ctx context.Context, scopeID string, link ledgerLink) ([]ledgerDelta, error) {
	rows, err := s.queryer.QueryContext(ctx, linksfreshnessstore.ReadLinkDeltasQuery,
		scopeID, link.generationID, link.priorID)
	if err != nil {
		return nil, fmt.Errorf("read ledger link deltas: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var deltas []ledgerDelta
	for rows.Next() {
		var delta ledgerDelta
		var category, classification, key string
		var tombstoned bool
		var priorState, currentState []byte
		if err := rows.Scan(&category, &classification, &key,
			&delta.priorKind, &delta.currentKind, &priorState, &currentState, &tombstoned); err != nil {
			return nil, fmt.Errorf("read ledger link deltas: %w", err)
		}
		delta.category, delta.classification, delta.key = category, classification, key
		delta.priorState, delta.currentState, delta.tombstoned = priorState, currentState, tombstoned
		deltas = append(deltas, delta)
	}
	return deltas, rows.Err()
}

// probeLedgerTombstones returns the final generation's tombstone fact_kinds
// per removed key, so the caller can tell retired from superseded exactly as
// the legacy tombstone join does.
func (s StatusStore) probeLedgerTombstones(
	ctx context.Context,
	scopeID, currentGenerationID string,
	removed []string,
) (map[string][]string, error) {
	rows, err := s.queryer.QueryContext(ctx, linksfreshnessstore.ReadTombstoneProbeQuery,
		scopeID, currentGenerationID, array.Of(removed))
	if err != nil {
		return nil, fmt.Errorf("probe ledger tombstones: %w", err)
	}
	defer func() { _ = rows.Close() }()
	probes := map[string][]string{}
	for rows.Next() {
		var key, kind string
		if err := rows.Scan(&key, &kind); err != nil {
			return nil, fmt.Errorf("probe ledger tombstones: %w", err)
		}
		probes[key] = append(probes[key], kind)
	}
	return probes, rows.Err()
}

// readLedgerUnchangedSamples reads the live keys of every category outside
// the composed changed set, ordered exactly as the legacy samples.
func (s StatusStore) readLedgerUnchangedSamples(
	ctx context.Context,
	scopeID string,
	links []ledgerLink,
	sampleLimit int,
) (map[changedsince.Category][]changedsince.Sample, error) {
	query, err := linksfreshnessstore.BuildChainUnchangedSamplesQuery(len(links))
	if err != nil {
		return nil, err
	}
	samples := map[changedsince.Category][]changedsince.Sample{}
	for _, category := range changedsince.Categories {
		args := make([]any, 0, 2*len(links)+3)
		args = append(args, scopeID, string(category))
		for _, link := range links {
			args = append(args, link.generationID, link.priorID)
		}
		args = append(args, sampleLimit+1)
		rows, err := s.queryer.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("read ledger unchanged samples: %w", err)
		}
		var categorySamples []changedsince.Sample
		for rows.Next() {
			var key, kind string
			if err := rows.Scan(&key, &kind); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("read ledger unchanged samples: %w", err)
			}
			categorySamples = append(categorySamples, changedsince.Sample{StableFactKey: key, FactKind: kind})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read ledger unchanged samples: %w", err)
		}
		_ = rows.Close()
		if len(categorySamples) > 0 {
			samples[category] = categorySamples
		}
	}
	return samples, nil
}

// assembleLedgerCategories builds the per-category deltas from the composed
// changed entries, the unchanged samples, and the final link's live totals.
// Unchanged counts are live minus composed added and updated, mirroring the
// G1 oracle (keys minus changed); a negative total means the ledger
// disagrees with itself and falls back.
func assembleLedgerCategories(
	entries []ledgerComposedEntry,
	unchanged map[changedsince.Category][]changedsince.Sample,
	final ledgerLink,
	sampleLimit int,
) ([]changedsince.CategoryDelta, bool) {
	changed := map[changedsince.Category]map[changedsince.Classification][]changedsince.Sample{}
	for _, entry := range entries {
		if entry.classification == changedsince.Unchanged {
			continue
		}
		if changed[entry.category] == nil {
			changed[entry.category] = map[changedsince.Classification][]changedsince.Sample{}
		}
		changed[entry.category][entry.classification] = append(
			changed[entry.category][entry.classification],
			changedsince.Sample{StableFactKey: entry.key, FactKind: entry.kind})
	}
	categories := make([]changedsince.CategoryDelta, 0, len(changedsince.Categories))
	for _, category := range changedsince.Categories {
		delta := changedsince.CategoryDelta{Category: category}
		truncated := map[changedsince.Classification]bool{}
		for classification, bucket := range changed[category] {
			applyChangedSinceCount(&delta.Counts, string(classification), len(bucket))
			if len(bucket) > sampleLimit {
				bucket = bucket[:sampleLimit]
				truncated[classification] = true
				changed[category][classification] = bucket
			}
		}
		added := changedSinceClassificationCount(delta.Counts, changedsince.Added)
		updated := changedSinceClassificationCount(delta.Counts, changedsince.Updated)
		unchangedCount := final.liveKeys[category] - added - updated
		if unchangedCount < 0 {
			return nil, false
		}
		delta.Counts.Unchanged = unchangedCount
		samples := changed[category]
		if samples == nil {
			samples = map[changedsince.Classification][]changedsince.Sample{}
		}
		unchangedSamples := unchanged[category]
		if len(unchangedSamples) > sampleLimit {
			unchangedSamples = unchangedSamples[:sampleLimit]
			truncated[changedsince.Unchanged] = true
		}
		if len(unchangedSamples) > 0 {
			samples[changedsince.Unchanged] = unchangedSamples
		}
		if len(samples) > 0 {
			delta.Samples = samples
		}
		if len(truncated) > 0 {
			delta.Truncated = truncated
		}
		categories = append(categories, delta)
	}
	return categories, true
}

// ledgerReadError logs a ledger miss and maps it to the ledger_error
// fallback. A ledger read never fails a request: the legacy statement
// answers instead.
func (s StatusStore) ledgerReadError(ctx context.Context, scopeID, currentGenerationID string, err error) linksfreshnessstore.ReadFallbackReason {
	slog.WarnContext(ctx, "changed-since ledger read fell back to the legacy diff",
		slog.String("scope_id", scopeID),
		slog.String("current_generation_id", currentGenerationID),
		slog.String("reason", string(linksfreshnessstore.ReadFallbackLedgerError)),
		slog.String("error", err.Error()))
	return linksfreshnessstore.ReadFallbackLedgerError
}

// recordChangedSinceLedgerRead counts one ledger-read attempt by outcome.
// It is nil-safe: stores without instruments record nothing.
func (s StatusStore) recordChangedSinceLedgerRead(ctx context.Context, outcome linksfreshnessstore.ReadOutcome) {
	if s.Instruments == nil || s.Instruments.ChangedSinceLedgerReads == nil {
		return
	}
	s.Instruments.ChangedSinceLedgerReads.Add(context.WithoutCancel(ctx), 1,
		metric.WithAttributes(telemetry.AttrOutcome(outcome.Outcome())))
}
