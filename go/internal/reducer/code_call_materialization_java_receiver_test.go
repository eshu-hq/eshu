package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesJavaExplicitOuterThisFieldReceivers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "build-plugin", "src", "main", "java", "example", "BootZipCopyAction.java")
	writeReducerTestFile(t, filePath, `package example;

public class BootZipCopyAction {
    private final LayerResolver layerResolver;

    private final class Processor {
        void process(FileCopyDetails details) {
            Layer layer = BootZipCopyAction.this.layerResolver.getLayer(details);
        }
    }
}

class LayerResolver {
    Layer getLayer(FileCopyDetails details) {
        return null;
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
	assignReducerTestFunctionUID(t, payload, "process", "content-entity:java-process")
	assignReducerTestFunctionUID(t, payload, "getLayer", "content-entity:java-get-layer")

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

	assertReducerCodeCallRow(t, rows, "content-entity:java-process", "content-entity:java-get-layer")
}

func TestExtractCodeCallRowsResolvesJavaArgumentReturnTypeOverloads(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "build-plugin", "src", "main", "java", "example", "LayerResolver.java")
	writeReducerTestFile(t, filePath, `package example;

class LayerResolver {
    Layer getLayer(FileCopyDetails details) {
        return getLayer(asLibrary(details));
    }

    Layer getLayer(Library library) {
        return null;
    }

    Layer getLayer(String applicationResource) {
        return null;
    }

    private Library asLibrary(FileCopyDetails details) {
        return null;
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
	assignReducerTestDuplicateFunctionUIDs(
		t,
		payload,
		"getLayer",
		"content-entity:java-get-layer-details",
		"content-entity:java-get-layer-library",
		"content-entity:java-get-layer-string",
	)
	assignReducerTestFunctionUID(t, payload, "asLibrary", "content-entity:java-as-library")

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

	assertReducerCodeCallRow(t, rows, "content-entity:java-get-layer-details", "content-entity:java-get-layer-library")
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-get-layer-details", "content-entity:java-get-layer-string")
}
