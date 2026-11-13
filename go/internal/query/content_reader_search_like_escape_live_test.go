// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10604 fix 05b proof that content search matches literally: the Go substring oracle (strings.Contains on lowercased content) agrees with the reader on scoped (1+N repos) and unscoped file and entity paths for wildcard-bearing patterns
//eshu:readiness:env ESHU_LIKE_ESCAPE_PROOF_DISPOSABLE ESHU_LIKE_ESCAPE_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// likeEscapeRows is the fixed differential corpus: plain, %, _, and \
// bearing contents, a code-like mixed row, and a non-ASCII row, spread over
// two repositories. The entity source_cache column carries the same texts so
// both shapes prove the same contract.
var likeEscapeRows = []struct {
	repo, path, content string
}{
	{"repo-a", "src/a1.go", "call handler(x) here"},
	{"repo-a", "src/a2.go", "100%coverage guaranteed"},
	{"repo-a", "src/a3.go", "the han_ler literal stands"},
	{"repo-a", "src/a4.go", `fmt.Sprintf("%d", a_b)`},
	{"repo-a", "src/a5.go", `trailing backslash here \`},
	{"repo-a", "src/a6.go", "naïveté_100% mixed"},
	{"repo-a", "src/a7.go", "plain token alpha"},
	{"repo-b", "src/b1.go", "another handler here"},
	{"repo-b", "src/b2.go", "100%coverage again"},
	{"repo-b", "src/b3.go", "nothing alike"},
}

// likeEscapePatterns covers the differential classes: plain, each wildcard
// alone, every wildcard combined with literal runs, code-like mixtures, and
// non-ASCII.
var likeEscapePatterns = []string{
	"handler",
	"han_ler",
	"100%",
	"100%coverage",
	"%",
	"_",
	`\`,
	`fmt.Sprintf("%d", a_b)`,
	"a_b",
	"naïveté",
	"naïveté_100%",
	"trail",
	"absent-token-xyz",
}

// openLikeEscapeFixture creates the differential corpus in a disposable
// database with the shipped index shapes (trigram GIN on content and on the
// stored lowercase copy for the file walk, substring readiness functions), so
// every search path plans the way production does.
func openLikeEscapeFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_LIKE_ESCAPE_PROOF_DSN"),
		os.Getenv("ESHU_LIKE_ESCAPE_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
	postgresproof.InstallTrigramExtension(ctx, t, db)
	statements := []string{
		`CREATE TABLE content_files (
			repo_id text NOT NULL,
			relative_path text NOT NULL,
			commit_sha text,
			content text NOT NULL,
			content_lower text GENERATED ALWAYS AS (lower(content)) STORED,
			content_hash text NOT NULL,
			line_count integer NOT NULL,
			language text,
			artifact_type text,
			PRIMARY KEY (repo_id, relative_path)
		)`,
		`CREATE TABLE content_entities (
			entity_id text NOT NULL PRIMARY KEY,
			repo_id text NOT NULL,
			relative_path text NOT NULL,
			entity_type text NOT NULL,
			entity_name text NOT NULL,
			start_line integer NOT NULL,
			end_line integer NOT NULL,
			language text,
			source_cache text,
			metadata jsonb
		)`,
		`CREATE FUNCTION eshu_require_content_substring_indexes_ready() RETURNS boolean
			LANGUAGE sql STABLE AS 'SELECT true'`,
		`CREATE FUNCTION eshu_require_content_lower_trgm_index_ready() RETURNS boolean
			LANGUAGE sql STABLE AS 'SELECT true'`,
		`CREATE INDEX content_files_content_trgm_idx ON content_files USING gin (content gin_trgm_ops)`,
		`CREATE INDEX content_files_content_lower_trgm_idx ON content_files USING gin (content_lower gin_trgm_ops)`,
		`CREATE INDEX content_entities_source_cache_trgm_idx ON content_entities USING gin (source_cache gin_trgm_ops)`,
		`VACUUM ANALYZE content_files`,
		`VACUUM ANALYZE content_entities`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed like-escape fixture DDL: %v\n%s", err, statement)
		}
	}
	for i, row := range likeEscapeRows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO content_files
				(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
				VALUES ($1, $2, 'sha', $3, 'hash', 1, 'go', 'source')`,
			row.repo, row.path, row.content); err != nil {
			t.Fatalf("seed like-escape file row: %v", err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO content_entities
				(entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata)
				VALUES ($1, $2, $3, 'function', $4, $5, $5, 'go', $6, NULL)`,
			strings.Join([]string{"ent", row.repo, row.path}, "-"),
			row.repo, row.path, "entity-name", i+1, row.content); err != nil {
			t.Fatalf("seed like-escape entity row: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE content_files, content_entities`); err != nil {
		t.Fatalf("analyze like-escape fixture: %v", err)
	}
	return ctx, db
}

// goOracleFileKeys is the independent substring oracle over content_files: the
// (repo, path) keys whose content contains the pattern as a literal
// case-insensitive substring, in key order with the page window applied. It
// reads the bodies itself and never issues a LIKE predicate.
func goOracleFileKeys(t *testing.T, ctx context.Context, db *sql.DB, repos []string, pattern string, limit, offset int) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		`SELECT repo_id, relative_path, content FROM content_files ORDER BY repo_id, relative_path`)
	if err != nil {
		t.Fatalf("oracle file scan: %v", err)
	}
	defer func() { _ = rows.Close() }()
	want := map[string]bool{}
	for _, repo := range repos {
		want[repo] = true
	}
	lower := strings.ToLower(pattern)
	var keys []string
	for rows.Next() {
		var repo, path, content string
		if err := rows.Scan(&repo, &path, &content); err != nil {
			t.Fatalf("oracle file row: %v", err)
		}
		if len(repos) > 0 && !want[repo] {
			continue
		}
		if strings.Contains(strings.ToLower(content), lower) {
			keys = append(keys, repo+"/"+path)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("oracle file rows: %v", err)
	}
	sort.Strings(keys)
	from := min(offset, len(keys))
	to := min(offset+limit, len(keys))
	return keys[from:to]
}

