// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8950 disposable PostgreSQL 18 proof that an unchanged-content rewrite under a new generation through the real ContentWriter is a metadata-only HOT update: every row keeps its index entries and page, WAL stays near zero, and concurrent writers leave no torn rows; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// hotLiveShape is the findings-heavy batch the prototype is proven on:
// secret-dense bodies so the secret-lines trigger and the trigram indexes are
// in play, small enough for a unit-speed live test.
const (
	hotLiveFileCount = 12
	hotLiveSizeKB    = 200
)

// hotLiveBodies returns one stable body per file: every tenth line is a
// secret-like finding, so the batch is findings-heavy.
func hotLiveBodies(t *testing.T) []string {
	t.Helper()
	bodies := make([]string, hotLiveFileCount)
	for i := range bodies {
		bodies[i] = guardedSecretBody(fmt.Sprintf("hot%d", i), hotLiveSizeKB)
	}
	return bodies
}

// hotLiveMaterialization builds a full-snapshot Write of the bodies with the
// given commit and generation.
func hotLiveMaterialization(repoID, gen, commit string, bodies []string) content.Materialization {
	records := make([]content.Record, len(bodies))
	for i, body := range bodies {
		records[i] = content.Record{
			Path:   fmt.Sprintf("finding_%02d.go", i),
			Body:   body,
			Digest: fmt.Sprintf("hot-digest-%d", i),
			Metadata: map[string]string{
				"commit_sha": commit,
				"language":   "go",
			},
		}
	}
	return content.Materialization{
		RepoID: repoID, ScopeID: "scope-8950-hot", GenerationID: gen, Records: records,
		// A re-index under a new generation arrives as a full snapshot.
		FullSnapshot: true,
	}
}

// contentFilesIndexBytes maps each content_files index name to its size, so a
// HOT-only rewrite can prove it added no index entry.
func contentFilesIndexBytes(t *testing.T, ctx context.Context, db *sql.DB) map[string]int64 {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT indexrelid::regclass::text, pg_relation_size(indexrelid)
FROM pg_index WHERE indrelid = 'content_files'::regclass`)
	if err != nil {
		t.Fatalf("index sizes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]int64{}
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			t.Fatalf("scan index size: %v", err)
		}
		got[name] = size
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index sizes: %v", err)
	}
	return got
}

// hotLiveCounters carries the HOT proof inputs around one rewrite.
type hotLiveCounters struct {
	beforeWAL  writerWALSample
	beforeIdx  map[string]int64
	beforeRows map[string]contentFileVersionRow
}

// hotStatLine returns the pg_stat HOT counters for the log line. They are
// corroboration only: stats flush timing is not load-bearing for the verdict.
func hotStatLine(ctx context.Context, db *sql.DB) string {
	var upd, hot, live, dead sql.NullInt64
	_ = db.QueryRowContext(ctx, `
SELECT n_tup_upd, n_tup_hot_upd, n_live_tup, n_dead_tup
FROM pg_stat_user_tables WHERE relname = 'content_files'`).Scan(&upd, &hot, &live, &dead)
	return fmt.Sprintf("upd=%v hot=%v live=%v dead=%v", upd.Int64, hot.Int64, live.Int64, dead.Int64)
}

// prepareHotLiveTable applies the prototype's storage preconditions test-locally:
// room for same-page versions and no background vacuum noise. No migration.
func prepareHotLiveTable(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `ALTER TABLE content_files SET (fillfactor = 50, autovacuum_enabled = false)`); err != nil {
		t.Fatalf("set fillfactor: %v", err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM (FULL, ANALYZE) content_files`); err != nil {
		t.Fatalf("vacuum full: %v", err)
	}
}

// ctidPage splits a (page,offset) ctid into its page.
func ctidPage(ctid string) string {
	if i := strings.Index(ctid, ","); i >= 0 {
		return ctid[:i]
	}
	return ctid
}

