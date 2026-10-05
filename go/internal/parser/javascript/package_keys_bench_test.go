// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// BenchmarkParsePathTypeScriptPackageImportCalls parses one consumer file that
// binds many names from bare packages and calls each of them several times,
// the input the #7601 binding step works hardest on: every call is a keying
// candidate, so the redeclared-name walk always runs. Compare against the same
// benchmark on the base commit to read the step's cost.
func BenchmarkParsePathTypeScriptPackageImportCalls(b *testing.B) {
	repoRoot := b.TempDir()
	writeBenchFile(b, filepath.Join(repoRoot, "package.json"), `{"name": "bench-app"}`)
	filePath := filepath.Join(repoRoot, "consumer.ts")
	writeBenchFile(b, filePath, generatePackageImportConsumerSource(200, 10))

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

// generatePackageImportConsumerSource writes importCount named imports from
// bare packages and, inside exported functions, callsPerImport plain calls
// plus one namespace-member call per import.
func generatePackageImportConsumerSource(importCount, callsPerImport int) string {
	var source strings.Builder
	for i := range importCount {
		fmt.Fprintf(&source, "import { helper%d } from \"@bench/pkg%d\";\n", i, i%20)
		fmt.Fprintf(&source, "import * as ns%d from \"@bench/pkg%d\";\n", i, i%20)
	}
	for i := range importCount {
		fmt.Fprintf(&source, "\nexport function caller%d(value: number) {\n", i)
		for range callsPerImport {
			fmt.Fprintf(&source, "  helper%d(value);\n", i)
		}
		fmt.Fprintf(&source, "  return ns%d.member%d(value);\n}\n", i, i)
	}
	return source.String()
}

// BenchmarkParsePathJavaScriptCorpus parses every JavaScript and TypeScript
// file under ESHU_JSTS_BENCH_CORPUS once per iteration, with each first-level
// directory treated as one repository root. It is skipped when the variable is
// unset, so it runs only where a real corpus is checked out.
func BenchmarkParsePathJavaScriptCorpus(b *testing.B) {
	corpusRoot := strings.TrimSpace(os.Getenv("ESHU_JSTS_BENCH_CORPUS"))
	if corpusRoot == "" {
		b.Skip("ESHU_JSTS_BENCH_CORPUS not set")
	}
	type corpusFile struct{ repoRoot, path string }
	var files []corpusFile
	repos, err := os.ReadDir(corpusRoot)
	if err != nil {
		b.Fatalf("read corpus %q: %v", corpusRoot, err)
	}
	for _, repo := range repos {
		// A corpus of symlinks to checked-out repositories is the common
		// setup; WalkDir does not descend through a symlinked root.
		repoRoot, err := filepath.EvalSymlinks(filepath.Join(corpusRoot, repo.Name()))
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", "dist", "build", "coverage", ".git", ".next", "out", "vendor":
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
				if !strings.HasSuffix(path, ".min.js") {
					files = append(files, corpusFile{repoRoot: repoRoot, path: path})
				}
			}
			return nil
		})
	}
	if len(files) == 0 {
		b.Fatalf("no JavaScript or TypeScript files under %q", corpusRoot)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		b.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	b.ReportMetric(float64(len(files)), "files/op")
	for b.Loop() {
		for _, file := range files {
			// A file the parser rejects is skipped the same way on both
			// sides of a before/after comparison.
			_, _ = engine.ParsePath(file.repoRoot, file.path, false, parser.Options{})
		}
	}
}
