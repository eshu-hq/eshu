// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"path/filepath"
	"syscall"
	"testing"
)

// TestResolveNativeSnapshotFileSetDropsSymlinkToFIFO proves a link to a
// non-regular file is dropped even when the FIFO's name falls in the link's own
// admission category (a `.conf` name shares the pin files' parser). Admitting
// it would block the content read on an unwritten pipe.
func TestResolveNativeSnapshotFileSetDropsSymlinkToFIFO(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.conf"), 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	mustSymlink(t, "pipe.conf", filepath.Join(root, ".nvmrc"))
	got := discoverSymlinkFixture(t, root)
	if containsPath(got, ".nvmrc") {
		t.Fatalf("link to FIFO admitted; discovered %v", got)
	}
}
