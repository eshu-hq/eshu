package json

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParsePackageJSONPreservesOrderedMetadataAndDependencyRows(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package.json", `{
  "zeta": true,
  "alpha": true,
  "scripts": {
    "test": "vitest",
    "build": "tsc"
  },
  "dependencies": {
    "zod": "^3.0.0",
    "@types/node": "^20.0.0"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if got, want := topLevelKeys(t, payload), []string{"zeta", "alpha", "scripts", "dependencies", "devDependencies"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level keys = %#v, want %#v", got, want)
	}
	if got, want := bucketNames(t, payload, "variables"), []string{"zod", "@types/node", "typescript"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("variables = %#v, want %#v", got, want)
	}
	if got, want := bucketNames(t, payload, "functions"), []string{"test", "build"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("functions = %#v, want %#v", got, want)
	}
}

func TestParsePackageLockJSONEmitsExactDependencyRows(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package-lock.json", `{
  "name": "eshu",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "dependencies": {
        "vite": "^5.4.11"
      }
    },
    "node_modules/vite": {
      "version": "5.4.21"
    },
    "node_modules/@esbuild/linux-x64": {
      "version": "0.21.5",
      "optional": true
    },
    "node_modules/@vitest/mocker": {
      "version": "2.1.9",
      "dev": true
    }
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	got := dependencyRowsByName(rows)
	for name, version := range map[string]string{
		"vite":               "5.4.21",
		"@esbuild/linux-x64": "0.21.5",
		"@vitest/mocker":     "2.1.9",
	} {
		row, ok := got[name]
		if !ok {
			t.Fatalf("dependency row %q missing from %#v", name, rows)
		}
		if row["value"] != version {
			t.Fatalf("%s value = %#v, want %q", name, row["value"], version)
		}
		if row["package_manager"] != "npm" || row["config_kind"] != "dependency" {
			t.Fatalf("%s metadata = %#v, want npm dependency", name, row)
		}
		if row["section"] != "package-lock" {
			t.Fatalf("%s section = %#v, want package-lock", name, row["section"])
		}
	}
}

func TestParsePackageLockJSONPreservesDependencyChainRows(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package-lock.json", `{
  "name": "eshu",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "dependencies": {
        "vite": "^5.4.11"
      }
    },
    "node_modules/vite": {
      "version": "5.4.21",
      "dependencies": {
        "rollup": "^4.30.0"
      }
    },
    "node_modules/rollup": {
      "version": "4.34.9",
      "dependencies": {
        "fsevents": "~2.3.3"
      }
    },
    "node_modules/fsevents": {
      "version": "2.3.3"
    }
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	got := dependencyRowsByName(rows)
	assertPackageLockChain(t, got["vite"], []string{"vite"}, 1, true)
	assertPackageLockChain(t, got["rollup"], []string{"vite", "rollup"}, 2, false)
	assertPackageLockChain(t, got["fsevents"], []string{"vite", "rollup", "fsevents"}, 3, false)
}