// assertMetadataOnlyHOT is the shared verdict: every row moved its version
// (the lineage columns changed), stayed on its page, added no index entry,
// and the rewrite wrote near-zero WAL. indexed_at and content must not move.
func assertMetadataOnlyHOT(t *testing.T, ctx context.Context, db *sql.DB, repoID string, c hotLiveCounters, walBytes int64, walLimit int64) {
	t.Helper()
	after := readContentFileVersions(t, ctx, db, repoID)
	if len(after) != hotLiveFileCount {
		t.Fatalf("rows after rewrite = %d, want %d", len(after), hotLiveFileCount)
	}
	for path, was := range c.beforeRows {
		now := after[path]
		if now.xmin == was.xmin {
			t.Errorf("%s kept xmin %s: the rewrite must move the lineage version", path, was.xmin)
		}
		if ctidPage(now.ctid) != ctidPage(was.ctid) {
			t.Errorf("%s left page %s for %s: not a HOT update", path, ctidPage(was.ctid), ctidPage(now.ctid))
		}
		if !now.indexedAt.Equal(was.indexedAt) {
			t.Errorf("%s moved indexed_at %s -> %s: a metadata-only rewrite must not touch it", path, was.indexedAt, now.indexedAt)
		}
		if now.content != was.content {
			t.Errorf("%s changed content on a metadata-only rewrite", path)
		}
		if now.commitSHA.String == was.commitSHA.String || now.generation.String == was.generation.String {
			t.Errorf("%s kept lineage (%q/%q): the rewrite must move commit and generation", path, now.commitSHA.String, now.generation.String)
		}
	}
	afterIdx := contentFilesIndexBytes(t, ctx, db)
	for name, before := range c.beforeIdx {
		if got := afterIdx[name]; got != before {
			t.Errorf("index %s changed size %d -> %d: a HOT rewrite adds no index entry", name, before, got)
		}
	}
	t.Logf("metadata-only rewrite: WAL %d bytes (limit %d), stats %s", walBytes, walLimit, hotStatLine(ctx, db))
	if walBytes > walLimit {
		t.Errorf("metadata-only rewrite wrote %d WAL bytes, want <= %d", walBytes, walLimit)
	}
}

// TestContentWriterMetadataOnlyRewriteIsHOTLive is the #8950 failing
// regression: an unchanged-content rewrite under a new generation must be a
// metadata-only HOT update. Against the single-statement guarded upsert it
// fails: every row leaves its page and the indexes grow.
func TestContentWriterMetadataOnlyRewriteIsHOTLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	const repoID = "repo-8950-hot"
	writer, _ := newLiveOutcomeWriter(t, database)

	bodies := hotLiveBodies(t)
	if _, err := writer.Write(ctx, hotLiveMaterialization(repoID, "gen-1", "c1", bodies)); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	prepareHotLiveTable(t, ctx, database)

	fenceVersion := func() int64 {
		t.Helper()
		var v int64
		if err := database.QueryRowContext(ctx, `SELECT coalesce((SELECT version FROM config_artifact_content_versions WHERE repo_id = $1), 0)`, repoID).Scan(&v); err != nil {
			t.Fatalf("fence version: %v", err)
		}
		return v
	}
	c := hotLiveCounters{
		beforeWAL:  sampleWriterWAL(t, ctx, database),
		beforeIdx:  contentFilesIndexBytes(t, ctx, database),
		beforeRows: readContentFileVersions(t, ctx, database, repoID),
	}
	beforeFence := fenceVersion()
	start := time.Now()
	if _, err := writer.Write(ctx, hotLiveMaterialization(repoID, "gen-2", "c2", bodies)); err != nil {
		t.Fatalf("rewrite Write: %v", err)
	}
	wall := time.Since(start)
	// The metadata UPDATE is a real content_files write: the statement-level
	// version fence must still stage the repository and bump at commit.
	if got := fenceVersion(); got <= beforeFence {
		t.Errorf("fence version = %d after rewrite, want > %d", got, beforeFence)
	}
	walBytes := writerWALBetween(t, c.beforeWAL, sampleWriterWAL(t, ctx, database))
	t.Logf("rewrite wall %s for %d files of ~%d KiB", wall, hotLiveFileCount, hotLiveSizeKB)

	// 2.4 MiB of bodies: a metadata rewrite is tens of KiB of heap tuples.
	// 256 KiB is two orders of magnitude above the measured prototype and
	// still below the legacy ~900 KiB for the same shape.
	assertMetadataOnlyHOT(t, ctx, database, repoID, c, walBytes, 256<<10)

	var secretCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM content_file_secret_lines WHERE repo_id = $1`, repoID).Scan(&secretCount); err != nil {
		t.Fatalf("secret lines count: %v", err)
	}
	if secretCount == 0 {
		t.Errorf("secret-lines side table is empty after the rewrite: findings must survive a metadata-only rewrite")
	}

	// The language inventory still reads the last content/indexed change, not
	// the replay: MAX(indexed_at) is the seed stamp on every row.
	var lastIndexed time.Time
	var fileCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*), MAX(indexed_at) FROM content_files WHERE repo_id = $1`, repoID).Scan(&fileCount, &lastIndexed); err != nil {
		t.Fatalf("language rollup read: %v", err)
	}
	if fileCount != hotLiveFileCount {
		t.Errorf("rollup file count = %d, want %d", fileCount, hotLiveFileCount)
	}
	if !lastIndexed.Equal(c.beforeRows["finding_00.go"].indexedAt) {
		t.Errorf("rollup MAX(indexed_at) = %s, want the seed stamp %s", lastIndexed, c.beforeRows["finding_00.go"].indexedAt)
	}
}

