// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Live proof for issue #11460. It runs only against the disposable PostgreSQL
// named by ESHU_BACKFILL_BOUNDS_PROOF_DSN with
// ESHU_BACKFILL_BOUNDS_PROOF_DISPOSABLE=1.
package backfill_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11460 disposable PostgreSQL 18 proof that the keyset-paged anchor load returns the whole matching corpus through bounded pages (EXPLAIN ANALYZE BUFFERS of both page shapes, loader mean/total), stamps the per-commit application_name on the wire, releases every snapshot before the next page, and lets a concurrent CREATE INDEX CONCURRENTLY build — the migration-354 shape — complete while the loader runs; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_BACKFILL_BOUNDS_PROOF_DISPOSABLE ESHU_BACKFILL_BOUNDS_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func backfillBoundsProofDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_BACKFILL_BOUNDS_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_BACKFILL_BOUNDS_PROOF_DSN and ESHU_BACKFILL_BOUNDS_PROOF_DISPOSABLE=1 to run the #11460 backfill-bounds proofs")
	}
	if os.Getenv("ESHU_BACKFILL_BOUNDS_PROOF_DISPOSABLE") != "1" {
		t.Fatal("ESHU_BACKFILL_BOUNDS_PROOF_DSN is set without ESHU_BACKFILL_BOUNDS_PROOF_DISPOSABLE=1")
	}
	return dsn
}

