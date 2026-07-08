// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

// Equivalence harness for epic #4831 child #4838 (collapsing the Java
// parser's redundant full-tree walks). Guarded by JAVA_PARSE_DUMP so it is
// inert in normal `go test` runs; only emits a manifest when a caller
// explicitly asks for one. Run before and after the walk-consolidation
// change and diff the two manifests to prove Parse() output is
// byte-identical across the corpus.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// javaEquivalenceVariants lists the Options combinations dumped for every
// corpus file. Each name must stay stable across before/after runs so the
// manifests line up for a diff.
func javaEquivalenceVariants() []struct {
	name    string
	options shared.Options
} {
	return []struct {
		name    string
		options shared.Options
	}{
		{"default", shared.Options{}},
		{"emit_dataflow", shared.Options{EmitDataflow: true}},
		{"index_source", shared.Options{IndexSource: true}},
	}
}

// TestDumpJavaParseCorpus writes a sorted `path\tvariant\tsha256` manifest of
// Parse() output for every *.java fixture under JAVA_PARSE_CORPUS (default
// tests/fixtures) to the file named by JAVA_PARSE_DUMP. It skips entirely
// when JAVA_PARSE_DUMP is unset, so it never runs in ordinary `go test ./...`
// or CI.
func TestDumpJavaParseCorpus(t *testing.T) {
	outPath := os.Getenv("JAVA_PARSE_DUMP")
	if outPath == "" {
		t.Skip("set JAVA_PARSE_DUMP=<path> to run the Java parse equivalence dump")
	}

	corpusRoot := os.Getenv("JAVA_PARSE_CORPUS")
	if corpusRoot == "" {
		corpusRoot = "../../../../tests/fixtures"
	}

	paths := javaEquivalenceCorpusPaths(t, corpusRoot)
	variants := javaEquivalenceVariants()

	lines := make([]string, 0, len(paths)*len(variants))
	for _, path := range paths {
		for _, variant := range variants {
			parser := tree_sitter.NewParser()
			if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_java.Language())); err != nil {
				t.Fatalf("SetLanguage(java): %v", err)
			}
			payload, err := Parse(path, false, variant.options, parser)
			parser.Close()
			if err != nil {
				t.Fatalf("Parse(%s, %s): %v", path, variant.name, err)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal(%s, %s): %v", path, variant.name, err)
			}
			sum := sha256.Sum256(encoded)
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", path, variant.name, hex.EncodeToString(sum[:])))
		}
	}
	sort.Strings(lines)

	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", outPath, err)
	}
}

func javaEquivalenceCorpusPaths(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".java" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk(%s): %v", root, err)
	}
	if len(paths) == 0 {
		t.Skipf("no java fixtures found under %q", root)
	}
	sort.Strings(paths)
	return paths
}
