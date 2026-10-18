// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// scopeIDBySlugQuery resolves repo slugs to scope ids in one statement
// (#8454): one page's slugs on the list path, the distinct successor slugs
// on the successor check. The comparison is case-insensitive because the
// probe stores the upstream full_name verbatim while ingestion normalizes
// the slug. It scans ingestion_scopes on the unindexed slug: 0.15 ms at the
// 802-scope QA shape, 11 ms at the 100k-scope stress shape. The scan stays
// proportional to the scope count, the smallest table in the annotation
// path, the same shape the known-scope read already accepts; a repo_slug
// expression index waits for evidence that scope counts outgrow it. The
// caller lowercases the input slugs; the SQL lowercases the stored side.
const scopeIDBySlugQuery = `
SELECT scope_id, payload->>'repo_slug' AS slug
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND lower(payload->>'repo_slug') = ANY($1)
ORDER BY scope_id
`

// maxGenerationBatchQuery reads one page's newest generation per scope in
// one statement (#8454): the batch form of the single-scope MAX the
// freshness read already issues, served by scope_generation_idx. 2.4 ms for
// a 500-id page over 500k generations.
const maxGenerationBatchQuery = `
SELECT scope_id, MAX(observed_at)
FROM scope_generations
WHERE scope_id = ANY($1)
GROUP BY scope_id
`

// PageSelectionStore annotates one list page with the same
// selection.Summarize the single-scope freshness read uses, off batched
// reads (#8454). Q1 reads the page's live observation rows in one
// statement; Q2 reads the page's newest generation per scope in one
// statement, lazily, only when a scope's aggregate needs rule (d); the
// successor check runs only when the page holds a not_listed entry with a
// successor slug. Any sub-query error fails the whole read: the caller
// never serves entries with evidence silently missing.
type PageSelectionStore struct {
	queryer db.Queryer
	now     func() time.Time
}

// NewPageSelectionStore builds a PageSelectionStore over queryer with the
// wall clock. Tests that need a fixed clock set the now field (same
// package) or serve canned rows.
func NewPageSelectionStore(queryer db.Queryer) PageSelectionStore {
	return PageSelectionStore{queryer: queryer, now: time.Now}
}

// ReadPageSelections annotates the page's scope ids.
func (s PageSelectionStore) ReadPageSelections(ctx context.Context, scopeIDs []string) (selection.PageSelections, error) {
	if s.queryer == nil {
		return selection.PageSelections{}, fmt.Errorf("read page selections: queryer is required")
	}
	now := s.now().UTC()
	live, err := membershipstore.ReadLiveScopeObservationsBatch(ctx, s.queryer, scopeIDs, now)
	if err != nil {
		return selection.PageSelections{}, err
	}
	return s.summarizeScopes(ctx, scopeIDs, live, now)
}

