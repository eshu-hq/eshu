package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesJavaLiteralReflectionReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "main", "java", "example", "ReflectionBootstrap.java")
	writeReducerTestFile(t, filePath, `package example;

public class ReflectionBootstrap {
    public void bootstrap() throws Exception {
        Class.forName("example.Plugin");
        Plugin.class.getDeclaredMethod("run", String.class);
    }
}

final class Plugin {
    void run(String value) {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "bootstrap", "content-entity:java-bootstrap")
	assignReducerTestFunctionUID(t, payload, "run", "content-entity:java-plugin-run")
	assignReducerTestClassUID(t, payload, "Plugin", "content-entity:java-plugin")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerReferencesRow(t, rows, "content-entity:java-bootstrap", "content-entity:java-plugin")
	assertReducerReferencesRow(t, rows, "content-entity:java-bootstrap", "content-entity:java-plugin-run")
}

func TestExtractCodeCallRowsIgnoresJavaDynamicReflectionStrings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "main", "java", "example", "ReflectionBootstrap.java")
	writeReducerTestFile(t, filePath, `package example;

public class ReflectionBootstrap {
    public void bootstrap(String className, String methodName) throws Exception {
        Class.forName(className);
        Plugin.class.getDeclaredMethod(methodName);
    }
}

final class Plugin {
    void run() {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	payload, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	assignReducerTestFunctionUID(t, payload, "bootstrap", "content-entity:java-bootstrap")
	assignReducerTestFunctionUID(t, payload, "run", "content-entity:java-plugin-run")
	assignReducerTestClassUID(t, payload, "Plugin", "content-entity:java-plugin")

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    reducerTestRelativePath(t, repoRoot, filePath),
				"parsed_file_data": payload,
			},
		},
	})

	assertReducerNoCodeCallRow(t, rows, "content-entity:java-bootstrap", "content-entity:java-plugin")
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-bootstrap", "content-entity:java-plugin-run")
}

func assertReducerReferencesRow(t *testing.T, rows []map[string]any, callerID string, calleeID string) {
	t.Helper()

	for _, row := range rows {
		if row["caller_entity_id"] != callerID || row["callee_entity_id"] != calleeID {
			continue
		}
		if got, want := row["relationship_type"], "REFERENCES"; got != want {
			t.Fatalf("relationship_type = %#v, want %#v in %#v", got, want, rows)
		}
		return
	}
	t.Fatalf("missing REFERENCES row %s -> %s in %#v", callerID, calleeID, rows)
}
