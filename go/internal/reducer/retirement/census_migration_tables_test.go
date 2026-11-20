// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// retirementCensusParents mirrors the live census's parent list in
// go/internal/storage/postgres/retirement_table_census.go: a table holding a
// foreign key to one of these is retirement-relevant whatever its columns
// are called.
var censusMigrationParents = []string{
	"ingestion_scopes",
	"scope_generations",
	"fact_records",
	"fact_work_items",
	"content_files",
}

// censusMigrationTable is one table the migration text defines.
type censusMigrationTable struct {
	columns []string
	parents []string
}

// stripMigrationComments removes -- line comments and /* */ blocks so quoted
// prose (migration headers document decisions at length) never parses as DDL.
func stripMigrationComments(text string) string {
	var kept strings.Builder
	rest := text
	for {
		start := strings.Index(rest, "/*")
		if start < 0 {
			kept.WriteString(rest)
			break
		}
		kept.WriteString(rest[:start])
		end := strings.Index(rest[start+2:], "*/")
		if end < 0 {
			break
		}
		rest = rest[start+2+end+2:]
	}
	var lines []string
	for _, line := range strings.Split(kept.String(), "\n") {
		if index := strings.Index(line, "--"); index >= 0 {
			line = line[:index]
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// splitTopLevel splits text on commas that are not nested in parentheses.
func splitTopLevel(text string) []string {
	var parts []string
	depth := 0
	start := 0
	for index, char := range text {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, text[start:index])
				start = index + 1
			}
		}
	}
	return append(parts, text[start:])
}

// censusMigrationColumnOf returns the column a definition line declares, or
// false for constraints, keys, and LIKE clauses.
func censusMigrationColumnOf(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	name := strings.Trim(fields[0], `"`)
	switch strings.ToUpper(name) {
	case "PRIMARY", "FOREIGN", "CONSTRAINT", "UNIQUE", "CHECK", "LIKE", "EXCLUDE":
		return "", false
	}
	for index, char := range name {
		if !isMigrationIdentChar(char, index) {
			return "", false
		}
	}
	return strings.ToLower(name), true
}

// isMigrationIdentChar reports whether char may appear in a DDL identifier,
// with digits barred from the first position.
func isMigrationIdentChar(char rune, index int) bool {
	if char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
		return true
	}
	return char >= '0' && char <= '9' && index > 0
}

// censusMigrationReferences returns the tables a DDL fragment points at with
// REFERENCES, inline or table-level.
func censusMigrationReferences(fragment string) []string {
	var parents []string
	upper := strings.ToUpper(fragment)
	offset := 0
	for {
		index := strings.Index(upper[offset:], "REFERENCES")
		if index < 0 {
			break
		}
		rest := strings.TrimSpace(fragment[offset+index+len("REFERENCES"):])
		name := rest
		for _, end := range []string{" ", "(", ",", ";", "\n", "\t", "\""} {
			if cut := strings.Index(name, end); cut >= 0 {
				name = name[:cut]
			}
		}
		name = strings.Trim(strings.ToLower(name), `"`)
		if name != "" {
			parents = append(parents, name)
		}
		offset += index + len("REFERENCES")
	}
	return parents
}

// indexCreateTable finds the next CREATE TABLE variant the live scan can
// see: plain and UNLOGGED (relkind r either way). TEMP tables live outside
// the scanned schema and match nothing here.
func indexCreateTable(upper string) int {
	best := -1
	for _, keyword := range []string{"CREATE TABLE ", "CREATE UNLOGGED TABLE "} {
		if index := strings.Index(upper, keyword); index >= 0 && (best < 0 || index < best) {
			best = index
		}
	}
	return best
}

// statementTableName reads a possibly qualified, possibly IF NOT EXISTS
// table name from the head of upper-cased DDL text, lower-cased.
func statementTableName(upper string) string {
	rest := strings.TrimSpace(upper)
	for _, prefix := range []string{"IF NOT EXISTS ", "ONLY "} {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, prefix))
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	name := strings.Trim(fields[0], `"`)
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	name = strings.ToLower(name)
	for index, char := range name {
		// Table names keep the historical rule (digits allowed anywhere);
		// only column position bars a leading digit.
		if !isMigrationIdentChar(char, index+1) {
			return ""
		}
	}
	if name == "" || name == "table" {
		return ""
	}
	return name
}

