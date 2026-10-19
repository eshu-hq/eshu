// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// TestCanonicalModuleUpsertMergesOnImportKeyOnly pins the #8168 Module upsert:
// the MERGE key is the single import_key property the module_import_key
// uniqueness constraint covers, and every property is written ON CREATE only.
// An unconditional SET would take an exclusive lock on every hot node (Go
// `context`, `fmt`) and hold it until the whole repository transaction
// commits, which serializes concurrent projections.
func TestCanonicalModuleUpsertMergesOnImportKeyOnly(t *testing.T) {
	t.Parallel()

	const want = "UNWIND $rows AS row\n" +
		"MERGE (m:Module {import_key: row.import_key})\n" +
		"ON CREATE SET m.name = row.name, m.lang = row.language, m.evidence_source = 'projector/canonical'"
	if canonicalNodeModuleUpsertCypher != want {
		t.Fatalf("canonicalNodeModuleUpsertCypher =\n%s\nwant\n%s", canonicalNodeModuleUpsertCypher, want)
	}
	if CanonicalNodeModuleUpsertCypher != canonicalNodeModuleUpsertCypher {
		t.Fatal("exported CanonicalNodeModuleUpsertCypher drifted from the production statement")
	}
}

// TestCanonicalImportEdgeMatchesModuleOnImportKey pins the IMPORTS target to
// the exact canonical node. A (name, lang) MATCH would also bind a semantic
// Module carrying the same name and language.
func TestCanonicalImportEdgeMatchesModuleOnImportKey(t *testing.T) {
	t.Parallel()

	if !strings.Contains(canonicalNodeImportEdgeCypher, "MATCH (m:Module {import_key: row.module_import_key})") {
		t.Fatalf("canonicalNodeImportEdgeCypher does not resolve the Module on import_key:\n%s", canonicalNodeImportEdgeCypher)
	}
	if strings.Contains(canonicalNodeImportEdgeCypher, "row.module_name") {
		t.Fatalf("canonicalNodeImportEdgeCypher still resolves the Module on name:\n%s", canonicalNodeImportEdgeCypher)
	}
}

// TestCanonicalModuleImportKeyIsTheTargetIDFormat pins the key to the
// relationships catalog's published IMPORTS target_id, name + "@" + lang. An
// empty language is a real language and keeps the separator.
func TestCanonicalModuleImportKeyIsTheTargetIDFormat(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, language, want string }{
		{"context", "go", "context@go"},
		{"time", "python", "time@python"},
		{"index", "", "index@"},
		{"@scope/pkg", "javascript", "@scope/pkg@javascript"},
	} {
		if got := CanonicalModuleImportKey(tc.name, tc.language); got != tc.want {
			t.Errorf("CanonicalModuleImportKey(%q, %q) = %q, want %q", tc.name, tc.language, got, tc.want)
		}
	}
}

// TestBuildModuleStatementsSortsRowsByImportKey proves every writer takes the
// Module uniqueness locks in one total order. Two repositories that list the
// same modules in different orders would otherwise each hold one key's index
// entry while waiting for the other's, which Neo4j breaks with
// DeadlockDetected.
func TestBuildModuleStatementsSortsRowsByImportKey(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(nil, 2, nil)
	mat := canonical.CanonicalMaterialization{Modules: []canonical.ModuleRow{
		{Name: "net/http", Language: "go"},
		{Name: "context", Language: "go"},
		{Name: "time", Language: "python"},
		{Name: "time", Language: "go"},
		{Name: "fmt", Language: ""},
	}}
	stmts := writer.buildModuleStatements(mat)
	var got []string
	for _, stmt := range stmts {
		if stmt.Cypher != canonicalNodeModuleUpsertCypher {
			t.Fatalf("statement Cypher = %q, want the Module upsert", stmt.Cypher)
		}
		rows, _ := stmt.Parameters["rows"].([]map[string]any)
		for _, row := range rows {
			key, _ := row["import_key"].(string)
			name, _ := row["name"].(string)
			language, _ := row["language"].(string)
			if key != CanonicalModuleImportKey(name, language) {
				t.Fatalf("row %v: import_key %q does not match its name and language", row, key)
			}
			got = append(got, key)
		}
	}
	want := []string{"context@go", "fmt@", "net/http@go", "time@go", "time@python"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("module rows across batches = %v, want sorted %v", got, want)
	}
	if len(stmts) != 3 {
		t.Fatalf("batches = %d, want 3 for 5 rows at batch size 2", len(stmts))
	}
}

