// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// moduleRowSet renders module rows as comparable (name, language) keys.
func moduleRowSet(rows []ModuleRow) map[moduleIdentity]struct{} {
	set := make(map[moduleIdentity]struct{}, len(rows))
	for _, row := range rows {
		set[moduleIdentity{name: row.Name, language: row.Language}] = struct{}{}
	}
	return set
}

func requireModuleIdentities(t *testing.T, rows []ModuleRow, want ...moduleIdentity) {
	t.Helper()
	got := moduleRowSet(rows)
	if len(got) != len(rows) {
		t.Fatalf("module rows contain a duplicate identity: %+v", rows)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d module rows %+v, want %d", len(rows), rows, len(want))
	}
	for _, identity := range want {
		if _, ok := got[identity]; !ok {
			t.Fatalf("missing Module{name=%q, lang=%q}; got %+v",
				identity.name, identity.language, rows)
		}
	}
}

// TestExtractImportsKeepsSameNamedModulesPerLanguage proves the projector does
// not collapse two same-named modules from different languages before they ever
// reach the graph writer. Fixing only the Cypher MERGE key would have left this
// in place: the extractor's own dedupe map was keyed on the module name, so the
// second language's module was dropped in process and the writer never saw it.
//
// `time` is a real collision in the corpus (Go's standard library and Python's),
// as are `path` (Go and JavaScript) and `basic` (Ruby and Python).
func TestExtractImportsKeepsSameNamedModulesPerLanguage(t *testing.T) {
	rows, modules, quarantined := extractImportsFromFiles([]parsedFileRef{
		{
			Path:     "/repos/proj/main.go",
			Language: "go",
			FactID:   "f-go",
			FactKind: "file",
			ParsedFileData: map[string]any{
				"imports": []any{map[string]any{"name": "time", "line_number": 3}},
			},
		},
		{
			Path:     "/repos/proj/main.py",
			Language: "python",
			FactID:   "f-py",
			FactKind: "file",
			ParsedFileData: map[string]any{
				"imports": []any{map[string]any{"name": "time", "source": "time", "line_number": 1}},
			},
		},
	})
	if len(quarantined) != 0 {
		t.Fatalf("unexpected quarantined facts: %+v", quarantined)
	}

	requireModuleIdentities(t, modules,
		moduleIdentity{name: "time", language: "go"},
		moduleIdentity{name: "time", language: "python"},
	)

	if len(rows) != 2 {
		t.Fatalf("got %d import rows %+v, want 2", len(rows), rows)
	}
	byFile := make(map[string]ImportRow, len(rows))
	for _, row := range rows {
		byFile[row.FilePath] = row
	}
	for path, wantLanguage := range map[string]string{
		"/repos/proj/main.go": "go",
		"/repos/proj/main.py": "python",
	} {
		row, ok := byFile[path]
		if !ok {
			t.Fatalf("no import row for %s; got %+v", path, rows)
		}
		if row.ModuleLanguage != wantLanguage {
			t.Fatalf("import row for %s targets module language %q, want %q -- "+
				"the edge would resolve to the wrong language's Module node",
				path, row.ModuleLanguage, wantLanguage)
		}
	}
}

// TestExtractImportsGivesUnknownLanguageItsOwnModule pins the empty-language
// rule: a module discovered only from files whose language could not be
// determined gets its own node, one whose `lang` property is the empty string.
// It never merges into a languaged module (that would attribute an
// unattributable import to a language the evidence does not support), and it
// cannot multiply, because all unknown-language importers of one name share
// that single empty-language node.
func TestExtractImportsGivesUnknownLanguageItsOwnModule(t *testing.T) {
	_, modules, quarantined := extractImportsFromFiles([]parsedFileRef{
		{
			Path:     "/repos/proj/a.go",
			Language: "go",
			FactID:   "f-go",
			FactKind: "file",
			ParsedFileData: map[string]any{
				"imports": []any{map[string]any{"name": "time"}},
			},
		},
		{
			Path:     "/repos/proj/vendored.bin",
			Language: "",
			FactID:   "f-unknown-1",
			FactKind: "file",
			ParsedFileData: map[string]any{
				"imports": []any{map[string]any{"name": "time"}},
			},
		},
		{
			Path:     "/repos/proj/other.bin",
			Language: "",
			FactID:   "f-unknown-2",
			FactKind: "file",
			ParsedFileData: map[string]any{
				"imports": []any{map[string]any{"name": "time"}},
			},
		},
	})
	if len(quarantined) != 0 {
		t.Fatalf("unexpected quarantined facts: %+v", quarantined)
	}
	requireModuleIdentities(t, modules,
		moduleIdentity{name: "time", language: ""},
		moduleIdentity{name: "time", language: "go"},
	)
}

