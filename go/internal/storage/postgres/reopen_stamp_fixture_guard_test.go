// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Guard for #9320 test fixtures: a test that hand-builds fact_work_items or
// shared_projection_intents instead of bootstrapping the schema must carry
// reopened_at when its table models the lifecycle that reopen writers move.
// The scheduled-bulk job found four admin replay fixtures without the column;
// the stamped replay then failed with SQLSTATE 42703 in a job that the changed
// packages' local proof never ran. The rule is structural, not a list: a
// fact_work_items copy with a status column, or a shared_projection_intents
// copy with a completed_at column, must name reopened_at in its column list,
// add it with ALTER TABLE in the same file, or apply by name the shipped
// migration that adds it (found in BootstrapDefinitions, not listed here).

// fixtureTableHeader matches the start of a hand-built copy of either table,
// including a schema-qualified name that Go assembles by concatenating a
// schema variable in front of the table name.
var fixtureTableHeader = regexp.MustCompile(
	`(?i)CREATE\s+(?:TEMP\s+|TEMPORARY\s+|UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[^(;]{0,80}?\b(fact_work_items|shared_projection_intents)\s*\(`)

// lifecycleColumn names, per table, the column whose presence means the
// fixture models rows that a reopen writer moves back to outstanding.
var lifecycleColumn = map[string]*regexp.Regexp{
	"fact_work_items":           regexp.MustCompile(`(?i)\bstatus\b`),
	"shared_projection_intents": regexp.MustCompile(`(?i)\bcompleted_at\b`),
}

// commentText matches a block comment, a SQL line comment, or a Go line
// comment. A mention of reopened_at inside a comment does not count, so the
// guard strips comments before it looks for the column, an ALTER, or a
// migration name. The strip is not string-aware: a '--' or 'http://x' literal
// also loses the rest of its line. It is therefore never used to find a table
// header, to bound its column list, or to decide whether the table models the
// lifecycle; over-stripping can only remove evidence of reopened_at, so it can
// flag a table but never clear one.
var commentText = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\n]*|//[^\n]*`)

// reopenedAtColumn matches reopened_at as a whole identifier in any case, so
// a longer name such as last_reopened_at does not count.
var reopenedAtColumn = regexp.MustCompile(`(?i)\breopened_at\b`)

// withoutComments returns text with every commentText match replaced by a
// space.
func withoutComments(text string) string {
	return commentText.ReplaceAllString(text, " ")
}

// alterAddsReopenedAt matches an ALTER TABLE that adds reopened_at; group 1
// is the table name.
var alterAddsReopenedAt = regexp.MustCompile(
	`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?[^;]{0,80}?\b(fact_work_items|shared_projection_intents)\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?reopened_at\b`)

// reopenedAtMigrations maps each table to the names of the shipped bootstrap
// definitions that add reopened_at to it.
func reopenedAtMigrations() map[string][]string {
	names := map[string][]string{}
	for _, def := range BootstrapDefinitions() {
		for _, match := range alterAddsReopenedAt.FindAllStringSubmatch(withoutComments(def.SQL), -1) {
			table := strings.ToLower(match[1])
			names[table] = append(names[table], def.Name)
		}
	}
	return names
}

// reopenFixtureViolations returns "file:line table" for each hand-built
// lifecycle table in files that never gains reopened_at. files maps a display
// name to file text; migrations maps a table to the definition names that add
// the column. The header, the column-list bounds, and the lifecycle column
// are read from the raw text; reopened_at evidence is read with comments
// removed. A file that names neither table is skipped before any regex work.
func reopenFixtureViolations(files map[string]string, migrations map[string][]string) []string {
	var violations []string
	for name, src := range files {
		lower := strings.ToLower(src)
		if !strings.Contains(lower, "fact_work_items") && !strings.Contains(lower, "shared_projection_intents") {
			continue
		}
		code := withoutComments(src)
		for _, match := range fixtureTableHeader.FindAllStringSubmatchIndex(src, -1) {
			table := strings.ToLower(src[match[2]:match[3]])
			columns := parenthesizedBody(src[match[1]:])
			if lifecycleColumn[table].MatchString(columns) && !reopenedAtColumn.MatchString(withoutComments(columns)) &&
				!fileAddsReopenedAt(code, table, migrations[table]) {
				line := strings.Count(src[:match[0]], "\n") + 1
				violations = append(violations, name+":"+strconv.Itoa(line)+" "+table)
			}
		}
	}
	sort.Strings(violations)
	return violations
}

