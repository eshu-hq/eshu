package collector

import "testing"

func TestEntityBucketsFromParsedPreservesFunctionPackageImportPath(t *testing.T) {
	parsed := map[string]any{
		"language": "go",
		"functions": []map[string]any{{
			"name":                "Handle",
			"line_number":         7,
			"class_context":       "Server",
			"package_import_path": "example.com/service/handlers",
		}},
	}

	buckets := entityBucketsFromParsed(parsed)
	functions := buckets["functions"]
	if len(functions) != 1 {
		t.Fatalf("len(functions) = %d, want 1", len(functions))
	}

	metadata := functions[0].Metadata
	if got, want := metadata["class_context"], "Server"; got != want {
		t.Fatalf("Metadata[class_context] = %#v, want %#v", got, want)
	}
	if got, want := metadata["package_import_path"], "example.com/service/handlers"; got != want {
		t.Fatalf("Metadata[package_import_path] = %#v, want %#v", got, want)
	}
}
