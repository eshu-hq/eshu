// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Live proof for #7978 on a disposable PostgreSQL database
// (ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN). Each test bootstraps the full
// production schema into its own schema, commits generations through the
// production IngestionStore, and drives the production ProjectorQueue claim
// and Ack. Row counts are read from the real tables, so an extra write cannot
// hide behind a fake.

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const reprojectProofScopeID = "git-repository-scope:proof/7978"

// reprojectProof is one isolated, fully bootstrapped proof schema.
type reprojectProof struct {
	t      *testing.T
	dsn    string
	db     *sql.DB
	store  IngestionStore
	scope  scope.IngestionScope
	clock  time.Time
	facts  int
	schema string
	// priorSeen records, per projected generation in claim order, whether the
	// claim reported a prior generation (false means a first-generation
	// projection that skips canonical retraction).
	priorSeen []bool
}

func openReprojectProof(t *testing.T) *reprojectProof {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN"))
	if baseDSN == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	schemaName := fmt.Sprintf("proof_7978_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = admin.Close()
	})
	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName+",public")
	parsed.RawQuery = query.Encode()
	dsn := parsed.String()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	return &reprojectProof{
		t: t, dsn: dsn, db: database, store: NewIngestionStore(SQLDB{DB: database}),
		scope: scope.IngestionScope{
			ScopeID: reprojectProofScopeID, SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: "proof/7978",
		},
		clock: time.Now().UTC().Add(-48 * time.Hour), facts: 200, schema: schemaName,
	}
}

// generation builds the next generation in observed order.
func (p *reprojectProof) generation(id, hint, sha, reason string) scope.ScopeGeneration {
	p.clock = p.clock.Add(time.Minute)
	return scope.ScopeGeneration{
		GenerationID: id, ScopeID: p.scope.ScopeID, ObservedAt: p.clock, IngestedAt: p.clock,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: hint, SourceCommitSHA: sha, ReconcileReason: reason,
	}
}

// commit stores generation with p.facts file facts and returns the wall time.
func (p *reprojectProof) commit(generation scope.ScopeGeneration) time.Duration {
	p.t.Helper()
	stream := make(chan facts.Envelope, p.facts)
	for i := 0; i < p.facts; i++ {
		stream <- facts.Envelope{
			FactID:        fmt.Sprintf("%s-fact-%04d", generation.GenerationID, i),
			ScopeID:       generation.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "file",
			StableFactKey: fmt.Sprintf("file:proof/7978:f%04d.go", i),
			ObservedAt:    generation.ObservedAt,
			Payload:       map[string]any{"relative_path": fmt.Sprintf("f%04d.go", i)},
		}
	}
	close(stream)
	start := time.Now()
	if err := p.store.CommitScopeGeneration(context.Background(), p.scope, generation, stream); err != nil {
		p.t.Fatalf("commit %s: %v", generation.GenerationID, err)
	}
	return time.Since(start)
}

// project claims and Acks every ready projector row, as the projector would.
func (p *reprojectProof) project() []string {
	p.t.Helper()
	ctx := context.Background()
	queue := NewProjectorQueue(SQLDB{DB: p.db}, "proof-worker", time.Minute)
	var projected []string
	for range 10 {
		work, ok, err := queue.Claim(ctx)
		if err != nil {
			p.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return projected
		}
		p.priorSeen = append(p.priorSeen, work.Scope.PreviousGenerationExists)
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			p.t.Fatalf("ack %s: %v", work.Generation.GenerationID, err)
		}
		projected = append(projected, work.Generation.GenerationID)
	}
	p.t.Fatal("projector queue did not drain in 10 claims")
	return nil
}

// rowCounts reads the tables an unchanged reconcile must not grow.
func (p *reprojectProof) rowCounts() map[string]int {
	p.t.Helper()
	counts := map[string]int{}
	for _, table := range []string{
		"scope_generations", "fact_records", "eshu_search_index_documents",
		"eshu_search_index_terms", "content_files", "fact_work_items",
	} {
		var n int
		if err := p.db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			p.t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = n
	}
	return counts
}

// workRow reads one generation's projector work row.
func (p *reprojectProof) workRow(generationID string) (status string, reopens int, owner sql.NullString) {
	p.t.Helper()
	status, reopens, owner, _ = p.workRowAttempts(generationID)
	return status, reopens, owner
}

// workRowAttempts reads one generation's projector work row with its attempt
// count.
func (p *reprojectProof) workRowAttempts(generationID string) (status string, reopens int, owner sql.NullString, attempts int) {
	p.t.Helper()
	if err := p.db.QueryRowContext(context.Background(), `
SELECT status, COALESCE((payload ->> 'reconcile_reopen_count')::int, 0), lease_owner, attempt_count
FROM fact_work_items WHERE stage = 'projector' AND generation_id = $1`, generationID).Scan(&status, &reopens, &owner, &attempts); err != nil {
		p.t.Fatalf("read work row %s: %v", generationID, err)
	}
	return status, reopens, owner, attempts
}

