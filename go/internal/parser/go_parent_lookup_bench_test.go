package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkParsePathGoIdentifierHeavy parses a synthetic Go file with many
// identifiers in value positions (function arguments, struct literals) and
// deeply nested call expressions. The Go dead-code helpers walk node.Parent()
// per identifier; before the per-parse goParentLookup landed, that pattern
// scaled as O(n_identifiers * depth^2) per file and saturated CPU on
// repo-scale corpora without committing facts (see #161). This benchmark is
// the focused regression gate that proves the parse path stays bounded.
func BenchmarkParsePathGoIdentifierHeavy(b *testing.B) {
	repoRoot := b.TempDir()
	filePath := filepath.Join(repoRoot, "heavy.go")
	writeBenchFile(b, filePath, generateIdentifierHeavyGoSource(80, 24))

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

// generateIdentifierHeavyGoSource produces Go source with functions that each
// contain a nested chain of call expressions and a composite-literal payload
// referencing many identifiers. The shape exercises the helpers that walked
// node.Parent() per identifier (function_value_references,
// function_literal_reachability, dead_code_semantic_helpers).
func generateIdentifierHeavyGoSource(functionCount, chainDepth int) string {
	var b strings.Builder
	b.WriteString("package heavy\n\n")
	b.WriteString("type handler func(string) bool\n\n")
	b.WriteString("type registry struct {\n")
	b.WriteString("\thandlers map[string]handler\n")
	b.WriteString("\tnested   *registry\n")
	b.WriteString("}\n\n")
	for i := range functionCount {
		fmt.Fprintf(&b, "func target%d(s string) bool { return s != \"\" }\n", i)
	}
	b.WriteString("\n")
	for i := range functionCount {
		fmt.Fprintf(&b, "func consume%d(r *registry) bool {\n", i)
		b.WriteString("\treturn ")
		for d := range chainDepth {
			fmt.Fprintf(&b, "r.handlers[\"k%d\"](\"v\") && ", d)
		}
		fmt.Fprintf(&b, "target%d(\"end\")\n", i)
		b.WriteString("}\n\n")
	}
	b.WriteString("func registerAll() *registry {\n")
	b.WriteString("\treturn &registry{handlers: map[string]handler{\n")
	for i := range functionCount {
		fmt.Fprintf(&b, "\t\t\"target%d\": target%d,\n", i, i)
	}
	b.WriteString("\t}}\n")
	b.WriteString("}\n")
	return b.String()
}

func writeBenchFile(b *testing.B, path, contents string) {
	b.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		b.Fatalf("write %s: %v", path, err)
	}
}
