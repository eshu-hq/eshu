// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/orneryd/nornicdb/pkg/storage"
	"github.com/stretchr/testify/require"
)

// canonicalNodeModuleUpsertLegacyCypher is a verbatim copy of the pre-change
// Eshu statement: MERGE on the module name alone, with the language settled by
// coalesce(). It is reproduced here, not read from Eshu source, so this harness
// can measure the OLD and NEW shapes side by side on the same pinned backend,
// same schema, and same populated store. That is the before/after the hot-path
// cost claim rests on.
const canonicalNodeModuleUpsertLegacyCypher = `UNWIND $rows AS row
MERGE (m:Module {name: row.name})
SET m.lang = coalesce(m.lang, row.language),
    m.evidence_source = 'projector/canonical'`

// readEshuModuleConst reads a named Go string constant out of Eshu's source, so
// the harness executes the production query text rather than a paraphrase of
// it. It takes testing.TB so the benchmark can use it too.
func readEshuModuleConst(tb testing.TB, path, name string) string {
	tb.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(tb, err)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range valueSpec.Names {
				if ident.Name != name || i >= len(valueSpec.Values) {
					continue
				}
				literal, ok := valueSpec.Values[i].(*ast.BasicLit)
				require.True(tb, ok)
				value, err := strconv.Unquote(literal.Value)
				require.NoError(tb, err)
				return value
			}
		}
	}
	tb.Fatalf("const %s not found in %s", name, path)
	return ""
}

// moduleTraceStore builds a store carrying the production Module schema index
// plus a populated Module label, so the MERGE lookup is measured against a
// realistic candidate set rather than an empty label.
//
// The population deliberately mixes both node classes that share the Module
// label in production: canonical import-graph modules (name + lang, no uid) and
// semantic declaration modules (uid-keyed, also carrying name and lang). Both
// sit in the module_name_lookup index, so both are candidates for a name
// lookup -- which is why the same-name candidate count is the number that
// decides whether (name, lang) costs more than (name).
//
// The seeded semantic modules carry a THIRD language on purpose. A semantic
// module sharing both the name and the language of a canonical import module is
// adopted by the canonical MERGE, which is pre-existing behavior (the old
// {name} key adopted it on the name alone, so the new key adopts strictly less
// often) but would make the identity readback below ambiguous.
func moduleTraceStore(tb testing.TB, namespace string, sameNameCount int) *StorageExecutor {
	tb.Helper()
	baseStore := newTestMemoryEngine(tb)
	store := storage.NewNamespacedEngine(baseStore, namespace)
	exec := NewStorageExecutor(store)
	ctx := context.Background()

	_, err := exec.Execute(ctx,
		"CREATE INDEX module_name_lookup IF NOT EXISTS FOR (m:Module) ON (m.name)", nil)
	require.NoError(tb, err)
	_, err = exec.Execute(ctx,
		"CREATE CONSTRAINT path IF NOT EXISTS FOR (f:File) REQUIRE f.path IS UNIQUE", nil)
	require.NoError(tb, err)

	for _, stmt := range []string{
		"CREATE (:File {path: '/repo/go/main.go', language: 'go', lang: 'go'})",
		"CREATE (:File {path: '/repo/py/main.py', language: 'python', lang: 'python'})",
	} {
		_, err = exec.Execute(ctx, stmt, nil)
		require.NoError(tb, err)
	}

	for i := 0; i < sameNameCount; i++ {
		_, err = exec.Execute(ctx,
			"CREATE (:Module {name: 'time', lang: 'ruby', uid: $uid})",
			map[string]interface{}{"uid": fmt.Sprintf("semantic-module-%d", i)})
		require.NoError(tb, err)
	}
	return exec
}

func moduleUpsertRows() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "time", "language": "go"},
		{"name": "time", "language": "python"},
	}
}

func importEdgeRows() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"file_path": "/repo/go/main.go", "module_name": "time", "module_language": "go",
			"imported_name": "", "alias": "", "line_number": 3,
			"generation_id": "generation-a",
		},
		{
			"file_path": "/repo/py/main.py", "module_name": "time", "module_language": "python",
			"imported_name": "", "alias": "", "line_number": 1,
			"generation_id": "generation-a",
		},
	}
}

