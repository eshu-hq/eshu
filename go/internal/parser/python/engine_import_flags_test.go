// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// pythonImportFlagFixture exercises every import context that decides whether
// a Python import can close a runtime import cycle (issue #7344): a module-
// level import runs at load time, a TYPE_CHECKING import never runs, a
// function-level import runs at call time, and a relative import whose module
// is missing on disk gets a synthesized "./x" fallback source.
const pythonImportFlagFixture = `from __future__ import annotations
import os
import typing as t
from typing import TYPE_CHECKING
from .b import B_CONST
from .missing import Ghost

if TYPE_CHECKING:
    from .b import BType
    import collections
else:
    from .b import RuntimeB

if t.TYPE_CHECKING:
    from .b import BTypeViaAttribute

if not TYPE_CHECKING:
    from .b import NegatedGuard


def late():
    from .b import LateB
    import json
    return LateB, json


class Holder:
    import decimal


if TYPE_CHECKING:
    def annotated():
        from .b import DoubleFlag
`

// TestDefaultEngineParsePathPythonImportFlags is the #7344 Python fixture gate:
// type_only, deferred, and inferred are set exactly where the import context
// says so, and stay absent (not false) everywhere else so unflagged entries are
// byte-identical to the pre-#7344 payload.
func TestDefaultEngineParsePathPythonImportFlags(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "pkg", "__init__.py"), "")
	writeTestFile(t, filepath.Join(repoRoot, "pkg", "b.py"), "B_CONST = 1\n")
	filePath := filepath.Join(repoRoot, "pkg", "a.py")
	writeTestFile(t, filePath, pythonImportFlagFixture)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	tests := []struct {
		name       string
		typeOnly   bool
		deferred   bool
		inferred   bool
		wantSource string
	}{
		{name: "os", wantSource: "os"},
		{name: "TYPE_CHECKING", wantSource: "typing"},
		{name: "B_CONST", wantSource: "./b"},
		{name: "Ghost", inferred: true, wantSource: "./missing"},
		{name: "BType", typeOnly: true, wantSource: "./b"},
		{name: "collections", typeOnly: true, wantSource: "collections"},
		{name: "RuntimeB", wantSource: "./b"},
		{name: "BTypeViaAttribute", typeOnly: true, wantSource: "./b"},
		{name: "NegatedGuard", wantSource: "./b"},
		{name: "LateB", deferred: true, wantSource: "./b"},
		{name: "json", deferred: true, wantSource: "json"},
		{name: "decimal", wantSource: "decimal"},
		{name: "DoubleFlag", typeOnly: true, deferred: true, wantSource: "./b"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := findPythonImport(t, got, tt.name, tt.wantSource)
			assertImportFlag(t, item, "type_only", tt.typeOnly)
			assertImportFlag(t, item, "deferred", tt.deferred)
			assertImportFlag(t, item, "inferred", tt.inferred)
		})
	}
}

// findPythonImport returns the imports row with the given name and source,
// failing the test when it is absent.
func findPythonImport(t *testing.T, payload map[string]any, name string, source string) map[string]any {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		if got, _ := item["name"].(string); got != name {
			continue
		}
		if got, _ := item["source"].(string); got != source {
			continue
		}
		return item
	}
	t.Fatalf("imports missing name=%q source=%q in %#v", name, source, items)
	return nil
}

// assertImportFlag requires a flag to be the boolean true when want is true
// and to be absent entirely when want is false: a stored false would change the
// payload of every unflagged import.
func assertImportFlag(t *testing.T, item map[string]any, flag string, want bool) {
	t.Helper()

	value, present := item[flag]
	if !want {
		if present {
			t.Fatalf("import %v flag %q = %#v, want absent", item["name"], flag, value)
		}
		return
	}
	if got, _ := value.(bool); !got {
		t.Fatalf("import %v flag %q = %#v, want true", item["name"], flag, value)
	}
}
