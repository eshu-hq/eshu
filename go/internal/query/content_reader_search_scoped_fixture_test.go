// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// This fixture backs the #7930 scoped file-content work-bound guard and the
// per-repo UNION ALL probe. It runs against a disposable PostgreSQL with
// pg_trgm (ESHU_SCOPED_SEARCH_PROOF_DSN and
// ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE=1) and skips without it, following the
// repo's disposable-PG live-test pattern.
//
// The corpus is synthetic but production-shaped: one 12,000-file repository
// (the issue's largest tested repository held 12,403 files) plus eleven
// 300-file repositories, with hex-word filler and planted tokens (a dense
// token, a medium token, a 3-row rare literal, a trigram-less empty token, a
// unicode token, a percent-bearing token, and an underscore-wildcard probe).
// It carries the shape that matters to the guard (a (repo_id, relative_path)
// primary key, the shipped content trigram GIN name, dense and selective
// predicates, grants late in key order so an unscoped-order walk must reject
// real rows) and none of any real corpus.

const (
	scopedProofDSNEnv   = "ESHU_SCOPED_SEARCH_PROOF_DSN"
	scopedProofOptInEnv = "ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE"

	scopedBigRepo        = "scope-repo-big"
	scopedBigRows        = 12000
	scopedSmallRepoCount = 11
	scopedSmallRows      = 300

	scopedDenseToken   = "handler"
	scopedMediumToken  = "widget"
	scopedRareToken    = "zq_rare_literal_k"
	scopedNoTrigram    = "zz"
	scopedUnicodeToken = "naïveté"
	scopedPercentFrag  = "100%coverage"
	scopedUnderscore   = "han_ler"
)

// scopedSmallRepo names the r-th small repository (r in 0..10).
func scopedSmallRepo(r int) string {
	return fmt.Sprintf("scope-repo-%02d", r)
}

// scopedCell is one shipped query shape under guard: single-scope cells run
// through searchFileContentPage, multi-scope cells through
// searchFileContentInRepos. Each cell is measured in both plan regimes
// (custom PREPARE/EXECUTE default and force_generic_plan), giving the 20-cell
// work-bound matrix.
type scopedCell struct {
	name    string
	multi   bool
	repoIDs []string
	pattern string
	limit   int
	offset  int
}

// scopedWorkboundCells returns the ten shipped cells of the #7930 guard:
// S1-S7 single-scope, M1-M3 multi-scope. S1 is the healthy common-term
// custom path the kill rule protects; S5/M2 carry a trigram-less empty term
// that forces a full recheck; M1/M3 hold grants late in key order so a
// key-ordered walk over the ANY predicate must reject real rows.
func scopedWorkboundCells() []scopedCell {
	return []scopedCell{
		{name: "S1 dense first page", repoIDs: []string{scopedBigRepo}, pattern: scopedDenseToken, limit: 10, offset: 0},
		{name: "S2 dense deep page", repoIDs: []string{scopedBigRepo}, pattern: scopedDenseToken, limit: 10, offset: 10},
		{name: "S3 medium term", repoIDs: []string{scopedBigRepo}, pattern: scopedMediumToken, limit: 10, offset: 0},
		{name: "S4 rare term", repoIDs: []string{scopedBigRepo}, pattern: scopedRareToken, limit: 10, offset: 0},
		{name: "S5 empty term without trigram", repoIDs: []string{scopedBigRepo}, pattern: scopedNoTrigram, limit: 10, offset: 0},
		{name: "S6 unicode term", repoIDs: []string{scopedSmallRepo(0)}, pattern: scopedUnicodeToken, limit: 10, offset: 0},
		{name: "S7 underscore wildcard", repoIDs: []string{scopedBigRepo}, pattern: scopedUnderscore, limit: 10, offset: 0},
		{name: "M1 multi dense late grants", multi: true, repoIDs: []string{scopedSmallRepo(8), scopedSmallRepo(9), scopedSmallRepo(10)}, pattern: scopedDenseToken, limit: 10, offset: 0},
		{name: "M2 multi empty term without trigram", multi: true, repoIDs: []string{scopedSmallRepo(8), scopedSmallRepo(9), scopedSmallRepo(10)}, pattern: scopedNoTrigram, limit: 10, offset: 0},
		{name: "M3 multi duplicate-grant deep page", multi: true, repoIDs: []string{scopedSmallRepo(9), scopedSmallRepo(9), scopedSmallRepo(10)}, pattern: scopedDenseToken, limit: 10, offset: 10},
	}
}

