// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestResolveNativeSnapshotFileSetCountsDroppedSymlinks proves an operator can
// see a symlink the target check dropped (#8809 review round 2). A dropped link
// is counted under the existing content skip stats as "symlink_target", which
// recordDiscoveryMetrics emits as skip_reason "content:symlink_target" and
// logDiscoveryStats emits as files_skipped.content.symlink_target. Admitted
// links and regular files must add nothing.
func TestResolveNativeSnapshotFileSetCountsDroppedSymlinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T, root string)
		wantDrops int
	}{
		{
			name: "link to a credential file is counted",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
			},
			wantDrops: 1,
		},
		{
			name: "previously indexed in-root config link is counted",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "vendor", "x", "app.conf"), "a=1\n")
				mustSymlink(t, "../vendor/x/app.conf", filepath.Join(root, "sub", "app.conf"))
			},
			wantDrops: 1,
		},
		{
			name: "two dropped links count twice",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, ".env"), "SECRET=not-a-real-secret\n")
				mustSymlink(t, ".env", filepath.Join(root, ".nvmrc"))
				mustSymlink(t, ".env", filepath.Join(root, ".node-version"))
			},
			wantDrops: 2,
		},
		{
			name: "admitted link and regular pin count nothing",
			setup: func(t *testing.T, root string) {
				writeCollectorTestFile(t, filepath.Join(root, "real", ".nvmrc"), "20.11.1\n")
				mustSymlink(t, "real/.nvmrc", filepath.Join(root, ".nvmrc"))
				writeCollectorTestFile(t, filepath.Join(root, ".python-version"), "3.12.2\n")
			},
			wantDrops: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
			tt.setup(t, root)
			resolved, err := filepath.EvalSymlinks(root)
			if err != nil {
				resolved = root
			}
			_, stats, err := resolveNativeSnapshotFileSet(
				resolved, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions())
			if err != nil {
				t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
			}
			if got := stats.FilesSkippedByContent[symlinkTargetSkipReason]; got != tt.wantDrops {
				t.Fatalf("FilesSkippedByContent[%q] = %d, want %d (stats %+v)",
					symlinkTargetSkipReason, got, tt.wantDrops, stats.FilesSkippedByContent)
			}
		})
	}
}
