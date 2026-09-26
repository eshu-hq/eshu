// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// secretLinesBenchCorpus reads real text files from the checkout (Go source,
// docs, YAML, SQL, shell) so the derivation regexes see real line shapes and a
// real secret density instead of a synthetic file mix.
func secretLinesBenchCorpus(t *testing.T, limit int) map[string][]content.Record {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	languages := map[string]string{
		".go": "go", ".md": "markdown", ".yaml": "yaml", ".yml": "yaml", ".sql": "sql",
		".sh": "shell", ".json": "json", ".py": "python", ".ts": "typescript", ".tf": "hcl",
	}
	byRepo := map[string][]content.Record{}
	count := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "node_modules" || name == ".gocache" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		language, ok := languages[filepath.Ext(name)]
		if !ok || count >= limit {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 || info.Size() > 512*1024 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(body, 0) >= 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		repo := "bench-" + strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		byRepo[repo] = append(byRepo[repo], content.Record{
			Path: filepath.ToSlash(rel), Body: string(body), Metadata: map[string]string{"language": language},
		})
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	if count < 500 {
		t.Fatalf("bench corpus has %d files, want at least 500", count)
	}
	return byRepo
}

func secretLinesBenchWrite(
	ctx context.Context, t *testing.T, db *sql.DB, corpus map[string][]content.Record, suffix string,
) time.Duration {
	t.Helper()
	writer := NewContentWriter(SQLDB{DB: db})
	repos := make([]string, 0, len(corpus))
	for repo := range corpus {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	started := time.Now()
	for _, repo := range repos {
		records := make([]content.Record, len(corpus[repo]))
		for i, record := range corpus[repo] {
			record.Body += suffix
			records[i] = record
		}
		if _, err := writer.Write(ctx, content.Materialization{
			RepoID: repo, ScopeID: "scope-" + repo, GenerationID: "gen-bench", SourceSystem: "git", Records: records,
		}); err != nil {
			t.Fatalf("write %s: %v", repo, err)
		}
	}
	return time.Since(started)
}

func secretLinesBenchMedian(values []time.Duration) time.Duration {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

// TestContentFileSecretLinesWriteCostLive measures what migration 131's
// triggers add to the real ContentWriter.Write path (#7125): the same real
// corpus is written into two disposable databases, one with the side table and
// triggers and one with them dropped (the pre-131 schema), five interleaved
// rounds with alternating first mover, each timing a first write (insert
// path), a write of changed content (update path), and a write of unchanged
// content (skip path). It logs medians as No-Regression Evidence and asserts
// only that the triggered database derived rows and stayed in parity.
//
// A third database keeps the triggers but writes from sessions that carry
// secretlines' deferred setting (the bootstrap-index regime, set with ALTER
// DATABASE so the pool's new connections inherit it): its cost over the
// trigger-less database is the price of the bulk-load gate, which should be
// close to zero. Every phase also reports Postgres backend CPU seconds read from
// /proc, which stays comparable when the host is oversubscribed and wall clock
// does not (a heavily loaded host still varies CPU, so compare rounds pairwise).
//
// Set ESHU_SECRET_LINES_BENCH=1, ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN, and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it. Set
// ESHU_SECRET_LINES_BENCH_FILES to cap the corpus (default 12000) and
// ESHU_SECRET_LINES_BENCH_DEFER_INDEXES=1 to drop the content trigram index in
// both databases, which models the deferred-index bulk-load bootstrap.
func TestContentFileSecretLinesWriteCostLive(t *testing.T) {
	if os.Getenv("ESHU_SECRET_LINES_BENCH") != "1" {
		t.Skip("set ESHU_SECRET_LINES_BENCH=1 to run the secret-lines write cost benchmark")
	}
	limit := 12000
	if raw := os.Getenv("ESHU_SECRET_LINES_BENCH_FILES"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			t.Fatalf("ESHU_SECRET_LINES_BENCH_FILES = %q", raw)
		}
		limit = parsed
	}
	corpus := secretLinesBenchCorpus(t, limit)

	open := func() (context.Context, *sql.DB) {
		ctx, db := postgresproof.OpenDisposableDatabase(t,
			os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
			os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
			45*time.Minute)
		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("apply bootstrap schema: %v", err)
		}
		if os.Getenv("ESHU_SECRET_LINES_BENCH_DEFER_INDEXES") == "1" {
			if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS content_files_content_trgm_idx`); err != nil {
				t.Fatalf("drop trigram index: %v", err)
			}
		}
		return ctx, db
	}
	ctxOn, dbOn := open()
	ctxOff, dbOff := open()
	ctxGate, dbGate := open()
	var gateName string
	if err := dbGate.QueryRowContext(ctxGate, `SELECT current_database()`).Scan(&gateName); err != nil {
		t.Fatal(err)
	}
	if _, err := dbGate.ExecContext(ctxGate, `ALTER DATABASE "`+gateName+`" SET eshu.secret_lines_derive = 'deferred'`); err != nil {
		t.Fatalf("mark the gated database deferred: %v", err)
	}
	dbGate.SetMaxIdleConns(0) // drop pooled connections so new ones inherit the setting
	dbGate.SetMaxIdleConns(2)
	for _, stmt := range []string{
		`DROP TRIGGER content_files_secret_lines_insert ON content_files`,
		`DROP TRIGGER content_files_secret_lines_update ON content_files`,
		`DROP TABLE content_file_secret_lines`,
	} {
		if _, err := dbOff.ExecContext(ctxOff, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	files := 0
	for _, records := range corpus {
		files += len(records)
	}
	type sample struct{ insert, changed, unchanged, insertCPU, changedCPU, unchangedCPU []time.Duration }
	var on, off, gate sample
	run := func(ctx context.Context, db *sql.DB, truncate string, into *sample) {
		if _, err := db.ExecContext(ctx, truncate); err != nil {
			t.Fatalf("%s: %v", truncate, err)
		}
		if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
			t.Fatalf("checkpoint: %v", err)
		}
		phase := func(suffix string, wall, cpu *[]time.Duration) {
			before := secretLinesBenchBackendCPU(ctx, t, db)
			*wall = append(*wall, secretLinesBenchWrite(ctx, t, db, corpus, suffix))
			*cpu = append(*cpu, secretLinesBenchBackendCPU(ctx, t, db)-before)
		}
		phase("", &into.insert, &into.insertCPU)
		phase("\n// touched\n", &into.changed, &into.changedCPU)
		phase("\n// touched\n", &into.unchanged, &into.unchangedCPU)
	}
	const truncateBase = "TRUNCATE content_entities, content_files, content_file_references"
	regimes := []func(){
		func() { run(ctxOff, dbOff, truncateBase, &off) },
		func() { run(ctxOn, dbOn, truncateBase+", content_file_secret_lines", &on) },
		func() { run(ctxGate, dbGate, truncateBase+", content_file_secret_lines", &gate) },
	}
	for round := 0; round < 5; round++ {
		for i := range regimes {
			regimes[(i+round)%len(regimes)]()
		}
	}

	var sideRows int
	if err := dbOn.QueryRowContext(ctxOn, `SELECT count(*) FROM content_file_secret_lines`).Scan(&sideRows); err != nil {
		t.Fatal(err)
	}
	if sideRows == 0 {
		t.Fatal("triggered database derived no side rows from the real corpus")
	}
	requireSecretLinesParity(t, ctxOn, dbOn, "after the write bench")
	report := func(name, metric string, offValues, onValues []time.Duration) {
		offMedian, onMedian := secretLinesBenchMedian(offValues), secretLinesBenchMedian(onValues)
		t.Logf("%-22s files=%d off_median=%.3fs on_median=%.3fs delta=%+.3fs (%+.1f%%, %+.3f ms/file) off=%v on=%v",
			name+" "+metric, files, offMedian.Seconds(), onMedian.Seconds(), (onMedian - offMedian).Seconds(),
			100*(onMedian.Seconds()-offMedian.Seconds())/offMedian.Seconds(),
			1000*(onMedian-offMedian).Seconds()/float64(files), offValues, onValues)
	}
	t.Logf("side rows derived from the real corpus: %d", sideRows)
	var gateRows int
	if err := dbGate.QueryRowContext(ctxGate, `SELECT count(*) FROM content_file_secret_lines`).Scan(&gateRows); err != nil {
		t.Fatal(err)
	}
	if gateRows != 0 {
		t.Fatalf("gated (deferred) database derived %d side rows, want 0", gateRows)
	}
	for _, regime := range []struct {
		label string
		with  sample
	}{{"triggers-on", on}, {"gated-deferred", gate}} {
		t.Logf("--- %s vs no triggers ---", regime.label)
		report("insert", "wall", off.insert, regime.with.insert)
		report("changed upsert", "wall", off.changed, regime.with.changed)
		report("unchanged", "wall", off.unchanged, regime.with.unchanged)
		report("insert", "backend-cpu", off.insertCPU, regime.with.insertCPU)
		report("changed upsert", "backend-cpu", off.changedCPU, regime.with.changedCPU)
		report("unchanged", "backend-cpu", off.unchangedCPU, regime.with.unchangedCPU)
	}
}

// secretLinesBenchBackendCPU returns the summed user+system CPU of this
// database's client backends, read from /proc through pg_read_file (the proof
// database runs as a superuser on the Postgres host). It is the load-robust
// companion to wall clock; it needs the pool to keep its connections between
// samples, which the bench's sequential writes do.
func secretLinesBenchBackendCPU(ctx context.Context, t *testing.T, db *sql.DB) time.Duration {
	t.Helper()
	var ticks float64
	if err := db.QueryRowContext(ctx, `
SELECT coalesce(sum(
  split_part(pg_read_file('/proc/' || pid || '/stat'), ' ', 14)::float8 +
  split_part(pg_read_file('/proc/' || pid || '/stat'), ' ', 15)::float8), 0)
FROM pg_stat_activity
WHERE datname = current_database() AND backend_type = 'client backend'`).Scan(&ticks); err != nil {
		t.Skipf("backend CPU is not readable from this Postgres host: %v", err)
	}
	return time.Duration(ticks * float64(time.Second) / 100) // USER_HZ is 100 on Linux
}
