// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

// Guarded 0/0 equivalence harness for epic #4831 child #4869 (multi-walk
// consolidation). Not run by default: set CS_PARSE_DUMP to a destination file
// to produce a "path\tvariant\tsha256" line per corpus file per Options
// variant. Compare a baseline dump (captured before the walk-consolidation
// change) against a post-change dump with `diff`/`comm -3`; any difference
// means the consolidation changed output and must be reverted or fixed, never
// accepted.
//
// encoding/json already sorts map[string]any keys at every nesting level when
// marshaling, so json.Marshal(payload) is already canonical for our
// byte-identity check; no separate key-sorting pass is needed.

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
	tree_sitter_c_sharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
)

// csEquivalenceCorpus returns every ".cs" fixture under CS_PARSE_CORPUS
// (default "../../../../tests/fixtures"), sorted for deterministic output.
func csEquivalenceCorpus(tb testing.TB) []string {
	tb.Helper()
	root := os.Getenv("CS_PARSE_CORPUS")
	if root == "" {
		root = "../../../../tests/fixtures"
	}
	var paths []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".cs" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		tb.Fatalf("filepath.Walk(%q) error = %v, want nil", root, err)
	}
	if len(paths) == 0 {
		tb.Skipf("no c# corpus under %q", root)
	}
	sort.Strings(paths)
	return paths
}

// csEquivalenceVariants are the Options combinations the dump proves
// byte-identical output for: the default parse and the IndexSource-enabled
// parse (which additionally emits function source text).
var csEquivalenceVariants = []struct {
	name    string
	options shared.Options
}{
	{name: "default", options: shared.Options{}},
	{name: "index_source", options: shared.Options{IndexSource: true}},
}

// TestDumpCSParseCorpus is a guarded, non-default test: it only runs and
// writes a dump when CS_PARSE_DUMP names an output file. Use it to capture a
// baseline before a change and a comparison dump after, then diff the two
// files; a clean diff is the 0/0 equivalence proof for this package's
// walk-consolidation work.
func TestDumpCSParseCorpus(t *testing.T) {
	out := os.Getenv("CS_PARSE_DUMP")
	if out == "" {
		t.Skip("CS_PARSE_DUMP not set; skipping corpus dump")
	}

	paths := csEquivalenceCorpus(t)
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_c_sharp.Language())); err != nil {
		t.Fatalf("SetLanguage(c#) error = %v, want nil", err)
	}

	lines := make([]string, 0, len(paths)*len(csEquivalenceVariants))
	for _, path := range paths {
		for _, variant := range csEquivalenceVariants {
			payload, err := Parse(path, false, variant.options, parser)
			if err != nil {
				t.Fatalf("Parse(%q, variant=%q) error = %v, want nil", path, variant.name, err)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal(%q, variant=%q) error = %v, want nil", path, variant.name, err)
			}
			sum := sha256.Sum256(encoded)
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", path, variant.name, hex.EncodeToString(sum[:])))
		}
	}
	sort.Strings(lines)

	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(out, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", out, err)
	}
}
