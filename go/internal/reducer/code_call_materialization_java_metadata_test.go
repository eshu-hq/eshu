package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestExtractCodeCallRowsResolvesJavaMetadataClassReferencesFromFileRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	metadataPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "services", "com.example.Plugin")
	writeReducerTestFile(t, metadataPath, `com.example.PluginImpl
`)
	classPath := filepath.Join(repoRoot, "src", "main", "java", "com", "example", "PluginImpl.java")
	writeReducerTestFile(t, classPath, `package com.example;

public final class PluginImpl {
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	metadataPayload, err := engine.ParsePath(repoRoot, metadataPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(metadata) error = %v, want nil", err)
	}
	classPayload, err := engine.ParsePath(repoRoot, classPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(class) error = %v, want nil", err)
	}
	assignReducerTestClassUID(t, classPayload, "PluginImpl", "content-entity:plugin-impl")
	metadataRelativePath := reducerTestRelativePath(t, repoRoot, metadataPath)
	classRelativePath := reducerTestRelativePath(t, repoRoot, classPath)

	_, rows := ExtractCodeCallRows([]facts.Envelope{
		{
			FactKind: "repository",
			Payload:  map[string]any{"repo_id": "repo-java"},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    metadataRelativePath,
				"parsed_file_data": metadataPayload,
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-java",
				"relative_path":    classRelativePath,
				"parsed_file_data": classPayload,
			},
		},
	})

	assertReducerReferencesRow(t, rows, "repo-java:"+metadataRelativePath, "content-entity:plugin-impl")
}