// openScopedWorkboundFixture creates the synthetic scoped-search corpus and
// returns a context and handle to the disposable database. It records the
// database collation and encoding for the ordering-oracle disclosure.
func openScopedWorkboundFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv(scopedProofDSNEnv),
		os.Getenv(scopedProofOptInEnv),
		5*time.Minute,
	)
	postgresproof.InstallTrigramExtension(ctx, t, db)
	statements := []string{
		`CREATE TABLE content_files (
			repo_id text NOT NULL,
			relative_path text NOT NULL,
			commit_sha text,
			content text NOT NULL,
			content_hash text NOT NULL,
			line_count integer NOT NULL,
			language text,
			artifact_type text,
			PRIMARY KEY (repo_id, relative_path)
		)`,
		fmt.Sprintf(`INSERT INTO content_files
			(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			SELECT '%[1]s',
			       'src/file-' || lpad(g::text, 6, '0') || '.go',
			       'sha',
			       (SELECT string_agg(md5(g::text || ':' || k::text), ' ') FROM generate_series(1, CASE WHEN g %% 20 = 0 THEN 120 WHEN g %% 2 = 1 THEN 8 ELSE 30 END) k)
			         || CASE WHEN g %% 4 = 0 THEN ' %[2]s' ELSE '' END
			         || CASE WHEN g %% 50 = 7 THEN ' %[3]s' ELSE '' END
			         || CASE WHEN g IN (101, 5901, 11999) THEN ' %[4]s' ELSE '' END
			         || CASE WHEN g %% 701 = 3 THEN ' %[5]s' ELSE '' END
			         || CASE WHEN g %% 333 = 5 THEN ' %[6]s' ELSE '' END,
			       md5(g::text), 1, 'go', 'source'
			FROM generate_series(1, %[7]d) g`,
			scopedBigRepo, scopedDenseToken, scopedMediumToken,
			scopedRareToken, scopedUnicodeToken, scopedPercentFrag, scopedBigRows),
		fmt.Sprintf(`INSERT INTO content_files
			(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			SELECT 'scope-repo-' || lpad(r::text, 2, '0'),
			       'src/mod-' || lpad(h::text, 4, '0') || '.go',
			       'sha',
			       (SELECT string_agg(md5(r::text || ':' || h::text || ':' || k::text), ' ') FROM generate_series(1, CASE WHEN h %% 20 = 0 THEN 60 WHEN h %% 2 = 1 THEN 8 ELSE 20 END) k)
			         || CASE WHEN h %% 4 = 0 THEN ' %[1]s' ELSE '' END
			         || CASE WHEN h %% 25 = 3 THEN ' %[2]s' ELSE '' END
			         || CASE WHEN r = 0 AND h IN (7, 150) THEN ' %[3]s' ELSE '' END
			         || CASE WHEN h %% 100 = 11 THEN ' %[4]s' ELSE '' END,
			       md5(r::text || ':' || h::text), 1, 'go', 'source'
			FROM generate_series(0, %[5]d) r CROSS JOIN generate_series(1, %[6]d) h`,
			scopedDenseToken, scopedMediumToken, scopedUnicodeToken, scopedPercentFrag,
			scopedSmallRepoCount-1, scopedSmallRows),
		`UPDATE content_files SET relative_path = 'src/über-mod.go'
			WHERE repo_id = 'scope-repo-00' AND relative_path = 'src/mod-0007.go'`,
		`CREATE INDEX content_files_content_trgm_idx ON content_files USING gin (content gin_trgm_ops)`,
		`VACUUM ANALYZE content_files`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed scoped search fixture: %v\n%s", err, statement)
		}
	}
	var collate, ctype, encoding string
	if err := db.QueryRowContext(ctx,
		`SELECT datcollate, datctype, pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database()`,
	).Scan(&collate, &ctype, &encoding); err != nil {
		t.Fatalf("read fixture collation: %v", err)
	}
	t.Logf("fixture collation datcollate=%s datctype=%s encoding=%s", collate, ctype, encoding)
	if encoding != "UTF8" {
		t.Fatalf("fixture encoding = %s, want UTF8 for the unicode oracle", encoding)
	}
	return ctx, db
}

// shippedScopedQuery derives the exact shipped statement for a cell from the
// production builders (fileContentSingleScopeWhere/fileContentMultiScopeWhere
// plus fileContentScopedQuery). No shipped SQL text is copied here: if the
// shipped builders move, this derivation moves with them.
func shippedScopedQuery(cell scopedCell) (string, []any) {
	if cell.multi {
		where := fileContentMultiScopeWhere(1, 2)
		return fileContentScopedQuery(where, 3, 4),
			[]any{strings.Join(cell.repoIDs, "\x1f"), cell.pattern, int64(cell.limit), int64(cell.offset)}
	}
	where := fileContentSingleScopeWhere(1, 2)
	return fileContentScopedQuery(where, 3, 4),
		[]any{cell.repoIDs[0], cell.pattern, int64(cell.limit), int64(cell.offset)}
}

