// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReducerTreeDirsCoversFamilySubpackages guards the scan that decides
// whether a registry fact kind has any consumer at all.
//
// go/internal/reducer used to be one package, and the scan globbed
// `<dir>/*.go`. Issue #6061 moves the reducer's domain families into
// subpackages, so a dispatch that relocates to a directory like
// reducer/obscoverage leaves the glob's reach while remaining reducer code.
// TestEveryRegistryKindHasConsumerOrDisclosure caught that once, but only
// because the moved kind had no second route; a kind that is also read from
// the query layer would let the scan silently narrow instead.
//
// The assertion binds to the directory that actually holds the observability
// coverage writer rather than to a directory name, so renaming the family does
// not quietly turn this into a test of nothing.
func TestReducerTreeDirsCoversFamilySubpackages(t *testing.T) {
	repoRoot := kindConsumerGateRepoRoot(t)

	reducerRoot := filepath.Join(repoRoot, "go/internal/reducer")
	const writer = "observability_coverage_correlation_writer.go"
	writerDir := ""
	err := filepath.Walk(reducerRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == writer {
			writerDir = filepath.Dir(path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", reducerRoot, err)
	}
	if writerDir == "" {
		t.Fatalf("did not find %s under %s; this test can no longer prove anything", writer, reducerRoot)
	}
	if writerDir == reducerRoot {
		t.Skipf("%s still lives in the reducer root, so subpackage coverage is not yet exercised", writer)
	}

	dirs, err := reducerTreeDirs(repoRoot)
	if err != nil {
		t.Fatalf("reducerTreeDirs: %v", err)
	}
	if len(dirs) < 2 {
		t.Fatalf("reducerTreeDirs returned %d dir(s) (%v); the scan is no longer seeing family subpackages", len(dirs), dirs)
	}

	found := false
	for _, dir := range dirs {
		if dir == writerDir {
			found = true
		}
		if strings.Contains(dir, string(filepath.Separator)+"testdata") {
			t.Errorf("testdata directory reached the consumer scan: %s", dir)
		}
	}
	if !found {
		t.Fatalf("reducerTreeDirs omitted %s, so a consumer living there reads as absent; got %d dirs", writerDir, len(dirs))
	}
}
