// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveNornicDBBinaryPrefersManagedInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	originalLookPath := localGraphLookPath
	t.Cleanup(func() {
		localGraphLookPath = originalLookPath
	})

	homeDir := t.TempDir()
	t.Setenv("ESHU_HOME", homeDir)
	t.Setenv("ESHU_NORNICDB_BINARY", "")
	managedPath := filepath.Join(homeDir, "bin", "nornicdb-headless")
	writeFakeNornicDBBinaryAt(t, managedPath, "NornicDB v1.0.43\n")
	localGraphLookPath = func(file string) (string, error) {
		t.Fatalf("localGraphLookPath(%q) called; managed install should win", file)
		return "", nil
	}

	got, err := ResolveGraphBinary()
	if err != nil {
		t.Fatalf("ResolveGraphBinary() error = %v, want nil", err)
	}
	if got != managedPath {
		t.Fatalf("ResolveGraphBinary() = %q, want %q", got, managedPath)
	}
}

// writeFakeNornicDBBinaryAt writes a fake NornicDB executable that prints
// versionOutput for `<path> version`. It is a deliberate small duplicate of
// graphinstall's and cmd/eshu's fixture helpers of the same shape: unexported
// test helpers do not cross a package boundary, and one test here is not worth
// a shared exported test-support package.
func writeFakeNornicDBBinaryAt(t *testing.T, path, versionOutput string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '" + versionOutput + "'; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { // #nosec G306 -- a test fixture must be executable
		t.Fatalf("os.WriteFile(fake binary) error = %v, want nil", err)
	}
}
