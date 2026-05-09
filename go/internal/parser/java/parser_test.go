package java

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

func TestParseEmitsJavaPayloadFromChildPackage(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "App.java")
	writeJavaTestFile(t, filePath, `package example;

import java.util.List;

public class App {
    public void run(List<String> names) {
        helper(names);
    }

    private void helper(List<String> names) {
    }
}
`)
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_java.Language())); err != nil {
		t.Fatalf("SetLanguage(java) error = %v, want nil", err)
	}
	defer parser.Close()

	got, err := Parse(filePath, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertJavaBucketItem(t, got, "classes", "App")
	assertJavaBucketItem(t, got, "functions", "run")
	assertJavaBucketItem(t, got, "imports", "java.util.List")
	call := assertJavaBucketItem(t, got, "function_calls", "helper")
	if call["class_context"] != "App" {
		t.Fatalf("helper class_context = %#v, want App", call["class_context"])
	}
}

func TestCallInferenceIndexResolvesReceiversFromSinglePass(t *testing.T) {
	t.Parallel()

	source := []byte(`package example;

public class SearchController {
    private SearchService fieldService;

    public void handle(SearchService parameterService) {
        SearchService localService = new SearchService();
        localService.search();
        parameterService.search();
        fieldService.search();
    }
}
`)
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_java.Language())); err != nil {
		t.Fatalf("SetLanguage(java) error = %v, want nil", err)
	}
	defer parser.Close()
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree")
	}
	defer tree.Close()

	index := buildJavaCallInferenceIndex(tree.RootNode(), source)
	got := map[string]string{}
	walkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "method_invocation" {
			return
		}
		fullName := javaCallFullName(node, source)
		if fullName == "" {
			return
		}
		got[fullName] = javaCallInferredObjectType(node, source, index)
	})

	want := map[string]string{
		"localService.search":     "SearchService",
		"parameterService.search": "SearchService",
		"fieldService.search":     "SearchService",
	}
	if len(got) != len(want) {
		t.Fatalf("inferred calls = %#v, want %#v", got, want)
	}
	for call, wantType := range want {
		if gotType := got[call]; gotType != wantType {
			t.Fatalf("inferred type for %s = %q, want %q; all calls %#v", call, gotType, wantType, got)
		}
	}
}

func writeJavaTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
}

func assertJavaBucketItem(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()
	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s bucket = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("%s missing item %q in %#v", bucket, name, items)
	return nil
}
