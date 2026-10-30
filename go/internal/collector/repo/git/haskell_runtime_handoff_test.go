// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHaskellRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "stack.yaml"), "packages:\n  - .\nresolver: lts-22.43\nextra-deps: []\n")
	observed := time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.HaskellRuntime) != 1 {
		t.Fatalf("Haskell handoffs = %d, want 1", len(snapshot.HaskellRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["haskell_runtime_evidence"]; ok {
			t.Fatal("private Haskell evidence persisted")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-haskell", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 1 || declarations[0].SourceKind != "stack_yaml_resolver" || declarations[0].Runtime != "haskell" || declarations[0].RawSpec != "lts-22.43" || declarations[0].LineNumber != 3 {
		t.Fatalf("Haskell declarations = %+v", declarations)
	}
}
