// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	maintenancestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
)

// Real-Postgres proofs for #7797: a refinalize re-projects each scope through
// one generation. When that generation is a delta (scope_generations.is_delta)
// it carries only the files that changed since its baseline, so a rebuild onto
// an empty graph restores only those files. The refinalize must say so, and
// for a git default-branch scope it must record a per-repository reindex
// watermark in the same transaction so a scheduled sync forces a full
// re-parse.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run RefinalizeDeltaActive -count=1
//
// The live-postgres-readiness runner sets the family pair
// ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DSN and
// ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DISPOSABLE=1 instead.

// refinalizeDeltaActiveLiveDB gives each #7797 proof a schema of its own. It
// bridges the runner's family DSN onto ESHU_POSTGRES_DSN; a family DSN without
// its disposable acknowledgment fails closed.
func refinalizeDeltaActiveLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv("ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DSN is set without ESHU_RECOVERY_DELTA_ACTIVE_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_DSN", dsn)
	}
	return openIsolatedLiveDB(t, "refinalize_delta_active", "set ESHU_POSTGRES_DSN to run the #7797 refinalize delta-active proofs")
}

// deltaActiveFixture names one seeded scope and the generation a refinalize
// should select for it.
type deltaActiveFixture struct {
	scopeID      string
	generationID string
	baseFullID   string
}

// seedDeltaScope seeds a scope whose selected generation is a delta over a
// superseded full generation: the shape a webhook-driven git repository has
// after one push. status is the scope status: "active" selects the delta as the
// active generation, "failed" leaves no active generation and makes the delta
// the newest failed generation.
func seedDeltaScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID, suffix, status string, isDelta bool) deltaActiveFixture {
	t.Helper()
	now := time.Now().UTC()
	fixture := deltaActiveFixture{
		scopeID:      scopeID,
		generationID: "delta-active-selected-" + suffix + "-" + scopeID,
		baseFullID:   "delta-active-base-full-" + suffix + "-" + scopeID,
	}
	activeID := any(nil)
	selectedStatus := "failed"
	if status == "active" {
		activeID = fixture.generationID
		selectedStatus = "active"
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, $3, $4, '{}'::jsonb)`,
		scopeID, now, status, activeID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM repository_reindex_requests WHERE scope_id = $1`,
			`DELETE FROM fact_work_items WHERE scope_id = $1`,
			`DELETE FROM scope_generations WHERE scope_id = $1`,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
		} {
			_, _ = database.ExecContext(cleanupCtx, statement, scopeID)
		}
	})
	seedDeltaGeneration(t, ctx, database, scopeID, fixture.baseFullID, "superseded", false, now.Add(-2*time.Hour))
	seedDeltaGeneration(t, ctx, database, scopeID, fixture.generationID, selectedStatus, isDelta, now.Add(-time.Hour))
	return fixture
}