// ensureMigrationTable returns the accumulator for a table, creating it.
func ensureMigrationTable(tables map[string]*censusMigrationTable, name string) *censusMigrationTable {
	table, ok := tables[name]
	if !ok {
		table = &censusMigrationTable{}
		tables[name] = table
	}
	return table
}

// parseMigrationTables folds one migration file into the table map: CREATE
// TABLE adds (PARTITION OF children roll up later), ALTER TABLE ADD/DROP
// COLUMN mutates, DROP TABLE removes. Statements run in file order. TEMP
// tables live outside the scanned schema and are skipped, like the live
// scan skips them.
func parseMigrationTables(tables map[string]*censusMigrationTable, text string) {
	text = stripMigrationComments(text)
	upper := strings.ToUpper(text)
	cursor := 0
	for cursor < len(text) {
		nextCreate := indexCreateTable(upper[cursor:])
		nextAlter := strings.Index(upper[cursor:], "ALTER TABLE ")
		nextDrop := strings.Index(upper[cursor:], "DROP TABLE ")
		kind, at := "", -1
		for _, candidate := range []struct {
			name string
			pos  int
		}{{"CREATE", nextCreate}, {"ALTER", nextAlter}, {"DROP", nextDrop}} {
			if candidate.pos >= 0 && (at < 0 || candidate.pos < at) {
				kind, at = candidate.name, candidate.pos
			}
		}
		if at < 0 {
			return
		}
		at += cursor
		rest := text[at:]
		upperRest := upper[at:]
		switch kind {
		case "DROP":
			name := statementTableName(upperRest[len("DROP TABLE "):])
			delete(tables, name)
			cursor = at + len("DROP TABLE ")
		case "ALTER":
			name := statementTableName(upperRest[len("ALTER TABLE "):])
			table := ensureMigrationTable(tables, name)
			alterBody := rest[len("ALTER TABLE ")+len(name):]
			if end := strings.Index(alterBody, ";"); end >= 0 {
				alterBody = alterBody[:end]
			}
			if index := strings.Index(strings.ToUpper(alterBody), "RENAME TO "); index >= 0 {
				if target := statementTableName(strings.ToUpper(alterBody[index+len("RENAME TO "):])); target != "" {
					if existing, ok := tables[name]; ok {
						delete(tables, name)
						tables[target] = existing
					}
				}
			}
			for rest := alterBody; ; {
				index := strings.Index(strings.ToUpper(rest), "ADD COLUMN ")
				if index < 0 {
					break
				}
				after := strings.TrimSpace(rest[index+len("ADD COLUMN "):])
				if strings.HasPrefix(strings.ToUpper(after), "IF NOT EXISTS ") {
					after = strings.TrimSpace(after[len("IF NOT EXISTS "):])
				}
				if column, ok := censusMigrationColumnOf(after); ok {
					table.columns = append(table.columns, column)
				}
				rest = after
			}
			for rest := alterBody; ; {
				index := strings.Index(strings.ToUpper(rest), "DROP COLUMN ")
				if index < 0 {
					break
				}
				after := strings.Fields(strings.TrimSpace(rest[index+len("DROP COLUMN "):]))
				if len(after) > 0 {
					dropped := strings.Trim(strings.ToLower(after[0]), `"`)
					kept := table.columns[:0]
					for _, column := range table.columns {
						if column != dropped {
							kept = append(kept, column)
						}
					}
					table.columns = kept
				}
				rest = strings.Join(after, " ")
			}
			table.parents = append(table.parents, censusMigrationReferences(alterBody)...)
			cursor = at + len("ALTER TABLE ")
		default:
			header := rest
			if strings.HasPrefix(strings.ToUpper(header), "CREATE UNLOGGED TABLE ") {
				header = header[len("CREATE UNLOGGED TABLE "):]
			} else {
				header = header[len("CREATE TABLE "):]
			}
			upperHeader := strings.ToUpper(header)
			// TEMP tables live outside the scanned schema.
			if strings.HasPrefix(upperHeader, "TEMP ") ||
				strings.HasPrefix(upperHeader, "TEMPORARY ") {
				cursor = at + len("CREATE TABLE ")
				continue
			}
			name := statementTableName(header)
			if name == "" {
				cursor = at + len("CREATE TABLE ")
				continue
			}
			open := strings.Index(header, "(")
			if open < 0 {
				cursor = at + len("CREATE TABLE ")
				continue
			}
			if index := strings.Index(upperHeader[:open], "PARTITION OF "); index >= 0 {
				after := header[index+len("PARTITION OF "):]
				parent := statementTableName(strings.ToUpper(after))
				tables[name] = &censusMigrationTable{parents: []string{"partition:" + parent}}
				cursor = at + len("CREATE TABLE ")
				continue
			}
			depth := 0
			close := -1
			for index := open; index < len(header); index++ {
				switch header[index] {
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						close = index
					}
				}
				if close >= 0 {
					break
				}
			}
			if close < 0 {
				cursor = at + len("CREATE TABLE ")
				continue
			}
			table := ensureMigrationTable(tables, name)
			for _, item := range splitTopLevel(header[open+1 : close]) {
				if column, ok := censusMigrationColumnOf(strings.TrimSpace(item)); ok {
					table.columns = append(table.columns, column)
				}
			}
			table.parents = append(table.parents, censusMigrationReferences(header[:close+1])...)
			cursor = at + len("CREATE TABLE ") + close + 1
		}
	}
}

