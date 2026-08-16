// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteArtifactFormats proves --out honours both formats, rejects an
// unknown one, and writes owner-only files.
func TestWriteArtifactFormats(t *testing.T) {
	t.Parallel()
	artifact, err := ExecuteOnboard(okDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}
	dir := t.TempDir()

	for _, tc := range []struct{ format, wantPrefix string }{
		{"", "# Hosted onboarding"},
		{"md", "# Hosted onboarding"},
		{"markdown", "# Hosted onboarding"},
		{"MD", "# Hosted onboarding"},
		{"json", "{"},
		{"JSON", "{"},
	} {
		path := filepath.Join(dir, "artifact-"+tc.format)
		if err := WriteArtifact(artifact, tc.format, path); err != nil {
			t.Fatalf("WriteArtifact(%q) err = %v", tc.format, err)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %q artifact: %v", tc.format, readErr)
		}
		if !strings.HasPrefix(string(data), tc.wantPrefix) {
			t.Fatalf("format %q wrote %q..., want prefix %q", tc.format, string(data[:min(40, len(data))]), tc.wantPrefix)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %q artifact: %v", tc.format, statErr)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("format %q wrote mode %o, want 600", tc.format, perm)
		}
	}

	if err := WriteArtifact(artifact, "yaml", filepath.Join(dir, "bad")); err == nil {
		t.Fatal("WriteArtifact() err = nil, want an unsupported-format rejection")
	} else if !strings.Contains(err.Error(), "supported formats are md, json") {
		t.Fatalf("unsupported-format error = %q, want the supported list", err.Error())
	}
}

// TestWriteArtifactReportsPathErrors proves an unwritable destination surfaces
// as an error rather than a silently skipped artifact.
func TestWriteArtifactReportsPathErrors(t *testing.T) {
	t.Parallel()
	artifact := Artifact{Command: "hosted-onboard", Team: "payments"}
	path := filepath.Join(t.TempDir(), "missing-dir", "artifact.md")
	if err := WriteArtifact(artifact, "md", path); err == nil {
		t.Fatal("WriteArtifact() err = nil, want a write failure")
	}
}
