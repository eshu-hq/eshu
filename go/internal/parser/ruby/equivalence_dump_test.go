// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

// Throwaway before/after equivalence harness for epic #4831 child #4842. Run
// only when RUBY_PARSE_DUMP is set; not part of the normal test suite.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
)

// TestDumpRubyParseCorpus parses every .rb fixture under RUBY_PARSE_CORPUS
// (default ../../../../tests/fixtures) with both Options{} and
// Options{IndexSource:true}, canonicalizes each payload to a
// recursively-key-sorted JSON document, and writes one
// "path\tvariant\tsha256" line per parse to the file named by
// RUBY_PARSE_DUMP, sorted by that line text. Comparing dumps taken before and
// after a change proves byte-identical parser output.
func TestDumpRubyParseCorpus(t *testing.T) {
	dumpPath := os.Getenv("RUBY_PARSE_DUMP")
	if dumpPath == "" {
		t.Skip("RUBY_PARSE_DUMP not set")
	}

	corpusRoot := os.Getenv("RUBY_PARSE_CORPUS")
	if corpusRoot == "" {
		corpusRoot = "../../../../tests/fixtures"
	}

	var paths []string
	err := filepath.Walk(corpusRoot, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".rb" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk(%q) error = %v, want nil", corpusRoot, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .rb fixtures found under %q", corpusRoot)
	}
	sort.Strings(paths)

	variants := []struct {
		name    string
		options shared.Options
	}{
		{name: "default", options: shared.Options{}},
		{name: "index_source", options: shared.Options{IndexSource: true}},
	}

	lines := make([]string, 0, len(paths)*len(variants))
	for _, path := range paths {
		for _, variant := range variants {
			parser := tree_sitter.NewParser()
			if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_ruby.Language())); err != nil {
				parser.Close()
				t.Fatalf("SetLanguage(ruby) error = %v, want nil", err)
			}
			payload, err := ParseWithParser(path, false, variant.options, parser)
			parser.Close()
			if err != nil {
				t.Fatalf("Parse(%q, variant=%q) error = %v, want nil", path, variant.name, err)
			}
			digest, err := canonicalPayloadDigest(payload)
			if err != nil {
				t.Fatalf("canonicalPayloadDigest(%q, variant=%q) error = %v, want nil", path, variant.name, err)
			}
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", path, variant.name, digest))
		}
	}
	sort.Strings(lines)

	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	if err := os.WriteFile(dumpPath, []byte(out), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", dumpPath, err)
	}
}

// canonicalPayloadDigest returns the sha256 hex digest of a payload's
// recursively key-sorted JSON encoding, so map key ordering never affects the
// comparison.
func canonicalPayloadDigest(payload map[string]any) (string, error) {
	canonical := canonicalizeValue(payload)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum), nil
}

// canonicalizeValue rebuilds maps as sorted key/value pairs (via
// json.Marshal's built-in map key sort, which already sorts string keys) and
// recurses into slices and nested maps so nested map key order never varies.
func canonicalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = canonicalizeValue(nested)
		}
		return out
	case []map[string]any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = canonicalizeValue(nested)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = canonicalizeValue(nested)
		}
		return out
	default:
		return value
	}
}
