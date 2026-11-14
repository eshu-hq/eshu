// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// RepositoryManifestFreshness is the generation state behind one
// repository-scoped manifest dependency read. A repository with an active
// generation but no manifest rows returns an empty page with ScopeFound true
// and the active generation identified, not an error; an unknown scope
// returns the same empty page shape with ScopeFound false.
type RepositoryManifestFreshness struct {
	ScopeFound           bool
	ActiveGenerationID   string
	GenerationStatus     string
	GenerationObservedAt time.Time
}

// repositoryManifestFreshnessQuery resolves the freshness behind one
// repository-scoped read: the scope's active generation and that
// generation's status. It runs inside the read's snapshot, so the freshness
// and the rows observe the same generations.
const repositoryManifestFreshnessQuery = `
SELECT COALESCE(scope.active_generation_id, ''),
       COALESCE(generation.status, ''),
       generation.observed_at
FROM ingestion_scopes AS scope
LEFT JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
WHERE scope.scope_id = $1
`

// ListActiveRepositoryManifestDependencyFacts loads one bounded page of the
// active, non-tombstoned git manifest dependency entities of one repository
// (#10660), for query serving. It reads the same active-generation rows
// through the same delta-overlay snapshot as the reducer's pair-probe read:
// the active page and the derived base pages share the caller's keyset
// cursor and fetch bound, overlay with the refusal-aware rule, and merge in
// (observed_at, fact_id) order.
//
// fetchLimit is the store's +1 lookahead bound: the caller passes its page
// limit plus one and derives truncation from the raw fetched count, never
// refetching. The read is refusal-aware like the retracting consumers'
// (#10109): a refused base fails with ManifestDeltaBaseRefusedError and
// refused true, never a silent delta-only page. A failed retention-probe
// statement fails with refused false so the caller retries.
func (s FactStore) ListActiveRepositoryManifestDependencyFacts(
	ctx context.Context,
	scopeID string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	fetchLimit int,
) ([]facts.Envelope, RepositoryManifestFreshness, bool, error) {
	var freshness RepositoryManifestFreshness
	if s.database == nil {
		return nil, freshness, false, fmt.Errorf("fact store database is required")
	}
	if scopeID == "" {
		return nil, freshness, false, nil
	}
	if fetchLimit < 1 {
		return nil, freshness, false, fmt.Errorf("repository manifest dependency fetch limit is required")
	}

	rawQuery := listActiveRepositoryManifestDependencyFactsQuery
	baseQuery, err := deriveDeltaOverlayBaseQuery(rawQuery, repositoryManifestDeltaOverlayScopeParam)
	if err != nil {
		return nil, freshness, false, err
	}
	if rawQuery, err = s.protectedImpactStaticFactSQL(rawQuery); err != nil {
		return nil, freshness, false, err
	}
	if baseQuery, err = s.protectedImpactStaticFactSQL(baseQuery); err != nil {
		return nil, freshness, false, err
	}
	// One read-only repeatable-read snapshot keeps the active page, the
	// delta-target lookup, the base page, the freshness lookup, and the
	// overlay inputs on the same active generations (#9991).
	started := time.Now()
	var loaded []facts.Envelope
	var refusals []deltaOverlayRefusal
	activeRows := 0
	sink := s.manifestDeltaOverlayTelemetry(manifestDependencyReadLabel(rawQuery))
	err = s.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		reader := snapshot.store
		active, err := reader.listActiveRepositoryManifestDependencyFactsPage(
			ctx, rawQuery, scopeID, cursorObservedAt, cursorFactID, fetchLimit)
		if err != nil {
			return err
		}
		activeRows = len(active)
		targets, err := snapshot.deltaTargets(ctx)
		if err != nil {
			reader.recordDeltaOverlayFailure(ctx, sink)
			return err
		}
		var base []facts.Envelope
		if baseGenerations, scan := deltaOverlayBaseGenerations(targets); scan {
			base, err = reader.listActiveRepositoryManifestDependencyFactsPage(
				ctx, baseQuery, scopeID, cursorObservedAt, cursorFactID, fetchLimit, baseGenerations)
			if err != nil {
				reader.recordDeltaOverlayFailure(ctx, sink)
				return err
			}
		}
		freshness, err = reader.repositoryManifestFreshness(ctx, scopeID)
		if err != nil {
			return err
		}
		loaded, refusals, err = reader.overlayDeltaBaseWithRefusals(ctx, sink, targets, active, base)
		return err
	})
	if err != nil {
		return nil, freshness, false, err
	}
	s.recordManifestDependencyRead(ctx, rawQuery, started, activeRows)
	// The refusing read never serves delta-only rows: a retracting consumer
	// must fail before its retract, and a serving read must not present a
	// partial union as the repository's complete manifest. A retryable probe
	// failure returns refused false so the caller retries instead of
	// treating it as a lost base.
	if rerr := manifestDeltaOverlayRefusal(manifestDependencyReadLabel(rawQuery), refusals); rerr != nil {
		var refusal *ManifestDeltaBaseRefusedError
		if errors.As(rerr, &refusal) {
			return nil, freshness, true, rerr
		}
		return nil, freshness, false, rerr
	}
	if len(loaded) > fetchLimit {
		loaded = loaded[:fetchLimit]
	}
	return loaded, freshness, false, nil
}

// listActiveRepositoryManifestDependencyFactsPage runs one repository-scoped
// manifest dependency page: the active text's own cursor and fetch bound,
// with the candidate base generation ids as $5 onward. The caller caps the
// overlay-merged rows at its fetch bound.
func (s FactStore) listActiveRepositoryManifestDependencyFactsPage(
	ctx context.Context,
	query string,
	scopeID string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	fetchLimit int,
	extra ...any,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	args := append([]any{
		manifestDependencyQueryExecMode,
		scopeID,
		cursor,
		cursorFactID,
		fetchLimit,
	}, extra...)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active repository manifest dependency facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, fetchLimit)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active repository manifest dependency facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
		if len(loaded) >= fetchLimit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active repository manifest dependency facts: %w", err)
	}
	return loaded, nil
}

// repositoryManifestFreshness resolves the freshness behind one
// repository-scoped read on the read's snapshot. An unknown scope returns
// the zero freshness with ScopeFound false.
func (s FactStore) repositoryManifestFreshness(ctx context.Context, scopeID string) (RepositoryManifestFreshness, error) {
	var freshness RepositoryManifestFreshness
	rows, err := s.database.QueryContext(ctx, repositoryManifestFreshnessQuery, manifestDependencyQueryExecMode, scopeID)
	if err != nil {
		return freshness, fmt.Errorf("repository manifest freshness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return freshness, rows.Err()
	}
	var observedAt sql.NullTime
	if err := rows.Scan(&freshness.ActiveGenerationID, &freshness.GenerationStatus, &observedAt); err != nil {
		return freshness, fmt.Errorf("repository manifest freshness: %w", err)
	}
	if err := rows.Err(); err != nil {
		return freshness, fmt.Errorf("repository manifest freshness: %w", err)
	}
	freshness.ScopeFound = true
	if observedAt.Valid {
		freshness.GenerationObservedAt = observedAt.Time
	}
	return freshness, nil
}
