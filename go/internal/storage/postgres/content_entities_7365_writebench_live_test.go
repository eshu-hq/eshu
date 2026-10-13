// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Entity-side counterpart of content_file_secret_lines_writebench_live_test.go
// for #7365: it replays the real ContentWriter.Write path with a synthetic
// file+entity corpus on two identically seeded disposable databases in
// A-B-B-A / B-A-A-B order and reports wall, per-stage, WAL, block-I/O,
// checkpoint, and wait-event deltas.
//
// Both databases keep the full index schema by default, which is the replay
// the issue prescribes (300-row entity batches, fixed pool and commit lanes,
// fixed concurrent writers). ESHU_CONTENT_ENTITIES_BENCH_NO_TRIGGERS=1 drops
// the migration-138 secret-lines triggers (and side table) in the B database
// to test the trigger factor; ESHU_CONTENT_ENTITIES_BENCH_NO_SOURCE_GIN=1
// additionally drops content_entities_source_trgm_idx there to test the
// source-cache index factor. Perturb one factor at a time.
//
// Set ESHU_CONTENT_ENTITIES_BENCH=1, ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN, and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it. Set
// ESHU_CONTENT_ENTITIES_BENCH_FILES (default 500) and
// ESHU_CONTENT_ENTITIES_BENCH_ENTS (default 100) to size the corpus, and
// ESHU_CONTENT_ENTITIES_BENCH_ROUNDS (default 2) for the A-B-B-A round count.
// The test asserts parity (equal calls, rows, and side-table derivation), not
// a timing bound: it is a diagnostic replay, so its medians are evidence, not
// a gate. The #7365 verdict and the next diagnostic step live in
// docs/internal/evidence/7365-entity-write-replay.md.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type entityBenchStageHandler struct {
	mu     sync.Mutex
	stages map[string][]float64
}

func (h *entityBenchStageHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *entityBenchStageHandler) Handle(_ context.Context, r slog.Record) error {
	var stage string
	var seconds float64
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "stage" {
			stage, _ = a.Value.Any().(string)
		}
		if a.Key == "duration_seconds" {
			seconds, _ = a.Value.Any().(float64)
		}
		return true
	})
	if stage != "" {
		h.mu.Lock()
		if h.stages == nil {
			h.stages = map[string][]float64{}
		}
		h.stages[stage] = append(h.stages[stage], seconds)
		h.mu.Unlock()
	}
	return nil
}
func (h *entityBenchStageHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *entityBenchStageHandler) WithGroup(string) slog.Handler      { return h }

