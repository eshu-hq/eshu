// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeedCopiesCollectorAndManifest proves the seed dispatch materializes
// the seed payload: the caller binary under the satellite path name plus the
// embedded component manifest, so the seed image build COPYs exactly what
// the binary carries.
func TestSeedCopiesCollectorAndManifest(t *testing.T) {
	t.Parallel()

	self := filepath.Join(t.TempDir(), "collector-fixture")
	if err := os.WriteFile(self, []byte("fake-collector-binary"), 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	dest := t.TempDir()
	ref := "example.invalid/media-seed@sha256:" + strings.Repeat("0", 64)
	if err := runSeed(dest, ref, self); err != nil {
		t.Fatalf("runSeed: %v", err)
	}
	collector, err := os.ReadFile(filepath.Join(dest, "collector"))
	if err != nil {
		t.Fatalf("read dest collector: %v", err)
	}
	if string(collector) != "fake-collector-binary" {
		t.Fatalf("dest collector carries %q, want the fixture binary bytes", collector)
	}
	if info, err := os.Stat(filepath.Join(dest, "collector")); err != nil {
		t.Fatalf("stat dest collector: %v", err)
	} else if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("dest collector mode %o is not executable", info.Mode().Perm())
	}
	manifest, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read dest manifest: %v", err)
	}
	if len(manifest) == 0 {
		t.Fatal("dest manifest.yaml is empty")
	}
	if string(manifest) != string(embeddedManifest) {
		t.Fatal("dest manifest.yaml differs from the embedded manifest the binary carries")
	}
}

// TestSeedRejectsNonDigestRef proves the seed dispatch fails closed on any
// image reference that is not digest-pinned, and leaves no partial payload
// behind.
func TestSeedRejectsNonDigestRef(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"",
		"example.invalid/media-seed:latest",
		"example.invalid/media-seed",
		"example.invalid/media-seed@sha256:xyz",
		"example.invalid/media-seed@sha256:" + strings.Repeat("0", 63),
		"example.invalid/media-seed@sha256:" + strings.Repeat("0", 64) + "1",
		"@sha256:" + strings.Repeat("0", 64),
		"example.invalid/media-seed@sha512:" + strings.Repeat("0", 128),
	} {
		self := filepath.Join(t.TempDir(), "collector-fixture")
		if err := os.WriteFile(self, []byte("fake-collector-binary"), 0o755); err != nil {
			t.Fatalf("write fixture binary: %v", err)
		}
		dest := t.TempDir()
		if err := runSeed(dest, ref, self); err == nil {
			t.Fatalf("ref %q: got nil error, want a digest-pin failure", ref)
		} else if !strings.Contains(err.Error(), "--image-ref") {
			t.Fatalf("ref %q: error %q does not name --image-ref", ref, err)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 0 {
			t.Fatalf("ref %q: dest holds %d files, want none (no partial payload)", ref, len(entries))
		}
	}
}