// seedDeltaGeneration inserts one generation with an explicit is_delta flag.
// activated_at is set for every status but failed, as activation stamps it.
func seedDeltaGeneration(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	scopeID, generationID, status string,
	isDelta bool,
	ingestedAt time.Time,
) {
	t.Helper()
	activatedAt := any(ingestedAt)
	if status == "failed" {
		activatedAt = nil
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'snapshot', $3, $4, $4, $5, $6)`,
		generationID, scopeID, isDelta, ingestedAt, status, activatedAt,
	); err != nil {
		t.Fatalf("seed scope_generations %s: %v", generationID, err)
	}
}

// reindexWatermark returns the stored reindex watermark for scopeID, or the
// zero time when the scope has no row.
func reindexWatermark(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) time.Time {
	t.Helper()
	var requestedAt time.Time
	err := database.QueryRowContext(ctx,
		`SELECT requested_at FROM repository_reindex_requests WHERE scope_id = $1`, scopeID,
	).Scan(&requestedAt)
	if err == sql.ErrNoRows {
		return time.Time{}
	}
	if err != nil {
		t.Fatalf("read reindex watermark %s: %v", scopeID, err)
	}
	return requestedAt.UTC()
}

// deltaActiveScopes seeds the outcome matrix one named-scope refinalize
// classifies: a delta-active git default-branch scope, a failed git scope
// whose newest failed generation is a delta, a full-active git scope, a
// delta-active git ref scope, and a delta-active scope of another collector.
type deltaActiveScopes struct {
	activeDelta, failedDelta, activeFull, refDelta, otherDelta deltaActiveFixture
}

func seedDeltaActiveScopes(t *testing.T, ctx context.Context, database *sql.DB, suffix string) deltaActiveScopes {
	t.Helper()
	return deltaActiveScopes{
		activeDelta: seedDeltaScope(t, ctx, database, "git-repository-scope:repo-a-"+suffix, suffix, "active", true),
		failedDelta: seedDeltaScope(t, ctx, database, "git-repository-scope:repo-b-"+suffix, suffix, "failed", true),
		activeFull:  seedDeltaScope(t, ctx, database, "git-repository-scope:repo-c-"+suffix, suffix, "active", false),
		refDelta:    seedDeltaScope(t, ctx, database, "git-repository-scope:repo-d-"+suffix+"@feature", suffix, "active", true),
		otherDelta:  seedDeltaScope(t, ctx, database, "other-collector-scope:repo-e-"+suffix, suffix, "active", true),
	}
}

func (s deltaActiveScopes) ids() []string {
	return []string{s.activeDelta.scopeID, s.failedDelta.scopeID, s.activeFull.scopeID, s.refDelta.scopeID, s.otherDelta.scopeID}
}

// TestRefinalizeDeltaActiveRequestsFullReindex is the #7797 failing-first
// regression. A refinalize of a delta-active git default-branch scope must
// record a reindex watermark the collector's read path sees, must report the
// scope as delta-active, and must still enqueue the delta's projector work. A
// full-active scope gets no row; a ref scope and another collector's scope are
// reported but get no row, because no reindex watermark can force them.
func TestRefinalizeDeltaActiveRequestsFullReindex(t *testing.T) {
	database, ctx := refinalizeDeltaActiveLiveDB(t)
	scopes := seedDeltaActiveScopes(t, ctx, database, testSuffix(t))

	instruments, reader := newEnqueueInstruments(t)
	store := NewRecoveryStore(SQLDB{DB: database}, WithRecoveryInstruments(instruments))
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{ScopeIDs: scopes.ids()}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	if got, want := result.Enqueued, 5; got != want {
		t.Fatalf("result.Enqueued = %d, want %d: delta-active scopes must still be re-projected", got, want)
	}
	for _, fixture := range []deltaActiveFixture{scopes.activeDelta, scopes.failedDelta} {
		if got := reindexWatermark(t, ctx, database, fixture.scopeID); got.IsZero() {
			t.Fatalf("no reindex watermark for delta git scope %q: the rebuild restores only the delta's files "+
				"and nothing forces the full re-parse that would restore the rest (#7797)", fixture.scopeID)
		}
		if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+fixture.scopeID+"_"+fixture.generationID); got != "pending" {
			t.Fatalf("projector work for delta generation of %q = %q, want pending", fixture.scopeID, got)
		}
	}
	for _, fixture := range []deltaActiveFixture{scopes.activeFull, scopes.refDelta, scopes.otherDelta} {
		if got := reindexWatermark(t, ctx, database, fixture.scopeID); !got.IsZero() {
			t.Fatalf("reindex watermark recorded for %q (%s), want none", fixture.scopeID, got)
		}
	}

	// The collector's own read path must see the watermark.
	watermarks, err := maintenancestore.NewRepositoryReindexStore(SQLDB{DB: database}).RepositoryReindexWatermarks(ctx, time.Time{})
	if err != nil {
		t.Fatalf("RepositoryReindexWatermarks() error = %v", err)
	}
	if _, ok := watermarks[scopes.activeDelta.scopeID]; !ok {
		t.Fatalf("collector watermark read %v does not name %q", watermarks, scopes.activeDelta.scopeID)
	}

	if got, want := result.DeltaActive.Total(), 4; got != want {
		t.Fatalf("result.DeltaActive.Total() = %d, want %d; ByOutcome = %v", got, want, result.DeltaActive.ByOutcome)
	}
	wantOutcome := map[string][]string{
		recovery.DeltaActiveOutcomeReindexRequested:   {scopes.activeDelta.scopeID, scopes.failedDelta.scopeID},
		recovery.DeltaActiveOutcomeReindexUnsupported: {scopes.refDelta.scopeID, scopes.otherDelta.scopeID},
	}
	for outcome, ids := range wantOutcome {
		if got := result.DeltaActive.ByOutcome[outcome]; got != len(ids) {
			t.Fatalf("result.DeltaActive.ByOutcome[%q] = %d, want %d", outcome, got, len(ids))
		}
		for _, id := range ids {
			if !refinalizeResultNamesScope(result.DeltaActive.Samples[outcome], id) {
				t.Fatalf("result.DeltaActive.Samples[%q] = %v, want it to name %q", outcome, result.DeltaActive.Samples[outcome], id)
			}
		}
	}
	if got := result.DeltaActive.ReindexRequestsWritten(); got.Count != 2 || len(got.ScopeIDs) != 2 {
		t.Fatalf("ReindexRequestsWritten() = %+v, want the two delta git default-branch scopes", got)
	}
	if refinalizeResultNamesScope(result.DeltaActive.Samples[recovery.DeltaActiveOutcomeReindexRequested], scopes.activeFull.scopeID) {
		t.Fatalf("full-active scope %q reported as delta-active", scopes.activeFull.scopeID)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got, want := deltaActiveCounterByOutcome(rm), map[string]int64{
		recovery.DeltaActiveOutcomeReindexRequested:   2,
		recovery.DeltaActiveOutcomeReindexUnsupported: 2,
	}; !maps.Equal(got, want) {
		t.Fatalf("eshu_dp_recovery_delta_active_scopes_total by outcome = %v, want %v", got, want)
	}
}

// deltaActiveCounterByOutcome sums eshu_dp_recovery_delta_active_scopes_total
// per outcome label.
func deltaActiveCounterByOutcome(rm metricdata.ResourceMetrics) map[string]int64 {
	got := make(map[string]int64)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_recovery_delta_active_scopes_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value(attribute.Key("outcome"))
				got[outcome.AsString()] += dp.Value
			}
		}
	}
	return got
}

// TestRefinalizeDeltaActiveConvergesAcrossTwoCalls proves the reindex request
// is idempotent: a second refinalize keeps one row per scope, never moves the
// watermark backward, and reports the same classification.
func TestRefinalizeDeltaActiveConvergesAcrossTwoCalls(t *testing.T) {
	database, ctx := refinalizeDeltaActiveLiveDB(t)
	scopes := seedDeltaActiveScopes(t, ctx, database, testSuffix(t))

	store := NewRecoveryStore(SQLDB{DB: database})
	filter := recovery.RefinalizeFilter{ScopeIDs: scopes.ids()}
	first, err := store.RefinalizeScopeProjections(ctx, filter, time.Now().UTC())
	if err != nil {
		t.Fatalf("first RefinalizeScopeProjections() error = %v", err)
	}
	firstMark := reindexWatermark(t, ctx, database, scopes.activeDelta.scopeID)
	second, err := store.RefinalizeScopeProjections(ctx, filter, time.Now().UTC())
	if err != nil {
		t.Fatalf("second RefinalizeScopeProjections() error = %v", err)
	}
	secondMark := reindexWatermark(t, ctx, database, scopes.activeDelta.scopeID)

	if firstMark.IsZero() || secondMark.Before(firstMark) {
		t.Fatalf("reindex watermark first/second = %s/%s, want set and never moved backward", firstMark, secondMark)
	}
	var rows int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM repository_reindex_requests WHERE scope_id = ANY($1)`, scopes.ids(),
	).Scan(&rows); err != nil {
		t.Fatalf("count reindex rows: %v", err)
	}
	if rows != 2 {
		t.Fatalf("reindex rows after two refinalizes = %d, want 2 (one per delta git default-branch scope)", rows)
	}
	if first.DeltaActive.Total() != second.DeltaActive.Total() ||
		first.DeltaActive.ByOutcome[recovery.DeltaActiveOutcomeReindexRequested] != second.DeltaActive.ByOutcome[recovery.DeltaActiveOutcomeReindexRequested] {
		t.Fatalf("delta-active report first/second = %v/%v, want equal", first.DeltaActive.ByOutcome, second.DeltaActive.ByOutcome)
	}
}
