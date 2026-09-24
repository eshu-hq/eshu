// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDropOversizedIndexKeysModuleNameBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		wantKept bool
	}{
		{name: "limit minus one", value: strings.Repeat("a", MaxIndexedKeyBytes-1), wantKept: true},
		{name: "at limit", value: strings.Repeat("a", MaxIndexedKeyBytes), wantKept: true},
		{name: "limit plus one", value: strings.Repeat("a", MaxIndexedKeyBytes+1), wantKept: false},
		// The limit is UTF-8 bytes, not characters: Neo4j sizes the index key
		// by its encoded bytes. 2667 three-byte runes are 8001 bytes but only
		// 2667 characters, so a rune-counting guard would wrongly keep this.
		{name: "three byte runes over limit", value: strings.Repeat("€", 2667), wantKept: false},
		{name: "three byte runes under limit", value: strings.Repeat("€", 2666), wantKept: true},
		{name: "four byte runes at limit", value: strings.Repeat("😀", MaxIndexedKeyBytes/4), wantKept: true},
		{name: "four byte runes over limit", value: strings.Repeat("😀", MaxIndexedKeyBytes/4+1), wantKept: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mat := CanonicalMaterialization{
				Modules: []ModuleRow{
					{Name: "react", Language: "typescript"},
					{Name: tt.value, Language: "typescript"},
				},
			}
			got, dropped := DropOversizedIndexKeys(mat)

			if tt.wantKept {
				if len(dropped) != 0 {
					t.Fatalf("dropped = %+v, want none", dropped)
				}
				if len(got.Modules) != 2 {
					t.Fatalf("Modules len = %d, want 2", len(got.Modules))
				}
				return
			}
			if len(got.Modules) != 1 || got.Modules[0].Name != "react" {
				t.Fatalf("Modules = %d rows, want only react", len(got.Modules))
			}
			if len(dropped) != 1 {
				t.Fatalf("dropped len = %d, want 1", len(dropped))
			}
			d := dropped[0]
			if d.Label != "Module" || d.Property != "name" {
				t.Fatalf("dropped label/property = %q/%q, want Module/name", d.Label, d.Property)
			}
			if d.KeyBytes != len(tt.value) {
				t.Fatalf("KeyBytes = %d, want %d", d.KeyBytes, len(tt.value))
			}
		})
	}
}

func TestDropOversizedIndexKeysEntityCompositeKey(t *testing.T) {
	t.Parallel()

	path := "/repo/src/app.ts"
	normal := EntityRow{EntityID: "content-entity:e_000000000001", Label: "Function", EntityName: "render", FilePath: path}
	// Function carries the (name, path, line_number) node-key constraint on
	// Neo4j, so the name and path share one index key: a name that fits alone
	// can still overflow the composite key.
	atLimit := EntityRow{
		EntityID:   "content-entity:e_000000000002",
		Label:      "Function",
		EntityName: strings.Repeat("n", MaxIndexedKeyBytes-len(path)),
		FilePath:   path,
	}
	overByName := EntityRow{
		EntityID:   "content-entity:e_000000000003",
		Label:      "Function",
		EntityName: strings.Repeat("n", MaxIndexedKeyBytes-len(path)+1),
		FilePath:   path,
	}
	longPath := "/" + strings.Repeat("p", 7000)
	overByPath := EntityRow{
		EntityID:   "content-entity:e_000000000004",
		Label:      "Class",
		EntityName: strings.Repeat("c", 1001),
		FilePath:   longPath,
	}
	mat := CanonicalMaterialization{Entities: []EntityRow{normal, atLimit, overByName, overByPath}}

	got, dropped := DropOversizedIndexKeys(mat)

	if len(got.Entities) != 2 || got.Entities[0].EntityID != normal.EntityID || got.Entities[1].EntityID != atLimit.EntityID {
		t.Fatalf("Entities kept = %v, want normal and atLimit", entityIDs(got.Entities))
	}
	if len(dropped) != 2 {
		t.Fatalf("dropped len = %d, want 2", len(dropped))
	}
	if d := dropped[0]; d.Label != "Function" || d.Property != "name" || d.EntityID != overByName.EntityID ||
		d.FilePath != path || d.KeyBytes != MaxIndexedKeyBytes+1 {
		t.Fatalf("dropped[0] = %+v, want Function/name over by one byte", d)
	}
	if d := dropped[1]; d.Label != "Class" || d.Property != "path" || d.EntityID != overByPath.EntityID {
		t.Fatalf("dropped[1] = %+v, want Class/path", d)
	}
}

