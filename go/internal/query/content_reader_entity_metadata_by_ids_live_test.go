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

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// #7247 live proof for the id-anchored language-query metadata read. It runs
// against a disposable PostgreSQL database with every bootstrap migration
// applied (so the real primary key and content_entities indexes exist), seeds
// a large repository with generate_series, and checks, for the graph page's
// 200 ids:
//
//   - the read returns exactly those rows, and nothing outside the scope;
//   - its plan is a primary-key index scan under a custom AND a forced
//     generic plan, with shared buffers bounded by the id count rather than
//     by the repository's row count.
//
// It logs the buffers of the filtered page read it replaces, under the same
// two plan modes, as non-gating comparison evidence.
const (
	metadataByIDsLiveRepo = "repository:r_metadata_live_big"
	// metadataByIDsLiveOtherRepo prefixes the three other repositories the
	// fixture seeds (suffixes 1..3, 40,000 PHP Functions each, paths sorting
	// before the big repository's); none is granted to the id lookup.
	metadataByIDsLiveOtherRepo = "repository:r_metadata_live_other"
	metadataByIDsLiveRows      = 40000
	metadataByIDsLiveFiles     = 2500
	metadataByIDsLivePage      = 200
)

func TestEntityMetadataByIDsLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedMetadataByIDsLiveFixture(ctx, t, db)

	// The graph page orders by relative_path, name; take its first 200 ids
	// the way the language-query graph read would hand them over.
	ids := metadataByIDsLiveGraphPageIDs(ctx, t, db)
	reader := NewContentReader(db)

	for _, tc := range []struct {
		name   string
		lookup taxonomy.LanguageEntityIDLookup
		want   int
	}{
		{"grant", taxonomy.LanguageEntityIDLookup{EntityIDs: ids, AllowedRepositoryIDs: []string{metadataByIDsLiveRepo}}, len(ids)},
		{"repository", taxonomy.LanguageEntityIDLookup{EntityIDs: ids, RepoID: metadataByIDsLiveRepo}, len(ids)},
		{"unscoped", taxonomy.LanguageEntityIDLookup{EntityIDs: ids}, len(ids)},
		{"other grant", taxonomy.LanguageEntityIDLookup{EntityIDs: ids, AllowedRepositoryIDs: []string{metadataByIDsLiveOtherRepo + "1"}}, 0},
		{"other repository", taxonomy.LanguageEntityIDLookup{EntityIDs: ids, RepoID: metadataByIDsLiveOtherRepo + "2"}, 0},
	} {
		rows, err := reader.ListEntityMetadataByIDsForAccess(ctx, tc.lookup)
		if err != nil {
			t.Fatalf("%s: ListEntityMetadataByIDsForAccess(): %v", tc.name, err)
		}
		if len(rows) != tc.want {
			t.Fatalf("%s: rows = %d, want %d", tc.name, len(rows), tc.want)
		}
		for _, row := range rows {
			if row.Metadata["ordinal"] == nil || row.SourceCache != "" {
				t.Fatalf("%s: row %+v lacks decoded metadata or carries source_cache", tc.name, row)
			}
		}
	}

	lookup := taxonomy.LanguageEntityIDLookup{EntityIDs: ids, AllowedRepositoryIDs: []string{metadataByIDsLiveRepo}}
	statement, args := buildEntityMetadataByIDsQuery(uniqueStrings(ids), lookup)
	page, pageArgs := metadataByIDsLivePageRead()
	for _, mode := range []string{"auto", "force_generic_plan"} {
		anchored := explainMetadataByIDsLive(ctx, t, db, mode, statement, args)
		if anchored.seqScan {
			t.Fatalf("%s: id lookup plan has a Seq Scan on content_entities:\n%s", mode, anchored.raw)
		}
		if !anchored.usesPrimaryKey {
			t.Fatalf("%s: id lookup plan does not use content_entities_pkey:\n%s", mode, anchored.raw)
		}
		if anchored.rows != len(ids) {
			t.Fatalf("%s: id lookup returned %d rows in EXPLAIN ANALYZE, want %d", mode, anchored.rows, len(ids))
		}
		// Each id is one primary-key probe: a btree descent (a few index
		// pages) plus one heap page. 10 buffers per id is a generous bound
		// that still fails if the read ever scales with the repository.
		if limit := 10 * len(ids); anchored.buffers > limit {
			t.Fatalf("%s: id lookup read %d shared buffers, want <= %d", mode, anchored.buffers, limit)
		}
		old := explainMetadataByIDsLive(ctx, t, db, mode, page, pageArgs)
		t.Logf("plan_cache_mode=%s anchored: rows=%d buffers=%d exec_ms=%.3f | page read: rows=%d buffers=%d exec_ms=%.3f (exec_ms non-gating)",
			mode, anchored.rows, anchored.buffers, anchored.execMS, old.rows, old.buffers, old.execMS)
	}
}

