// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDefaultEngineParsePathPythonModuleDocstringEmitsModuleMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "module_docstring.py")
	if err := os.WriteFile(filePath, []byte(`"""Utilities for payments."""

def ping():
    return True
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", filePath, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	modules, ok := got["modules"].([]map[string]any)
	if !ok {
		t.Fatalf("modules = %T, want []map[string]any", got["modules"])
	}
	var moduleItem map[string]any
	for _, item := range modules {
		if item["name"] == "module_docstring" {
			moduleItem = item
			break
		}
	}
	if moduleItem == nil {
		t.Fatalf("modules missing name %q in %#v", "module_docstring", modules)
	}
	if docstring, _ := moduleItem["docstring"].(string); docstring != "Utilities for payments." {
		t.Fatalf("docstring = %#v, want %#v", docstring, "Utilities for payments.")
	}
	if lang, _ := moduleItem["lang"].(string); lang != "python" {
		t.Fatalf("lang = %#v, want %#v", lang, "python")
	}
}
