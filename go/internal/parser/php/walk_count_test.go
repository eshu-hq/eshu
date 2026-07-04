// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// TestParseFullTreeWalkCount pins the number of shared.WalkNamed full-tree
// traversals Parse performs on a single file. Before the walk-collapse fix,
// Parse ran three shared.WalkNamed passes: collectPHPDeclarations (phase 1),
// emitPHPVariablesAndCalls (phase 2), and a dedicated route-attribute walk in
// buildPHPFrameworkSemantics -> phpSymfonyRoutes (route extraction). The route
// walk visited "attribute" nodes that phase 1 already visits for
// observePHPAttribute, so it collapses into phase 1. Phase 1 and phase 2
// cannot collapse into each other: phase 2 depends on whole-file type
// evidence (property types, return types, import aliases) that phase 1 only
// finishes collecting after the entire file is seen, per this package's
// AGENTS.md.
//
// This test counts shared.WalkNamed itself (via shared.SetWalkNamedHookForTest),
// not a manually annotated call site, so it also fails if a future change
// reintroduces the route walk (or any other full pass) as a plain
// shared.WalkNamed call anywhere in the PHP package without updating this
// count: the hook fires on every WalkNamed invocation regardless of who calls
// it. buildPHPParentLookup is a separate, stack-based traversal (not
// shared.WalkNamed, since it must index unnamed nodes too) and is therefore
// intentionally not counted here.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook, and a
// concurrently running test that also parses PHP (or any other
// shared.WalkNamed-based language) would pollute this count.
func TestParseFullTreeWalkCount(t *testing.T) {
	source := `<?php
namespace App\Http\Controllers;

use Symfony\Component\Routing\Attribute\Route;

final class ReportController {
    private Service $service;

    #[Route('/reports/{id}', methods: ['GET'], name: 'reports_show')]
    public function show(): string {
        return $this->service->render();
    }
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "ReportController.php")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())); err != nil {
		t.Fatalf("SetLanguage(PHP) error = %v, want nil", err)
	}

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	if _, err := Parse(path, false, shared.Options{}, parser); err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	const wantWalkNamedCalls = 2
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (phase 1 + phase 2)", walkNamedCalls, wantWalkNamedCalls)
	}
}
