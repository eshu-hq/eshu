// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestAbsentAcceptanceOrphansStarveHealthyIntentLive is the #7853 RED/GREEN
// proof: ~10,100 orphan intents (acceptance row absent because their
// scope_generations row is gone) plus one healthy intent in one partition.
// Pre-fix the healthy intent is never processed: every cycle hits the
// 10,000-row scan cap on orphans and the orphans are skipped forever, so the
// healthy intent starves. Post-fix the cycles drain the generation-gone
// orphans and the healthy intent is processed in the first cycle after the
// drain, while a torn-write intent (generation row present, acceptance
// absent) is never drained.
//
// It bootstraps its own schema, so a bare disposable Postgres is enough. Set
// ESHU_ABSENT_ACCEPTANCE_PROOF_DSN (or ESHU_POSTGRES_TEST_DSN) to run it;
// skipped otherwise.
func TestAbsentAcceptanceOrphansStarveHealthyIntentLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	conn := openAbsentAcceptanceProofSchema(t, ctx, "eshu_7853_drain")

	const (
		scope     = "git:scope-7853"
		domain    = "workload_dependency"
		orphans   = 10100
		tornUnit  = "unit-7853-torn"
		tornRun   = "run-7853-torn"
		tornGen   = "gen-7853-torn"
		healthyU  = "unit-7853-healthy"
		healthyR  = "run-7853-healthy"
		healthyG  = "gen-7853-healthy"
		healthyID = "intent-7853-healthy"
		tornID    = "intent-7853-torn"
	)
	now := time.Now().UTC()
	seedAbsentAcceptanceFixture(t, ctx, conn, absentAcceptanceFixture{
		Scope:     scope,
		Domain:    domain,
		Orphans:   orphans,
		TornID:    tornID,
		TornUnit:  tornUnit,
		TornRun:   tornRun,
		TornGen:   tornGen,
		HealthyID: healthyID,
		HealthyU:  healthyU,
		HealthyR:  healthyR,
		HealthyG:  healthyG,
		BaseTime:  now.Add(-time.Hour),
	})

	database := SQLDB{DB: conn}
	store := NewSharedIntentStore(database)
	edgeWriter := &absentAcceptanceRecordingEdgeWriter{}
	cfg := worker.PartitionProcessorConfig{
		Domain:         domain,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "test-7853-drain",
		LeaseTTL:       time.Minute,
		BatchLimit:     100,
		EvidenceSource: "test/7853",
	}

	healthyDone := false
	drainedFirstCycle := 0
	for cycle := 1; cycle <= 5; cycle++ {
		cycleStart := time.Now()
		result, err := worker.ProcessPartitionOnce(
			ctx,
			now,
			cfg,
			store,
			store,
			edgeWriter,
			NewAcceptedGenerationLookup(database),
			NewAcceptedGenerationPrefetch(database),
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		elapsed := time.Since(cycleStart)
		if err != nil {
			t.Fatalf("cycle %d: ProcessPartitionOnce: %v", cycle, err)
		}
		t.Logf("cycle %d: processed=%d stale=%d absent-drained=%d absent-torn=%d blocked=%d edge-writes=%d elapsed=%s",
			cycle, result.ProcessedIntents, result.StaleIntents,
			result.AbsentGenerationGoneIntents, result.AbsentAcceptanceTornWrites,
			result.BlockedReadiness, len(edgeWriter.written), elapsed)
		if cycle == 1 {
			drainedFirstCycle = result.AbsentGenerationGoneIntents
		}
		if pendingCompleted(t, ctx, store, domain, healthyID) {
			healthyDone = true
			t.Logf("healthy intent processed in cycle %d", cycle)
			break
		}
	}
	if !healthyDone {
		t.Fatalf("healthy intent %q was not processed within 5 cycles: absent-acceptance orphans starve it (#7853)", healthyID)
	}
	if drainedFirstCycle == 0 {
		t.Fatal("first cycle drained 0 absent-acceptance intents: the healthy intent completed without the #7853 drain path")
	}

	if !edgeWriter.saw(healthyID) {
		t.Fatalf("healthy intent %q marked completed but never written", healthyID)
	}
	assertAbsentAcceptanceDrainEndState(t, ctx, store, domain, absentAcceptanceEndState{
		HealthyID: healthyID,
		TornID:    tornID,
		Orphans:   orphans,
	})

	// The affected-count contract on real Postgres (#7853): re-marking an
	// already-completed orphan still matches its row (the statement does not
	// filter on completed_at), while an unknown id matches nothing. The
	// workers rely on this to tell a fully applied batch from rows deleted
	// mid-cycle.
	rematch, err := store.MarkIntentsCompleted(ctx, []string{"intent-7853-orphan-00000"}, now)
	if err != nil {
		t.Fatalf("re-mark drained orphan: %v", err)
	}
	if rematch != 1 {
		t.Fatalf("re-mark drained orphan affected = %d, want 1 (rows matched, not rows newly completed)", rematch)
	}
	nomatch, err := store.MarkIntentsCompleted(ctx, []string{"intent-7853-unknown"}, now)
	if err != nil {
		t.Fatalf("mark unknown intent: %v", err)
	}
	if nomatch != 0 {
		t.Fatalf("mark unknown intent affected = %d, want 0", nomatch)
	}
}

