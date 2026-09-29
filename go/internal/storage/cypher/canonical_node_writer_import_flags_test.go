// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// TestCanonicalNodeWriterCarriesImportFlagsAsExplicitBooleans pins the #7345
// writer contract. Every IMPORTS row carries type_only, deferred, and inferred
// as booleans, including false: the edge is MERGEd on its endpoints, so a
// re-projected edge matches the existing one, and only an unconditional SET of
// an explicit false can overwrite a stale true. A write that skipped false
// values, or guarded the SET with a CASE, would leave a flag set forever after
// the source stopped being type-only.
//
// It reads the emitted parameters and statement rather than the materialization,
// so it also catches the writer dropping a flag while it builds parameters.
func TestCanonicalNodeWriterCarriesImportFlagsAsExplicitBooleans(t *testing.T) {
	t.Parallel()

	mat := phaseOrderMaterialization()
	mat.Imports = []canonical.ImportRow{
		{FilePath: "/repos/my-repo/src/a.py", ModuleName: "./b", ModuleLanguage: "python", LineNumber: 1, TypeOnly: true},
		{FilePath: "/repos/my-repo/src/a.py", ModuleName: "./c", ModuleLanguage: "python", LineNumber: 2, Deferred: true},
		{FilePath: "/repos/my-repo/src/a.py", ModuleName: "./d", ModuleLanguage: "python", LineNumber: 3, Inferred: true},
		{FilePath: "/repos/my-repo/src/a.py", ModuleName: "./e", ModuleLanguage: "python", LineNumber: 4},
	}
	mat.Modules = []canonical.ModuleRow{
		{Name: "./b", Language: "python"},
		{Name: "./c", Language: "python"},
		{Name: "./d", Language: "python"},
		{Name: "./e", Language: "python"},
	}

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var cypher string
	var rows []map[string]any
	for _, call := range exec.calls {
		if strings.Contains(call.Cypher, "[r:IMPORTS]->") && strings.Contains(call.Cypher, "MERGE (f)") {
			cypher = call.Cypher
			batch, _ := call.Parameters["rows"].([]map[string]any)
			rows = append(rows, batch...)
		}
	}
	if len(rows) != 4 {
		t.Fatalf("emitted %d IMPORTS rows, want 4", len(rows))
	}

	// The statement: an unconditional SET of all three, and a MERGE that names
	// no relationship property (edge identity stays endpoint-only).
	for _, want := range []string{
		"r.type_only = row.type_only",
		"r.deferred = row.deferred",
		"r.inferred = row.inferred",
		"MERGE (f)-[r:IMPORTS]->(m)",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("IMPORTS statement missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "CASE") || strings.Contains(cypher, "IMPORTS {") {
		t.Errorf("IMPORTS statement must set the flags unconditionally and merge on endpoints only:\n%s", cypher)
	}

	// The parameters: every row carries all three as real booleans, false included.
	want := map[string][3]bool{
		"./b": {true, false, false},
		"./c": {false, true, false},
		"./d": {false, false, true},
		"./e": {false, false, false},
	}
	for _, row := range rows {
		module, _ := row["module_name"].(string)
		var got [3]bool
		for i, key := range []string{"type_only", "deferred", "inferred"} {
			value, present := row[key]
			b, isBool := value.(bool)
			if !present || !isBool {
				t.Fatalf("row %q key %q = %#v, want a boolean present on every row", module, key, value)
			}
			got[i] = b
		}
		if got != want[module] {
			t.Errorf("row %q flags [type_only deferred inferred] = %v, want %v", module, got, want[module])
		}
	}
}
