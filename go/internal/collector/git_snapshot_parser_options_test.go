package collector

import (
	"path/filepath"
	"testing"
)

func TestSnapshotParserOptionsUseModuleVariablesForJava(t *testing.T) {
	t.Parallel()

	got := snapshotParserOptions(filepath.Join("src", "main", "java", "Demo.java"), nil)
	if got.VariableScope != "module" {
		t.Fatalf("VariableScope = %q, want module for Java", got.VariableScope)
	}
	if !got.IndexSource {
		t.Fatal("IndexSource = false, want true")
	}
}

func TestSnapshotParserOptionsKeepAllVariablesForDynamicLanguages(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		filepath.Join("src", "handler.ts"),
		filepath.Join("src", "worker.js"),
		filepath.Join("src", "tasks.py"),
	} {
		got := snapshotParserOptions(path, nil)
		if got.VariableScope != "all" {
			t.Fatalf("%s VariableScope = %q, want all", path, got.VariableScope)
		}
	}
}
