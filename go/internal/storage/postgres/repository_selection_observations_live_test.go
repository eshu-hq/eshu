// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSelectionObservationStoreEvaluationLive proves the #7625 selection
// observation contract against real Postgres:
//
//  1. Cycle 1 over the QA-shape corpus (776 listed + 1 archived_excluded +
//     25 not_listed, all same-org) records exactly those states with zero
//     false positives: every listed scope reads selected, the archived one
//     reads archived_excluded, and the 25 absent ones read not_listed.
//  2. Cycle 2, 26 hours later with the same listing, confirms: not_listed
//     rows keep their cycle-1 state_since and reach cycle 2, and the
//     freshness read composes them into not_selected (no generation
//     observed after the exclusion began), while listed scopes stay
//     selected and the archived scope confirms into not_selected too.
//  3. Scopes outside the org, pinned-ref scopes, and slugless scopes never
//     gain rows.
//  4. The mass-miss guard trips and writes nothing when 86 previously
//     selected scopes vanish at once; an empty listing trips it too.
//  5. Explicit mode writes positive selected rows without org enumeration.
//  6. The evaluation reads are index-bound (EXPLAIN notes below).
//
// It runs in the live-postgres-readiness runner. Run locally with a disposable
// PostgreSQL 18 administrative database:
//
//	ESHU_SELECTION_OBSERVATION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	ESHU_SELECTION_OBSERVATION_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres -run TestSelectionObservationStoreEvaluationLive -count=1 -v
func TestSelectionObservationStoreEvaluationLive(t *testing.T) {
	dsn := os.Getenv("ESHU_SELECTION_OBSERVATION_PROOF_DSN")
	optIn := os.Getenv("ESHU_SELECTION_OBSERVATION_PROOF_DISPOSABLE")
	ctx, sqlDB := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	store := postgres.NewSelectionObservationStore(postgres.SQLDB{DB: sqlDB})

	now := time.Now().UTC().Truncate(time.Second)
	cycle1 := now.Add(-26 * time.Hour)
	staleObserved := cycle1.Add(-5 * 24 * time.Hour)
	freshObserved := now.Add(-time.Hour)

	// The QA-shape corpus: 776 listed, 25 transferred away (same org,
	// absent from the listing), 1 archived in place.
	listed := make([]string, 0, 776)
	for i := range 776 {
		id := fmt.Sprintf("scope:listed-%04d", i)
		listed = append(listed, id)
		seedSelectionScope(t, ctx, sqlDB, id, fmt.Sprintf("boatsgroup/repo-%04d", i), "repository", freshObserved)
	}
	for i := range 25 {
		id := fmt.Sprintf("scope:gone-%02d", i)
		seedSelectionScope(t, ctx, sqlDB, id, fmt.Sprintf("boatsgroup/gone-%02d", i), "repository", staleObserved)
	}
	seedSelectionScope(t, ctx, sqlDB, "scope:archived-00", "boatsgroup/old-archived", "repository", staleObserved)
	// Decoys that must never gain rows: another org, a pinned-ref scope of
	// a listed repo, and a scope with no slug at all.
	seedSelectionScope(t, ctx, sqlDB, "scope:other-org", "otherorg/repo", "repository", freshObserved)
	seedSelectionScope(t, ctx, sqlDB, "scope:listed-0000@ref", "boatsgroup/repo-0000", "repository_ref", freshObserved)
	seedSelectionScope(t, ctx, sqlDB, "scope:slugless", "", "repository", freshObserved)
	// One listed scope with a generation history, for the per-scope
	// MAX(observed_at) EXPLAIN below. Only one generation per scope may be
	// active (scope_generations_active_scope_idx), so history is
	// superseded.
	for i := 1; i <= 5; i++ {
		seedSelectionGeneration(t, ctx, sqlDB, "scope:listed-0000", fmt.Sprintf("gen-history-%d", i), freshObserved.Add(-time.Duration(i)*time.Hour), "superseded")
	}

	evaluation := scope.SelectionEvaluation{
		SelectorID:            "sel_qa",
		SelectorKind:          scope.SelectionSelectorKindGitHubOrg,
		Org:                   "boatsgroup",
		EvaluatedAt:           cycle1,
		LivenessWindowSeconds: 48 * 3600,
	}
	for i, id := range listed {
		evaluation.Listed = append(evaluation.Listed, scope.EvaluatedRepository{ScopeID: id, GitHubID: int64(1000 + i)})
	}
	evaluation.Archived = []scope.EvaluatedRepository{{ScopeID: "scope:archived-00", GitHubID: 9999}}

	first, err := store.RecordSelectionEvaluation(ctx, evaluation)
	if err != nil {
		t.Fatalf("cycle 1 RecordSelectionEvaluation() error = %v", err)
	}
	if first.Outcome != scope.SelectionEvaluationEvaluated {
		t.Fatalf("cycle 1 outcome = %q, want evaluated", first.Outcome)
	}
	if first.KnownScopes != 802 {
		t.Fatalf("cycle 1 known scopes = %d, want 802 (decoys excluded)", first.KnownScopes)
	}
	if first.Selected != 776 || first.ArchivedExcluded != 1 || first.RuleExcluded != 0 || first.NotListed != 25 {
		t.Fatalf("cycle 1 counts = %d/%d/%d/%d, want 776/1/0/25",
			first.Selected, first.ArchivedExcluded, first.RuleExcluded, first.NotListed)
	}
	if !first.PriorEvaluatedAt.IsZero() {
		t.Fatalf("cycle 1 prior evaluated_at = %v, want zero (first evaluation)", first.PriorEvaluatedAt)
	}
	// Zero false positives: every listed scope reads selected after cycle 1.
	assertSelectionRowState(t, ctx, store, "scope:listed-0420", scope.SelectionStateSelected, 1)
	assertSelectionRowState(t, ctx, store, "scope:archived-00", scope.SelectionStateArchivedExcluded, 1)
	assertSelectionRowState(t, ctx, store, "scope:gone-07", scope.SelectionStateNotListed, 1)
	for _, decoy := range []string{"scope:other-org", "scope:listed-0000@ref", "scope:slugless"} {
		rows, err := store.ReadSelectionObservations(ctx, decoy)
		if err != nil {
			t.Fatalf("ReadSelectionObservations(%s) error = %v", decoy, err)
		}
		if len(rows) != 0 {
			t.Fatalf("ReadSelectionObservations(%s) = %d rows, want 0", decoy, len(rows))
		}
	}

	// Cycle 2 with the same listing confirms every exclusion (two
	// evaluations 26 hours apart).
	evaluation.EvaluatedAt = now
	second, err := store.RecordSelectionEvaluation(ctx, evaluation)
	if err != nil {
		t.Fatalf("cycle 2 RecordSelectionEvaluation() error = %v", err)
	}
	if second.Outcome != scope.SelectionEvaluationEvaluated {
		t.Fatalf("cycle 2 outcome = %q, want evaluated", second.Outcome)
	}
	if !second.PriorEvaluatedAt.Equal(cycle1) {
		t.Fatalf("cycle 2 prior evaluated_at = %v, want %v", second.PriorEvaluatedAt, cycle1)
	}
	assertSelectionRowState(t, ctx, store, "scope:gone-07", scope.SelectionStateNotListed, 2)
	assertSelectionRowState(t, ctx, store, "scope:archived-00", scope.SelectionStateArchivedExcluded, 2)
	assertSelectionRowState(t, ctx, store, "scope:listed-0420", scope.SelectionStateSelected, 2)

	// The freshness read composes the confirmed rows: a gone scope reads
	// not_selected, a listed scope reads selected, and the archived scope
	// confirms into not_selected too.
	assertLiveSelectionState(t, ctx, store, "scope:gone-07", status.RepositorySelectionNotSelected, now)
	assertLiveSelectionState(t, ctx, store, "scope:listed-0420", status.RepositorySelectionSelected, now)
	assertLiveSelectionState(t, ctx, store, "scope:archived-00", status.RepositorySelectionNotSelected, now)

	// A generation observed after the exclusion began flips the read to
	// excluded_still_ingested: something is still ingesting the scope.
	seedSelectionGeneration(t, ctx, sqlDB, "scope:gone-07", "gen-after-exclusion", now.Add(-time.Minute), "pending")
	assertLiveSelectionState(t, ctx, store, "scope:gone-07", status.RepositorySelectionExcludedStillIngested, now)

	// The mass-miss guard: 86 previously selected scopes vanish at once
	// (over max(10, 10% of 802)=80), so the store writes nothing.
	rowsBefore := countSelectionRows(t, ctx, sqlDB)
	tripped := evaluation
	tripped.Listed = tripped.Listed[:690]
	guarded, err := store.RecordSelectionEvaluation(ctx, tripped)
	if err != nil {
		t.Fatalf("guard RecordSelectionEvaluation() error = %v", err)
	}
	if guarded.Outcome != scope.SelectionEvaluationGuardTripped {
		t.Fatalf("guard outcome = %q, want guard_tripped", guarded.Outcome)
	}
	if guarded.KnownScopes != 802 || guarded.NewlyMissing != 86 {
		t.Fatalf("guard known/newly-missing = %d/%d, want 802/86", guarded.KnownScopes, guarded.NewlyMissing)
	}
	if got := countSelectionRows(t, ctx, sqlDB); got != rowsBefore {
		t.Fatalf("observation rows after guard trip = %d, want unchanged %d", got, rowsBefore)
	}

	// An empty listing trips the guard too.
	empty := evaluation
	empty.Listed, empty.Archived, empty.RuleExcluded = nil, nil, nil
	emptyOutcome, err := store.RecordSelectionEvaluation(ctx, empty)
	if err != nil {
		t.Fatalf("empty RecordSelectionEvaluation() error = %v", err)
	}
	if emptyOutcome.Outcome != scope.SelectionEvaluationGuardTripped {
		t.Fatalf("empty outcome = %q, want guard_tripped", emptyOutcome.Outcome)
	}
	if got := countSelectionRows(t, ctx, sqlDB); got != rowsBefore {
		t.Fatalf("observation rows after empty listing = %d, want unchanged %d", got, rowsBefore)
	}

	// Explicit mode writes positive selected rows without org enumeration,
	// even for a scope no githubOrg selector would enumerate.
	explicit := scope.SelectionEvaluation{
		SelectorID:            "sel_explicit",
		SelectorKind:          scope.SelectionSelectorKindExplicit,
		EvaluatedAt:           now,
		LivenessWindowSeconds: 48 * 3600,
		Listed:                []scope.EvaluatedRepository{{ScopeID: "scope:other-org"}},
	}
	explicitOutcome, err := store.RecordSelectionEvaluation(ctx, explicit)
	if err != nil {
		t.Fatalf("explicit RecordSelectionEvaluation() error = %v", err)
	}
	if explicitOutcome.Outcome != scope.SelectionEvaluationEvaluated || explicitOutcome.Selected != 1 {
		t.Fatalf("explicit outcome = %+v, want evaluated with 1 selected", explicitOutcome)
	}
	explicitRows, err := store.ReadSelectionObservations(ctx, "scope:other-org")
	if err != nil {
		t.Fatalf("ReadSelectionObservations(other-org) error = %v", err)
	}
	if len(explicitRows) != 1 || explicitRows[0].State != scope.SelectionStateSelected {
		t.Fatalf("explicit rows = %+v, want one selected row", explicitRows)
	}

	// EXPLAIN notes (PostgreSQL 18, seeded corpus above): every evaluation
	// read is index-bound.
	assertSelectionExplainUsesIndex(t, ctx, sqlDB,
		"SELECT MAX(observed_at) FROM scope_generations WHERE scope_id = $1",
		"scope:listed-0000",
		"Index",
		"the per-scope MAX(observed_at) stays on the scope's own indexed rows")
	assertSelectionExplainUsesIndex(t, ctx, sqlDB,
		"SELECT scope_id, payload->>'repo_slug' AS repo_slug FROM ingestion_scopes WHERE source_system = 'git' AND scope_kind = 'repository'",
		"",
		"ingestion_scopes_source_idx",
		"the org enumeration stays on the (source_system, scope_kind) index prefix")
	assertSelectionExplainUsesIndex(t, ctx, sqlDB,
		"SELECT scope_id, state, evaluated_at FROM repository_selection_observations WHERE selector_id = $1",
		"sel_qa",
		"repository_selection_observations_selector_evaluated_idx",
		"the prior-row read stays on the (selector_id, evaluated_at) index")
}

