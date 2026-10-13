// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// TestCanonicalNodeWriterCarriesImportSourcePackage pins the writer half
// of the package lifts (#7347/#7348). Every IMPORTS row carries the
// importing file's own package (Go import path or npm name) as
// source_package, empty string included: the edge is MERGEd on its
// endpoints, so a re-projected edge matches the existing one, and only an
// unconditional SET of an explicit value can overwrite a stale package
// after the file moved packages. A write that skipped empty values would
// keep the old package on the edge forever, and a legacy edge (no
// property) reads back as package-unknown, never as an empty package
// that would collapse every legacy edge onto one node.
func TestCanonicalNodeWriterCarriesImportSourcePackage(t *testing.T) {
	t.Parallel()

	mat := phaseOrderMaterialization()
	mat.Imports = []canonical.ImportRow{
		{FilePath: "/repos/my-repo/pkg/a/a.go", ModuleName: "example.com/repo/pkg/b", ModuleLanguage: "go", LineNumber: 1, SourcePackage: "example.com/repo/pkg/a"},
		{FilePath: "/repos/my-repo/src/a.py", ModuleName: "b", ModuleLanguage: "python", LineNumber: 2},
		{
			FilePath: "/repos/my-repo/packages/a/index.ts", ModuleName: "@cycles/b",
			ModuleLanguage: "typescript", LineNumber: 3, SourcePackage: "@cycles/a",
		},
		{FilePath: "/repos/my-repo/src/plain.ts", ModuleName: "react", ModuleLanguage: "typescript", LineNumber: 5},
	}
	mat.Modules = []canonical.ModuleRow{
		{Name: "example.com/repo/pkg/b", Language: "go"},
		{Name: "b", Language: "python"},
		{Name: "@cycles/b", Language: "typescript"},
		{Name: "react", Language: "typescript"},
	}

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
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
	if !strings.Contains(cypher, "r.source_package = row.source_package") {
		t.Errorf("IMPORTS statement missing unconditional source_package SET:\n%s", cypher)
	}

	want := map[string]string{
		"example.com/repo/pkg/b": "example.com/repo/pkg/a",
		"b":                      "",
		"@cycles/b":              "@cycles/a",
		"react":                  "",
	}
	for _, row := range rows {
		module, _ := row["module_name"].(string)
		value, present := row["source_package"]
		pkg, isString := value.(string)
		if !present || !isString {
			t.Fatalf("row %q source_package = %#v, want a string present on every row", module, value)
		}
		if pkg != want[module] {
			t.Errorf("row %q source_package = %q, want %q", module, pkg, want[module])
		}
	}
}