// fileAddsReopenedAt reports whether code, file text with comments removed,
// adds reopened_at to table with its own ALTER TABLE or applies a shipped
// migration that adds it, named as a quoted string.
func fileAddsReopenedAt(code, table string, migrations []string) bool {
	for _, match := range alterAddsReopenedAt.FindAllStringSubmatch(code, -1) {
		if strings.EqualFold(match[1], table) {
			return true
		}
	}
	for _, name := range migrations {
		if strings.Contains(code, strconv.Quote(name)) {
			return true
		}
	}
	return false
}

// parenthesizedBody returns text up to the parenthesis that closes the one
// just before it, so nested CHECK (...) clauses and type modifiers stay inside.
func parenthesizedBody(text string) string {
	depth := 1
	for i, r := range text {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[:i]
			}
		}
	}
	return text
}

// moduleTestSources returns every Go test file in the module, keyed by its
// path relative to the module root.
func moduleTestSources(t *testing.T) map[string]string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", ".."))
	files := map[string]string{}
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		files[rel] = string(src)
		return nil
	})
	if err != nil {
		t.Fatalf("walk module test sources: %v", err)
	}
	return files
}

// TestHandBuiltQueueFixturesCarryReopenedAt holds every hand-built lifecycle
// fixture in the module to the #9320 column rule.
func TestHandBuiltQueueFixturesCarryReopenedAt(t *testing.T) {
	t.Parallel()
	migrations := reopenedAtMigrations()
	if len(migrations["fact_work_items"]) == 0 || len(migrations["shared_projection_intents"]) == 0 {
		t.Fatalf("no shipped migration adds reopened_at to both tables: %v", migrations)
	}
	if violations := reopenFixtureViolations(moduleTestSources(t), migrations); len(violations) > 0 {
		t.Fatalf("hand-built queue fixtures lack reopened_at; stamped writers fail on them with SQLSTATE 42703 (#9320):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestReopenFixtureGuardBitesOnPlantedTables proves the guard is not satisfied
// by an empty scan or by text that only looks like the column. Planted
// lifecycle tables fail it, including tables whose only mention of
// reopened_at is a comment, a longer identifier, an ALTER inside a SQL
// comment, or a migration name inside a Go comment, and tables where a '--'
// or '//' string literal would let a comment strip hide the closing paren or
// the lifecycle column. Twins that name the column (in any case), ALTER it in,
// or apply the shipped migration pass, and a table with no lifecycle column
// stays out of scope. QUEUE and INTENTS stand
// in for the table names so this file's own text does not match the guard.
func TestReopenFixtureGuardBitesOnPlantedTables(t *testing.T) {
	t.Parallel()
	named := strings.NewReplacer("UPQUEUE", "FACT_WORK_ITEMS", "QUEUE", "fact_work_items", "INTENTS", "shared_projection_intents")
	migrations := map[string][]string{"shared_projection_intents": {"intent_reopen_migration"}}
	cases := []struct {
		name    string
		src     string
		flagged int
	}{
		{"temp table with a CHECK clause", "const q = `CREATE TEMP TABLE QUEUE (\n    work_item_id text PRIMARY KEY, status text NOT NULL,\n    CHECK (status <> '')\n)`", 1},
		{"schema-qualified table", "const q = `CREATE TABLE `+schema+`.QUEUE (work_item_id text, status text)`", 1},
		{"intents table", "const q = `CREATE TABLE INTENTS (intent_id text, completed_at timestamptz)`", 1},
		{"column only in a line comment", "const q = `CREATE TABLE QUEUE (\n    status text -- reopened_at deliberately omitted\n)`", 1},
		{"column only in a block comment", "const q = `CREATE TABLE QUEUE (status text /* reopened_at */)`", 1},
		{"longer identifier", "const q = `CREATE TABLE QUEUE (status text, last_reopened_at timestamptz)`", 1},
		{"ALTER only in a SQL comment", "const q = `CREATE TABLE QUEUE (status text)`\nconst a = `-- ALTER TABLE QUEUE ADD COLUMN reopened_at timestamptz`", 1},
		{"ALTER of a longer identifier", "const q = `CREATE TABLE QUEUE (status text)`\nconst a = `ALTER TABLE QUEUE ADD COLUMN reopened_at_old timestamptz`", 1},
		{"string -- before the closing paren", "const q = `CREATE TABLE QUEUE (status text, CHECK (status <> '--'))`\nconst r = `CREATE TABLE other_table (reopened_at timestamptz)`", 1},
		{"string // before the closing paren", "const q = `CREATE TABLE QUEUE (status text, source text DEFAULT 'http://x')`\nconst r = `CREATE TABLE other_table (reopened_at timestamptz)`", 1},
		{"string -- before the lifecycle column", "const q = `CREATE TABLE QUEUE (kind text DEFAULT '--', status text)`", 1},
		{"upper-case table name", "const q = `CREATE TABLE UPQUEUE (status text)`", 1},
		{"lifecycle column only in a comment", "const q = `CREATE TABLE QUEUE (work_item_id text -- status lives elsewhere\n)`", 1},
		{"migration named only in a Go comment", "const q = `CREATE TABLE INTENTS (intent_id text, completed_at timestamptz)`\n// applies \"intent_reopen_migration\"", 1},
		{"column listed", "const q = `CREATE TEMP TABLE QUEUE (\n    work_item_id text PRIMARY KEY, status text NOT NULL,\n    CHECK (status <> ''),\n    reopened_at timestamptz\n)`", 0},
		{"column listed in upper case", "const q = `CREATE TABLE QUEUE (status TEXT, REOPENED_AT TIMESTAMPTZ)`", 0},
		{"column listed after a comment", "const q = `CREATE TABLE QUEUE (\n    status text, -- lifecycle (reopen stamp below)\n    reopened_at timestamptz\n)`", 0},
		{"column added by ALTER", "const q = `CREATE TABLE `+schema+`.QUEUE (work_item_id text, status text)`\nconst a = `ALTER TABLE QUEUE ADD COLUMN IF NOT EXISTS reopened_at TIMESTAMPTZ NULL`", 0},
		{"shipped migration applied", "const q = `CREATE TABLE INTENTS (intent_id text, completed_at timestamptz)`\nvar applied = []string{\"intent_reopen_migration\"}", 0},
		{"no lifecycle column", "const q = `CREATE TABLE QUEUE (work_item_id TEXT PRIMARY KEY)`", 0},
	}
	for _, tc := range cases {
		files := map[string]string{"planted_test.go": named.Replace("package planted\n" + tc.src + "\n")}
		if got := reopenFixtureViolations(files, migrations); len(got) != tc.flagged {
			t.Errorf("%s: guard flagged %d tables, want %d: %v", tc.name, len(got), tc.flagged, got)
		}
	}

	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	own, err := os.ReadFile(here)
	if err != nil {
		t.Fatalf("read guard source: %v", err)
	}
	if got := fixtureTableHeader.FindAllStringIndex(string(own), -1); len(got) != 0 {
		t.Errorf("the guard's own source matches the fixture header %d times; it must stay out of scope", len(got))
	}
}