func seedSelectionScope(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	scopeID, slug, kind string,
	observedAt time.Time,
) {
	t.Helper()
	payload := "{}"
	if slug != "" {
		payload = fmt.Sprintf(`{"repo_slug": %q}`, slug)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
		 VALUES ($1, $2, 'git', $1, 'git', $1, $3, $3, 'active', $4::jsonb)`,
		scopeID, kind, observedAt, payload); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	seedSelectionGeneration(t, ctx, database, scopeID, "gen-"+scopeID, observedAt, "active")
}

func seedSelectionGeneration(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	scopeID, generationID string,
	observedAt time.Time,
	generationStatus string,
) {
	t.Helper()
	// Generation IDs must be unique table-wide; scope IDs contain colons,
	// which are legal in the text key.
	generationID = strings.ReplaceAll(generationID, ":", "_") + "-" + fmt.Sprintf("%d", observedAt.Unix())
	if _, err := database.ExecContext(ctx,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'push', false, $3, $3, $4)`,
		generationID, scopeID, observedAt, generationStatus); err != nil {
		t.Fatalf("seed generation %s/%s: %v", scopeID, generationID, err)
	}
}

func assertSelectionRowState(
	t *testing.T,
	ctx context.Context,
	store postgres.SelectionObservationStore,
	scopeID, wantState string,
	wantCycles int,
) {
	t.Helper()
	rows, err := store.ReadSelectionObservations(ctx, scopeID)
	if err != nil {
		t.Fatalf("ReadSelectionObservations(%s) error = %v", scopeID, err)
	}
	if len(rows) != 1 {
		t.Fatalf("ReadSelectionObservations(%s) = %d rows, want 1", scopeID, len(rows))
	}
	row := rows[0]
	if row.State != wantState {
		t.Fatalf("row state for %s = %q, want %q", scopeID, row.State, wantState)
	}
	if row.StateCycleCount != wantCycles {
		t.Fatalf("row cycle count for %s = %d, want %d", scopeID, row.StateCycleCount, wantCycles)
	}
}

