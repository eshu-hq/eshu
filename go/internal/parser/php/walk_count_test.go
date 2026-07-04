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

// TestParseFullTreeWalkCount pins the number of independent full-tree AST
// traversals Parse performs on a single file. Before the walk-collapse fix,
// Parse ran four full-tree passes: buildPHPParentLookup (parent-edge index),
// collectPHPDeclarations (phase 1), emitPHPVariablesAndCalls (phase 2), and
// buildPHPFrameworkSemantics -> phpSymfonyRoutes (route extraction). The route
// walk visits "attribute" nodes that phase 1 already visits for
// observePHPAttribute, so it collapses into phase 1. Phase 1 and phase 2
// cannot collapse into each other: phase 2 depends on whole-file type
// evidence (property types, return types, import aliases) that phase 1 only
// finishes collecting after the entire file is seen, per this package's
// AGENTS.md. This test fails if a future change reintroduces a fourth full
// walk; it does not by itself prove phase 1/phase 2 stayed separate (the
// byte-identity tests guard that), since an incorrect phase 1/phase 2 merge
// would still report a lower, "improved" count.
func TestParseFullTreeWalkCount(t *testing.T) {
	t.Parallel()

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

	var walkCount int
	restore := SetFullWalkHookForTest(func() { walkCount++ })
	defer restore()

	if _, err := Parse(path, false, shared.Options{}, parser); err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	const wantWalks = 3
	if walkCount != wantWalks {
		t.Fatalf("full-tree walk count = %d, want %d", walkCount, wantWalks)
	}
}