// absentAcceptanceFixture describes the #7853 live fixture:
// generation-gone orphans (no acceptance row, no scope_generations row),
// one torn-write intent (generation row present, acceptance absent), and one
// healthy intent (both present). The torn intent is the oldest row and the
// healthy intent the newest, so the healthy intent sits past the 10,000-row
// scan cap until the orphans drain.
type absentAcceptanceFixture struct {
	Scope     string
	Domain    string
	Orphans   int
	TornID    string
	TornUnit  string
	TornRun   string
	TornGen   string
	HealthyID string
	HealthyU  string
	HealthyR  string
	HealthyG  string
	BaseTime  time.Time
}

func seedAbsentAcceptanceFixture(t *testing.T, ctx context.Context, conn *sql.DB, fx absentAcceptanceFixture) {
	t.Helper()

	if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'git','git-collector',$1,'git',$1,$2,$2,'active','{}'::jsonb)`, fx.Scope, fx.BaseTime); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	// One active generation per scope (scope_generations_active_scope_idx),
	// so the torn and healthy generations live in their own scopes. Orphans
	// carry generation-less rows, so they share one scope row.
	tornScope := fx.Scope + "-torn"
	healthyScope := fx.Scope + "-healthy"
	for _, scope := range []string{tornScope, healthyScope} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
			VALUES ($1,'git','git-collector',$1,'git',$1,$2,$2,'active','{}'::jsonb)`, scope, fx.BaseTime); err != nil {
			t.Fatalf("insert scope %s: %v", scope, err)
		}
	}
	for _, gen := range []struct {
		id    string
		scope string
	}{{fx.TornGen, tornScope}, {fx.HealthyG, healthyScope}} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
			VALUES ($1,$2,'snapshot',$3,$3,'active','{}'::jsonb)`, gen.id, gen.scope, fx.BaseTime); err != nil {
			t.Fatalf("insert generation %s: %v", gen.id, err)
		}
	}

	rows := make([]reducer.SharedProjectionIntentRow, 0, fx.Orphans+2)
	// Torn-write intent first (oldest): its generation row exists, so the
	// drain must leave it pending forever.
	rows = append(rows, reducer.SharedProjectionIntentRow{
		IntentID:         fx.TornID,
		ProjectionDomain: fx.Domain,
		PartitionKey:     "7853-p0",
		ScopeID:          tornScope,
		AcceptanceUnitID: fx.TornUnit,
		RepositoryID:     "git:repo-7853",
		SourceRunID:      fx.TornRun,
		GenerationID:     fx.TornGen,
		Payload:          map[string]any{},
		CreatedAt:        fx.BaseTime,
	})
	for i := 0; i < fx.Orphans; i++ {
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID:         fmt.Sprintf("intent-7853-orphan-%05d", i),
			ProjectionDomain: fx.Domain,
			PartitionKey:     "7853-p0",
			ScopeID:          fx.Scope,
			AcceptanceUnitID: fmt.Sprintf("unit-7853-orphan-%05d", i),
			RepositoryID:     "git:repo-7853",
			SourceRunID:      "run-7853-orphan",
			GenerationID:     fmt.Sprintf("gen-7853-orphan-%05d", i),
			Payload:          map[string]any{},
			CreatedAt:        fx.BaseTime.Add(time.Duration(i+1) * 10 * time.Millisecond),
		})
	}
	// Healthy intent newest: past the scan cap until the orphans drain.
	rows = append(rows, reducer.SharedProjectionIntentRow{
		IntentID:         fx.HealthyID,
		ProjectionDomain: fx.Domain,
		PartitionKey:     "7853-p0",
		ScopeID:          healthyScope,
		AcceptanceUnitID: fx.HealthyU,
		RepositoryID:     "git:repo-7853",
		SourceRunID:      fx.HealthyR,
		GenerationID:     fx.HealthyG,
		Payload:          map[string]any{},
		CreatedAt:        fx.BaseTime.Add(time.Duration(fx.Orphans+1) * 10 * time.Millisecond),
	})
	if err := NewSharedIntentStore(SQLDB{DB: conn}).UpsertIntents(ctx, rows); err != nil {
		t.Fatalf("upsert fixture intents: %v", err)
	}
	if err := NewSharedProjectionAcceptanceStore(SQLDB{DB: conn}).Upsert(ctx, []SharedProjectionAcceptance{{
		ScopeID:          healthyScope,
		AcceptanceUnitID: fx.HealthyU,
		SourceRunID:      fx.HealthyR,
		GenerationID:     fx.HealthyG,
		AcceptedAt:       fx.BaseTime,
		UpdatedAt:        fx.BaseTime,
	}}); err != nil {
		t.Fatalf("upsert healthy acceptance: %v", err)
	}
}

// absentAcceptanceEndState is the asserted post-drain state.
type absentAcceptanceEndState struct {
	HealthyID string
	TornID    string
	Orphans   int
}

func assertAbsentAcceptanceDrainEndState(t *testing.T, ctx context.Context, store *SharedIntentStore, domain string, want absentAcceptanceEndState) {
	t.Helper()

	pending, err := store.ListPendingDomainIntents(ctx, domain, want.Orphans+10)
	if err != nil {
		t.Fatalf("list pending intents: %v", err)
	}
	pendingByID := make(map[string]struct{}, len(pending))
	for _, row := range pending {
		pendingByID[row.IntentID] = struct{}{}
	}
	if _, ok := pendingByID[want.HealthyID]; ok {
		t.Fatalf("healthy intent %q still pending after the drain", want.HealthyID)
	}
	if _, ok := pendingByID[want.TornID]; !ok {
		t.Fatalf("torn-write intent %q was drained: its generation row still exists, so the drain must leave it pending", want.TornID)
	}
	for i := 0; i < want.Orphans; i++ {
		if _, ok := pendingByID[fmt.Sprintf("intent-7853-orphan-%05d", i)]; ok {
			t.Fatalf("orphan intent intent-7853-orphan-%05d still pending after the drain", i)
		}
	}
	if len(pending) != 1 {
		t.Fatalf("pending intents = %d, want exactly 1 (the torn-write intent)", len(pending))
	}
}

// pendingCompleted reports whether the intent is no longer pending.
func pendingCompleted(t *testing.T, ctx context.Context, store *SharedIntentStore, domain, intentID string) bool {
	t.Helper()

	pending, err := store.ListPendingDomainIntents(ctx, domain, 20000)
	if err != nil {
		t.Fatalf("list pending intents: %v", err)
	}
	for _, row := range pending {
		if row.IntentID == intentID {
			return false
		}
	}
	return true
}

// absentAcceptanceRecordingEdgeWriter records the rows the worker retracts
// and writes so the live proof can assert the healthy intent projected.
type absentAcceptanceRecordingEdgeWriter struct {
	written []string
}

func (w *absentAcceptanceRecordingEdgeWriter) RetractEdges(_ context.Context, _ string, _ []sharedintent.Row, _ string) error {
	return nil
}

func (w *absentAcceptanceRecordingEdgeWriter) WriteEdges(_ context.Context, _ string, rows []sharedintent.Row, _ string) (sharedintent.WriteReport, error) {
	for _, row := range rows {
		w.written = append(w.written, row.IntentID)
	}
	return sharedintent.WriteReport{}, nil
}

func (w *absentAcceptanceRecordingEdgeWriter) saw(intentID string) bool {
	for _, id := range w.written {
		if id == intentID {
			return true
		}
	}
	return false
}

// openAbsentAcceptanceProofSchema bootstraps the full schema in an isolated
// schema on a disposable Postgres, following openSupersededProofSchema. The
// DSN is ESHU_ABSENT_ACCEPTANCE_PROOF_DSN, falling back to
// ESHU_POSTGRES_TEST_DSN; with neither set the test skips.
func openAbsentAcceptanceProofSchema(t *testing.T, ctx context.Context, prefix string) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_ABSENT_ACCEPTANCE_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_ABSENT_ACCEPTANCE_PROOF_DSN or ESHU_POSTGRES_TEST_DSN to run the absent-acceptance live proof")
	}
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	admin := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := admin.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		t.Fatalf("ensure public pg_trgm extension: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE")
	})
	conn := openActiveOCIWarningIndexProofDB(t, activeOCIWarningIndexSchemaDSN(t, dsn, schema))
	if err := ApplyBootstrap(ctx, SQLDB{DB: conn}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	return conn
}