// goOracleEntityKeys is the same oracle over content_entities source_cache,
// in the entity key order with the page window applied.
func goOracleEntityKeys(t *testing.T, ctx context.Context, db *sql.DB, repos []string, pattern string, limit, offset int) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		`SELECT repo_id, relative_path, start_line, entity_id, source_cache
		 FROM content_entities
		 ORDER BY repo_id, relative_path, start_line, entity_id`)
	if err != nil {
		t.Fatalf("oracle entity scan: %v", err)
	}
	defer func() { _ = rows.Close() }()
	want := map[string]bool{}
	for _, repo := range repos {
		want[repo] = true
	}
	lower := strings.ToLower(pattern)
	var keys []string
	for rows.Next() {
		var repo, path, entityID string
		var startLine int
		var cache sql.NullString
		if err := rows.Scan(&repo, &path, &startLine, &entityID, &cache); err != nil {
			t.Fatalf("oracle entity row: %v", err)
		}
		if len(repos) > 0 && !want[repo] {
			continue
		}
		if strings.Contains(strings.ToLower(cache.String), lower) {
			keys = append(keys, repo+"/"+path)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("oracle entity rows: %v", err)
	}
	from := min(offset, len(keys))
	to := min(offset+limit, len(keys))
	return keys[from:to]
}

