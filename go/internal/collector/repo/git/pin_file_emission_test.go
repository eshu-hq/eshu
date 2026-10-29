// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestSnapshotRepositoryEmitsARuntimeDeclarationForEveryAdmittedPinName drives
// the real native snapshotter and fact streamer over a repository holding one
// file per admitted version-pin name, each in its own package directory. Every
// name must reach the content hook and emit at least one
// runtime_declaration.version fact with its own source kind (#8969 review:
// .swift-version was admitted by the parser registry yet emitted nothing).
// The registry check keeps this list honest: each name must resolve to
// raw_text, so the test exercises the admission path, not a stand-in.
func TestSnapshotRepositoryEmitsARuntimeDeclarationForEveryAdmittedPinName(t *testing.T) {
	t.Parallel()

	pins := map[string]struct{ body, sourceKind, runtime string }{
		".nvmrc":              {"20.11.1\n", "nvmrc", "node"},
		".node-version":       {"20\n", "node_version", "node"},
		".python-version":     {"3.12.1\n", "python_version", "python"},
		".ruby-version":       {"3.2.2\n", "ruby_version", "ruby"},
		".java-version":       {"17\n", "java_version", "java"},
		".go-version":         {"1.22.1\n", "go_version", "go"},
		".terraform-version":  {"1.6.0\n", "terraform_version", "terraform"},
		".terragrunt-version": {"0.58.0\n", "terragrunt_version", "terragrunt"},
		".tool-versions":      {"nodejs 20.11.1\n", "tool_versions", "node"},
		".sdkmanrc":           {"java=17.0.2-tem\n", "sdkmanrc", "java"},
		"rust-toolchain":      {"1.75.0\n", "rust_toolchain", "rust"},
		"rust-toolchain.toml": {"[toolchain]\nchannel = \"1.78.0\"\n", "rust_toolchain", "rust"},
		".swift-version":      {"5.9\n", "swift_version", "swift"},
		".php-version":        {"8.3.12\n", "php_version", "php"},
	}

	registry := parser.DefaultRegistry()
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.go"), "package main\n")
	for name, pin := range pins {
		rel := "pkg" + name + "/" + name
		if definition, ok := registry.LookupByPath(rel); !ok || definition.ParserKey != "raw_text" {
			t.Fatalf("parser registry does not admit %q as raw_text (got %q, %v)", rel, definition.ParserKey, ok)
		}
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), pin.body)
	}

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	observed := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	repo := testCollectorRepositoryMetadata(resolvedRepoRoot)
	collected := buildStreamingGeneration(resolvedRepoRoot, repo, "run-8969", observed, snapshot, false, "")
	got := decodeRuntimeDeclarations(t, drainFactChannel(collected.Facts))

	seen := map[string]bool{}
	for _, v := range got {
		name := filepath.Base(v.RelativePath)
		pin, ok := pins[name]
		if !ok {
			t.Errorf("unexpected declaration from %q: %+v", v.RelativePath, v)
			continue
		}
		if v.SourceKind != pin.sourceKind || v.Runtime != pin.runtime || v.ScopeDir != "pkg"+name {
			t.Errorf("%s declaration = source_kind %q runtime %q scope %q, want %q %q %q",
				name, v.SourceKind, v.Runtime, v.ScopeDir, pin.sourceKind, pin.runtime, "pkg"+name)
		}
		seen[name] = true
	}
	for name := range pins {
		if !seen[name] {
			t.Errorf("admitted pin file %q emitted no runtime_declaration.version fact", name)
		}
	}
}
