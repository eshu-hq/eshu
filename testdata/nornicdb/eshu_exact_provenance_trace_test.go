package cypher

import (
	"context"
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

// TestEshuExactProvenanceQueriesUseIndexedMergeHotPath executes the exact Eshu
// provenance query constants inside the pinned NornicDB package so the test can
// inspect its internal hot-path trace.
func TestEshuExactProvenanceQueriesUseIndexedMergeHotPath(t *testing.T) {
	eshuRoot := os.Getenv("ESHU_ROOT")
	require.NotEmpty(t, eshuRoot)

	baseStore := newTestMemoryEngine(t)
	store := storage.NewNamespacedEngine(baseStore, "eshu-exact-provenance")
	exec := NewStorageExecutor(store)
	ctx := context.Background()

	for _, stmt := range []string{
		"CREATE CONSTRAINT repository_id_unique IF NOT EXISTS FOR (n:Repository) REQUIRE n.id IS UNIQUE",
		"CREATE CONSTRAINT package_uid_unique IF NOT EXISTS FOR (n:Package) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT package_version_uid_unique IF NOT EXISTS FOR (n:PackageVersion) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT container_image_digest_unique IF NOT EXISTS FOR (n:ContainerImage) REQUIRE n.digest IS UNIQUE",
		"CREATE (:Repository {id: 'repository:acme/app'})",
		"CREATE (:Package {uid: 'package:acme/app'})",
		"CREATE (:PackageVersion {uid: 'package:acme/app@1.0.0'})",
		"CREATE (:ContainerImage {digest: 'sha256:child'})",
		"CREATE (:ContainerImage {digest: 'sha256:base'})",
	} {
		_, err := exec.Execute(ctx, stmt, nil)
		require.NoError(t, err, stmt)
	}

	provenanceFile := filepath.Join(eshuRoot, "go/internal/storage/cypher/provenance_edge_writer.go")
	derivedFile := filepath.Join(eshuRoot, "go/internal/storage/cypher/derived_from_edge_writer.go")
	cases := []struct {
		name      string
		file      string
		constName string
		row       map[string]interface{}
	}{
		{
			name:      "publishes-package",
			file:      provenanceFile,
			constName: "canonicalProvenancePublishesPackageCypher",
			row: map[string]interface{}{
				"repository_id": "repository:acme/app", "package_id": "package:acme/app",
				"scope_id": "scope-a", "evidence_source": "reducer/package-ownership",
				"generation_id": "generation-a", "evidence_kinds": []string{"PACKAGE_OWNERSHIP_CORRELATION"},
				"source_tool": "unknown",
			},
		},
		{
			name:      "publishes-package-version",
			file:      provenanceFile,
			constName: "canonicalProvenancePublishesPackageVersionCypher",
			row: map[string]interface{}{
				"repository_id": "repository:acme/app", "version_id": "package:acme/app@1.0.0",
				"scope_id": "scope-a", "evidence_source": "reducer/package-publication",
				"generation_id": "generation-a", "evidence_kinds": []string{"PACKAGE_PUBLICATION_CORRELATION"},
				"source_tool": "unknown",
			},
		},
		{
			name:      "built-from",
			file:      provenanceFile,
			constName: "canonicalProvenanceBuiltFromCypher",
			row: map[string]interface{}{
				"digest": "sha256:child", "repository_id": "repository:acme/app",
				"scope_id": "scope-a", "evidence_source": "reducer/container-image-identity",
				"generation_id": "generation-a", "evidence_kinds": []string{"CONTAINER_IMAGE_IDENTITY_EXACT_DIGEST"},
				"source_tool": "oci",
			},
		},
		{
			name:      "derived-from",
			file:      derivedFile,
			constName: "canonicalProvenanceDerivedFromCypher",
			row: map[string]interface{}{
				"digest": "sha256:child", "base_digest": "sha256:base",
				"scope_id": "scope-a", "evidence_source": "reducer/container-image-base-image",
				"generation_id": "generation-a", "evidence_kinds": []string{"CONTAINER_IMAGE_DERIVED_FROM"},
				"attribution_basis": "dockerfile", "source_tool": "oci",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := readEshuStringConst(t, tc.file, tc.constName)
			_, err := exec.Execute(ctx, query, map[string]interface{}{
				"rows": []map[string]interface{}{tc.row},
			})
			require.NoError(t, err)
			trace := exec.LastHotPathTrace()
			t.Logf("%s trace: %+v", tc.constName, trace)
			require.True(t, trace.UnwindMergeChainBatch)
			require.True(t, trace.MergeSchemaLookupUsed)
			require.False(t, trace.MergeScanFallbackUsed)
			require.False(t, trace.OuterScanFallbackUsed)
		})
	}
}

func readEshuStringConst(t *testing.T, path, name string) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)
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
				require.True(t, ok)
				value, err := strconv.Unquote(literal.Value)
				require.NoError(t, err)
				return value
			}
		}
	}
	t.Fatalf("constant %s not found in %s", name, path)
	return ""
}
