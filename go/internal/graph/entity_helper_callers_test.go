// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEntityMergeHelpersHaveNoProductionCaller is the proof named by the
// anchor-census markers on the entity merge helpers in batch.go and entity.go
// (#7212). Those helpers take the node label from their caller and let the
// caller add property keys, so their label set is not bounded by the helper
// itself. They are a port of the original Python persistence layer and today
// only tests call them. The proof is that no non-test file outside this package
// calls them: a new production caller fails here, and the author must then bound
// the labels the caller passes (and add the proof) before the marker can stand.
func TestEntityMergeHelpersHaveNoProductionCaller(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go module root %s: %v", root, err)
	}
	call := regexp.MustCompile(`\b(graph\.)?(BatchMergeEntities|BuildEntityMergeStatement|MergeEntity)\(`)
	graphDir := filepath.Join(root, "internal", "graph") + string(filepath.Separator)
	var callers []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", "node_modules", ".gocache":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, graphDir) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if call.Match(raw) {
			rel, _ := filepath.Rel(root, path)
			callers = append(callers, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(callers) > 0 {
		t.Fatalf("entity merge helpers have production caller(s): %v", callers)
	}
}
