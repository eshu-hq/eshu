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
// including a schema-qualified name that Go assembles by concatenation, such
// as CREATE TABLE `+schema+`.fact_work_items (.
var fixtureTableHeader = regexp.MustCompile(
	`(?i)CREATE\s+(?:TEMP\s+|TEMPORARY\s+|UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[^(;]{0,80}?\b(fact_work_items|shared_projection_intents)\s*\(`)

// lifecycleColumn names, per table, the column whose presence means the
// fixture models rows that a reopen writer moves back to outstanding.
var lifecycleColumn = map[string]*regexp.Regexp{
	"fact_work_items":           regexp.MustCompile(`(?i)\bstatus\b`),
	"shared_projection_intents": regexp.MustCompile(`(?i)\bcompleted_at\b`),
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
		for _, match := range alterAddsReopenedAt.FindAllStringSubmatch(def.SQL, -1) {
			table := strings.ToLower(match[1])
			names[table] = append(names[table], def.Name)
		}
	}
	return names
}

// reopenFixtureViolations returns "file:line table" for each hand-built
// lifecycle table in files that never gains reopened_at. files maps a display
// name to file text; migrations maps a table to the definition names that add
// the column.
func reopenFixtureViolations(files map[string]string, migrations map[string][]string) []string {
	var violations []string
	for name, src := range files {
		for _, match := range fixtureTableHeader.FindAllStringSubmatchIndex(src, -1) {
			table := strings.ToLower(src[match[2]:match[3]])
			columns := parenthesizedBody(src[match[1]:])
			if lifecycleColumn[table].MatchString(columns) && !strings.Contains(columns, "reopened_at") &&
				!fileAddsReopenedAt(src, table, migrations[table]) {
				line := strings.Count(src[:match[0]], "\n") + 1
				violations = append(violations, name+":"+strconv.Itoa(line)+" "+table)
			}
		}
	}
	sort.Strings(violations)
	return violations
}

// fileAddsReopenedAt reports whether src adds reopened_at to table with its
// own ALTER TABLE or applies a shipped migration that adds it, named as a
// quoted string.
func fileAddsReopenedAt(src, table string, migrations []string) bool {
	for _, match := range alterAddsReopenedAt.FindAllStringSubmatch(src, -1) {
		if strings.EqualFold(match[1], table) {
			return true
		}
	}
	for _, name := range migrations {
		if strings.Contains(src, strconv.Quote(name)) {
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
// by an empty scan: planted lifecycle tables fail it; twins that name the
// column, ALTER it in, or apply the shipped migration pass; and a table with
// no lifecycle column stays out of scope. QUEUE and INTENTS stand in for the
// table names so this file's own text does not match the guard.
func TestReopenFixtureGuardBitesOnPlantedTables(t *testing.T) {
	t.Parallel()
	named := strings.NewReplacer("QUEUE", "fact_work_items", "INTENTS", "shared_projection_intents")
	migrations := map[string][]string{"shared_projection_intents": {"intent_reopen_migration"}}
	planted := map[string]string{
		"planted_test.go": named.Replace("package planted\n" +
			"const temp = `CREATE TEMP TABLE QUEUE (\n    work_item_id text PRIMARY KEY, status text NOT NULL,\n    CHECK (status <> '')\n)`\n" +
			"const qualified = `CREATE TABLE `+schema+`.QUEUE (work_item_id text, status text)`\n" +
			"const intents = `CREATE TABLE INTENTS (intent_id text, completed_at timestamptz)`\n"),
	}
	if got := reopenFixtureViolations(planted, migrations); len(got) != 3 {
		t.Fatalf("guard flagged %d planted tables, want 3: %v", len(got), got)
	}
	clean := map[string]string{
		"listed_test.go": named.Replace("package clean\n" +
			"const temp = `CREATE TEMP TABLE QUEUE (\n    work_item_id text PRIMARY KEY, status text NOT NULL,\n    CHECK (status <> ''),\n    reopened_at timestamptz\n)`\n" +
			"const minimal = `CREATE TABLE QUEUE (work_item_id TEXT PRIMARY KEY)`\n"),
		"altered_test.go": named.Replace("package clean\n" +
			"const qualified = `CREATE TABLE `+schema+`.QUEUE (work_item_id text, status text)`\n" +
			"const alter = `ALTER TABLE QUEUE ADD COLUMN IF NOT EXISTS reopened_at TIMESTAMPTZ NULL`\n"),
		"migrated_test.go": named.Replace("package clean\n" +
			"const intents = `CREATE TABLE INTENTS (intent_id text, completed_at timestamptz)`\n" +
			"var applied = []string{\"intent_reopen_migration\"}\n"),
	}
	if got := reopenFixtureViolations(clean, migrations); len(got) != 0 {
		t.Fatalf("guard flagged fixtures that gain reopened_at or have no lifecycle column: %v", got)
	}
}
