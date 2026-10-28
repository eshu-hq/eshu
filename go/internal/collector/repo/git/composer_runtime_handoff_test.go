// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestComposerRuntimeHandoff proves parser-owned evidence reaches the fact
// stream while private transport values stay out of persisted file facts.
func TestComposerRuntimeHandoff(t *testing.T) {
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "composer.json"), "{\n  \"require\": {\"php\": \"7.4\"},\n  \"require\": {\"php\": \"^8.2\"},\n  \"config\": {\"platform\": {\"php\": \"8.3.1\"}}\n}")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "composer.lock"), "{\n  \"platform\": {\"php\": \">=8.2\"},\n  \"platform-overrides\": {\"php\": \"8.3.1\"}\n}")
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ComposerRuntime) != 2 {
		t.Fatalf("composer handoffs = %d", len(snapshot.ComposerRuntime))
	}
	for _, data := range snapshot.FileData {
		if _, found := data["composer_runtime_evidence"]; found {
			t.Fatal("private handoff remained in FileData")
		}
	}
	repo := testCollectorRepositoryMetadata(repoRoot)
	collected := buildStreamingGeneration(repoRoot, repo, "run-composer", observed, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	declarations := decodeDeclarationList(t, envelopes)
	if len(declarations) != 4 {
		t.Fatalf("runtime declarations = %d, want 4: %+v", len(declarations), declarations)
	}
	for _, d := range declarations {
		if d.RawSpec == "7.4" || d.LineNumber < 2 {
			t.Fatalf("stale value or line: %+v", d)
		}
	}
	for _, env := range envelopes {
		if strings.Contains(env.FactKind, "file") && strings.Contains(strings.TrimSpace(env.StableFactKey), "composer_runtime_evidence") {
			t.Fatalf("private handoff reached file fact: %+v", env)
		}
	}
}

// TestComposerContentVersionFixtureDeclarations pins all PHP source kinds in
// the same discovered fixture that seals the collector content epoch.
func TestComposerContentVersionFixtureDeclarations(t *testing.T) {
	repoRoot := t.TempDir()
	copyContentVersionFixture(t, filepath.Join(contentVersionFixtureRoot, "php-app"), repoRoot)
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-composer-fixture", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	want := map[string]struct {
		path, raw, kind string
		line            int
	}{
		"php_version":                          {".php-version", "8.3.12", "exact", 1},
		"composer_json_require_php":            {"composer.json", "^8.2", "range", 3},
		"composer_json_platform_php":           {"composer.json", "8.3.12", "exact", 4},
		"composer_lock_platform_php":           {"composer.lock", ">=8.1", "range", 8},
		"composer_lock_platform_overrides_php": {"composer.lock", "8.3.13", "exact", 9},
	}
	if len(declarations) != len(want) {
		t.Fatalf("PHP declarations = %d, want %d: %+v", len(declarations), len(want), declarations)
	}
	for _, got := range declarations {
		expected, ok := want[got.SourceKind]
		if !ok {
			t.Errorf("unexpected source kind %q: %+v", got.SourceKind, got)
			continue
		}
		if got.RelativePath != expected.path || got.RawSpec != expected.raw || got.SpecKind != expected.kind || got.LineNumber != expected.line || got.Runtime != "php" || got.ScopeDir != "." {
			t.Errorf("%s = %+v, want path=%q raw=%q kind=%q line=%d runtime=php scope=.", got.SourceKind, got, expected.path, expected.raw, expected.kind, expected.line)
		}
		delete(want, got.SourceKind)
	}
	if len(want) > 0 {
		t.Errorf("missing PHP source kinds: %v", want)
	}
}

func TestComposerRuntimeHandoffAcceptsAbsentPHP(t *testing.T) {
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "composer.json"), `{"require":{"acme/lib":"^1.0"}}`)
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, accepted := snapshot.ComposerRuntime["composer.json"]; !accepted || len(snapshot.ComposerRuntimeUnparsed) != 0 {
		t.Fatalf("absent PHP was not accepted as a parsed document: %+v", snapshot.ComposerRuntimeUnparsed)
	}
	collected := buildStreamingGeneration(repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-composer-absent", observed, snapshot, false, "")
	if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 0 {
		t.Fatalf("absent PHP emitted %+v", got)
	}
	if collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("valid Composer document without PHP requested recollection")
	}
}

func TestComposerRuntimeHandoffRejectsChangedBody(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "composer.json")
	writeCollectorTestFile(t, path, `{"require":{"php":"8.2"}}`)
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	writeCollectorTestFile(t, path, `{"require":{"php":"8.3"}}`)
	repo := testCollectorRepositoryMetadata(repoRoot)
	collected := buildStreamingGeneration(repoRoot, repo, "run-composer-changed", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 0 {
		t.Fatalf("mixed source revisions: %+v", declarations)
	}
	if collected.Generation.PreparseIdentityDropped == nil || !collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("changed composer body did not mark generation dropped")
	}
}

func TestComposerRuntimeHandoffRetriesUnparsedFile(t *testing.T) {
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "composer.json"), `{"require":{"php":"8.2",}`)
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ComposerRuntimeExpected {
		t.Fatal("native snapshot did not mark Composer parse attempt")
	}
	repo := testCollectorRepositoryMetadata(repoRoot)
	collected := buildStreamingGeneration(repoRoot, repo, "run-composer-invalid", observed, snapshot, false, "")
	if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 0 {
		t.Fatalf("invalid composer emitted %+v", got)
	}
	if collected.Generation.PreparseIdentityDropped == nil || !collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("unparsed composer file did not request recollection")
	}
}

func TestParserFileFactEnvelopeOmitsComposerTransport(t *testing.T) {
	data := map[string]any{"path": "/tmp/composer.json", "composer_runtime_evidence": "private transport"}
	envelope := ParserFileFactEnvelope("/tmp", "repo-1", "scope-1", "generation-1", time.Now(), data, false)
	if strings.Contains(fmt.Sprint(envelope.Payload), "composer_runtime_evidence") {
		t.Fatal("private composer parser transport persisted in file fact")
	}
}