// TestContentWriterMetadataOnlySavesWalWallLive compares the legacy
// single-statement rewrite (an unobserved writer, which takes the old path)
// with the observed prototype rewrite on identical findings-heavy batches.
func TestContentWriterMetadataOnlySavesWalWallLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	database.SetMaxOpenConns(2)
	t.Run("foreign_backend_wal_is_ignored", func(t *testing.T) {
		writerConn, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire writer backend: %v", err)
		}
		defer func() { _ = writerConn.Close() }()
		foreignConn, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire foreign backend: %v", err)
		}
		defer func() { _ = foreignConn.Close() }()
		var schema string
		if err := writerConn.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
			t.Fatalf("current schema: %v", err)
		}
		table := quoteSQLIdentifier(schema) + `.wal_measurement_noise`
		if _, err := foreignConn.ExecContext(ctx, `CREATE TABLE `+table+` (body text)`); err != nil {
			t.Fatalf("create foreign WAL table: %v", err)
		}
		before := sampleWriterWAL(t, ctx, writerConn)
		foreignBefore := sampleWriterWAL(t, ctx, foreignConn)
		if before.pid == foreignBefore.pid {
			t.Fatal("noise and writer measurements share one backend")
		}
		if _, err := foreignConn.ExecContext(ctx, `INSERT INTO `+table+` SELECT repeat(md5(g::text), 32) FROM generate_series(1, 1000) AS g`); err != nil {
			t.Fatalf("write foreign WAL: %v", err)
		}
		if got := writerWALBetween(t, foreignBefore, sampleWriterWAL(t, ctx, foreignConn)); got <= 0 {
			t.Errorf("foreign backend added %d WAL bytes, want positive", got)
		} else {
			t.Logf("foreign backend WAL %d bytes", got)
		}
		if got := writerWALBetween(t, before, sampleWriterWAL(t, ctx, writerConn)); got != 0 {
			t.Errorf("foreign backend added %d bytes to writer WAL, want 0", got)
		} else {
			t.Logf("writer WAL after foreign write %d bytes", got)
		}
		before = sampleWriterWAL(t, ctx, writerConn)
		if _, err := writerConn.ExecContext(ctx, `INSERT INTO `+table+` VALUES (repeat('own-wal-', 1000))`); err != nil {
			t.Fatalf("write own WAL: %v", err)
		}
		if got := writerWALBetween(t, before, sampleWriterWAL(t, ctx, writerConn)); got <= 0 {
			t.Errorf("own backend added %d WAL bytes, want positive", got)
		} else {
			t.Logf("own backend WAL %d bytes", got)
		}
	})
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	bodies := hotLiveBodies(t)

	legacyWriter := NewContentWriter(SQLDB{DB: database})
	protoWriter, protoReader := newLiveOutcomeWriter(t, database)

	rewrite := func(repoID string, writer ContentWriter) (walBytes int64, wall time.Duration, totalDelta int64) {
		t.Helper()
		if _, err := writer.Write(ctx, hotLiveMaterialization(repoID, "gen-1", "c1", bodies)); err != nil {
			t.Fatalf("seed Write: %v", err)
		}
		prepareHotLiveTable(t, ctx, database)
		var beforeTotal int64
		if err := database.QueryRowContext(ctx, `SELECT pg_total_relation_size('content_files')`).Scan(&beforeTotal); err != nil {
			t.Fatalf("total size: %v", err)
		}
		beforeWAL := sampleWriterWAL(t, ctx, database)
		start := time.Now()
		if _, err := writer.Write(ctx, hotLiveMaterialization(repoID, "gen-2", "c2", bodies)); err != nil {
			t.Fatalf("rewrite Write: %v", err)
		}
		wall = time.Since(start)
		walBytes = writerWALBetween(t, beforeWAL, sampleWriterWAL(t, ctx, database))
		var afterTotal int64
		if err := database.QueryRowContext(ctx, `SELECT pg_total_relation_size('content_files')`).Scan(&afterTotal); err != nil {
			t.Fatalf("total size: %v", err)
		}
		return walBytes, wall, afterTotal - beforeTotal
	}

	legacyWAL, legacyWall, legacySize := rewrite("repo-8950-legacy", legacyWriter)
	protoWAL, protoWall, protoSize := rewrite("repo-8950-proto", protoWriter)
	if legacyWAL <= 0 {
		t.Fatal("legacy writer produced no attributed WAL: comparison denominator is invalid")
	}
	t.Logf("legacy rewrite: WAL %d bytes (%.2f MiB) wall %s total-size delta %d bytes",
		legacyWAL, float64(legacyWAL)/(1<<20), legacyWall, legacySize)
	t.Logf("prototype rewrite: WAL %d bytes (%.2f MiB) wall %s total-size delta %d bytes",
		protoWAL, float64(protoWAL)/(1<<20), protoWall, protoSize)

	if protoWAL*20 > legacyWAL {
		t.Errorf("prototype WAL %d is not at least 20x below legacy WAL %d", protoWAL, legacyWAL)
	}
	if protoSize >= legacySize {
		t.Errorf("prototype total-size delta %d is not below legacy delta %d", protoSize, legacySize)
	}

	// The share the merged #8950 counter records for a new-generation replay
	// of a findings-heavy batch: every row and every byte is metadata_only.
	var totalBytes int64
	for _, body := range bodies {
		totalBytes += int64(len(body))
	}
	attrs := map[string]string{"outcome": "metadata_only", "snapshot": "full", "batch_class": "small"}
	var rm metricdata.ResourceMetrics
	if err := protoReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upserts_total", attrs); got != hotLiveFileCount {
		t.Errorf("metadata_only rows = %d, want %d", got, hotLiveFileCount)
	}
	if got := sumPoints(t, rm, "eshu_dp_content_file_upsert_body_bytes_total", attrs); got != totalBytes {
		t.Errorf("metadata_only bytes = %d, want %d", got, totalBytes)
	}
}