// ReadPageSelectionsBySlug annotates the page's repo slugs: it resolves the
// slugs to scope ids first (one statement, skipped for an empty page), then
// annotates exactly like ReadPageSelections.
func (s PageSelectionStore) ReadPageSelectionsBySlug(ctx context.Context, slugs []string) (selection.PageSelections, error) {
	if s.queryer == nil {
		return selection.PageSelections{}, fmt.Errorf("read page selections: queryer is required")
	}
	out := selection.PageSelections{ByScope: map[string]selection.Summary{}, ScopeBySlug: map[string]string{}}
	if len(slugs) == 0 {
		return out, nil
	}
	lowered := make([]string, len(slugs))
	for i, slug := range slugs {
		lowered[i] = strings.ToLower(slug)
	}
	rows, err := s.queryer.QueryContext(ctx, scopeIDBySlugQuery, lowered)
	if err != nil {
		return selection.PageSelections{}, fmt.Errorf("resolve page scope ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopeIDs []string
	for rows.Next() {
		var scopeID, slug string
		if err := rows.Scan(&scopeID, &slug); err != nil {
			return selection.PageSelections{}, fmt.Errorf("resolve page scope ids: %w", err)
		}
		out.ScopeBySlug[strings.ToLower(slug)] = scopeID
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Err(); err != nil {
		return selection.PageSelections{}, fmt.Errorf("resolve page scope ids: %w", err)
	}
	now := s.now().UTC()
	live, err := membershipstore.ReadLiveScopeObservationsBatch(ctx, s.queryer, scopeIDs, now)
	if err != nil {
		return selection.PageSelections{}, err
	}
	annotated, err := s.summarizeScopes(ctx, scopeIDs, live, now)
	if err != nil {
		return selection.PageSelections{}, err
	}
	out.ByScope = annotated.ByScope
	return out, nil
}

// summarizeScopes aggregates every scope id with selection.SummarizePage.
// The newest generation per scope is fetched once, lazily, the first time a
// scope's aggregate needs rule (d): a page no scope needs it for costs no
// generation query at all, and the memoized fetch serves both passes below.
// The first pass finds which successor slugs need resolving; the second
// applies them. live must be the live rows of scopeIDs at now.
func (s PageSelectionStore) summarizeScopes(
	ctx context.Context,
	scopeIDs []string,
	live map[string][]selection.Observation,
	now time.Time,
) (selection.PageSelections, error) {
	var (
		generations map[string]time.Time
		fetched     bool
	)
	latestGeneration := func(scopeID string) (time.Time, error) {
		if !fetched {
			fetched = true
			fetchedAt, err := s.readMaxGenerations(ctx, scopeIDs)
			if err != nil {
				return time.Time{}, err
			}
			generations = fetchedAt
		}
		return generations[scopeID], nil
	}
	summaries, err := selection.SummarizePage(scopeIDs, live, now, latestGeneration, func(string) bool { return false })
	if err != nil {
		return selection.PageSelections{}, err
	}
	resolved, err := s.resolveSuccessors(ctx, summaries, live, now)
	if err != nil {
		return selection.PageSelections{}, err
	}
	if len(resolved) > 0 {
		summaries, err = selection.SummarizePage(scopeIDs, live, now, latestGeneration, func(slug string) bool {
			return resolved[strings.ToLower(slug)]
		})
		if err != nil {
			return selection.PageSelections{}, err
		}
	}
	return selection.PageSelections{ByScope: summaries}, nil
}

// readMaxGenerations reads the newest observed_at per scope id in one
// statement. Scopes with no generation are absent from the map, which reads
// as zero time: no generation ever observed.
func (s PageSelectionStore) readMaxGenerations(ctx context.Context, scopeIDs []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(scopeIDs))
	if len(scopeIDs) == 0 {
		return out, nil
	}
	rows, err := s.queryer.QueryContext(ctx, maxGenerationBatchQuery, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("read page max generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scopeID string
		var observedAt time.Time
		if err := rows.Scan(&scopeID, &observedAt); err != nil {
			return nil, fmt.Errorf("read page max generations: %w", err)
		}
		out[scopeID] = observedAt.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read page max generations: %w", err)
	}
	return out, nil
}

// resolveSuccessors reports which successor slugs on the page resolve to a
// scope with a live selected row. It runs only when a page summary carries a
// successor slug (otherwise the page costs no successor query at all). The
// slugs resolve to scope ids in one statement; scopes already in the page's
// live rows decide from those, and only off-page successors cost one more
// bounded live-rows batch read.
func (s PageSelectionStore) resolveSuccessors(
	ctx context.Context,
	summaries map[string]selection.Summary,
	live map[string][]selection.Observation,
	now time.Time,
) (map[string]bool, error) {
	var slugs []string
	seen := map[string]bool{}
	for _, summary := range summaries {
		if summary.Reason != selection.StateNotListed || summary.SuccessorSlug == "" {
			continue
		}
		if key := strings.ToLower(summary.SuccessorSlug); !seen[key] {
			seen[key] = true
			slugs = append(slugs, key)
		}
	}
	if len(slugs) == 0 {
		return nil, nil
	}
	rows, err := s.queryer.QueryContext(ctx, scopeIDBySlugQuery, slugs)
	if err != nil {
		return nil, fmt.Errorf("resolve successor scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	scopeBySlug := make(map[string]string, len(slugs))
	var missing []string
	for rows.Next() {
		var scopeID, slug string
		if err := rows.Scan(&scopeID, &slug); err != nil {
			return nil, fmt.Errorf("resolve successor scopes: %w", err)
		}
		scopeBySlug[strings.ToLower(slug)] = scopeID
		if _, ok := live[scopeID]; !ok {
			missing = append(missing, scopeID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve successor scopes: %w", err)
	}
	if len(missing) > 0 {
		extra, err := membershipstore.ReadLiveScopeObservationsBatch(ctx, s.queryer, missing, now)
		if err != nil {
			return nil, err
		}
		for scopeID, observations := range extra {
			live[scopeID] = append(live[scopeID], observations...)
		}
	}
	selected := make(map[string]bool, len(slugs))
	for slug, scopeID := range scopeBySlug {
		for _, observation := range live[scopeID] {
			if observation.State == selection.StateSelected {
				selected[slug] = true
				break
			}
		}
	}
	return selected, nil
}