// TestUnchangedReconcileStoresNothingLive is the #7978 row-count proof: an
// unchanged reconciliation adds no generation, fact, search, or content row,
// re-opens the active generation's projector work once, keeps the scope fresh
// for the sweep, and re-projects through the production claim and Ack.
func TestUnchangedReconcileStoresNothingLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	ctx := context.Background()

	firstCommit := p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	if got := p.project(); strings.Join(got, ",") != "gen-a" {
		t.Fatalf("projected = %v, want [gen-a]", got)
	}
	before := p.rowCounts()
	var activatedAt time.Time
	if err := p.db.QueryRowContext(ctx, `SELECT activated_at FROM scope_generations WHERE generation_id = 'gen-a'`).Scan(&activatedAt); err != nil {
		t.Fatalf("read activation: %v", err)
	}

	reconcileStart := time.Now().UTC()
	reconcileCommit := p.commit(p.generation("gen-b", "hint-a", "sha-a", "interval_elapsed"))
	after := p.rowCounts()
	for table, n := range before {
		if after[table] != n {
			t.Fatalf("%s rows %d -> %d after an unchanged reconcile, want unchanged", table, n, after[table])
		}
	}
	if status, reopens, _, attempts := p.workRowAttempts("gen-a"); status != "pending" || reopens != 1 || attempts != 0 {
		t.Fatalf("gen-a work = %s reopens=%d attempts=%d, want pending reopens=1 attempts=0", status, reopens, attempts)
	}
	var reason string
	if err := p.db.QueryRowContext(ctx, `SELECT payload ->> 'reconcile_reason' FROM scope_generations WHERE generation_id = 'gen-a'`).Scan(&reason); err != nil || reason != "interval_elapsed" {
		t.Fatalf("gen-a reconcile_reason = %q (%v), want interval_elapsed", reason, err)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil {
		t.Fatalf("FullReconcileState: %v", err)
	}
	requestedAt := state.ReconcileRequestedAt
	if requestedAt.Before(reconcileStart.Add(-time.Second)) || !state.ReconcileProjectedAt.IsZero() {
		t.Fatalf("request=%v projected=%v, want a request >= %v and no Ack yet", requestedAt, state.ReconcileProjectedAt, reconcileStart)
	}

	// A retried or repeated reconciliation while the re-open is pending is a
	// no-op: no write, no second re-open.
	p.commit(p.generation("gen-c", "hint-a", "sha-a", "interval_elapsed"))
	if status, reopens, _ := p.workRow("gen-a"); status != "pending" || reopens != 1 {
		t.Fatalf("gen-a work after repeat = %s reopens=%d, want pending reopens=1", status, reopens)
	}
	if again, err := p.store.FullReconcileState(ctx, p.scope.ScopeID); err != nil || !again.ReconcileRequestedAt.Equal(requestedAt) {
		t.Fatalf("request after a pending repeat = %v (%v), want unchanged %v", again.ReconcileRequestedAt, err, requestedAt)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonReconcileReprojectionPending); got != 1 {
		t.Fatalf("pending skips = %d, want 1", got)
	}

	if got := p.project(); strings.Join(got, ",") != "gen-a" {
		t.Fatalf("re-projected = %v, want [gen-a]", got)
	}
	// gen-a is the scope's only generation. Its first projection is a first
	// generation; its re-projection is not, so canonical retraction runs and
	// drift the graph holds beyond gen-a is removed.
	if len(p.priorSeen) != 2 || p.priorSeen[0] || !p.priorSeen[1] {
		t.Fatalf("PreviousGenerationExists per claim = %v, want [false true]", p.priorSeen)
	}
	var status string
	var activatedAfter time.Time
	var pointer string
	if err := p.db.QueryRowContext(ctx, `
SELECT generation.status, generation.activated_at, scope.active_generation_id
FROM scope_generations AS generation
JOIN ingestion_scopes AS scope ON scope.scope_id = generation.scope_id
WHERE generation.generation_id = 'gen-a'`).Scan(&status, &activatedAfter, &pointer); err != nil {
		t.Fatalf("read gen-a after re-projection: %v", err)
	}
	if status != "active" || !activatedAfter.Equal(activatedAt) || pointer != "gen-a" {
		t.Fatalf("gen-a after re-projection status=%s activated=%v pointer=%s; want active, original activation, pointer gen-a",
			status, activatedAfter, pointer)
	}
	if final := p.rowCounts(); final["scope_generations"] != before["scope_generations"] || final["fact_records"] != before["fact_records"] {
		t.Fatalf("rows after re-projection = %v, want %v", final, before)
	}
	acked, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil {
		t.Fatalf("FullReconcileState after Ack: %v", err)
	}
	if acked.ReconcileProjectedAt.Before(requestedAt) || !acked.LastProjectedFullAt.Equal(acked.ReconcileProjectedAt) {
		t.Fatalf("after Ack request=%v projected=%v last=%v, want projected >= request and last = projected",
			acked.ReconcileRequestedAt, acked.ReconcileProjectedAt, acked.LastProjectedFullAt)
	}

	// Before (#7978): the same reconciliation with a blank hint, which the
	// collector used to send, stores a whole new generation.
	legacyBefore := p.rowCounts()
	legacyCommit := p.commit(p.generation("gen-legacy", "", "sha-a", "interval_elapsed"))
	legacyAfter := p.rowCounts()
	t.Logf("PERF unchanged reconcile: first-commit=%s fixed-reconcile=%s legacy-blank-hint=%s facts=%d",
		firstCommit, reconcileCommit, legacyCommit, p.facts)
	t.Logf("ROWS fixed: scope_generations +%d fact_records +%d; legacy: scope_generations +%d fact_records +%d fact_work_items +%d",
		after["scope_generations"]-before["scope_generations"], after["fact_records"]-before["fact_records"],
		legacyAfter["scope_generations"]-legacyBefore["scope_generations"],
		legacyAfter["fact_records"]-legacyBefore["fact_records"],
		legacyAfter["fact_work_items"]-legacyBefore["fact_work_items"])
	if legacyAfter["fact_records"]-legacyBefore["fact_records"] != p.facts {
		t.Fatalf("legacy baseline stored %d facts, want %d", legacyAfter["fact_records"]-legacyBefore["fact_records"], p.facts)
	}
}