// TestContentWriterMetadataOnlyConcurrentNoTornRowsLive proves the writer
// contract under concurrency: concurrent metadata-only writers plus a content
// editor on shared keys produce no error, no deadlock, and every row ends as
// one writer's complete (commit, generation, content) triple — never a mix.
func TestContentWriterMetadataOnlyConcurrentNoTornRowsLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-race"
	bodies := hotLiveBodies(t)

	seedWriter, _ := newLiveOutcomeWriter(t, database)
	if _, err := seedWriter.Write(ctx, hotLiveMaterialization(repoID, "gen-1", "c1", bodies)); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	prepareHotLiveTable(t, ctx, database)

	type triple struct{ commit, gen, body string }
	want := map[string][]triple{}
	for i := range bodies {
		path := fmt.Sprintf("finding_%02d.go", i)
		want[path] = append(want[path],
			triple{commit: "c-meta-a", gen: "gen-a", body: bodies[i]},
			triple{commit: "c-meta-b", gen: "gen-b", body: bodies[i]},
		)
	}
	editedBody := bodies[0] + "-- editor change\n"
	// finding_00.go deterministically ends as a metadata writer's triple:
	// the holder commits the edit while the writers wait on its row lock, so
	// both writers miss the metadata recheck and overwrite it last.

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	// The holder keeps a row lock on the edited key in an open transaction
	// while the metadata writers start, so their recheck runs against the
	// edited version: the forced interleave the issue notes was never
	// written. The holder commits the edit before the writers finish, so the
	// writers (which carry the old body) must miss in the metadata UPDATE's
	// content recheck and fall through to the guarded upsert; the row still
	// ends as exactly one writer's complete triple.
	holderReady := make(chan struct{})
	releaseHolder := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			errs <- err
			return
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `SELECT 1 FROM content_files WHERE repo_id = $1 AND relative_path = $2 FOR UPDATE`,
			repoID, "finding_00.go"); err != nil {
			errs <- err
			return
		}
		close(holderReady)
		<-releaseHolder
		if _, err := tx.ExecContext(ctx, `UPDATE content_files SET content = $3, commit_sha = 'c-edit', generation_id = 'gen-edit'
WHERE repo_id = $1 AND relative_path = $2`, repoID, "finding_00.go", editedBody); err != nil {
			errs <- err
			return
		}
		if err := tx.Commit(); err != nil {
			errs <- err
			return
		}
	}()

	<-holderReady
	for _, gen := range []string{"gen-a", "gen-b"} {
		wg.Add(1)
		go func(gen, commit string) {
			defer wg.Done()
			w, _ := newLiveOutcomeWriter(t, database)
			if _, err := w.Write(ctx, hotLiveMaterialization(repoID, gen, commit, bodies)); err != nil {
				errs <- err
			}
		}(gen, "c-meta-"+gen[len(gen)-1:])
	}
	// Wait until both writers block on the held row lock (or finish the rest
	// of their batch around it), then release the edit.
	maxWaiters := 0
	waitDeadline := time.Now().Add(30 * time.Second)
	for {
		var waiters int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE wait_event_type = 'Lock' AND query LIKE '%content_files%'`).Scan(&waiters); err != nil {
			errs <- err
			break
		}
		if waiters > maxWaiters {
			maxWaiters = waiters
		}
		if waiters >= 2 || time.Now().After(waitDeadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("forced interleave engaged with %d lock waiters", maxWaiters)
	if maxWaiters < 2 {
		t.Errorf("saw %d lock waiters, want >= 2: the forced interleave did not engage", maxWaiters)
	}
	close(releaseHolder)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Write: %v", err)
	}

	after := readContentFileVersions(t, ctx, database, repoID)
	for path, options := range want {
		got := triple{commit: after[path].commitSHA.String, gen: after[path].generation.String, body: after[path].content}
		match := false
		for _, o := range options {
			if got == o {
				match = true
				break
			}
		}
		if !match {
			t.Errorf("%s ended as (%q/%q/%d bytes): torn across writers", path, got.commit, got.gen, len(got.body))
		}
	}
}
