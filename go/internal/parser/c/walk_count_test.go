// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package c

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
)

// TestParseFullTreeWalkCount pins the number of shared.WalkNamed full-tree
// traversals Parse performs on a single file, following the pattern
// established by internal/parser/php/walk_count_test.go (#4515 P2b) and
// internal/parser/cpp/walk_count_test.go (#4924). Before the walk-collapse
// fix (issue #4870), annotateCDeadCodeRoots ran a second full-tree
// shared.WalkNamed pass over "call_expression" and "declaration" nodes --
// node kinds the main walk already visits -- purely to resolve them against
// payload["functions"], a map the main walk has already fully populated by
// the time the second walk starts. The fix gathers those candidate node
// pointers during the main walk (shared.CloneNode) and resolves them via an
// in-memory loop, eliminating the second traversal.
//
// This test counts shared.WalkNamed itself (via
// shared.SetWalkNamedHookForTest), not a manually annotated call site, so it
// also fails if a future change reintroduces a second full pass as a plain
// shared.WalkNamed call anywhere in the C package.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestParseFullTreeWalkCount(t *testing.T) {
	source := `#include <signal.h>

typedef void (*Handler)(int);

static void target(int x) {
    int local = x;
    (void)local;
}

static void caller(void) {
    signal(2, target);
    register_callback(target);
    Handler h = target;
    (void)h;
}

int main(void) {
    return 0;
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "walkcount.c")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_c.Language())); err != nil {
		t.Fatalf("SetLanguage(C) error = %v, want nil", err)
	}
	defer parser.Close()

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	functions, _ := payload["functions"].([]map[string]any)
	if len(functions) == 0 {
		t.Fatal("payload[\"functions\"] is empty; fixture must contain at least one function so the dead-code-roots pass is exercised")
	}

	const wantWalkNamedCalls = 7
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (main walk + bounded firstNamedDescendant subtree scans; dead-code-roots second full-tree walk eliminated by gather-then-resolve, issue #4870)", walkNamedCalls, wantWalkNamedCalls)
	}
}
