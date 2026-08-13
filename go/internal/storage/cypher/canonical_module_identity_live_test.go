// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// modIdentModuleName stands in for a module name that genuinely exists in more
// than one language: Go's standard `time` package and Python's `time` module
// are unrelated modules that happen to share a name, and `path` (Go and
// JavaScript) and `basic` (Ruby and Python) collide the same way. The suffix
// makes the name synthetic on purpose. This test's cleanup deletes every
// :Module carrying the name, and a bare "time" would take real corpus nodes
// with it the first time someone points ESHU_CYPHER_BOLT_DSN at a shared
// backend. Nothing in the identity being proven depends on the literal string.
const modIdentModuleName = "time-6102-modident"

const (
	modIdentGoFilePath     = "/eshu-test/modident/go/main.go"
	modIdentPythonFilePath = "/eshu-test/modident/py/main.py"
)

// modIdentMaterialization builds the canonical materialization for one Go file
// and one Python file that each import a module named `time`. Both the module
// rows and the import rows carry the importing file's language, which is what
// the (name, language) Module identity keys on.
func modIdentMaterialization() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		GenerationID: "gen-modident",
		ScopeID:      "scope-modident",
		Modules: []projector.ModuleRow{
			{Name: modIdentModuleName, Language: "go"},
			{Name: modIdentModuleName, Language: "python"},
		},
		Imports: []projector.ImportRow{
			{
				FilePath:       modIdentGoFilePath,
				ModuleName:     modIdentModuleName,
				ModuleLanguage: "go",
				LineNumber:     3,
			},
			{
				FilePath:       modIdentPythonFilePath,
				ModuleName:     modIdentModuleName,
				ModuleLanguage: "python",
				LineNumber:     1,
			},
		},
	}
}

