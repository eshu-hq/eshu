// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestDumpGroovyParseCorpus is a manual, opt-in 0/0 differential harness for
// issue #4845. It is NOT a CI gate: standing protection for this package is
// the package test suite plus the B-12 golden snapshot. Set GROOVY_PARSE_DUMP
// to run it; it walks GROOVY_PARSE_CORPUS (default ../../../../tests/fixtures)
// for *.groovy, *.gradle, and Jenkinsfile* files, parses each with
// shared.Options{IndexSource: true}, and prints one canonical
// "path\tvariant\tsha256" line per file to stdout in sorted order. Run once
// before a change and once after; an empty diff is the accuracy proof.
func TestDumpGroovyParseCorpus(t *testing.T) {
	if os.Getenv("GROOVY_PARSE_DUMP") == "" {
		t.Skip("set GROOVY_PARSE_DUMP=1 to run the manual 0/0 differential dump")
	}

	root := os.Getenv("GROOVY_PARSE_CORPUS")
	if root == "" {
		root = "../../../../tests/fixtures"
	}

	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasSuffix(name, ".groovy") || strings.HasSuffix(name, ".gradle") || strings.HasPrefix(name, "jenkinsfile") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus %s: %v", root, err)
	}
	sort.Strings(paths)

	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		payload, parseErr := Parse(path, false, shared.Options{IndexSource: true})
		if parseErr != nil {
			t.Fatalf("Parse(%s): %v", path, parseErr)
		}
		canonical, marshalErr := canonicalJSON(payload)
		if marshalErr != nil {
			t.Fatalf("canonicalJSON(%s): %v", path, marshalErr)
		}
		sum := sha256.Sum256(canonical)
		lines = append(lines, fmt.Sprintf("%s\tparse\t%x", path, sum))
	}
	sort.Strings(lines)

	for _, line := range lines {
		fmt.Println(line)
	}
}

// canonicalJSON marshals v with sorted map keys via json.Marshal (Go's
// encoding/json already sorts map[string]any keys) so the output hash is
// stable across runs regardless of map iteration order.
func canonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