// scopedPlanNode is the EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) subset the
// work-bound guard reads. PG 18 reports bitmap heap usage as flat
// "Exact Heap Blocks"/"Lossy Heap Blocks" fields, not a nested object.
type scopedPlanNode struct {
	NodeType           string           `json:"Node Type"`
	IndexName          string           `json:"Index Name"`
	ActualLoops        *float64         `json:"Actual Loops"`
	SharedHitBlocks    *int64           `json:"Shared Hit Blocks"`
	SharedReadBlocks   *int64           `json:"Shared Read Blocks"`
	ExactHeapBlocks    *int64           `json:"Exact Heap Blocks"`
	LossyHeapBlocks    *int64           `json:"Lossy Heap Blocks"`
	RowsRemovedFilter  *int64           `json:"Rows Removed by Filter"`
	RowsRemovedRecheck *int64           `json:"Rows Removed by Index Recheck"`
	Plans              []scopedPlanNode `json:"Plans"`
}

// scopedWorkStats is the per-cell work account: root buffer totals (parents
// already include their children, so only the root counts for buffers) plus
// executed-node sums for heap blocks and removed rows.
type scopedWorkStats struct {
	sharedHit  int64
	sharedRead int64
	heapExact  int64
	heapLossy  int64
	removed    int64
	unexecuted []string
	shapes     []string
}

func (n scopedPlanNode) executed() bool {
	return n.ActualLoops != nil && *n.ActualLoops > 0
}

func parseScopedWorkStats(raw string) (scopedWorkStats, error) {
	var plans []struct {
		Plan scopedPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		return scopedWorkStats{}, fmt.Errorf("decode plan: %v; %s", err, raw)
	}
	stats := scopedWorkStats{}
	var walk func(n scopedPlanNode, root bool)
	walk = func(n scopedPlanNode, root bool) {
		if root {
			if n.SharedHitBlocks != nil {
				stats.sharedHit = *n.SharedHitBlocks
			}
			if n.SharedReadBlocks != nil {
				stats.sharedRead = *n.SharedReadBlocks
			}
		}
		if !n.executed() {
			stats.unexecuted = append(stats.unexecuted, n.NodeType+"/"+n.IndexName)
		} else {
			stats.shapes = append(stats.shapes, n.NodeType+"/"+n.IndexName)
			if n.NodeType == "Bitmap Heap Scan" {
				if n.ExactHeapBlocks != nil {
					stats.heapExact += *n.ExactHeapBlocks
				}
				if n.LossyHeapBlocks != nil {
					stats.heapLossy += *n.LossyHeapBlocks
				}
			}
			if n.RowsRemovedFilter != nil {
				stats.removed += *n.RowsRemovedFilter
			}
			if n.RowsRemovedRecheck != nil {
				stats.removed += *n.RowsRemovedRecheck
			}
		}
		for _, child := range n.Plans {
			walk(child, false)
		}
	}
	walk(plans[0].Plan, true)
	return stats, nil
}

// explainScopedWork runs a warm-cache EXPLAIN (ANALYZE, BUFFERS) of a
// statement with bound prepared params through a protocol-level named prepared
// statement, the same Parse/Bind/Execute path the serving reader uses.
// generic=false keeps the default custom plan; generic=true forces the generic
// plan with plan_cache_mode = force_generic_plan. Each run uses its own
// transaction so SET LOCAL cannot leak between regimes.
func explainScopedWork(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any, generic bool) scopedWorkStats {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if generic {
		if _, err := tx.ExecContext(ctx, `SET LOCAL plan_cache_mode = force_generic_plan`); err != nil {
			t.Fatalf("force generic plan: %v", err)
		}
	}
	stmt, err := tx.PrepareContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+query)
	if err != nil {
		t.Fatalf("prepare cell statement: %v\n%s", err, query)
	}
	defer func() { _ = stmt.Close() }()
	var raw string
	if err := stmt.QueryRowContext(ctx, args...).Scan(&raw); err != nil {
		t.Fatalf("explain cell: %v", err)
	}
	stats, err := parseScopedWorkStats(raw)
	if err != nil {
		t.Fatalf("parse cell plan: %v", err)
	}
	return stats
}