// TestMergeImportModulesKeysOnNameAndLanguage guards the seam between the
// entity-derived module set (a repository's own declared modules) and the
// import-derived one. Keyed on name alone, the discovered Python `basic` was
// silently dropped because a Ruby `basic` was already present, so the graph
// never learned about it -- the writer's collision reproduced one layer up.
func TestMergeImportModulesKeysOnNameAndLanguage(t *testing.T) {
	existing := []ModuleRow{{Name: "basic", Language: "ruby"}}
	discovered := []ModuleRow{
		{Name: "basic", Language: "python"},
		{Name: "basic", Language: "ruby"},
	}
	requireModuleIdentities(t, mergeImportModules(existing, discovered),
		moduleIdentity{name: "basic", language: "ruby"},
		moduleIdentity{name: "basic", language: "python"},
	)
}

// TestExtractModulesFromEntitiesKeysOnNameAndLanguage covers the other producer
// of Module rows: entity facts whose entity_type maps to the Module label. It
// deduped on entity_name alone, so a Ruby module and a Python module sharing a
// name yielded one row.
func TestExtractModulesFromEntitiesKeysOnNameAndLanguage(t *testing.T) {
	moduleEntityFact := func(factID, name, language string) facts.Envelope {
		return facts.Envelope{
			FactID:   factID,
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":       "repo-abc",
				"entity_type":   "Module",
				"entity_name":   name,
				"relative_path": factID + ".src",
				"language":      language,
			},
		}
	}
	rows := extractModulesFromEntities([]facts.Envelope{
		moduleEntityFact("e-1", "inheritance", "ruby"),
		moduleEntityFact("e-2", "inheritance", "python"),
		moduleEntityFact("e-3", "inheritance", "ruby"),
	})
	requireModuleIdentities(t, rows,
		moduleIdentity{name: "inheritance", language: "ruby"},
		moduleIdentity{name: "inheritance", language: "python"},
	)
}

// TestBuildCanonicalMaterializationImportsResolveToDeclaredModules pins the
// invariant the graph writer's phase G silently depends on: every ImportRow
// must name a (module, language) pair that some ModuleRow in the SAME
// materialization declares.
//
// Phase F MERGEs Module on (name, lang); phase G resolves the edge target with
// `MATCH (m:Module {name: row.module_name, lang: row.module_language})`. A
// MATCH that finds nothing produces no row, so the MERGE never runs and the
// IMPORTS edge is not written -- no error, no log, just a missing edge. That
// makes a producer that forgets ModuleLanguage invisible until someone counts
// edges on a live backend, which is exactly how it slipped past the fixtures in
// the offlinetier live tests.
//
// Both producers of Module rows are exercised in one materialization: parsed
// file imports (extractImportsFromFiles) and content_entity module facts
// (extractRelationships).
func TestBuildCanonicalMaterializationImportsResolveToDeclaredModules(t *testing.T) {
	t.Parallel()

	result, quarantined := buildCanonicalMaterialization(testScope(), testGeneration(), []facts.Envelope{
		importRepositoryFact(),
		// Same module name from two unrelated ecosystems: two Module nodes.
		fileFactWithImports("f-go", "main.go", "go", []map[string]any{
			{"name": "time", "line_number": 3},
		}),
		fileFactWithImports("f-py", "client.py", "python", []map[string]any{
			{"name": "time", "source": "time", "line_number": 1},
		}),
		// Same package from two languages of one ecosystem: also two nodes,
		// which is the trade-off the (name, language) key accepts on purpose.
		fileFactWithImports("f-ts", "src/app.ts", "typescript", []map[string]any{
			{"name": "Router", "source": "express", "line_number": 2},
		}),
		fileFactWithImports("f-js", "src/app.js", "javascript", []map[string]any{
			{"name": "Router", "source": "express", "line_number": 2},
		}),
		// The other producer: a content_entity import fact.
		{
			FactID:   "i-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"module_name":     "requests",
				"imported_module": "requests",
				"imported_name":   "Session",
				"relative_path":   "src/client.py",
				"language":        "python",
				"line_number":     3,
			},
		},
	})
	if len(quarantined) != 0 {
		t.Fatalf("unexpected quarantined facts: %+v", quarantined)
	}

	// Assert the count first: an empty or short Imports slice would make the
	// loop below pass while checking nothing.
	if len(result.Imports) != 5 {
		t.Fatalf("got %d import rows %+v, want 5 (4 parsed-file + 1 content_entity)",
			len(result.Imports), result.Imports)
	}

	declared := moduleRowSet(result.Modules)
	for _, row := range result.Imports {
		identity := moduleIdentity{name: row.ModuleName, language: row.ModuleLanguage}
		if _, ok := declared[identity]; !ok {
			t.Fatalf("import row %+v targets Module{name=%q, lang=%q}, which no module row declares; "+
				"phase G would match no node and drop the edge without an error. Declared: %+v",
				row, row.ModuleName, row.ModuleLanguage, result.Modules)
		}
	}

	// And the two same-named pairs really did stay apart, so the invariant
	// above was checked against a materialization that exercises the split.
	for _, want := range []moduleIdentity{
		{name: "time", language: "go"},
		{name: "time", language: "python"},
		{name: "express", language: "typescript"},
		{name: "express", language: "javascript"},
	} {
		if _, ok := declared[want]; !ok {
			t.Fatalf("missing Module{name=%q, lang=%q}; got %+v", want.name, want.language, result.Modules)
		}
	}
}
