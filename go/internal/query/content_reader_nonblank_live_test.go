// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7126 disposable PostgreSQL 18 proof that the non-blank statement of the needed-content hydration equals strings.TrimSpace on every Unicode White_Space code point, on text with whitespace on either side, and on large out-of-line values, plus the trim set checked against unicode.IsSpace over the whole 0x10FFFF range; one local PostgreSQL 18 run took about 1.5 s
//eshu:readiness:env ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE ESHU_SCOPED_SEARCH_PROOF_DSN ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The proofs in this file run against a disposable PostgreSQL
// (ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1) and skip without it. They pin
// that the non-blank statement of #7126 decides exactly as strings.TrimSpace
// does, in the database's own encoding and locale.

const nonBlankLiveRepo = "repo-nonblank"

func openNonBlankLiveFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if _, err := db.ExecContext(ctx, `CREATE TABLE content_files (
		repo_id text NOT NULL,
		relative_path text NOT NULL,
		commit_sha text,
		content text NOT NULL,
		content_hash text NOT NULL,
		line_count integer NOT NULL,
		language text,
		artifact_type text,
		PRIMARY KEY (repo_id, relative_path)
	)`); err != nil {
		t.Fatalf("create content_files: %v", err)
	}
	return ctx, db
}

func insertNonBlankRow(ctx context.Context, t *testing.T, db *sql.DB, repoID, relativePath, content string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count)
		 VALUES ($1, $2, $3, 'h', 1)`, repoID, relativePath, content); err != nil {
		t.Fatalf("insert %q: %v", relativePath, err)
	}
}

// TestNonBlankRepoFilePathsMatchesTrimSpaceLive proves the statement against
// strings.TrimSpace on the classes the extractors can meet: empty, every
// Unicode White_Space code point, whitespace runs, the byte-order mark, the
// zero-width space, non-blank text with whitespace on either side, and large
// values that PostgreSQL stores out of line.
func TestNonBlankRepoFilePathsMatchesTrimSpaceLive(t *testing.T) {
	ctx, db := openNonBlankLiveFixture(t)
	reader := NewContentReader(db)

	contents := map[string]string{
		"empty":          "",
		"bom-only":       "\ufeff",
		"zero-width":     "\u200b",
		"bom-then-space": "\ufeff \n",
		"mongolian-vs":   "\u180e",
		"text":           "a",
		"text-padded":    " \t\r\n\u00a0\u3000a\u2028 \n",
		"mixed-space":    " \t\r\n\v\f\u0085\u00a0\u1680\u2000\u200a\u2028\u2029\u202f\u205f\u3000 ",
		"nul-adjacent":   "\u00a0\u00a1",
		"big-blank":      strings.Repeat(" \t\n", 700_000),
		"big-text-last":  strings.Repeat(" \n", 900_000) + "x",
		"big-text-first": "x" + strings.Repeat(" \n", 900_000),
		"big-text-mid":   strings.Repeat("\u3000", 400_000) + "x" + strings.Repeat("\u3000", 400_000),
	}
	for r := rune(1); r <= 0x3100; r++ {
		if unicode.IsSpace(r) || r == 0x180e || r == 0x200b || r == 0xfeff || r%251 == 0 {
			contents[fmt.Sprintf("rune-%04x", r)] = string(r)
			contents[fmt.Sprintf("rune-%04x-pair", r)] = string([]rune{r, r})
			contents[fmt.Sprintf("rune-%04x-in-text", r)] = "a" + string(r) + "b"
		}
	}

	want := map[string]bool{}
	paths := make([]string, 0, len(contents))
	for name, content := range contents {
		insertNonBlankRow(ctx, t, db, nonBlankLiveRepo, name, content)
		paths = append(paths, name)
		want[name] = strings.TrimSpace(content) != ""
	}
	// The same path in another repository must not leak in; an absent path is
	// simply not returned.
	insertNonBlankRow(ctx, t, db, "repo-other", "only-in-other", "text")
	paths = append(paths, "only-in-other", "never-stored", "text")

	got, err := reader.NonBlankRepoFilePaths(ctx, nonBlankLiveRepo, append(paths, paths...))
	if err != nil {
		t.Fatalf("NonBlankRepoFilePaths() error = %v", err)
	}
	gotSet := map[string]int{}
	for _, relativePath := range got {
		gotSet[relativePath]++
	}
	var wrong []string
	for name, nonBlank := range want {
		if (gotSet[name] == 1) != nonBlank || gotSet[name] > 1 {
			wrong = append(wrong, fmt.Sprintf("%s (TrimSpace non-blank=%v, reported %d times)", name, nonBlank, gotSet[name]))
		}
	}
	for _, extra := range []string{"only-in-other", "never-stored"} {
		if gotSet[extra] != 0 {
			wrong = append(wrong, extra+" reported")
		}
	}
	sort.Strings(wrong)
	if len(wrong) > 0 {
		t.Fatalf("statement disagrees with strings.TrimSpace on %d of %d contents, first: %v", len(wrong), len(want), wrong[:min(len(wrong), 10)])
	}
}

// TestNonBlankSpaceSetMatchesGoOverEveryRuneLive checks the trim set itself
// over the whole code point range: a character is trimmed in PostgreSQL
// exactly when unicode.IsSpace holds. Surrogates cannot be stored.
func TestNonBlankSpaceSetMatchesGoOverEveryRuneLive(t *testing.T) {
	ctx, db := openNonBlankLiveFixture(t)

	rows, err := db.QueryContext(ctx, `
		SELECT cp FROM generate_series(1, 1114111) cp
		WHERE cp NOT BETWEEN 55296 AND 57343
		  AND btrim(chr(cp), $1::text) = ''
		ORDER BY cp`, contentartifacts.SpaceCharacters())
	if err != nil {
		t.Fatalf("query trimmed code points: %v", err)
	}
	defer func() { _ = rows.Close() }()
	trimmed := map[rune]bool{}
	for rows.Next() {
		var cp int
		if err := rows.Scan(&cp); err != nil {
			t.Fatalf("scan: %v", err)
		}
		trimmed[rune(cp)] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for r := rune(1); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if trimmed[r] != unicode.IsSpace(r) {
			t.Fatalf("U+%04X: PostgreSQL trims=%v, unicode.IsSpace=%v", r, trimmed[r], unicode.IsSpace(r))
		}
	}
	if len(trimmed) != 25 {
		t.Fatalf("trimmed %d code points, want the 25 of Unicode White_Space", len(trimmed))
	}
}