// TestEshuExactModuleIdentityQueriesUseIndexedMergeHotPath executes the exact
// Eshu Module statements inside the pinned NornicDB package so the test can
// inspect its internal hot-path trace.
//
// The claim under test: moving the canonical Module MERGE key from {name} to
// {name, lang} does NOT push the write off the schema-lookup path onto a full
// Module label scan. NornicDB's findMergeNode tries an exact composite index,
// then a unique constraint, then the smallest single-property index candidate
// set, and only marks MergeScanFallbackUsed when none of those matched. Module
// cannot take a uniqueness constraint (the semantic entity path shares the
// label and MERGEs on uid) and this schema has no composite index, so the
// existing single-property module_name_lookup index is what must carry it.
func TestEshuExactModuleIdentityQueriesUseIndexedMergeHotPath(t *testing.T) {
	eshuRoot := os.Getenv("ESHU_ROOT")
	require.NotEmpty(t, eshuRoot)

	cypherFile := filepath.Join(eshuRoot, "go/internal/storage/cypher/canonical_node_cypher.go")
	ctx := context.Background()
	exec := moduleTraceStore(t, "eshu-exact-module-identity", 25)

	for _, tc := range []struct {
		name      string
		constName string
		params    map[string]interface{}
	}{
		{
			name:      "module-upsert",
			constName: "canonicalNodeModuleUpsertCypher",
			params:    map[string]interface{}{"rows": moduleUpsertRows()},
		},
		{
			name:      "import-edge",
			constName: "canonicalNodeImportEdgeCypher",
			params:    map[string]interface{}{"rows": importEdgeRows()},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := readEshuModuleConst(t, cypherFile, tc.constName)
			_, err := exec.Execute(ctx, query, tc.params)
			require.NoError(t, err)
			trace := exec.LastHotPathTrace()
			t.Logf("%s trace: %+v", tc.constName, trace)
			require.True(t, trace.MergeSchemaLookupUsed,
				"the MERGE must resolve through the schema index, not a label scan")
			require.False(t, trace.MergeScanFallbackUsed,
				"a scan fallback here would make every Module write cost the whole label population")
			require.False(t, trace.OuterScanFallbackUsed)
		})
	}

	// The identity holds on the backend: two canonical import modules, one per
	// language, and each file's IMPORTS edge on its own language's module.
	modules, err := exec.Execute(ctx,
		"MATCH (m:Module {name: 'time'}) WHERE m.uid IS NULL RETURN m.lang AS lang ORDER BY m.lang", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"lang"}, modules.Columns)
	require.Len(t, modules.Rows, 2)
	require.Equal(t, "go", modules.Rows[0][0])
	require.Equal(t, "python", modules.Rows[1][0])

	edges, err := exec.Execute(ctx,
		"MATCH (f:File)-[:IMPORTS]->(m:Module) RETURN f.path AS path, m.lang AS lang ORDER BY f.path", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"path", "lang"}, edges.Columns)
	require.Len(t, edges.Rows, 2)
	require.Equal(t, "go", edges.Rows[0][1])
	require.Equal(t, "python", edges.Rows[1][1])
}

// BenchmarkEshuModuleUpsertMergeKey measures the canonical Module upsert with
// the legacy {name} key against the (name, lang) key on the same pinned
// backend, same schema, and the same same-name candidate population. Both
// shapes resolve through the same module_name_lookup index and therefore load
// the same candidate set; the only added work is comparing one more property
// per candidate in Go.
func BenchmarkEshuModuleUpsertMergeKey(b *testing.B) {
	eshuRoot := os.Getenv("ESHU_ROOT")
	if eshuRoot == "" {
		b.Skip("ESHU_ROOT not set")
	}
	cypherFile := filepath.Join(eshuRoot, "go/internal/storage/cypher/canonical_node_cypher.go")
	ctx := context.Background()

	for _, sameNameCount := range []int{1, 25, 200} {
		for _, variant := range []struct {
			name     string
			constant string
			fromEshu bool
		}{
			{name: "legacy-name-only", constant: canonicalNodeModuleUpsertLegacyCypher},
			{name: "name-and-lang", constant: "canonicalNodeModuleUpsertCypher", fromEshu: true},
		} {
			b.Run(fmt.Sprintf("candidates=%d/%s", sameNameCount, variant.name), func(b *testing.B) {
				exec := moduleTraceStore(b, fmt.Sprintf("bench-%d-%s", sameNameCount, variant.name), sameNameCount)
				query := variant.constant
				if variant.fromEshu {
					query = readEshuModuleConst(b, cypherFile, variant.constant)
				}
				rows := map[string]interface{}{"rows": moduleUpsertRows()}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := exec.Execute(ctx, query, rows); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
