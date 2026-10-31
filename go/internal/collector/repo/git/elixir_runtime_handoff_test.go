// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// TestElixirRuntimeNativeHandoff proves a committed mix.exs flows from the
// native Elixir parse through the private snapshot handoff to one guarded
// mix_exs_elixir declaration, with no parser transport left in file facts.
package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestElixirRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "mix.exs"), "defmodule Demo.MixProject do\n  use Mix.Project\n\n  def project do\n    [\n      app: :demo,\n      version: \"0.1.0\",\n      elixir: \"~> 1.17\",\n      deps: deps()\n    ]\n  end\nend\n")
	observed := time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ElixirRuntime) != 1 {
		t.Fatalf("Elixir handoffs = %d, want 1", len(snapshot.ElixirRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["elixir_runtime_evidence"]; ok {
			t.Fatal("private Elixir evidence persisted")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-elixir", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 1 || declarations[0].SourceKind != "mix_exs_elixir" || declarations[0].Runtime != "elixir" || declarations[0].RawSpec != "~> 1.17" || declarations[0].LineNumber != 8 {
		t.Fatalf("Elixir declarations = %+v", declarations)
	}
}
