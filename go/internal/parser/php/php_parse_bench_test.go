// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		b.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	for b.Loop() {
		if _, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{}); err != nil {
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

// BenchmarkParsePathPHPWithComposerLock parses a consumer file that imports 40
// classes from locked packages and calls each through `new` and a static
// method, under a composer.lock with 120 packages, the size of a Symfony
// application's lock (symfony/demo locks 121 psr-4 prefixes). It is the #7601
// regression gate for the package-key rules: the lock is decoded once per
// (path, stat), so the per-file cost is the directory walk and the prefix
// lookups. On a revision without the key rules the lock is simply ignored, so
// the same benchmark measures the before side.
func BenchmarkParsePathPHPWithComposerLock(b *testing.B) {
	repoRoot := b.TempDir()
	writeBenchFile(b, filepath.Join(repoRoot, "composer.json"), `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "src/"}}}`)
	writeBenchFile(b, filepath.Join(repoRoot, "composer.lock"), generateComposerLock(120))
	sourceDir := filepath.Join(repoRoot, "src", "Http")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(sourceDir, "Consumer.php")
	writeBenchFile(b, filePath, generateLockConsumerPHPSource(40))

	engine, err := parser.DefaultEngine()
	if err != nil {
		b.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	for b.Loop() {
		if _, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{}); err != nil {
			b.Fatalf("ParsePath() error = %v, want nil", err)
		}
	}
}

// generateComposerLock returns a composer.lock with packageCount packages,
// each autoloading one psr-4 prefix Vendor<i>\Package\.
func generateComposerLock(packageCount int) string {
	var b strings.Builder
	b.WriteString(`{"packages": [`)
	for i := range packageCount {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"name": "vendor%d/package", "version": "1.%d.0", "autoload": {"psr-4": {"Vendor%d\\Package\\": "src/"}}}`, i, i, i)
	}
	b.WriteString(`], "packages-dev": []}`)
	return b.String()
}

// generateLockConsumerPHPSource returns a class that imports importCount
// locked classes and calls each through `new` and a static method.
func generateLockConsumerPHPSource(importCount int) string {
	var b strings.Builder
	b.WriteString("<?php\nnamespace App\\Http;\n\n")
	for i := range importCount {
		fmt.Fprintf(&b, "use Vendor%d\\Package\\Service%d;\n", i, i)
	}
	b.WriteString("\nfinal class Consumer {\n    public function run(): void {\n")
	for i := range importCount {
		fmt.Fprintf(&b, "        $a%d = new Service%d();\n", i, i)
		fmt.Fprintf(&b, "        $b%d = Service%d::create($a%d);\n", i, i, i)
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

// BenchmarkParsePathPHPMemberCalls parses a consumer class that works the
// #8280 steps under a 120-package composer.lock: 40 methods, each writing one
// variable by `new` of a locked class and calling two methods on it, calling a
// property chain, and calling a `use function` import. Every method body is
// one variable scope, summarized once. Compare against the same benchmark on
// the base commit, where none of these calls is keyed.
func BenchmarkParsePathPHPMemberCalls(b *testing.B) {
	repoRoot := b.TempDir()
	writeBenchFile(b, filepath.Join(repoRoot, "composer.json"), `{"name": "acme/app", "autoload": {"psr-4": {"App\\": "src/"}}}`)
	writeBenchFile(b, filepath.Join(repoRoot, "composer.lock"), generateComposerLock(120))
	sourceDir := filepath.Join(repoRoot, "src", "Http")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	var source strings.Builder
	source.WriteString("<?php\nnamespace App\\Http;\n\n")
	for i := range 40 {
		fmt.Fprintf(&source, "use Vendor%d\\Package\\Service%d;\n", i, i)
		fmt.Fprintf(&source, "use function Vendor%d\\Package\\helper%d;\n", i, i)
	}
	source.WriteString("\nfinal class Consumer {\n")
	for i := range 40 {
		fmt.Fprintf(&source, "    public function run%d(): void {\n", i)
		fmt.Fprintf(&source, "        $service = new Service%d();\n", i)
		source.WriteString("        $service->start();\n        $service->stop();\n")
		source.WriteString("        $this->repository->find(1);\n")
		fmt.Fprintf(&source, "        helper%d($service->name);\n    }\n", i)
	}
	source.WriteString("}\n")
	filePath := filepath.Join(sourceDir, "Consumer.php")
	writeBenchFile(b, filePath, source.String())

	engine, err := parser.DefaultEngine()
	if err != nil {
		b.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	for b.Loop() {
		if _, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{}); err != nil {
			b.Fatalf("ParsePath() error = %v, want nil", err)
		}
	}
}
