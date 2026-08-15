// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/graphinstall"
)

// TestRunInstallNornicDBRejectsFullWithExplicitSource and
// TestRunInstallNornicDBPrintsJSON exercise runInstallNornicDB itself --
// cobra flag reading and the printed exit-code-path JSON -- so they stay
// here rather than moving to graphinstall with the rest of
// graph_install_test.go.

func TestRunInstallNornicDBRejectsFullWithExplicitSource(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("from", "/tmp/nornicdb-headless", "")
	cmd.Flags().String("sha256", "", "")
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().Bool("full", true, "")

	err := runInstallNornicDB(cmd, nil)
	if err == nil {
		t.Fatal("runInstallNornicDB() error = nil, want full/source conflict")
	}
	if !strings.Contains(err.Error(), "--full is reserved for future no-argument release installs") {
		t.Fatalf("runInstallNornicDB() error = %q, want actionable conflict", err.Error())
	}
}

func TestRunInstallNornicDBPrintsJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a Unix executable script")
	}

	t.Setenv("ESHU_HOME", t.TempDir())
	source := writeFakeNornicDBBinaryForCmdTest(t, "NornicDB v1.0.42\n")
	cmd := &cobra.Command{}
	cmd.Flags().String("from", source, "")
	cmd.Flags().String("sha256", "", "")
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().Bool("full", false, "")

	output := captureStdout(t, func() {
		if err := runInstallNornicDB(cmd, nil); err != nil {
			t.Fatalf("runInstallNornicDB() error = %v, want nil", err)
		}
	})

	var got graphinstall.Result
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("json.Unmarshal(output) error = %v, output=%q", err, output)
	}
	if got.Version != "v1.0.42" {
		t.Fatalf("Version = %q, want %q", got.Version, "v1.0.42")
	}
	if !got.Installed {
		t.Fatal("Installed = false for first install, want true")
	}
}

// writeFakeNornicDBBinaryForCmdTest and writeFakeNornicDBBinaryAtForCmdTest
// are a small, deliberate duplicate of graphinstall's test fixture helpers
// of the same shape (install_helpers_test.go). They write a fake NornicDB
// executable that prints versionOutput for `<path> version`; cmd/eshu cannot
// import graphinstall's unexported test helpers across the package
// boundary, and the two remaining cmd/eshu tests that need one are not worth
// a shared exported test-support package.
func writeFakeNornicDBBinaryForCmdTest(t *testing.T, versionOutput string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nornicdb-headless")
	writeFakeNornicDBBinaryAtForCmdTest(t, path, versionOutput)
	return path
}

func writeFakeNornicDBBinaryAtForCmdTest(t *testing.T, path, versionOutput string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '" + versionOutput + "'; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile(fake binary) error = %v, want nil", err)
	}
}