// seedBoundsCorpus writes one 12,000-row matching scope and one 50-row
// non-matching scope, all stamped with one observed_at so the keyset cursor
// advances on fact_id alone.
func seedBoundsCorpus(t *testing.T, ctx context.Context, database *sql.DB) time.Time {
	t.Helper()
	at := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	for _, scope := range []struct{ id, repo, gen string }{
		{"git:scope-bounds-a", "repo-bounds-a", "gen-bounds-a"},
		{"git:scope-bounds-b", "repo-bounds-b", "gen-bounds-b"},
	} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', $1, 'git', $2, $3, $3, 'active', $4)`,
			scope.id, scope.repo, at, scope.gen); err != nil {
			t.Fatalf("seed scope %s: %v", scope.id, err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'active')`,
			scope.gen, scope.id, at); err != nil {
			t.Fatalf("seed generation %s: %v", scope.gen, err)
		}
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'seed-fact-' || LPAD(g::text, 6, '0'), 'git:scope-bounds-a', 'gen-bounds-a', 'content',
       'stable-' || g, 'git', 'src-' || g, $1, $1,
       jsonb_build_object('repo_id', 'repo-other',
                          'content_path', 'svc.tf',
                          'content_body', 'module "x" { source = "./payments-service" }')
FROM generate_series(1, 12000) g`, at); err != nil {
		t.Fatalf("seed matching facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'noise-fact-' || LPAD(g::text, 6, '0'), 'git:scope-bounds-b', 'gen-bounds-b', 'content',
       'noise-stable-' || g, 'git', 'noise-src-' || g, $1, $1,
       jsonb_build_object('repo_id', 'repo-quiet',
                          'content_path', 'notes.txt',
                          'content', 'nothing relevant here')
FROM generate_series(1, 50) g`, at); err != nil {
		t.Fatalf("seed noise facts: %v", err)
	}
	return at
}

func boundsCatalog() []relationships.CatalogEntry {
	return []relationships.CatalogEntry{
		{RepoID: "repo-new", Aliases: []string{"repo-new", "payments-service"}},
		{RepoID: "repo-other", Aliases: []string{"repo-other", "other-service"}},
	}
}

// explainBoundsShape runs EXPLAIN (ANALYZE, BUFFERS) for one page shape and
// returns the plan text. The shape must carry the LIMIT into the plan; the
// buffer and timing lines are the evidence, logged by the caller.
func explainBoundsShape(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
	if err != nil {
		t.Fatalf("explain page shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	return plan.String()
}

// TestBackfillBoundsLiveProof is the live half of the #11460 evidence: the
// paged shape against a real planner and heap, the loader's mean/total over
// the seeded corpus, the per-commit application_name on the wire, and a
// concurrent CREATE INDEX CONCURRENTLY build completing while the loader
// runs — the migration-354 wait-for-old-snapshots shape that stalled for
// 3h37m under the old unbounded read.
func TestBackfillBoundsLiveProof(t *testing.T) {
	dsn := backfillBoundsProofDSN(t)
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t, dsn, os.Getenv("ESHU_BACKFILL_BOUNDS_PROOF_DISPOSABLE"), 10*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		},
	)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	at := seedBoundsCorpus(t, ctx, database)
	adapter := postgres.SQLDB{DB: database}

	likeTerms := []string{"%payments-service%", "%other-service%"}
	firstPlan := explainBoundsShape(t, ctx, database,
		backfill.ListOnboardedRepoScopedRelationshipFactRecordsFirstPageQuery, likeTerms, 5000)
	if !strings.Contains(firstPlan, "Limit") {
		t.Fatalf("first-page plan carries no Limit:\n%s", firstPlan)
	}
	t.Logf("first-page plan:\n%s", firstPlan)

	cursorPlan := explainBoundsShape(t, ctx, database,
		backfill.ListOnboardedRepoScopedRelationshipFactRecordsCursorPageQuery,
		likeTerms, at, "seed-fact-006000", 5000)
	if !strings.Contains(cursorPlan, "Limit") {
		t.Fatalf("cursor-page plan carries no Limit:\n%s", cursorPlan)
	}
	t.Logf("cursor-page plan:\n%s", cursorPlan)

	// Loader mean/total over three full passes, asserting the whole matching
	// corpus comes back exactly once with no noise row admitted.
	var total time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		loaded, stats, err := backfill.LoadAnchorScopedRelationshipFactsBudget(
			ctx, adapter, boundsCatalog(), boundsCatalog(), backfill.RelationshipFactLoadBudget{},
		)
		elapsed := time.Since(start)
		total += elapsed
		if err != nil {
			t.Fatalf("paged anchor load pass %d: %v", i, err)
		}
		if len(loaded) != 12000 {
			t.Fatalf("pass %d loaded %d facts, want 12000", i, len(loaded))
		}
		seen := make(map[string]struct{}, len(loaded))
		for _, envelope := range loaded {
			if !strings.HasPrefix(envelope.FactID, "seed-fact-") {
				t.Fatalf("pass %d admitted non-matching fact %q", i, envelope.FactID)
			}
			if _, dup := seen[envelope.FactID]; dup {
				t.Fatalf("pass %d loaded %q twice", i, envelope.FactID)
			}
			seen[envelope.FactID] = struct{}{}
		}
		t.Logf("pass %d: facts=%d rows=%d bytes=%d total_s=%.2f", i, len(loaded), stats.Rows, stats.Bytes, elapsed.Seconds())
	}
	t.Logf("loader mean_s=%.2f total_s=%.2f over 3 passes of 12000 facts", (total / 3).Seconds(), total.Seconds())

	// The per-commit application_name must reach the wire: sample
	// pg_stat_activity while the loader runs. SET LOCAL reverts at page
	// commit, so sampling after the load would see nothing by design.
	seenApp := make(map[string]struct{})
	probeStop := make(chan struct{})
	probeDone := make(chan error, 1)
	go func() {
		defer close(probeDone)
		for {
			select {
			case <-probeStop:
				probeDone <- nil
				return
			default:
			}
			if _, _, err := backfill.LoadAnchorScopedRelationshipFactsBudget(
				ctx, adapter, boundsCatalog(), boundsCatalog(), backfill.RelationshipFactLoadBudget{},
			); err != nil {
				probeDone <- err
				return
			}
		}
	}()
	probeDeadline := time.Now().Add(60 * time.Second)
	for {
		if _, ok := seenApp[backfill.BackfillApplicationNamePerCommit]; ok {
			break
		}
		if time.Now().After(probeDeadline) {
			break
		}
		rows, err := database.QueryContext(ctx,
			`SELECT DISTINCT application_name FROM pg_stat_activity WHERE application_name LIKE 'eshu-backfill%'`)
		if err != nil {
			t.Fatalf("sample pg_stat_activity: %v", err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				_ = rows.Close()
				t.Fatalf("scan application_name: %v", err)
			}
			seenApp[name] = struct{}{}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("application_name rows: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(probeStop)
	if err := <-probeDone; err != nil {
		t.Fatalf("application_name probe load: %v", err)
	}
	if _, ok := seenApp[backfill.BackfillApplicationNamePerCommit]; !ok {
		t.Fatalf("per-commit application_name never observed on the wire, saw %v", seenApp)
	}
	t.Logf("application_names observed: %v", seenApp)
	var lingering int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_stat_activity WHERE application_name LIKE 'eshu-backfill%' AND state <> 'idle'`).Scan(&lingering); err != nil {
		t.Fatalf("count lingering backfill backends: %v", err)
	}
	if lingering != 0 {
		t.Fatalf("%d backfill backends still working after the loads returned", lingering)
	}

	// The migration-354 shape: a concurrent index build must complete while
	// the loader keeps running. Under the old unbounded read the build
	// waited for the loader's single old snapshot; with per-page short
	// transactions there is never an old snapshot to wait for.
	loaderStop := make(chan struct{})
	loaderDone := make(chan error, 1)
	go func() {
		defer close(loaderDone)
		for {
			select {
			case <-loaderStop:
				loaderDone <- nil
				return
			default:
			}
			if _, _, err := backfill.LoadAnchorScopedRelationshipFactsBudget(
				ctx, adapter, boundsCatalog(), boundsCatalog(), backfill.RelationshipFactLoadBudget{},
			); err != nil {
				loaderDone <- err
				return
			}
		}
	}()
	buildStart := time.Now()
	_, buildErr := database.ExecContext(ctx,
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS bounds_proof_cic_idx ON fact_records ((payload->>'repo_id'))`)
	buildElapsed := time.Since(buildStart)
	close(loaderStop)
	if err := <-loaderDone; err != nil {
		t.Fatalf("loader failed during concurrent build: %v", err)
	}
	if buildErr != nil {
		t.Fatalf("concurrent index build failed while the loader ran: %v", buildErr)
	}
	t.Logf("concurrent CREATE INDEX CONCURRENTLY completed in %.2fs while the loader ran", buildElapsed.Seconds())
	if _, err := database.ExecContext(ctx, `DROP INDEX IF EXISTS bounds_proof_cic_idx`); err != nil {
		t.Fatalf("drop proof index: %v", err)
	}
}