func TestParseJSONCAcceptsCommentsAndTrailingCommas(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "turbo.jsonc", `{
  "$schema": "./node_modules/turbo/schema.json",
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": ["dist/**"],
    },
    // Supabase-style JSONC config comments should not block parsing.
    "lint": {
      "outputs": [],
    },
  },
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if got, want := topLevelKeys(t, payload), []string{"$schema", "tasks"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level keys = %#v, want %#v", got, want)
	}
}

func TestStripTrailingCommasPreservesStringCommas(t *testing.T) {
	t.Parallel()

	source := `{
  "literal": ",}",
  "array": [
    "keep,]",
  ],
}`

	got := stripTrailingCommas(source)
	want := `{
  "literal": ",}",
  "array": [
    "keep,]"
  ]
}`
	if got != want {
		t.Fatalf("stripTrailingCommas() = %q, want %q", got, want)
	}
}

func TestParseDBTManifestUsesSuppliedLineageExtractor(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "dbt_manifest.json", `{
  "metadata": {"dbt_version": "1.7.0"},
  "sources": {
    "source.raw.orders": {
      "resource_type": "source",
      "database": "raw",
      "schema": "public",
      "identifier": "orders",
      "name": "orders",
      "columns": {"id": {"name": "id"}}
    }
  },
  "macros": {},
  "nodes": {
    "model.analytics.order_metrics": {
      "resource_type": "model",
      "database": "analytics",
      "schema": "public",
      "identifier": "order_metrics",
      "name": "order_metrics",
      "compiled_code": "select id from raw.public.orders",
      "depends_on": {"nodes": ["source.raw.orders"]},
      "columns": {"id": {"name": "id"}}
    }
  }
}`)
	extractCalled := false

	payload, err := Parse(path, false, shared.Options{}, Config{
		LineageExtractor: func(compiledSQL string, modelName string, relationColumnNames map[string][]string) CompiledModelLineage {
			extractCalled = true
			if compiledSQL == "" {
				t.Fatalf("compiledSQL is empty")
			}
			if modelName != "order_metrics" {
				t.Fatalf("modelName = %q, want order_metrics", modelName)
			}
			return CompiledModelLineage{
				ColumnLineage: []ColumnLineage{{
					OutputColumn:  "id",
					SourceColumns: []string{"raw.public.orders.id"},
				}},
				ProjectionCount: 1,
			}
		},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if !extractCalled {
		t.Fatalf("LineageExtractor was not called")
	}
	assertRelationshipPresent(t, payload, "COLUMN_DERIVES_FROM", "analytics.public.order_metrics.id", "raw.public.orders.id")
}

func writeJSONTestFile(t *testing.T, name string, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
	return path
}

func topLevelKeys(t *testing.T, payload map[string]any) []string {
	t.Helper()

	metadata, ok := payload["json_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("json_metadata = %T, want map[string]any", payload["json_metadata"])
	}
	keys, ok := metadata["top_level_keys"].([]string)
	if !ok {
		t.Fatalf("json_metadata.top_level_keys = %T, want []string", metadata["top_level_keys"])
	}
	return keys
}

func bucketNames(t *testing.T, payload map[string]any, key string) []string {
	t.Helper()

	rows, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row["name"].(string))
	}
	return names
}

func dependencyRowsByName(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		if name != "" {
			out[name] = row
		}
	}
	return out
}

func assertPackageLockChain(
	t *testing.T,
	row map[string]any,
	wantPath []string,
	wantDepth int,
	wantDirect bool,
) {
	t.Helper()
	if row == nil {
		t.Fatalf("dependency row missing")
	}
	gotPath, ok := row["dependency_path"].([]string)
	if !ok {
		t.Fatalf("dependency_path = %T %#v, want []string", row["dependency_path"], row["dependency_path"])
	}
	if !reflect.DeepEqual(gotPath, wantPath) {
		t.Fatalf("dependency_path = %#v, want %#v", gotPath, wantPath)
	}
	if got := row["dependency_depth"]; got != wantDepth {
		t.Fatalf("dependency_depth = %#v, want %d", got, wantDepth)
	}
	if got := row["direct_dependency"]; got != wantDirect {
		t.Fatalf("direct_dependency = %#v, want %v", got, wantDirect)
	}
}

func assertRelationshipPresent(t *testing.T, payload map[string]any, relationshipType string, sourceName string, targetName string) {
	t.Helper()

	relationships, ok := payload["data_relationships"].([]map[string]any)
	if !ok {
		t.Fatalf("data_relationships = %T, want []map[string]any", payload["data_relationships"])
	}
	for _, relationship := range relationships {
		if relationship["type"] == relationshipType &&
			relationship["source_name"] == sourceName &&
			relationship["target_name"] == targetName {
			return
		}
	}
	t.Fatalf("relationship %s %s -> %s not found in %#v", relationshipType, sourceName, targetName, relationships)
}
