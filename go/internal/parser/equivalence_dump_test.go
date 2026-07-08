// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDumpJSParseCorpus is a guarded 0/0-equivalence harness for issue #4868
// (JS/TS in-file multi-walk consolidation). It is inert unless JSTS_PARSE_DUMP
// is set: with it unset, the test is a no-op skip so it never runs in normal
// CI or `go test ./...` invocations.
//
// When JSTS_PARSE_DUMP=<out-file> is set, this test parses every .ts, .tsx,
// .js, and .jsx file under JSTS_PARSE_CORPUS (default "../../../tests/fixtures"
// relative to this package) through the real Engine.ParsePath entrypoint --
// the same dispatch every production caller uses, so runtimeLanguage and
// outputLanguage are resolved exactly as the collector resolves them -- under
// two Options variants (zero-value, and IndexSource:true), and writes one
// sorted "path\tvariant\tsha256" line per (file, variant) to <out-file>. The
// sha256 covers the deterministic (sorted-map-key) JSON encoding of the parser
// payload.
//
// Usage: capture a baseline before a change, capture again after, and diff
// the two files. An empty diff is the 0/0 equivalence proof required before
// any JS/TS parser walk consolidation lands.
func TestDumpJSParseCorpus(t *testing.T) {
	outPath := strings.TrimSpace(os.Getenv("JSTS_PARSE_DUMP"))
	if outPath == "" {
		t.Skip("JSTS_PARSE_DUMP not set; equivalence dump is opt-in only")
	}

	corpusRoot := strings.TrimSpace(os.Getenv("JSTS_PARSE_CORPUS"))
	if corpusRoot == "" {
		corpusRoot = "../../../tests/fixtures"
	}
	absCorpusRoot, err := filepath.Abs(corpusRoot)
	if err != nil {
		t.Fatalf("resolve corpus root %q: %v", corpusRoot, err)
	}

	paths := jsParseCorpusFiles(t, absCorpusRoot)
	if len(paths) == 0 {
		t.Fatalf("no .ts/.tsx/.js/.jsx files found under %q", absCorpusRoot)
	}

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	variants := []struct {
		name    string
		options Options
	}{
		{name: "default", options: Options{}},
		{name: "index_source", options: Options{IndexSource: true}},
	}

	lines := make([]string, 0, len(paths)*len(variants))
	for _, path := range paths {
		relPath, err := filepath.Rel(absCorpusRoot, path)
		if err != nil {
			t.Fatalf("relativize %q: %v", path, err)
		}
		for _, variant := range variants {
			payload, err := engine.ParsePath(absCorpusRoot, path, false, variant.options)
			if err != nil {
				t.Fatalf("ParsePath(%q, variant=%s) error = %v, want nil", path, variant.name, err)
			}
			digest, err := jsParsePayloadDigest(payload)
			if err != nil {
				t.Fatalf("digest ParsePath(%q, variant=%s) error = %v, want nil", path, variant.name, err)
			}
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", filepath.ToSlash(relPath), variant.name, digest))
		}
	}

	sort.Strings(lines)
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(outPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", outPath, err)
	}
}

// jsParseCorpusFiles returns every .ts, .tsx, .js, and .jsx file under root,
// sorted for deterministic dump ordering.
func jsParseCorpusFiles(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".ts", ".tsx", ".js", ".jsx":
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus root %q: %v", root, err)
	}
	sort.Strings(paths)
	return paths
}

// jsParsePayloadDigest returns the hex sha256 of payload's JSON encoding.
// encoding/json sorts map[string]any keys alphabetically at every nesting
// level, so this digest is stable across runs for byte-identical payloads.
func jsParsePayloadDigest(payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
