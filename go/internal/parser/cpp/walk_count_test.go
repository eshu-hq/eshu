// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

// TestParseFullTreeWalkCount pins the number of shared.WalkNamed full-tree
// traversals Parse performs on a single file, following the pattern
// established by internal/parser/php/walk_count_test.go (#4515 P2b). Before
// the walk-collapse fix (issue #4841, epic #4831), buildCPPFrameworkSemantics
// ran a dedicated full-tree shared.WalkNamed pass over "call_expression"
// nodes, a node kind the main walk already visits (for appendCall) with no
// dependency on any other collector's output, so it now runs from that same
// case instead of a second traversal.
//
// annotateCPPDeadCodeRoots stays a separate pass: it reads payload["functions"]
// after the main walk has fully populated it, a genuine dependency the
// walk-collapse epic's caution calls out, so it is unaffected by this merge
// and both the main walk and the dead-code-roots walk remain present. Bounded
// per-node helpers (firstNamedDescendant, used by appendCPPFunction and
// annotateCPPMethodRuntimeRoots) also call shared.WalkNamed over small
// declarator subtrees, not the whole file, and are counted here too since
// they are unaffected by the merge and present identically before and after.
//
// This test counts shared.WalkNamed itself (via
// shared.SetWalkNamedHookForTest), not a manually annotated call site, so it
// also fails if a future change reintroduces the route walk (or any other
// full pass) as a plain shared.WalkNamed call anywhere in the C++ package.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestParseFullTreeWalkCount(t *testing.T) {
	source := `#include <crow.h>
#include <drogon/drogon.h>
#include <pistache/router.h>

void health() {}
void createOrder() {}

void registerRoutes() {
    CROW_ROUTE(app, "/health").methods("GET"_method)(health);
    drogon::app().registerHandler("/orders", createOrder, {drogon::Post});
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "routes.cpp")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_cpp.Language())); err != nil {
		t.Fatalf("SetLanguage(C++) error = %v, want nil", err)
	}

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	semantics, _ := payload["framework_semantics"].(map[string]any)
	if semantics == nil {
		t.Fatal("payload[\"framework_semantics\"] = nil, want non-nil (fixture has Crow and Drogon routes)")
	}

	const wantWalkNamedCalls = 14
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (main walk + bounded firstNamedDescendant/other subtree scans; dead-code-roots second walk eliminated by gather-then-resolve #4924)", walkNamedCalls, wantWalkNamedCalls)
	}
}
