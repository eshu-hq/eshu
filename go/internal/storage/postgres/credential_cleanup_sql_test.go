// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// credentialCleanupPage is the operator page whose SQL fences the #8833
// cleanup proofs run verbatim, so the tested SQL is the SQL operators copy.
const credentialCleanupPage = "../../../../docs/public/deployment/credential-file-cleanup.md"

// credentialCleanupPred is the path predicate the page documents.
const credentialCleanupPred = `(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$`

var (
	sqlFencePattern       = regexp.MustCompile("(?s)```sql\n(.*?)```")
	statementHeadPattern  = regexp.MustCompile(`(?m)^-- Statement ([A-Za-z]+):`)
	censusHeadPattern     = regexp.MustCompile(`(?m)^-- census ([0-9]+):`)
	psqlVariablePattern   = regexp.MustCompile(`:'([a-z]+)'`)
	sqlLineCommentPattern = regexp.MustCompile(`(?m)--[^\n]*$`)
)

// credentialCleanupFences returns the page's census and purge fences.
func credentialCleanupFences(t *testing.T) (census, purge string) {
	t.Helper()
	page, err := os.ReadFile(credentialCleanupPage)
	if err != nil {
		t.Fatalf("read %s: %v", credentialCleanupPage, err)
	}
	fences := sqlFencePattern.FindAllStringSubmatch(string(page), -1)
	if len(fences) != 2 {
		t.Fatalf("%s holds %d sql fences, want the census and the purge", credentialCleanupPage, len(fences))
	}
	return fences[0][1], fences[1][1]
}

// splitSections splits a fence at each header the pattern matches and returns
// the text under each header by its name.
func splitSections(t *testing.T, fence string, head *regexp.Regexp) map[string]string {
	t.Helper()
	locations := head.FindAllStringSubmatchIndex(fence, -1)
	if len(locations) == 0 {
		t.Fatal("fence has no section headers")
	}
	sections := make(map[string]string, len(locations))
	for i, location := range locations {
		end := len(fence)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		sections[fence[location[2]:location[3]]] = fence[location[0]:end]
	}
	return sections
}

// bindPSQL replaces psql :'name' variables with quoted literals, as psql -v
// does, so the page's text runs unchanged through database/sql.
func bindPSQL(t *testing.T, text string, vars map[string]string) string {
	t.Helper()
	return psqlVariablePattern.ReplaceAllStringFunc(text, func(match string) string {
		name := psqlVariablePattern.FindStringSubmatch(match)[1]
		value, ok := vars[name]
		if !ok {
			t.Fatalf("statement uses psql variable %q, which the test does not bind", name)
		}
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	})
}

// splitStatements splits a script on statement-ending semicolons. The page's
// statements hold no semicolon inside a string or a body.
func splitStatements(text string) []string {
	var statements []string
	for _, part := range strings.Split(sqlLineCommentPattern.ReplaceAllString(text, ""), ";") {
		if strings.TrimSpace(part) != "" {
			statements = append(statements, strings.TrimSpace(part))
		}
	}
	return statements
}

// runScript runs one statement script on a single connection, as psql -f
// does, and returns the rows the data-modifying statements touched.
func runScript(t *testing.T, ctx context.Context, database *sql.DB, script string) int64 {
	t.Helper()
	return runScriptHolding(t, ctx, database, script, 0)
}

// runScriptHolding is runScript with a pause after the statement that locks
// the batch (CREATE TEMP TABLE ... FOR UPDATE), so a concurrent writer meets
// the held row locks. The pause is the race proof's lever; the page's SQL text
// is unchanged.
func runScriptHolding(t *testing.T, ctx context.Context, database *sql.DB, script string, hold time.Duration) int64 {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var touched int64
	for _, statement := range splitStatements(script) {
		result, err := conn.ExecContext(ctx, statement)
		if err != nil {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
			t.Fatalf("run %.80q: %v", statement, err)
		}
		head := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(head, "UPDATE") || strings.HasPrefix(head, "DELETE") || strings.HasPrefix(head, "WITH") {
			n, _ := result.RowsAffected()
			touched += n
		}
		if hold > 0 && strings.HasPrefix(head, "CREATE TEMP TABLE") {
			time.Sleep(hold)
		}
	}
	return touched
}

// queryRowsMap runs one read-only statement and returns its single row by
// column name.
func queryRowsMap(t *testing.T, ctx context.Context, database *sql.DB, statement string) map[string]sql.NullString {
	t.Helper()
	rows, err := database.QueryContext(ctx, statement)
	if err != nil {
		t.Fatalf("query %.80q: %v", statement, err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	out := map[string]sql.NullString{}
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		prefix := ""
		if values[0].Valid && columns[0] == "file_kind" {
			prefix = values[0].String + "."
		}
		for i, column := range columns {
			out[prefix+column] = values[i]
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestCredentialCleanupSearchFactKindOwnsNoKeySidecar pins the #8813 ruling's
// premise for statement B: a reducer_eshu_search_document fact owns no key
// sidecar rows, so deleting it by fact_id leaves no orphan key.
func TestCredentialCleanupSearchFactKindOwnsNoKeySidecar(t *testing.T) {
	t.Parallel()

	if slices.Contains(factKeySidecarKinds, EshuSearchDocumentFactKind) {
		t.Fatalf("%s owns key sidecar rows; the purge's unpaired DELETE would orphan them", EshuSearchDocumentFactKind)
	}
	_, purge := credentialCleanupFences(t)
	if !strings.Contains(purge, "fact_kind = '"+EshuSearchDocumentFactKind+"'") {
		t.Fatalf("statement B no longer filters on %s", EshuSearchDocumentFactKind)
	}
}
