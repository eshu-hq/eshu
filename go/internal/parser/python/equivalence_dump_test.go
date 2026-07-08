// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// TestDumpPyParseCorpus is a guarded prove-theory-first equivalence harness for
// issue #4867 (consolidating the Python parser's multi-walk into fewer AST
// passes). It is not a normal test: it only runs when PY_PARSE_DUMP is set, and
// its job is to write a stable, sorted "path\tvariant\tsha256" line per corpus
// file so the same dump can be captured before and after a walk-consolidation
// change and diffed for byte-identical output (0/0 symmetric-diff proof).
//
// Run with:
//
//	PY_PARSE_DUMP=/tmp/out.txt go test ./internal/parser/python -run TestDumpPyParseCorpus -v
//
// Optionally override the corpus root with PY_PARSE_CORPUS (defaults to the
// shared tests/fixtures tree also used by the throwaway walk-count benchmark).
func TestDumpPyParseCorpus(t *testing.T) {
	outPath := os.Getenv("PY_PARSE_DUMP")
	if outPath == "" {
		t.Skip("PY_PARSE_DUMP not set; skipping equivalence dump")
	}

	corpusRoot := os.Getenv("PY_PARSE_CORPUS")
	if corpusRoot == "" {
		corpusRoot = "../../../../tests/fixtures"
	}
	absCorpusRoot, err := filepath.Abs(corpusRoot)
	if err != nil {
		t.Fatalf("resolve corpus root %q: %v", corpusRoot, err)
	}

	var paths []string
	walkErr := filepath.Walk(absCorpusRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".py") {
			paths = append(paths, p)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk corpus root %q: %v", absCorpusRoot, walkErr)
	}
	if len(paths) == 0 {
		t.Skipf("no python corpus files under %q", absCorpusRoot)
	}
	sort.Strings(paths)

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatalf("SetLanguage(python): %v", err)
	}

	variants := []struct {
		name    string
		options shared.Options
	}{
		{name: "default", options: shared.Options{}},
		{name: "index_source", options: shared.Options{IndexSource: true}},
	}

	lines := make([]string, 0, len(paths)*len(variants))
	for _, path := range paths {
		relPath, err := filepath.Rel(absCorpusRoot, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)
		for _, variant := range variants {
			payload, err := Parse(absCorpusRoot, path, false, variant.options, parser)
			if err != nil {
				t.Fatalf("Parse(%q, variant=%s): %v", path, variant.name, err)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal(%q, variant=%s): %v", path, variant.name, err)
			}
			sum := sha256.Sum256(encoded)
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", relPath, variant.name, hex.EncodeToString(sum[:])))
		}
	}
	sort.Strings(lines)

	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		t.Fatalf("mkdir for %q: %v", outPath, err)
	}
	file, err := os.Create(outPath) // #nosec G304 -- test-only dump path controlled by PY_PARSE_DUMP env var
	if err != nil {
		t.Fatalf("create %q: %v", outPath, err)
	}
	defer func() {
		_ = file.Close()
	}()
	writer := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			t.Fatalf("write %q: %v", outPath, err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush %q: %v", outPath, err)
	}
}
