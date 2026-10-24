// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestSnapshotRepositoryEmitsVersionPinContentFacts drives the real native
// snapshotter and the real fact streamer over a repository holding a version
// pin next to credential-bearing dotfiles (#8809). The pin must reach the
// content store write path with its verbatim body and the raw_text language;
// the credential-bearing files must produce no content fact at all.
func TestSnapshotRepositoryEmitsVersionPinContentFacts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.go"), "package main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".nvmrc"), "20.11.1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.78.0\"\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".npmrc"), "//registry.example.test/:_authToken=not-a-real-token\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".env"), "SECRET=not-a-real-secret\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time {
		return time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	}}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	collected := buildStreamingGeneration(
		resolvedRepoRoot,
		testCollectorRepositoryMetadata(resolvedRepoRoot),
		"run-8809",
		time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC),
		snapshot,
		false,
		"",
	)
	gotBodies := map[string]string{}
	gotLanguages := map[string]string{}
	for _, envelope := range drainFactChannel(collected.Facts) {
		if envelope.FactKind != "content" {
			continue
		}
		path, _ := envelope.Payload["content_path"].(string)
		body, _ := envelope.Payload["content_body"].(string)
		language, _ := envelope.Payload["language"].(string)
		gotBodies[path] = body
		gotLanguages[path] = language
	}

	if got, want := gotBodies[".nvmrc"], "20.11.1\n"; got != want {
		t.Fatalf(".nvmrc content_body = %q, want %q; all=%v", got, want, gotBodies)
	}
	if got, want := gotLanguages[".nvmrc"], "raw_text"; got != want {
		t.Fatalf(".nvmrc language = %q, want %q", got, want)
	}
	if _, ok := gotBodies["rust-toolchain.toml"]; !ok {
		t.Fatalf("rust-toolchain.toml missing from content facts; all=%v", gotBodies)
	}
	// .npmrc is admitted under the metadata policy (#8978): a content fact
	// with an empty body, never the credential line.
	if body, ok := gotBodies[".npmrc"]; !ok || body != "" {
		t.Fatalf(".npmrc content_body = %q (present=%v), want a present empty-body row", body, ok)
	}
	if _, ok := gotBodies[".env"]; ok {
		t.Fatal(".env produced a content fact; credential-bearing dotfiles must stay out")
	}
}
