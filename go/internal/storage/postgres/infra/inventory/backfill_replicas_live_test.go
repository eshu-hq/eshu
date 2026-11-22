// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// Live proofs for the reducer-owned backfill (#11196): every reducer replica
// runs the backfill, so concurrent passes must serialize per repository on the
// derive lock, record the marker exactly once, and never deadlock against a
// live derive. Run with ESHU_POSTGRES_DSN as in derive_live_test.go.

// beginCountingDB counts per-repository derive transactions, so a test can
// state how much duplicate derive work concurrent replicas did.
type beginCountingDB struct {
	postgres.SQLDB
	begins atomic.Int64
}

func (c *beginCountingDB) Begin(ctx context.Context) (db.Transaction, error) {
	c.begins.Add(1)
	return c.SQLDB.Begin(ctx)
}

// labelsParam renders inventory.Labels as a Postgres text[] literal.
func labelsParam() string {
	return "{" + strings.Join(inventory.Labels, ",") + "}"
}

// seedCorpus writes repos repositories of rowsPerRepo content rows each: three
// of every four rows carry an infra label, the fourth is a Function the derive
// must skip. Entity and repository ids carry prefix, so a test never touches
// another test's rows. It returns the repository ids.
func seedCorpus(t *testing.T, ctx context.Context, sqlDB *sql.DB, prefix string, repos, rowsPerRepo int) []string {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
SELECT $1::text || '/' || r || '/' || i, $1::text || '/' || r, 'dir' || (i % 20) || '/f' || (i % 50) || '.tf',
       CASE WHEN i % 4 = 3 THEN 'Function' ELSE ($4::text[])[1 + (i % cardinality($4::text[]))] END,
       'n' || i, 1, 2, '', '{"provider":"aws","environment":"prod"}'::jsonb, now()
FROM generate_series(1, $2::int) AS r, generate_series(1, $3::int) AS i`,
		prefix, repos, rowsPerRepo, labelsParam()); err != nil {
		t.Fatalf("seed corpus: %v", err)
	}
	t.Cleanup(func() {
		// The test context may be done by cleanup time.
		cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, stmt := range []string{
			`DELETE FROM infra_resource_entities WHERE repo_id LIKE $1::text || '/%'`,
			`DELETE FROM content_entities WHERE repo_id LIKE $1::text || '/%'`,
		} {
			if _, err := sqlDB.ExecContext(cleanCtx, stmt, prefix); err != nil {
				t.Logf("cleanup %q: %v", stmt, err)
			}
		}
	})
	ids := make([]string, 0, repos)
	for r := 1; r <= repos; r++ {
		ids = append(ids, fmt.Sprintf("%s/%d", prefix, r))
	}
	return ids
}

// assertTableEqualsContent fails when the table rows of the prefix's
// repositories differ from their infra-labelled content_entities rows in
// either direction.
func assertTableEqualsContent(t *testing.T, ctx context.Context, sqlDB *sql.DB, prefix string) {
	t.Helper()
	var missing, extra int
	if err := sqlDB.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM (
    SELECT entity_id, repo_id, relative_path, entity_type FROM content_entities
    WHERE repo_id LIKE $1::text || '/%' AND entity_type = ANY($2::text[])
    EXCEPT SELECT entity_id, repo_id, relative_path, label FROM infra_resource_entities
    WHERE repo_id LIKE $1::text || '/%') m),
 (SELECT count(*) FROM (
    SELECT entity_id, repo_id, relative_path, label FROM infra_resource_entities
    WHERE repo_id LIKE $1::text || '/%'
    EXCEPT SELECT entity_id, repo_id, relative_path, entity_type FROM content_entities
    WHERE repo_id LIKE $1::text || '/%' AND entity_type = ANY($2::text[])) x)`,
		prefix, labelsParam()).Scan(&missing, &extra); err != nil {
		t.Fatalf("compare table to content: %v", err)
	}
	if missing != 0 || extra != 0 {
		t.Fatalf("table differs from content: %d content rows missing from the table, %d table rows without content", missing, extra)
	}
}