// TestLiveCanonicalModuleIdentityIsNameAndLanguage proves that the canonical
// import-graph Module node is identified by (name, language), not by name
// alone, on the pinned NornicDB backend.
//
// Before this was fixed, phase F MERGEd on `{name}` and phase G matched on
// `{name}` too, so ONE global node existed per module name across every
// language in the corpus. `SET m.lang = coalesce(m.lang, row.language)` then
// froze whichever language wrote first. The graph therefore asserted that a Go
// file imports a Python module, and a query filtering
// `(m:Module {name: 'time', lang: 'go'})` silently missed every Go importer
// whose node had been stamped `python` by an unrelated repository.
//
// The test drives the production statement builders -- w.buildModuleStatements
// and w.buildStructuralEdgeStatements -- not hand-written Cypher, so reverting
// either statement constant turns it red.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a NornicDB backend.
func TestLiveCanonicalModuleIdentityIsNameAndLanguage(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (m:Module {name: $name}) DETACH DELETE m`,
			map[string]any{"name": modIdentModuleName})
		_ = boltWriteStatement(ctx, runner,
			`UNWIND $paths AS p MATCH (f:File {path: p}) DETACH DELETE f`,
			map[string]any{"paths": []string{modIdentGoFilePath, modIdentPythonFilePath}})
	}
	cleanup()
	t.Cleanup(cleanup)

	// The Module MERGE is anchored by the same schema index production uses.
	if err := boltWriteStatement(ctx, runner,
		"CREATE INDEX module_name_lookup IF NOT EXISTS FOR (m:Module) ON (m.name)", nil); err != nil {
		t.Fatalf("create module name index: %v", err)
	}
	if err := boltWriteStatement(ctx, runner,
		`CREATE (:File {path: $go_path, language: 'go', lang: 'go', evidence_source: 'projector/canonical'}),
                (:File {path: $py_path, language: 'python', lang: 'python', evidence_source: 'projector/canonical'})`,
		map[string]any{"go_path": modIdentGoFilePath, "py_path": modIdentPythonFilePath}); err != nil {
		t.Fatalf("seed files: %v", err)
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 100, nil)
	mat := modIdentMaterialization()
	for _, stmt := range writer.buildModuleStatements(mat) {
		if err := runner.runCypherGroup(ctx, stmt); err != nil {
			t.Fatalf("module upsert: %v", err)
		}
	}
	for _, stmt := range writer.buildStructuralEdgeStatements(mat) {
		if err := runner.runCypherGroup(ctx, stmt); err != nil {
			t.Fatalf("structural edges: %v", err)
		}
	}

	// A Go `time` and a Python `time` are two different modules.
	langs, err := runner.runCypher(ctx,
		`MATCH (m:Module {name: $name}) RETURN m.lang AS lang ORDER BY m.lang`,
		map[string]any{"name": modIdentModuleName})
	if err != nil {
		t.Fatalf("read module nodes: %v", err)
	}
	gotLangs := make([]string, 0, len(langs))
	for _, row := range langs {
		value, _ := row["lang"].(string)
		gotLangs = append(gotLangs, value)
	}
	if len(gotLangs) != 2 || gotLangs[0] != "go" || gotLangs[1] != "python" {
		t.Fatalf("Module %q nodes: got langs %q, want exactly [go python] -- "+
			"a single node means the MERGE key collapsed two languages into one module",
			modIdentModuleName, gotLangs)
	}

	// Each file's IMPORTS edge must land on its own language's module.
	for _, tc := range []struct {
		filePath string
		wantLang string
	}{
		{filePath: modIdentGoFilePath, wantLang: "go"},
		{filePath: modIdentPythonFilePath, wantLang: "python"},
	} {
		rows, err := runner.runCypher(ctx,
			`MATCH (f:File {path: $path})-[:IMPORTS]->(m:Module)
             RETURN m.lang AS lang, m.name AS name ORDER BY m.lang`,
			map[string]any{"path": tc.filePath})
		if err != nil {
			t.Fatalf("read import edges for %s: %v", tc.filePath, err)
		}
		if len(rows) != 1 {
			t.Fatalf("file %s: got %d IMPORTS edges, want exactly 1 -- "+
				"more than one means the edge MATCH resolved every same-named module",
				tc.filePath, len(rows))
		}
		gotLang, _ := rows[0]["lang"].(string)
		gotName, _ := rows[0]["name"].(string)
		if gotName != modIdentModuleName || gotLang != tc.wantLang {
			t.Fatalf("file %s imports Module{name=%q, lang=%q}, want {name=%q, lang=%q}",
				tc.filePath, gotName, gotLang, modIdentModuleName, tc.wantLang)
		}
	}
}

// TestLiveCanonicalModuleIdentityReadoptsExistingLanguagedNode proves the
// upgrade path for an already-indexed deployment. A Module node written by the
// pre-fix writer carries {name, lang} already (the old writer always SET lang,
// even to the empty string), so the new (name, language) MERGE key MATCHES it
// rather than orphaning it. Only a node whose stamped language no longer
// matches any importer is left behind, and that node stays visible to the
// #5327 orphan sweep, which owns exactly this node class.
func TestLiveCanonicalModuleIdentityReadoptsExistingLanguagedNode(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (m:Module {name: $name}) DETACH DELETE m`,
			map[string]any{"name": modIdentModuleName})
		_ = boltWriteStatement(ctx, runner,
			`UNWIND $paths AS p MATCH (f:File {path: p}) DETACH DELETE f`,
			map[string]any{"paths": []string{modIdentGoFilePath, modIdentPythonFilePath}})
	}
	cleanup()
	t.Cleanup(cleanup)

	// Exactly what the pre-fix writer left behind: one global node per name,
	// stamped with whichever language happened to be projected first.
	if err := boltWriteStatement(ctx, runner,
		`CREATE (:Module {name: $name, lang: 'go', evidence_source: 'projector/canonical'})`,
		map[string]any{"name": modIdentModuleName}); err != nil {
		t.Fatalf("seed legacy module: %v", err)
	}
	if err := boltWriteStatement(ctx, runner,
		`CREATE (:File {path: $go_path, language: 'go', lang: 'go', evidence_source: 'projector/canonical'}),
                (:File {path: $py_path, language: 'python', lang: 'python', evidence_source: 'projector/canonical'})`,
		map[string]any{"go_path": modIdentGoFilePath, "py_path": modIdentPythonFilePath}); err != nil {
		t.Fatalf("seed files: %v", err)
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 100, nil)
	mat := modIdentMaterialization()
	for _, stmt := range writer.buildModuleStatements(mat) {
		if err := runner.runCypherGroup(ctx, stmt); err != nil {
			t.Fatalf("module upsert: %v", err)
		}
	}

	// Two nodes, not three: the legacy Go node was re-adopted, and only the
	// Python module is new.
	count, err := boltCount(ctx, runner,
		`MATCH (m:Module {name: $name}) RETURN count(m) AS count`,
		map[string]any{"name": modIdentModuleName})
	if err != nil {
		t.Fatalf("count module nodes: %v", err)
	}
	if count != 2 {
		t.Fatalf("Module %q node count after upgrade: got %d, want 2 "+
			"(the legacy lang=go node re-adopted, plus a new lang=python node)",
			modIdentModuleName, count)
	}
}
