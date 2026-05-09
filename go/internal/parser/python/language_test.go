package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

func TestParseEmitsPythonAdapterSurface(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "service.py")
	source := `from fastapi import FastAPI

app = FastAPI()

class Service:
    """Coordinates work."""

    def run(self, item: str) -> bool:
        helper(item)
        return True

@app.get("/health")
def health():
    return {"ok": True}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}

	got, err := Parse(t.TempDir(), path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketHasName(t, got, "classes", "Service")
	assertBucketHasName(t, got, "functions", "run")
	assertBucketHasName(t, got, "functions", "health")
	assertBucketHasName(t, got, "function_calls", "helper")
}

func assertBucketHasName(t *testing.T, payload map[string]any, bucket string, name string) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s bucket has type %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			return
		}
	}
	t.Fatalf("%s bucket missing %q in %#v", bucket, name, items)
}