func markerRows(t *testing.T, ctx context.Context, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM infra_resource_entity_backfill_markers WHERE marker_name = $1`,
		inventory.BackfillMarker).Scan(&n); err != nil {
		t.Fatalf("count markers: %v", err)
	}
	return n
}

// openReplicas opens n independent pools, one per simulated reducer replica,
// each wrapped to count derive transactions. The pools close at test end.
func openReplicas(t *testing.T, n int) []*beginCountingDB {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	replicas := make([]*beginCountingDB, 0, n)
	for i := 0; i < n; i++ {
		pool, err := inventory.OpenWriterDB(dsn)
		if err != nil {
			t.Fatalf("open replica %d: %v", i, err)
		}
		pool.SetMaxOpenConns(4)
		t.Cleanup(func() { _ = pool.Close() })
		replicas = append(replicas, &beginCountingDB{SQLDB: postgres.SQLDB{DB: pool}})
	}
	return replicas
}

type replicaOutcome struct {
	result inventory.BackfillResult
	err    error
}

// runReplicas starts every replica's Backfiller.Run behind one barrier and
// waits for all of them.
func runReplicas(ctx context.Context, replicas []*beginCountingDB) []replicaOutcome {
	outcomes := make([]replicaOutcome, len(replicas))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			outcomes[i].result, outcomes[i].err = inventory.Backfiller{DB: replicas[i]}.Run(ctx)
		}()
	}
	close(start)
	wg.Wait()
	return outcomes
}

// TestBackfillLiveReplicasRaceOnTheLockAndTheMarker: several reducer replicas
// start the backfill at once while a live content writer keeps changing the
// same repositories, one deleted entity and one path derive at a time. The run must finish with no deadlock and no error, one
// marker row, and a table equal to content_entities, and no replica may do
// more per-repository derives than there are repositories.
//
// Lock ordering under test: every derive transaction takes the one
// per-repository advisory lock first and holds no other advisory lock, so
// replicas and the live writer cannot form a lock-order cycle. A resurrected
// stale row (a backfill that read content before a writer's commit and wrote
// after it) would leave a table row without content, which
// assertTableEqualsContent catches.
func TestBackfillLiveReplicasRaceOnTheLockAndTheMarker(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	resetMarker(t, ctx, sqlDB)
	prefix := uniqueRepo(t)
	const replicaCount, repoCount, rowsPerRepo = 4, 6, 2000
	repos := seedCorpus(t, ctx, sqlDB, prefix, repoCount, rowsPerRepo)

	// The live writer changes a path's content, commits it, then derives that
	// path: the order the content writer uses.
	writerStop := make(chan struct{})
	var writerDone sync.WaitGroup
	var writerDerives atomic.Int64
	var writerErr atomic.Value
	writerDone.Add(1)
	go func() {
		defer writerDone.Done()
		database := postgres.SQLDB{DB: sqlDB}
		for step := 0; ; step++ {
			select {
			case <-writerStop:
				return
			default:
			}
			repo := repos[step%len(repos)]
			entity := 1 + (step/len(repos))%rowsPerRepo
			path := fmt.Sprintf("dir%d/f%d.tf", entity%20, entity%50)
			if _, err := sqlDB.ExecContext(ctx,
				`DELETE FROM content_entities WHERE entity_id = $1`, fmt.Sprintf("%s/%d", repo, entity)); err != nil {
				writerErr.Store(err)
				return
			}
			if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{path}); err != nil {
				writerErr.Store(err)
				return
			}
			writerDerives.Add(1)
		}
	}()

	replicas := openReplicas(t, replicaCount)
	began := time.Now()
	outcomes := runReplicas(ctx, replicas)
	elapsed := time.Since(began)
	close(writerStop)
	writerDone.Wait()
	if err, _ := writerErr.Load().(error); err != nil {
		t.Fatalf("live writer failed during the backfill: %v", err)
	}

	var ran, already int
	var derives int64
	for i, outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("replica %d Run() error = %v, want nil (a deadlock or serialization failure surfaces here)", i, outcome.err)
		}
		if outcome.result.AlreadyComplete {
			already++
		} else {
			ran++
		}
		got := replicas[i].begins.Load()
		derives += got
		if got > int64(outcome.result.Repositories) {
			t.Fatalf("replica %d ran %d derives for %d repositories, want at most one each",
				i, got, outcome.result.Repositories)
		}
	}
	if ran == 0 {
		t.Fatal("no replica ran the backfill, want at least one")
	}
	if got := markerRows(t, ctx, sqlDB); got != 1 {
		t.Fatalf("marker rows = %d, want exactly 1", got)
	}
	assertTableEqualsContent(t, ctx, sqlDB, prefix)
	t.Logf("replicas=%d repositories=%d ran=%d already_complete=%d backfill_derives=%d live_writer_derives=%d wall=%s",
		replicaCount, repoCount, ran, already, derives, writerDerives.Load(), elapsed.Round(time.Millisecond))
	resetMarker(t, ctx, sqlDB)
}

// TestBackfillLiveTimedCorpus times the backfill over a scaled corpus with one
// replica and then with several, and reports the WAL each run wrote. Scale it
// with ESHU_INFRA_BACKFILL_REPOS and ESHU_INFRA_BACKFILL_ROWS_PER_REPO. The
// default is small enough to run in the ledger; the figures are comparable
// only within one run on one host.
func TestBackfillLiveTimedCorpus(t *testing.T) {
	sqlDB, _ := liveDB(t)
	// The shared 90s test context is too short for a scaled corpus.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(cancel)
	repoCount := envInt(t, "ESHU_INFRA_BACKFILL_REPOS", 20)
	rowsPerRepo := envInt(t, "ESHU_INFRA_BACKFILL_ROWS_PER_REPO", 100)
	prefix := uniqueRepo(t)
	seedCorpus(t, ctx, sqlDB, prefix, repoCount, rowsPerRepo)

	for _, replicaCount := range []int{1, 3} {
		resetMarker(t, ctx, sqlDB)
		if _, err := sqlDB.ExecContext(ctx, `DELETE FROM infra_resource_entities WHERE repo_id LIKE $1::text || '/%'`, prefix); err != nil {
			t.Fatalf("clear table: %v", err)
		}
		replicas := openReplicas(t, replicaCount)
		walBefore := walPosition(t, ctx, sqlDB)
		began := time.Now()
		outcomes := runReplicas(ctx, replicas)
		elapsed := time.Since(began)
		walBytes := walPosition(t, ctx, sqlDB) - walBefore
		var derives, inserted int64
		for i, outcome := range outcomes {
			if outcome.err != nil {
				t.Fatalf("replicas=%d replica %d Run() error = %v", replicaCount, i, outcome.err)
			}
			derives += replicas[i].begins.Load()
			inserted += outcome.result.RowsInserted
		}
		if got := markerRows(t, ctx, sqlDB); got != 1 {
			t.Fatalf("replicas=%d marker rows = %d, want 1", replicaCount, got)
		}
		assertTableEqualsContent(t, ctx, sqlDB, prefix)
		var tableRows int64
		if err := sqlDB.QueryRowContext(ctx,
			`SELECT count(*) FROM infra_resource_entities WHERE repo_id LIKE $1::text || '/%'`, prefix).Scan(&tableRows); err != nil {
			t.Fatalf("count table rows: %v", err)
		}
		t.Logf("replicas=%d repos=%d rows_per_repo=%d table_rows=%d wall_seconds=%.3f derive_txs=%d rows_inserted_all_replicas=%d wal_bytes=%d",
			replicaCount, repoCount, rowsPerRepo, tableRows, elapsed.Seconds(), derives, inserted, walBytes)
	}
	resetMarker(t, ctx, sqlDB)
}

func walPosition(t *testing.T, ctx context.Context, sqlDB *sql.DB) int64 {
	t.Helper()
	var pos int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0')::bigint`).Scan(&pos); err != nil {
		t.Fatalf("read wal position: %v", err)
	}
	return pos
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("%s = %q, want a positive integer", key, raw)
	}
	return n
}
