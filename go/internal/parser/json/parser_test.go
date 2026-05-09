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