func TestDropOversizedIndexKeysDropsRowsReferencingOversizedKeys(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", MaxIndexedKeyBytes+1)
	path := "/repo/a.ts"
	mat := CanonicalMaterialization{
		Modules: []ModuleRow{{Name: "react", Language: "typescript"}, {Name: big, Language: "typescript"}},
		Imports: []ImportRow{
			{FilePath: path, ModuleName: "react", ModuleLanguage: "typescript"},
			{FilePath: path, ModuleName: big, ModuleLanguage: "typescript"},
		},
		Parameters: []ParameterRow{
			{ParamName: "props", FilePath: path, FunctionName: "render", FunctionLine: 3},
			{ParamName: big, FilePath: path, FunctionName: "render", FunctionLine: 3},
			{ParamName: "ok", FilePath: path, FunctionName: big, FunctionLine: 9},
		},
		ClassMembers: []ClassMemberRow{
			{ClassName: "App", FunctionName: "render", FilePath: path, FunctionLine: 3},
			{ClassName: big, FunctionName: "render", FilePath: path, FunctionLine: 3},
		},
		NestedFuncs: []NestedFunctionRow{
			{OuterName: "render", InnerName: "inner", FilePath: path, InnerLine: 4},
			{OuterName: "render", InnerName: big, FilePath: path, InnerLine: 5},
		},
	}

	got, dropped := DropOversizedIndexKeys(mat)

	if len(got.Imports) != 1 || got.Imports[0].ModuleName != "react" {
		t.Fatalf("Imports kept = %d, want only the react import", len(got.Imports))
	}
	if len(got.Parameters) != 1 || got.Parameters[0].ParamName != "props" {
		t.Fatalf("Parameters kept = %d, want only props", len(got.Parameters))
	}
	if len(got.ClassMembers) != 1 || got.ClassMembers[0].ClassName != "App" {
		t.Fatalf("ClassMembers kept = %d, want only App", len(got.ClassMembers))
	}
	if len(got.NestedFuncs) != 1 || got.NestedFuncs[0].InnerName != "inner" {
		t.Fatalf("NestedFuncs kept = %d, want only inner", len(got.NestedFuncs))
	}
	// Only nodes the write would have created are reported: the Module and
	// the Parameter. Edge rows that only MATCH an oversized key are dropped
	// with them but are not separate skipped entities.
	var labels []string
	for _, d := range dropped {
		labels = append(labels, d.Label+"."+d.Property)
	}
	if want := []string{"Module.name", "Parameter.name"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("dropped = %v, want %v", labels, want)
	}
}

func TestDropOversizedIndexKeysLeavesNormalMaterializationUntouched(t *testing.T) {
	t.Parallel()

	mat := CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Modules:      []ModuleRow{{Name: "react", Language: "typescript"}},
		Imports:      []ImportRow{{FilePath: "/r/a.ts", ModuleName: "react", ModuleLanguage: "typescript"}},
		Entities: []EntityRow{{
			EntityID: "content-entity:e_000000000001", Label: "Function", EntityName: "render",
			FilePath: "/r/a.ts", Metadata: map[string]any{"k": "v"},
		}},
		Parameters:   []ParameterRow{{ParamName: "props", FilePath: "/r/a.ts", FunctionName: "render"}},
		ClassMembers: []ClassMemberRow{{ClassName: "App", FunctionName: "render", FilePath: "/r/a.ts"}},
		NestedFuncs:  []NestedFunctionRow{{OuterName: "render", InnerName: "inner", FilePath: "/r/a.ts"}},
	}

	got, dropped := DropOversizedIndexKeys(mat)

	if len(dropped) != 0 {
		t.Fatalf("dropped = %+v, want none", dropped)
	}
	if !reflect.DeepEqual(got, mat) {
		t.Fatalf("materialization changed:\n got %+v\nwant %+v", got, mat)
	}
	// No copy on the common path: the same backing arrays flow to the writer.
	if &got.Entities[0] != &mat.Entities[0] || &got.Modules[0] != &mat.Modules[0] {
		t.Fatal("DropOversizedIndexKeys copied slices when nothing was dropped")
	}
}

func TestDropOversizedIndexKeysValuePrefixIsBoundedValidUTF8(t *testing.T) {
	t.Parallel()

	mat := CanonicalMaterialization{Modules: []ModuleRow{{Name: strings.Repeat("é", MaxIndexedKeyBytes)}}}
	_, dropped := DropOversizedIndexKeys(mat)
	if len(dropped) != 1 {
		t.Fatalf("dropped len = %d, want 1", len(dropped))
	}
	prefix := dropped[0].ValuePrefix
	if len(prefix) == 0 || len(prefix) > oversizedValuePrefixBytes {
		t.Fatalf("ValuePrefix len = %d, want 1..%d", len(prefix), oversizedValuePrefixBytes)
	}
	if !utf8.ValidString(prefix) {
		t.Fatalf("ValuePrefix %q is not valid UTF-8", prefix)
	}
}

func entityIDs(rows []EntityRow) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.EntityID)
	}
	return ids
}

// BenchmarkDropOversizedIndexKeysNoneDropped measures the common path the
// canonical writer pays on every repository: a large materialization with no
// oversized key, where the guard only reads string lengths and copies nothing.
func BenchmarkDropOversizedIndexKeysNoneDropped(b *testing.B) {
	mat := CanonicalMaterialization{
		Entities:     make([]EntityRow, 50000),
		Modules:      make([]ModuleRow, 5000),
		Imports:      make([]ImportRow, 20000),
		Parameters:   make([]ParameterRow, 20000),
		ClassMembers: make([]ClassMemberRow, 10000),
		NestedFuncs:  make([]NestedFunctionRow, 5000),
	}
	for i := range mat.Entities {
		mat.Entities[i] = EntityRow{EntityName: "handleRequest", FilePath: "/repos/service/src/handlers/request.go"}
	}
	for i := range mat.Modules {
		mat.Modules[i] = ModuleRow{Name: "github.com/eshu-hq/eshu/go/internal/telemetry"}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, dropped := DropOversizedIndexKeys(mat)
		if len(dropped) != 0 {
			b.Fatal("unexpected drop")
		}
	}
}
