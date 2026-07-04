// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkParsePathPHPRouteHeavy parses a synthetic PHP controller with many
// classes, methods, and Symfony Route attributes. Before the walk-collapse fix
// (#4515 P2b), Parse ran four independent full-tree traversals per file:
// buildPHPParentLookup, collectPHPDeclarations (phase 1),
// emitPHPVariablesAndCalls (phase 2), and a dedicated route-attribute
// shared.WalkNamed walk in buildPHPFrameworkSemantics. The route walk visited
// "attribute" nodes that phase 1 already visits for observePHPAttribute, so it
// folded into phase 1, dropping the shared.WalkNamed call count from 3 to 2
// per file (buildPHPParentLookup is a separate stack-based traversal, not
// shared.WalkNamed, and is unaffected). TestParseFullTreeWalkCount in
// internal/parser/php pins that shared.WalkNamed==2 invariant directly against
// the traversal primitive. This benchmark is the regression gate that proves
// the parse path stays bounded on route-heavy PHP, the shape that exercised
// the now-removed walk most.
func BenchmarkParsePathPHPRouteHeavy(b *testing.B) {
	repoRoot := b.TempDir()
	filePath := filepath.Join(repoRoot, "heavy.php")
	writeBenchFile(b, filePath, generateRouteHeavyPHPSource(60))

	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for b.Loop() {
		if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
			b.Fatalf("ParsePath() error = %v, want nil", err)
		}
	}
}

// generateRouteHeavyPHPSource produces a PHP file with methodCount controller
// actions, each carrying a Symfony Route attribute, a typed property
// reference, and a chained method call, exercising phase 1 (declarations,
// imports, route attributes), phase 2 (variables and calls), and the parent
// lookup in one representative shape.
func generateRouteHeavyPHPSource(methodCount int) string {
	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString("namespace App\\Http\\Controllers;\n\n")
	b.WriteString("use Symfony\\Component\\Routing\\Attribute\\Route;\n\n")
	b.WriteString("final class HeavyController {\n")
	b.WriteString("    private Service $service;\n\n")
	for i := range methodCount {
		fmt.Fprintf(&b, "    #[Route('/reports/%d/{id}', methods: ['GET'], name: 'reports_show_%d')]\n", i, i)
		fmt.Fprintf(&b, "    public function show%d(int $id): string {\n", i)
		fmt.Fprintf(&b, "        $result = $this->service->render(%d, $id);\n", i)
		b.WriteString("        return $result;\n")
		b.WriteString("    }\n\n")
	}
	b.WriteString("}\n")
	return b.String()
}