// TestLikeEscapeDifferentialLive is the fix-05b core proof: on a corpus and
// pattern set that include %, _, and \, every search path returns exactly the
// Go substring oracle's rows. Before the escape lands, the wildcard rows fail
// (LIKE over-matches); after, all four paths agree with Contains.
func TestLikeEscapeDifferentialLive(t *testing.T) {
	ctx, db := openLikeEscapeFixture(t)
	reader := NewContentReader(db).WithUnscopedSearch(10*time.Second, nil)

	scopedFileSingle := func(pattern string, limit, offset int) []string {
		files, err := reader.searchFileContentPage(ctx, "repo-a", pattern, limit, offset)
		if err != nil {
			t.Fatalf("scoped file single %q: %v", pattern, err)
		}
		var keys []string
		for _, f := range files {
			keys = append(keys, f.RepoID+"/"+f.RelativePath)
		}
		return keys
	}
	scopedFileMulti := func(pattern string, limit, offset int) []string {
		files, err := reader.searchFileContentInRepos(ctx, []string{"repo-a", "repo-b"}, pattern, limit, offset)
		if err != nil {
			t.Fatalf("scoped file multi %q: %v", pattern, err)
		}
		var keys []string
		for _, f := range files {
			keys = append(keys, f.RepoID+"/"+f.RelativePath)
		}
		return keys
	}
	scopedEntitySingle := func(pattern string, limit, offset int) []string {
		entities, err := reader.searchEntityContentPage(ctx, "repo-a", pattern, limit, offset)
		if err != nil {
			t.Fatalf("scoped entity single %q: %v", pattern, err)
		}
		var keys []string
		for _, e := range entities {
			keys = append(keys, e.RepoID+"/"+e.RelativePath)
		}
		return keys
	}
	scopedEntityMulti := func(pattern string, limit, offset int) []string {
		entities, err := reader.searchEntityContentInRepos(ctx, []string{"repo-a", "repo-b"}, pattern, limit, offset)
		if err != nil {
			t.Fatalf("scoped entity multi %q: %v", pattern, err)
		}
		var keys []string
		for _, e := range entities {
			keys = append(keys, e.RepoID+"/"+e.RelativePath)
		}
		return keys
	}

	for _, pattern := range likeEscapePatterns {
		for _, page := range []struct{ limit, offset int }{{100, 0}, {3, 0}, {3, 3}} {
			if got, want := scopedFileSingle(pattern, page.limit, page.offset),
				goOracleFileKeys(t, ctx, db, []string{"repo-a"}, pattern, page.limit, page.offset); !equalKeys(got, want) {
				t.Errorf("scoped file single pattern %q %+v: got %v, want oracle %v", pattern, page, got, want)
			}
			if got, want := scopedFileMulti(pattern, page.limit, page.offset),
				goOracleFileKeys(t, ctx, db, []string{"repo-a", "repo-b"}, pattern, page.limit, page.offset); !equalKeys(got, want) {
				t.Errorf("scoped file multi pattern %q %+v: got %v, want oracle %v", pattern, page, got, want)
			}
			if got, want := scopedEntitySingle(pattern, page.limit, page.offset),
				goOracleEntityKeys(t, ctx, db, []string{"repo-a"}, pattern, page.limit, page.offset); !equalKeys(got, want) {
				t.Errorf("scoped entity single pattern %q %+v: got %v, want oracle %v", pattern, page, got, want)
			}
			if got, want := scopedEntityMulti(pattern, page.limit, page.offset),
				goOracleEntityKeys(t, ctx, db, []string{"repo-a", "repo-b"}, pattern, page.limit, page.offset); !equalKeys(got, want) {
				t.Errorf("scoped entity multi pattern %q %+v: got %v, want oracle %v", pattern, page, got, want)
			}
		}

		// The unscoped walk serves the whole corpus in one exact page at this
		// size; the oracle spans both repositories.
		unscopedFiles, err := reader.SearchFilesUnscoped(ctx, pattern, 100, 0, "", "", 0)
		if err != nil {
			t.Fatalf("unscoped file %q: %v", pattern, err)
		}
		if unscopedFiles.Partial != nil {
			t.Fatalf("unscoped file %q: partial inside a 10 s budget: %+v", pattern, unscopedFiles.Partial)
		}
		var fileKeys []string
		for _, f := range unscopedFiles.Files {
			fileKeys = append(fileKeys, f.RepoID+"/"+f.RelativePath)
		}
		if want := goOracleFileKeys(t, ctx, db, nil, pattern, 100, 0); !equalKeys(fileKeys, want) {
			t.Errorf("unscoped file pattern %q: got %v, want oracle %v", pattern, fileKeys, want)
		}

		unscopedEntities, err := reader.SearchEntitiesUnscoped(ctx, pattern, 100, 0, querycontract.SearchCursor{})
		if err != nil {
			t.Fatalf("unscoped entity %q: %v", pattern, err)
		}
		if unscopedEntities.Partial != nil {
			t.Fatalf("unscoped entity %q: partial inside a 10 s budget: %+v", pattern, unscopedEntities.Partial)
		}
		var entityKeys []string
		for _, e := range unscopedEntities.Entities {
			entityKeys = append(entityKeys, e.RepoID+"/"+e.RelativePath)
		}
		if want := goOracleEntityKeys(t, ctx, db, nil, pattern, 100, 0); !equalKeys(entityKeys, want) {
			t.Errorf("unscoped entity pattern %q: got %v, want oracle %v", pattern, entityKeys, want)
		}
	}
}

func equalKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