// censusMigrationHit mirrors the live census predicate in
// go/internal/storage/postgres/retirement_table_census.go: a column whose
// name holds repo or scope, the partition_key column, or a foreign key to
// a retirement parent. Partition children roll up to their parent.
func censusMigrationHit(table string, entry *censusMigrationTable) (columns []string, viaFK bool) {
	for _, column := range entry.columns {
		if strings.Contains(column, "repo") || strings.Contains(column, "scope") || column == "partition_key" {
			columns = append(columns, column)
		}
	}
	parents := map[string]bool{}
	for _, parent := range entry.parents {
		if strings.HasPrefix(parent, "partition:") {
			continue
		}
		parents[parent] = true
	}
	for _, parent := range censusMigrationParents {
		if parents[parent] {
			viaFK = true
		}
	}
	return columns, viaFK
}

// TestRetirementCensusCoversMigratedTables is the hermetic tripwire for the
// live census: parsing the committed migrations must name no scope-,
// repository-, or partition-keyed table missing from RetireTableDispositions.
// A migration author meets this in seconds under go test instead of fifteen
// minutes into CI, where the live proof fails with the same table name.
// The live test stays authoritative for views, exotic DDL, and anything this
// text scan cannot see; this test must never pass while the live one fails
// for a table defined here.
func TestRetirementCensusCoversMigratedTables(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller cannot locate this test file")
	}
	// .../go/internal/reducer/retirement/census_migration_tables_test.go
	// -> .../go/internal/storage/postgres/migrations.
	migrations := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "storage", "postgres", "migrations"))
	entries, err := os.ReadDir(migrations)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	tables := map[string]*censusMigrationTable{}
	partitionOf := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(migrations, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		parseMigrationTables(tables, string(content))
		for name, table := range tables {
			for _, parent := range table.parents {
				if target, ok := strings.CutPrefix(parent, "partition:"); ok {
					partitionOf[name] = target
				}
			}
		}
	}
	hits := map[string][]string{}
	for name, table := range tables {
		if parent, ok := partitionOf[name]; ok {
			name = parent
			table = tables[parent]
			if table == nil {
				continue
			}
		}
		columns, viaFK := censusMigrationHit(name, table)
		if len(columns) > 0 || viaFK {
			hits[name] = columns
		}
	}
	var unclassified []string
	for name := range hits {
		if _, ok := RetireTableDispositionFor(name); !ok {
			unclassified = append(unclassified, name)
		}
	}
	sort.Strings(unclassified)
	for _, name := range unclassified {
		t.Errorf("table %q is not in the retirement census; record its disposition in RetireTableDispositions (go/internal/reducer/retirement/census.go)", name)
	}
	var stale []string
	for name, entry := range RetireTableDispositions {
		if _, ok := hits[name]; !ok && !entry.InvisibleToScan {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("table %q is in the retirement census but no migration defines it; drop the stale row", name)
	}
}
