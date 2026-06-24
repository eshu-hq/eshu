// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
)

func TestSnapshotParserOptionsUseModuleVariablesForJava(t *testing.T) {
	t.Parallel()

	got := snapshotParserOptions(filepath.Join("src", "main", "java", "Demo.java"), nil, false, "repo-alpha")
	if got.VariableScope != "module" {
		t.Fatalf("VariableScope = %q, want module for Java", got.VariableScope)
	}
	if !got.IndexSource {
		t.Fatal("IndexSource = false, want true")
	}
}

func TestSnapshotParserOptionsKeepAllVariablesForDynamicLanguages(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		filepath.Join("src", "handler.ts"),
		filepath.Join("src", "worker.js"),
		filepath.Join("src", "tasks.py"),
	} {
		got := snapshotParserOptions(path, nil, false, "repo-alpha")
		if got.VariableScope != "all" {
			t.Fatalf("%s VariableScope = %q, want all", path, got.VariableScope)
		}
	}
}

// TestSnapshotParserOptionsThreadsEmitDataflow proves the emit-dataflow gate is
// threaded into the parser options and is off by default.
func TestSnapshotParserOptionsThreadsEmitDataflow(t *testing.T) {
	t.Parallel()

	if got := snapshotParserOptions(filepath.Join("src", "handler.go"), nil, false, "repo-alpha"); got.EmitDataflow {
		t.Fatal("EmitDataflow = true, want false when gate off")
	}
	if got := snapshotParserOptions(filepath.Join("src", "handler.go"), nil, true, "repo-alpha"); !got.EmitDataflow {
		t.Fatal("EmitDataflow = false, want true when gate on")
	}
}

// TestSnapshotParserOptionsThreadsRepositoryID proves collector-owned stable
// repository identity reaches value-flow-capable parsers without deriving it
// from local paths or commit generations.
func TestSnapshotParserOptionsThreadsRepositoryID(t *testing.T) {
	t.Parallel()

	got := snapshotParserOptions(filepath.Join("src", "handler.go"), nil, true, "repo-alpha")
	if got.RepositoryID != "repo-alpha" {
		t.Fatalf("RepositoryID = %q, want repo-alpha", got.RepositoryID)
	}
}

// TestLoadEmitDataflowGate proves the ESHU_EMIT_DATAFLOW contract is
// affirmative-only and off by default.
func TestLoadEmitDataflowGate(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "on": true, " On ": true,
		"": false, "0": false, "false": false, "off": false, "no": false, "maybe": false,
	}
	for raw, want := range cases {
		got := LoadEmitDataflowGate(func(key string) string {
			if key != "ESHU_EMIT_DATAFLOW" {
				t.Fatalf("unexpected env key %q", key)
			}
			return raw
		})
		if got != want {
			t.Fatalf("LoadEmitDataflowGate(%q) = %v, want %v", raw, got, want)
		}
	}
}
