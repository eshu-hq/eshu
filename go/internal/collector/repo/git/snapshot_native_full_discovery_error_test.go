// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnapshotRepositoryReturnsFullDiscoveryErrorWithFileTargets proves a
// failed full discovery is reported even when FileTargets is set. The delta
// call used to overwrite the error from the full walk, so the caller saw a
// later, unrelated failure (or none) instead of the cause (#8809).
func TestSnapshotRepositoryReturnsFullDiscoveryErrorWithFileTargets(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "missing")
	_, err := NativeRepositorySnapshotter{}.SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath:    root,
		FileTargets: []string{filepath.Join(root, "main.go")},
	})
	if err == nil {
		t.Fatal("SnapshotRepository() error = nil, want the full discovery error")
	}
	if !strings.Contains(err.Error(), "resolve repository file sets") {
		t.Fatalf("SnapshotRepository() error = %v, want it to carry the full discovery error", err)
	}
}
