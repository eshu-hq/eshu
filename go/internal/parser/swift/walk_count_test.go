// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter_swift "github.com/indigo-net/Brf.it/pkg/parser/treesitter/grammars/swift"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestCollectSwiftSemanticFactsWalkCount pins the number of shared.WalkNamed
// full-tree traversals collectSwiftSemanticFacts performs on a single file,
// following the pattern established by internal/parser/php/walk_count_test.go
// (#4515 P2b). Before the walk-collapse fix (issue #4841, epic #4831),
// collectSwiftSemanticFacts ran three independent full-tree passes on top of
// the manual-recursion conformance/method collector: swiftHasImport
// (shared.WalkNamed), swiftVaporRouteReceivers (shared.WalkNamed), and an
// inline shared.WalkNamed scanning "value_argument" nodes for Vapor `use:`
// handlers. None of them consumes another's output while collecting, so they
// now run as extra cases inside the same manual-recursion walk
// (collectSwiftFileFacts) that already visits every node for conformances,
// dropping the shared.WalkNamed count for this stage from 3 to 0. Only the
// Vapor-gated route-entries pass (swiftVaporRouteEntries), which genuinely
// depends on the now-complete route-receiver map, still runs afterward — it
// is also manual recursion, so it contributes shared.WalkNamed calls only
// when it walks into a `.group(...)` closure (not exercised by this
// fixture) or resolves an inheritance clause (also not present here).
//
// The test targets collectSwiftSemanticFacts directly (not the full Parse)
// because the main swiftExtractor.walk also calls swiftInheritanceBases,
// which runs a bounded shared.WalkNamed over an inheritance_specifier
// subtree; using a fixture with no type inheritance keeps that path silent
// for this count too, so the number in this test isolates only the
// framework-evidence walks this issue targets.
//
// This test counts shared.WalkNamed itself (via
// shared.SetWalkNamedHookForTest), not a manually annotated call site, so it
// also fails if a future change reintroduces one of the collapsed walks.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestCollectSwiftSemanticFactsWalkCount(t *testing.T) {
	source := `import Vapor

func routes(_ app: Application) throws {
    app.get("health", use: health)
}

func health(req: Request) async throws -> String {
    "ok"
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "Routes.swift")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_swift.Language())); err != nil {
		t.Fatalf("SetLanguage(Swift) error = %v, want nil", err)
	}

	src, tree, err := swiftSourceAndTree(path, parser)
	if err != nil {
		t.Fatalf("swiftSourceAndTree() error = %v, want nil", err)
	}
	defer tree.Close()

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	facts := collectSwiftSemanticFacts(tree.RootNode(), src)
	if len(facts.vaporRouteEntries) != 1 {
		t.Fatalf("len(facts.vaporRouteEntries) = %d, want 1 (fixture has one Vapor route)", len(facts.vaporRouteEntries))
	}

	const wantWalkNamedCalls = 0
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (merged into manual-recursion pre-pass)", walkNamedCalls, wantWalkNamedCalls)
	}
}