// TestReconcileIdentityChangesStillStoreLive proves the duplicate definition
// is exact on real Postgres: a changed hint (content or version), a changed
// commit, and a default-branch change each store a generation and its facts.
func TestReconcileIdentityChangesStillStoreLive(t *testing.T) {
	p := openReprojectProof(t)
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	cases := []struct {
		id, hint, sha        string
		defaultBranchChanged bool
	}{
		{id: "gen-hint", hint: "hint-b", sha: "sha-a"},
		{id: "gen-sha", hint: "hint-b", sha: "sha-b"},
		{id: "gen-branch", hint: "hint-b", sha: "sha-b", defaultBranchChanged: true},
	}
	for _, tc := range cases {
		before := p.rowCounts()
		generation := p.generation(tc.id, tc.hint, tc.sha, "interval_elapsed")
		generation.DefaultBranchChanged = tc.defaultBranchChanged
		p.commit(generation)
		after := p.rowCounts()
		if after["scope_generations"] != before["scope_generations"]+1 || after["fact_records"] != before["fact_records"]+p.facts {
			t.Fatalf("%s: rows %v -> %v, want one generation and %d facts", tc.id, before, after, p.facts)
		}
		var payload string
		if err := p.db.QueryRowContext(context.Background(), `SELECT payload::text FROM scope_generations WHERE generation_id = $1`, tc.id).Scan(&payload); err != nil {
			t.Fatalf("%s: read payload: %v", tc.id, err)
		}
		if !strings.Contains(payload, `"reconcile_reason": "interval_elapsed"`) {
			t.Fatalf("%s: payload = %s, want reconcile_reason", tc.id, payload)
		}
		if tc.defaultBranchChanged && !strings.Contains(payload, `"default_branch_changed": true`) {
			t.Fatalf("%s: payload = %s, want default_branch_changed", tc.id, payload)
		}
		p.project()
	}
}

