// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathGoImportsCarryNoFlags is the #7344 Go decision
// gate. Go has no type-only, deferred, or inferred import, and the compiler
// rejects an import cycle even when one edge is a blank (`_`) or dot (`.`)
// import, so every form is retained as an ordinary edge and none is flagged.
// Dropping a blank or dot import would hide a cycle the compiler refuses to
// build; flagging it would let the cycle query exclude a real edge. The
// local binding is still recorded: named aliases keep their alias, and blank
// and dot imports record "_" and "." so delta derivation (#10301) can tell a
// file whose bare names may come from a dot-imported package apart from one
// whose bare names are provably same-package.
func TestDefaultEngineParsePathGoImportsCarryNoFlags(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.go")
	parsertest.WriteFile(t, filePath, `package main

import (
	"fmt"
	str "strings"
	_ "embed"
	. "math"
)

func main() { fmt.Println(str.ToUpper("x"), Pi) }
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	seen := map[string]bool{}
	aliases := map[string]any{}
	for _, item := range items {
		name, _ := item["name"].(string)
		seen[name] = true
		aliases[name] = item["alias"]
		for _, flag := range []string{"type_only", "deferred", "inferred"} {
			if value, present := item[flag]; present {
				t.Errorf("go import %q flag %q = %#v, want absent", name, flag, value)
			}
		}
	}
	// Blank and dot imports record their binding so delta derivation can see
	// them; a named alias keeps its alias and a plain import keeps none.
	for name, want := range map[string]any{"embed": "_", "math": ".", "strings": "str"} {
		if got := aliases[name]; got != want {
			t.Errorf("go import %q alias = %#v, want %#v", name, got, want)
		}
	}
	if got := aliases["fmt"]; got != nil {
		t.Errorf(`go plain import "fmt" alias = %#v, want absent`, got)
	}
	for _, want := range []string{"fmt", "strings", "embed", "math"} {
		if !seen[want] {
			t.Errorf("go imports missing %q (blank and dot imports must be retained): %#v", want, items)
		}
	}
}