// selectScopedRows executes a statement with bound prepared params under one
// regime and returns byte fingerprints of the ordered 8-field rows.
func selectScopedRows(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any, generic bool) []string {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin select tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if generic {
		if _, err := tx.ExecContext(ctx, `SET LOCAL plan_cache_mode = force_generic_plan`); err != nil {
			t.Fatalf("force generic plan: %v", err)
		}
	}
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		t.Fatalf("prepare rows statement: %v\n%s", err, query)
	}
	defer func() { _ = stmt.Close() }()
	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		t.Fatalf("execute rows statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	return fingerprintScopedRows(t, rows)
}

// fingerprintScopedRows scans the shipped 8-column file-content SELECT into
// deterministic per-row fingerprints preserving DB order.
func fingerprintScopedRows(t *testing.T, rows *sql.Rows) []string {
	t.Helper()
	var out []string
	for rows.Next() {
		var repo, path, sha, content, hash, language, artifact string
		var lines int
		if err := rows.Scan(&repo, &path, &sha, &content, &hash, &lines, &language, &artifact); err != nil {
			t.Fatalf("scan scoped row: %v", err)
		}
		out = append(out, strings.Join([]string{repo, path, sha, content, hash, fmt.Sprint(lines), language, artifact}, "\x1f"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scoped rows: %v", err)
	}
	return out
}

// fingerprintFileContents fingerprints reader-level FileContent values with
// the same field order as the SQL oracle so the two can be compared directly.
func fingerprintFileContents(files []FileContent) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, strings.Join([]string{
			file.RepoID, file.RelativePath, file.CommitSHA, file.Content,
			file.ContentHash, fmt.Sprint(file.LineCount), file.Language, file.ArtifactType,
		}, "\x1f"))
	}
	return out
}

// scopedRemovedCap is the per-cell removed-rows bound: at most 200 rows, or
// ten times the requested window, whichever is larger.
func scopedRemovedCap(limit, offset int) int64 {
	if cap := int64(10 * (limit + offset)); cap > 200 {
		return cap
	}
	return 200
}

// reportScopedWorkBounds logs the full work account for a cell-regime and
// returns whether any bound is exceeded, without failing the test. Probes use
// this: a red probe where shipped is also red is a NO-GAIN verdict, not a
// test failure (only the guard fails shipped-red cells).
func reportScopedWorkBounds(t *testing.T, cellName, regime string, stats scopedWorkStats, limit, offset int) bool {
	t.Helper()
	removedCap := scopedRemovedCap(limit, offset)
	t.Logf("work cell=%q regime=%s shared_hit=%d shared_read=%d heap_exact=%d heap_lossy=%d removed=%d (cap %d) shapes=%s",
		cellName, regime, stats.sharedHit, stats.sharedRead, stats.heapExact, stats.heapLossy, stats.removed, removedCap, strings.Join(stats.shapes, " "))
	if len(stats.unexecuted) > 0 {
		t.Logf("work cell=%q regime=%s never-executed branches (reported, not failed): %s",
			cellName, regime, strings.Join(stats.unexecuted, " "))
	}
	return workViolated(stats, limit, offset)
}

// checkScopedWorkBounds logs the full work account for a cell-regime and
// fails the test when any bound is exceeded. Unexecuted branches are reported
// here, never failed: a plan that skips a branch did less work, not more.
// workViolated reports whether stats exceed the guard caps without logging a
// test failure: the probe test must read the shipped verdict, not re-fail it
// (the guard already fails shipped-red cells).
func workViolated(stats scopedWorkStats, limit, offset int) bool {
	return stats.sharedHit > 400 || stats.sharedRead != 0 || stats.heapExact > 100 || stats.removed > scopedRemovedCap(limit, offset)
}

func checkScopedWorkBounds(t *testing.T, cellName, regime string, stats scopedWorkStats, limit, offset int) bool {
	t.Helper()
	violated := reportScopedWorkBounds(t, cellName, regime, stats, limit, offset)
	removedCap := scopedRemovedCap(limit, offset)
	if stats.sharedHit > 400 {
		t.Errorf("work cell=%q regime=%s: shared hit buffers = %d, want <= 400", cellName, regime, stats.sharedHit)
		violated = true
	}
	if stats.sharedRead != 0 {
		t.Errorf("work cell=%q regime=%s: shared read buffers = %d, want 0 (warm cache)", cellName, regime, stats.sharedRead)
		violated = true
	}
	if stats.heapExact > 100 {
		t.Errorf("work cell=%q regime=%s: exact heap blocks = %d, want <= 100", cellName, regime, stats.heapExact)
		violated = true
	}
	if stats.removed > removedCap {
		t.Errorf("work cell=%q regime=%s: rows removed = %d, want <= %d", cellName, regime, stats.removed, removedCap)
		violated = true
	}
	return violated
}
