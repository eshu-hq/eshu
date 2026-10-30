// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRustRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"demo\"\nversion = \"0.1.0\"\nrust-version = \"1.78.0\"\n[dependencies]\nserde = \"1\"\n")
	observed := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.RustRuntime) != 1 {
		t.Fatalf("Rust handoffs = %d, want 1", len(snapshot.RustRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["rust_runtime_evidence"]; ok {
			t.Fatal("private Rust evidence persisted")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-rust", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 1 || declarations[0].SourceKind != "cargo_package_rust_version" || declarations[0].Runtime != "rust" || declarations[0].RawSpec != "1.78.0" || declarations[0].LineNumber != 4 {
		t.Fatalf("Rust declarations = %+v", declarations)
	}
}