func seedMetadataByIDsLiveFixture(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO content_entities
			(entity_id, repo_id, relative_path, entity_type, entity_name,
			 start_line, end_line, language, source_cache, metadata, indexed_at)
		SELECT 'content-entity:e_big_' || lpad(g::text, 6, '0'),
		       $1,
		       'src/dir' || (g %% 50) || '/file' || (g %% %[1]d) || '.php',
		       CASE WHEN g %% 10 = 0 THEN 'Class' ELSE 'Function' END,
		       CASE WHEN g %% 400 = 1 THEN 'handleQz' || g ELSE 'fn' || md5(g::text) END,
		       g, g + 5, 'php',
		       repeat('x', 200),
		       jsonb_build_object('ordinal', g, 'docstring', 'doc ' || g),
		       now()
		FROM generate_series(1, %[2]d) AS g
	`, metadataByIDsLiveFiles, metadataByIDsLiveRows), metadataByIDsLiveRepo)
	if err != nil {
		t.Fatalf("seed big repository: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO content_entities
			(entity_id, repo_id, relative_path, entity_type, entity_name,
			 start_line, end_line, language, source_cache, metadata, indexed_at)
		SELECT 'content-entity:e_other_' || r || '_' || g, $1 || r, 'aaa/dir' || (g % 50) || '/f' || g || '.php',
		       'Function', 'other' || md5(g::text), g, g + 1, 'php', repeat('y', 200),
		       jsonb_build_object('ordinal', g), now()
		FROM generate_series(1, 3) AS r, generate_series(1, 40000) AS g
	`, metadataByIDsLiveOtherRepo)
	if err != nil {
		t.Fatalf("seed other repository: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE content_entities"); err != nil {
		t.Fatalf("analyze content_entities: %v", err)
	}
}

func metadataByIDsLiveGraphPageIDs(ctx context.Context, t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT entity_id FROM content_entities
		WHERE repo_id = $1 AND entity_type = 'Function'
		ORDER BY relative_path, entity_name
		LIMIT $2
	`, metadataByIDsLiveRepo, metadataByIDsLivePage)
	if err != nil {
		t.Fatalf("read graph-page ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]string, 0, metadataByIDsLivePage)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan graph-page id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) != metadataByIDsLivePage {
		t.Fatalf("graph-page ids = %d (err %v), want %d", len(ids), err, metadataByIDsLivePage)
	}
	return ids
}

// metadataByIDsLivePageRead renders the pre-#7247 enrichment read for the big
// repository's Function page with a two-character name pattern that matches
// one row in 400 -- the selective-pattern shape the issue measured as the
// worst generic-plan case: the 200-row page cannot fill early, so the read
// walks every candidate row.
func metadataByIDsLivePageRead() (string, []any) {
	filters, args, nextArg := buildLanguageTypeEntityFilters(
		"", []string{metadataByIDsLiveRepo}, normalizedLanguageVariants("php"), "Function", "qz",
	)
	where := strings.Join(filters, " AND ")
	statement := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE EXISTS (SELECT 1 FROM content_entities WHERE %s) AND %s
		ORDER BY relative_path, start_line, entity_name
		LIMIT $%d
	`, where, where, nextArg)
	return statement, append(args, metadataByIDsLivePage)
}

type metadataByIDsLivePlan struct {
	raw            string
	rows           int
	buffers        int
	execMS         float64
	seqScan        bool
	usesPrimaryKey bool
}

// explainMetadataByIDsLive runs EXPLAIN (ANALYZE, BUFFERS) on one pinned
// connection under the given plan_cache_mode. The statement is PREPAREd with
// its bind values as typed literals, so force_generic_plan really plans it
// without the values, the way a cached pgx statement can be planned.
func explainMetadataByIDsLive(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	mode string,
	statement string,
	args []any,
) metadataByIDsLivePlan {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
		t.Fatalf("set plan_cache_mode: %v", err)
	}
	types := make([]string, 0, len(args))
	literals := make([]string, 0, len(args))
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			types = append(types, "text")
			literals = append(literals, "'"+strings.ReplaceAll(v, "'", "''")+"'")
		case int:
			types = append(types, "integer")
			literals = append(literals, fmt.Sprint(v))
		case *array.StringArray:
			value, err := v.Value()
			if err != nil {
				t.Fatalf("render array literal: %v", err)
			}
			types = append(types, "text[]")
			literals = append(literals, "'"+strings.ReplaceAll(value.(string), "'", "''")+"'")
		default:
			t.Fatalf("unsupported explain arg %T", arg)
		}
	}
	name := "p7247_" + strings.ReplaceAll(mode, "_", "")
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PREPARE %s(%s) AS %s", name, strings.Join(types, ", "), statement)); err != nil {
		t.Fatalf("prepare %s: %v", name, err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "DEALLOCATE "+name) }()

	var raw string
	query := fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE %s(%s)", name, strings.Join(literals, ", "))
	if err := conn.QueryRowContext(ctx, query).Scan(&raw); err != nil {
		t.Fatalf("explain %s: %v", name, err)
	}
	var doc []struct {
		Plan          map[string]any `json:"Plan"`
		ExecutionTime float64        `json:"Execution Time"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || len(doc) != 1 {
		t.Fatalf("decode explain json: %v", err)
	}
	plan := metadataByIDsLivePlan{
		raw:     raw,
		rows:    int(doc[0].Plan["Actual Rows"].(float64)),
		buffers: int(doc[0].Plan["Shared Hit Blocks"].(float64) + doc[0].Plan["Shared Read Blocks"].(float64)),
		execMS:  doc[0].ExecutionTime,
	}
	walkMetadataByIDsLivePlan(doc[0].Plan, &plan)
	return plan
}

func walkMetadataByIDsLivePlan(node map[string]any, plan *metadataByIDsLivePlan) {
	if node["Node Type"] == "Seq Scan" && node["Relation Name"] == "content_entities" {
		plan.seqScan = true
	}
	if node["Index Name"] == "content_entities_pkey" {
		plan.usesPrimaryKey = true
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if childNode, ok := child.(map[string]any); ok {
			walkMetadataByIDsLivePlan(childNode, plan)
		}
	}
}