func assertLiveSelectionState(
	t *testing.T,
	ctx context.Context,
	store postgres.SelectionObservationStore,
	scopeID string,
	want status.RepositorySelectionState,
	now time.Time,
) {
	t.Helper()
	rows, err := store.ReadSelectionObservations(ctx, scopeID)
	if err != nil {
		t.Fatalf("ReadSelectionObservations(%s) error = %v", scopeID, err)
	}
	latest, err := store.LatestGenerationObservedAt(ctx, scopeID)
	if err != nil {
		t.Fatalf("LatestGenerationObservedAt(%s) error = %v", scopeID, err)
	}
	selection := status.ComputeRepositorySelectionState(rows, latest, now)
	if selection.State != want {
		t.Fatalf("selection state for %s = %q, want %q (rows=%+v latest=%v)",
			scopeID, selection.State, want, rows, latest)
	}
}

func countSelectionRows(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM repository_selection_observations`).Scan(&count); err != nil {
		t.Fatalf("count observation rows: %v", err)
	}
	return count
}

// assertSelectionExplainUsesIndex runs EXPLAIN over one evaluation read and
// requires the plan to name the expected index. The full plan is logged so
// the proof carries its own evidence.
func assertSelectionExplainUsesIndex(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	statement, arg, wantIndex, why string,
) {
	t.Helper()
	var rows *sql.Rows
	var err error
	if arg == "" {
		rows, err = database.QueryContext(ctx, "EXPLAIN "+statement)
	} else {
		rows, err = database.QueryContext(ctx, "EXPLAIN "+statement, arg)
	}
	if err != nil {
		t.Fatalf("explain %s: %v", statement, err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	joined := strings.Join(plan, "\n")
	t.Logf("EXPLAIN %s:\n%s", statement, joined)
	if !strings.Contains(joined, wantIndex) {
		t.Fatalf("EXPLAIN plan for %q names no %s:\n%s\n(%s)", statement, wantIndex, joined, why)
	}
}