// TestReconcileReopenLosesLockRaceToNewerActivationLive is the #7978
// concurrency proof for the fence. A reconciliation reads gen-a as active and
// matching, then waits on the scope row that a newer activation holds. When
// that activation commits (gen-a superseded, gen-n active), the re-open's
// statement sees the settled state, matches nothing, and the snapshot is
// dropped: no row is written and gen-a's work row stays as it was.
func TestReconcileReopenLosesLockRaceToNewerActivationLive(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
	p := openReprojectProof(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	before := p.rowCounts()

	activation, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin activation: %v", err)
	}
	defer func() { _ = activation.Rollback() }()
	for _, statement := range []string{
		`SELECT scope_id FROM ingestion_scopes WHERE scope_id = '` + reprojectProofScopeID + `' FOR NO KEY UPDATE`,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = 'gen-a'`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, source_commit_sha,
		     observed_at, ingested_at, status, activated_at)
		 VALUES ('gen-n', '` + reprojectProofScopeID + `', 'snapshot', 'hint-n', 'sha-n', now(), now(), 'active', now())`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-n' WHERE scope_id = '` + reprojectProofScopeID + `'`,
	} {
		if _, err := activation.ExecContext(ctx, statement); err != nil {
			t.Fatalf("activation statement: %v\n%s", err, statement)
		}
	}

	done := make(chan time.Duration, 1)
	go func() { done <- p.commit(p.generation("gen-r", "hint-a", "sha-a", "graph_dirty")) }()
	waitForReconcileScopeLockWait(ctx, t, p.db, p.schema)
	if err := activation.Commit(); err != nil {
		t.Fatalf("commit activation: %v", err)
	}
	elapsed := <-done
	t.Logf("reconcile waited on the scope lock and finished in %s", elapsed)

	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonReconcileSuperseded); got != 1 {
		t.Fatalf("reconcile_superseded skips = %d, want 1", got)
	}
	if status, reopens, _ := p.workRow("gen-a"); status != "succeeded" || reopens != 0 {
		t.Fatalf("gen-a work = %s reopens=%d, want succeeded and never re-opened", status, reopens)
	}
	var reason sql.NullString
	if err := p.db.QueryRowContext(ctx, `SELECT payload ->> 'reconcile_reason' FROM scope_generations WHERE generation_id = 'gen-a'`).Scan(&reason); err != nil || reason.Valid {
		t.Fatalf("gen-a reconcile_reason = %v (%v), want none", reason, err)
	}
	after := p.rowCounts()
	if after["fact_records"] != before["fact_records"] || after["scope_generations"] != before["scope_generations"]+1 {
		t.Fatalf("rows %v -> %v, want only the simulated gen-n row added", before, after)
	}
}

// TestReconcileReprojectionSupersededBeforeClaimIsNoopLive: the re-open
// commits, then a newer delta generation activates before the re-opened row
// is claimed (here the re-opened row sits in retry backoff). The claim sweep
// retires the re-opened row as superseded instead of projecting gen-a over
// gen-n, and the sweep no longer counts gen-a's reconcile stamp, so the scope
// is due for a full reconciliation again.
func TestReconcileReprojectionSupersededBeforeClaimIsNoopLive(t *testing.T) {
	p := openReprojectProof(t)
	ctx := context.Background()
	p.commit(p.generation("gen-a", "hint-a", "sha-a", ""))
	p.project()
	p.commit(p.generation("gen-r", "hint-a", "sha-a", "interval_elapsed"))
	if status, reopens, _ := p.workRow("gen-a"); status != "pending" || reopens != 1 {
		t.Fatalf("gen-a work = %s reopens=%d, want pending reopens=1", status, reopens)
	}
	if _, err := p.db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'retrying', visible_at = now() + interval '1 hour'
WHERE stage = 'projector' AND generation_id = 'gen-a'`); err != nil {
		t.Fatalf("park gen-a re-open: %v", err)
	}
	delta := p.generation("gen-n", "hint-n", "sha-n", "")
	delta.IsDelta = true
	delta.DeltaBaselineCommitSHA = "sha-a"
	p.commit(delta)
	if got := p.project(); strings.Join(got, ",") != "gen-n" {
		t.Fatalf("projected = %v, want [gen-n]", got)
	}
	if _, err := p.db.ExecContext(ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = now() - interval '1 second'
WHERE stage = 'projector' AND generation_id = 'gen-a'`); err != nil {
		t.Fatalf("release gen-a re-open: %v", err)
	}
	if got := p.project(); len(got) != 0 {
		t.Fatalf("projected after gen-n activated = %v, want none (gen-a superseded)", got)
	}
	if status, _, _ := p.workRow("gen-a"); status != "superseded" {
		t.Fatalf("gen-a re-open status = %s, want superseded", status)
	}
	state, err := p.store.FullReconcileState(ctx, p.scope.ScopeID)
	if err != nil {
		t.Fatalf("FullReconcileState: %v", err)
	}
	if age := time.Since(state.LastProjectedFullAt); age < 24*time.Hour {
		t.Fatalf("LastProjectedFullAt age = %s, want gen-a's old ingest time (stamp ignored once superseded)", age)
	}
}

// waitForReconcileScopeLockWait polls until a backend in schema waits on a row lock.
func waitForReconcileScopeLockWait(ctx context.Context, t *testing.T, database *sql.DB, schema string) {
	t.Helper()
	for ctx.Err() == nil {
		var waiting int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE wait_event_type = 'Lock' AND datname = current_database() AND query LIKE '%ingestion_scopes%FOR NO KEY UPDATE%'`).Scan(&waiting); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no reconcile waited on the scope lock in schema %s", schema)
}