// TestCanonicalModuleRowsSortsWithoutMutatingInput pins the exported helper
// conformance shares with the writer.
func TestCanonicalModuleRowsSortsWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	input := []canonical.ModuleRow{{Name: "b", Language: "go"}, {Name: "a", Language: "go"}}
	rows := CanonicalModuleRows(input)
	if len(rows) != 2 || rows[0]["import_key"] != "a@go" || rows[1]["import_key"] != "b@go" {
		t.Fatalf("CanonicalModuleRows = %v, want a@go then b@go", rows)
	}
	if input[0].Name != "b" {
		t.Fatalf("CanonicalModuleRows reordered its input: %v", input)
	}
	if CanonicalModuleRows(nil) != nil {
		t.Fatal("CanonicalModuleRows(nil) should be nil")
	}
}

// TestImportEdgeRowsSortByModuleImportKey proves the IMPORTS MERGE also takes
// its Module node locks in the shared order. Creating a relationship locks the
// target node's relationship group; measured on the pinned Neo4j with sorted
// Module rows but unsorted edge rows, 8 concurrent writers hit
// DeadlockDetected in 87 of 800 transactions, and 0 once the edge rows sort.
func TestImportEdgeRowsSortByModuleImportKey(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(nil, 100, nil)
	mat := canonical.CanonicalMaterialization{
		GenerationID: "gen",
		Imports: []canonical.ImportRow{
			{FilePath: "b.go", ModuleName: "time", ModuleLanguage: "go", LineNumber: 4},
			{FilePath: "a.go", ModuleName: "time", ModuleLanguage: "go", LineNumber: 9},
			{FilePath: "a.go", ModuleName: "context", ModuleLanguage: "go", LineNumber: 3},
			{FilePath: "a.go", ModuleName: "time", ModuleLanguage: "go", LineNumber: 2},
		},
	}
	var rows []map[string]any
	for _, stmt := range writer.buildStructuralEdgeStatements(mat) {
		if stmt.Cypher == canonicalNodeImportEdgeCypher {
			batch, _ := stmt.Parameters["rows"].([]map[string]any)
			rows = append(rows, batch...)
		}
	}
	var got []string
	for _, row := range rows {
		key, _ := row["module_import_key"].(string)
		path, _ := row["file_path"].(string)
		got = append(got, key+"|"+path)
	}
	want := []string{"context@go|a.go", "time@go|a.go", "time@go|a.go", "time@go|b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("IMPORTS rows = %v, want %v", got, want)
	}
	if line, _ := rows[1]["line_number"].(int); line != 2 {
		t.Fatalf("rows for one (module, file) pair keep line order: second row line = %v, want 2", rows[1]["line_number"])
	}
}

// TestParseCanonicalModuleImportKeyPendingFailsClosed proves the pending-check
// answer is read strictly: both binaries write the schema marker only on a
// parsed zero, so a missing row, a missing column, or a non-integer value must
// be an error, never a silent zero that admits import_key writers.
func TestParseCanonicalModuleImportKeyPendingFailsClosed(t *testing.T) {
	t.Parallel()

	unkeyed, adopted, err := ParseCanonicalModuleImportKeyPending([]map[string]any{{"unkeyed": int64(197), "adopted_imports": int64(50)}})
	if err != nil || unkeyed != 197 || adopted != 50 {
		t.Fatalf("parse = (%d, %d, %v), want (197, 50, nil)", unkeyed, adopted, err)
	}
	for name, rows := range map[string][]map[string]any{
		"no row":         nil,
		"two rows":       {{"unkeyed": int64(0), "adopted_imports": int64(0)}, {"unkeyed": int64(0), "adopted_imports": int64(0)}},
		"missing column": {{"unkeyed": int64(0)}},
		"string value":   {{"unkeyed": "0", "adopted_imports": int64(0)}},
		"float value":    {{"unkeyed": int64(0), "adopted_imports": 0.0}},
	} {
		if _, _, err := ParseCanonicalModuleImportKeyPending(rows); err == nil {
			t.Errorf("%s: parse error = nil, want a refusal", name)
		}
	}
}