func entityBenchMedian(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := slices.Clone(values)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

func entityBenchEnvInt(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// entityBenchCorpus builds a deterministic synthetic corpus: files text files
// with Go-shaped bodies (every 20th carrying a secret-pattern line so the
// secret-lines triggers do real derivation work when present) and entsPerFile
// function entities per file with code-shaped source caches that exercise the
// source-cache trigram index.
func entityBenchCorpus(files, entsPerFile int) (records []content.Record, entities []content.EntityRecord) {
	for f := 0; f < files; f++ {
		path := fmt.Sprintf("pkg/mod-%d/file-%d.go", f%20, f)
		var body strings.Builder
		body.WriteString("package mod\n\n")
		for l := 0; l < 120; l++ {
			fmt.Fprintf(&body, "// line %d of synthetic file %d with stable content shape\n", l, f)
		}
		if f%20 == 0 {
			body.WriteString("const testPassword = \"hunter2-secret-value\"\n")
		}
		records = append(records, content.Record{
			Path:     path,
			Body:     body.String(),
			Metadata: map[string]string{"language": "go"},
		})
		for e := 0; e < entsPerFile; e++ {
			var src strings.Builder
			fmt.Fprintf(&src, "func handler%d_%d(req request) response {\n", f, e)
			for l := 0; l < 12; l++ {
				fmt.Fprintf(&src, "\tstep%d := transform(input, %d) // entity body line\n", l, l)
			}
			src.WriteString("}\n")
			entities = append(entities, content.EntityRecord{
				EntityID:    fmt.Sprintf("bench-entity-%d-%d", f, e),
				Path:        path,
				EntityType:  "function",
				EntityName:  fmt.Sprintf("handler%d_%d", f, e),
				StartLine:   1,
				EndLine:     15,
				Language:    "go",
				SourceCache: src.String(),
			})
		}
	}
	return records, entities
}

type entityBenchIO struct {
	tables map[string][4]int64
	idx    map[string][2]int64
	wal    string
	timed  int64
	req    int64
}

func entityBenchSnapshot(ctx context.Context, t *testing.T, db *sql.DB) entityBenchIO {
	t.Helper()
	out := entityBenchIO{tables: map[string][4]int64{}, idx: map[string][2]int64{}}
	rows, err := db.QueryContext(ctx, `
SELECT relname, heap_blks_read, heap_blks_hit, idx_blks_read, idx_blks_hit
FROM pg_statio_all_tables
WHERE relname IN ('content_entities','content_files','content_file_secret_lines')`)
	if err != nil {
		t.Fatalf("statio tables: %v", err)
	}
	for rows.Next() {
		var name string
		var v [4]int64
		if err := rows.Scan(&name, &v[0], &v[1], &v[2], &v[3]); err != nil {
			rows.Close()
			t.Fatalf("scan statio: %v", err)
		}
		out.tables[name] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("statio rows: %v", err)
	}
	rows2, err := db.QueryContext(ctx, `
SELECT relname, idx_blks_read, idx_blks_hit
FROM pg_statio_all_indexes
WHERE relname IN ('content_entities_source_trgm_idx','content_entities_pkey','content_files_pkey')`)
	if err != nil {
		t.Fatalf("statio indexes: %v", err)
	}
	for rows2.Next() {
		var name string
		var r, h int64
		if err := rows2.Scan(&name, &r, &h); err != nil {
			rows2.Close()
			t.Fatalf("scan statio idx: %v", err)
		}
		out.idx[name] = [2]int64{r, h}
	}
	rows2.Close()
	if err := rows2.Err(); err != nil {
		t.Fatalf("statio idx rows: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&out.wal); err != nil {
		t.Fatalf("wal lsn: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT checkpoints_timed, checkpoints_req FROM pg_stat_bgwriter`).Scan(&out.timed, &out.req); err != nil {
		t.Fatalf("bgwriter: %v", err)
	}
	return out
}

func entityBenchWALBytes(ctx context.Context, t *testing.T, db *sql.DB, before, after string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRowContext(ctx, `SELECT ($2::pg_lsn - $1::pg_lsn)`, before, after).Scan(&n); err != nil {
		t.Fatalf("wal diff: %v", err)
	}
	return n
}

// prefixBenchEntityIDs namespaces the shared corpus entity IDs per
// repository. content_entities keys entity_id globally, so two repos writing
// the same corpus without this would steal each other's rows through
// ON CONFLICT DO UPDATE and destroy the populated seed.
func prefixBenchEntityIDs(source []content.EntityRecord, repo string) []content.EntityRecord {
	out := make([]content.EntityRecord, len(source))
	for i, e := range source {
		e.EntityID = repo + "/" + e.EntityID
		out[i] = e
	}
	return out
}

func entityBenchLoadavg() string {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(raw))
}

// TestContentEntitiesWriteCostBenchLive replays the #7365 prescribed
// comparison: the same real-writer corpus written into identically populated
// databases in interleaved A-B-B-A order with equal calls, rows, and bytes.
func TestContentEntitiesWriteCostBenchLive(t *testing.T) {
	if os.Getenv("ESHU_CONTENT_ENTITIES_BENCH") != "1" {
		t.Skip("set ESHU_CONTENT_ENTITIES_BENCH=1 to run the entity write-cost benchmark")
	}
	adminDSN := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN")
	optIn := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE")
	files := entityBenchEnvInt("ESHU_CONTENT_ENTITIES_BENCH_FILES", 500)
	ents := entityBenchEnvInt("ESHU_CONTENT_ENTITIES_BENCH_ENTS", 100)
	rounds := entityBenchEnvInt("ESHU_CONTENT_ENTITIES_BENCH_ROUNDS", 2)
	noTriggers := os.Getenv("ESHU_CONTENT_ENTITIES_BENCH_NO_TRIGGERS") == "1"
	noSourceGIN := os.Getenv("ESHU_CONTENT_ENTITIES_BENCH_NO_SOURCE_GIN") == "1"

	open := func() (context.Context, *sql.DB) {
		ctx, db := postgresproof.OpenDisposableDatabase(t, adminDSN, optIn, 45*time.Minute)
		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("apply bootstrap schema: %v", err)
		}
		db.SetMaxOpenConns(10)
		return ctx, db
	}
	ctxA, dbA := open()
	ctxB, dbB := open()
	if noTriggers || noSourceGIN {
		for _, stmt := range []string{
			`DROP TRIGGER content_files_secret_lines_insert ON content_files`,
			`DROP TRIGGER content_files_secret_lines_update ON content_files`,
			`DROP TABLE content_file_secret_lines`,
		} {
			if _, err := dbB.ExecContext(ctxB, stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
	if noSourceGIN {
		if _, err := dbB.ExecContext(ctxB, `DROP INDEX content_entities_source_trgm_idx`); err != nil {
			t.Fatalf("drop source trigram index: %v", err)
		}
	}

	var ver string
	_ = dbA.QueryRowContext(ctxA, `SELECT version()`).Scan(&ver)
	t.Logf("postgres: %.120s", ver)
	for _, q := range []string{`shared_buffers`, `max_connections`, `synchronous_commit`, `max_wal_size`, `checkpoint_completion_target`} {
		var v string
		_ = dbA.QueryRowContext(ctxA, `SHOW `+q).Scan(&v)
		t.Logf("setting %s=%s", q, v)
	}
	t.Logf("writer lanes: max_open_conns=10 entity_batch_concurrency=4 entity_batch_size=300")
	t.Logf("host loadavg before: %s", entityBenchLoadavg())

	records, entities := entityBenchCorpus(files, ents)
	t.Logf("corpus: %d files, %d entities", len(records), len(entities))
	mat := func(repo string) content.Materialization {
		return content.Materialization{
			RepoID: repo, ScopeID: "scope-" + repo, GenerationID: "gen-bench",
			SourceSystem: "git", Records: records, Entities: prefixBenchEntityIDs(entities, repo),
		}
	}
	write := func(ctx context.Context, db *sql.DB, h *entityBenchStageHandler, repo string) time.Duration {
		writer := NewContentWriter(SQLDB{DB: db}).WithBatchConcurrency(4)
		if h != nil {
			writer = writer.WithLogger(slog.New(h))
		}
		stop := make(chan struct{})
		var wg sync.WaitGroup
		counts := map[string]int64{}
		var smu sync.Mutex
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					rows, err := db.QueryContext(ctx, `
SELECT coalesce(wait_event_type,'CPU'), coalesce(wait_event,'CPU'), count(*)
FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()
AND state <> 'idle'
GROUP BY 1, 2`)
					if err != nil {
						continue
					}
					for rows.Next() {
						var typ, ev string
						var n int64
						if err := rows.Scan(&typ, &ev, &n); err != nil {
							rows.Close()
							break
						}
						smu.Lock()
						counts[typ+"/"+ev] += n
						smu.Unlock()
					}
					rows.Close()
				}
			}
		}()
		start := time.Now()
		_, err := writer.Write(ctx, mat(repo))
		elapsed := time.Since(start)
		close(stop)
		wg.Wait()
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		top := []string{}
		smu.Lock()
		for k, v := range counts {
			top = append(top, fmt.Sprintf("%s=%d", k, v))
		}
		smu.Unlock()
		sort.Strings(top)
		t.Logf("wait samples: %v", top)
		return elapsed
	}
	cleanMeasured := func(ctx context.Context, db *sql.DB) {
		if _, err := db.ExecContext(ctx, `DELETE FROM content_entities WHERE repo_id='bench-measured'`); err != nil {
			t.Fatalf("clean entities: %v", err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM content_files WHERE repo_id='bench-measured'`); err != nil {
			t.Fatalf("clean files: %v", err)
		}
		if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE) content_entities, content_files`); err != nil {
			t.Fatalf("vacuum: %v", err)
		}
		if _, err := db.ExecContext(ctx, `CHECKPOINT`); err != nil {
			t.Fatalf("checkpoint: %v", err)
		}
	}
	countRows := func(ctx context.Context, db *sql.DB, table string) int64 {
		var n int64
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE repo_id='bench-measured'`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	// Identically populated base state, kept across rounds so the measured
	// writes land on populated tables and indexes rather than empty ones.
	dbs := []struct {
		name string
		ctx  context.Context
		db   *sql.DB
	}{{"A", ctxA, dbA}, {"B", ctxB, dbB}}
	for i, c := range dbs {
		base := mat(fmt.Sprintf("bench-base-%d", i))
		base.ScopeID = "scope-" + base.RepoID
		w := NewContentWriter(SQLDB{DB: c.db}).WithBatchConcurrency(4)
		if _, err := w.Write(c.ctx, base); err != nil {
			t.Fatalf("seed base %s: %v", c.name, err)
		}
	}
	// Warmup (discarded): one measured write per database so buffer caches
	// are warm before the timed rounds.
	for _, c := range dbs {
		write(c.ctx, c.db, nil, "bench-measured")
		cleanMeasured(c.ctx, c.db)
	}

	type sample struct {
		wall     []float64
		entities []float64
		files    []float64
		wal      []float64
		entRead  []float64
		srcGIN   []float64
		ckpt     []float64
	}
	var on, off sample
	byName := map[string]struct {
		ctx context.Context
		db  *sql.DB
	}{"A": {ctxA, dbA}, "B": {ctxB, dbB}}
	for r := 0; r < rounds; r++ {
		order := []string{"A", "B", "B", "A"}
		if r%2 == 1 {
			order = []string{"B", "A", "A", "B"}
		}
		for _, name := range order {
			c := byName[name]
			cleanMeasured(c.ctx, c.db)
			before := entityBenchSnapshot(c.ctx, t, c.db)
			h := &entityBenchStageHandler{}
			wall := write(c.ctx, c.db, h, "bench-measured")
			after := entityBenchSnapshot(c.ctx, t, c.db)
			nEnt := countRows(c.ctx, c.db, "content_entities")
			nFile := countRows(c.ctx, c.db, "content_files")
			if nEnt != int64(len(entities)) || nFile != int64(len(records)) {
				t.Fatalf("parity %s: entities=%d files=%d, want %d %d",
					name, nEnt, nFile, len(entities), len(records))
			}
			var nSide int64
			if !noTriggers {
				nSide = countRows(c.ctx, c.db, "content_file_secret_lines")
				if nSide == 0 {
					t.Fatalf("parity %s: triggers on but no side rows derived for the measured repo", name)
				}
			}
			wal := float64(entityBenchWALBytes(c.ctx, t, c.db, before.wal, after.wal))
			entRead := float64(after.tables["content_entities"][0] - before.tables["content_entities"][0])
			srcGIN := float64(after.idx["content_entities_source_trgm_idx"][0] - before.idx["content_entities_source_trgm_idx"][0])
			ckpt := float64((after.timed + after.req) - (before.timed + before.req))
			target := &on
			if name == "B" {
				target = &off
			}
			target.wall = append(target.wall, wall.Seconds())
			target.entities = append(target.entities, entityBenchMedian(h.stages["upsert_entities"]))
			target.files = append(target.files, entityBenchMedian(h.stages["upsert_files"]))
			target.wal = append(target.wal, wal)
			target.entRead = append(target.entRead, entRead)
			target.srcGIN = append(target.srcGIN, srcGIN)
			target.ckpt = append(target.ckpt, ckpt)
			t.Logf("round write %s wall=%.3fs upsert_entities=%.3fs upsert_files=%.3fs wal=%.1fMB ent_heap_read=%v src_gin_read=%v ckpt=%v rows_ent=%d rows_side=%d",
				name, wall.Seconds(), entityBenchMedian(h.stages["upsert_entities"]), entityBenchMedian(h.stages["upsert_files"]),
				wal/1048576, entRead, srcGIN, ckpt, nEnt, nSide)
		}
	}

	report := func(label string, a, b []float64, unit string) {
		ma, mb := entityBenchMedian(a), entityBenchMedian(b)
		var pct float64
		if ma != 0 {
			pct = 100 * (mb - ma) / ma
		}
		t.Logf("%-16s A_median=%.3f%s B_median=%.3f%s delta=%+.3f%s (%+.1f%%) A=%v B=%v",
			label, ma, unit, mb, unit, mb-ma, unit, pct, a, b)
	}
	t.Logf("--- A vs B, %d files %d entities rounds=%d no_triggers=%v no_source_gin=%v ---",
		len(records), len(entities), rounds, noTriggers, noSourceGIN)
	report("wall", on.wall, off.wall, "s")
	report("upsert_entities", on.entities, off.entities, "s")
	report("upsert_files", on.files, off.files, "s")
	report("wal", on.wal, off.wal, "B")
	report("ent_heap_read", on.entRead, off.entRead, "blks")
	report("src_gin_read", on.srcGIN, off.srcGIN, "blks")
	report("checkpoints", on.ckpt, off.ckpt, "")
	t.Logf("host loadavg after: %s", entityBenchLoadavg())

	// One rolled-back EXPLAIN of a single 300-row entity batch per database,
	// built from the shipped batch constants so the plan cannot drift from
	// the production statement shape.
	for _, c := range dbs {
		tx, err := c.db.BeginTx(c.ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		q := strings.TrimSuffix(upsertContentEntityBatchPrefix, "VALUES ") +
			"SELECT 'bench-plan-'||g, 'bench-plan', 'p', 't', 'n', 1, 2, NULL, NULL, NULL, NULL, NULL, NULL, 'src '||g, '{}'::jsonb, now() FROM generate_series(1,300) g " +
			upsertContentEntityBatchSuffix
		rows, err := tx.QueryContext(c.ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) `+q)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("explain %s: %v", c.name, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				break
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		_ = tx.Rollback()
		t.Logf("entity batch plan %s:\n%s", c.name, plan.String())
	}
}
