// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

func decodeRuntimeDeclarations(t *testing.T, envelopes []facts.Envelope) map[string]runtimedeclarationv1.Version {
	t.Helper()
	out := map[string]runtimedeclarationv1.Version{}
	for _, env := range factsByKind(envelopes, facts.RuntimeDeclarationVersionFactKind) {
		v, err := factschema.DecodeRuntimeDeclarationVersion(factschema.Envelope{
			FactKind: env.FactKind, SchemaVersion: env.SchemaVersion, Payload: env.Payload,
		})
		if err != nil {
			t.Fatalf("decode %s: %v", env.StableFactKey, err)
		}
		out[v.RelativePath+"#"+v.RawSpec] = v
	}
	return out
}

// TestStreamFactsEmitsRuntimeDeclarationsFromContentFileMetas proves the
// content-loop hook on the two-phase re-read path: pin files at the root and in
// a monorepo package emit one declaration per token with the right scope_dir,
// while a credential carrier in the same snapshot emits nothing.
func TestStreamFactsEmitsRuntimeDeclarationsFromContentFileMetas(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	files := map[string]string{
		".nvmrc":                      "20\n",
		"services/api/.tool-versions": "nodejs 20.11.1 18.0.0\n",
		"services/web/.node-version":  "lts/iron\n",
		".npmrc":                      "//registry.example.com/:_authToken=secret\n",
	}
	var metas []model.ContentFileMeta
	for rel, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
		metas = append(metas, model.ContentFileMeta{RelativePath: rel, Digest: "sha1:" + rel})
	}
	snapshot := RepositorySnapshot{FileCount: len(metas), ContentFileMetas: metas}

	repo := testCollectorRepositoryMetadata(repoRoot)
	collected := buildStreamingGeneration(repoRoot, repo, "run-1", time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	got := decodeRuntimeDeclarations(t, envelopes)
	if len(got) != 4 {
		t.Fatalf("runtime_declaration.version count = %d, want 4: %+v", len(got), got)
	}
	cases := []struct{ key, scope, kind, runtime, spec string }{
		{".nvmrc#20", ".", "partial", "node", "20"},
		{"services/api/.tool-versions#20.11.1", "services/api", "exact", "node", "20.11.1"},
		{"services/api/.tool-versions#18.0.0", "services/api", "exact", "node", "18.0.0"},
		{"services/web/.node-version#lts/iron", "services/web", "alias", "node", "lts/iron"},
	}
	for _, tc := range cases {
		v, ok := got[tc.key]
		if !ok {
			t.Fatalf("missing declaration %q in %+v", tc.key, got)
		}
		if v.ScopeDir != tc.scope || v.SpecKind != tc.kind || v.Runtime != tc.runtime || v.RepoID != repo.ID {
			t.Errorf("declaration %q = %+v, want scope=%s kind=%s runtime=%s repo=%s", tc.key, v, tc.scope, tc.kind, tc.runtime, repo.ID)
		}
	}
	for key := range got {
		if filepath.Base(key[:len(key)-len(got[key].RawSpec)-1]) == ".npmrc" {
			t.Fatalf(".npmrc emitted a declaration: %s", key)
		}
	}
}

// TestStreamFactsEmitsRuntimeDeclarationsFromLegacyContentFiles proves the same
// hook on the legacy path where bodies are already in memory.
func TestStreamFactsEmitsRuntimeDeclarationsFromLegacyContentFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoRoot)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFiles: []model.ContentFileSnapshot{{
			RelativePath: "tools/.python-version", Body: "3.12\n", Digest: "sha1:py",
		}},
	}
	collected := buildStreamingGeneration(repoRoot, repo, "run-1", time.Date(2026, time.October, 24, 9, 30, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}
	got := decodeRuntimeDeclarations(t, envelopes)
	v, ok := got["tools/.python-version#3.12"]
	if !ok || len(got) != 1 {
		t.Fatalf("declarations = %+v, want exactly tools/.python-version#3.12", got)
	}
	if v.ScopeDir != "tools" || v.Runtime != "python" || v.SpecKind != "partial" {
		t.Fatalf("declaration = %+v", v)
	}
}

// TestStreamFactsEmitsRuntimeDeclarationsOnDelta proves a delta generation
// re-emits the declarations of a changed pin file, so an edited .nvmrc reaches
// the fact store without a full re-scan.
func TestStreamFactsEmitsRuntimeDeclarationsOnDelta(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".nvmrc"), "22\n")
	repo := testCollectorRepositoryMetadata(repoRoot)
	snapshot := RepositorySnapshot{
		FileCount:        1,
		Delta:            true,
		ContentFileMetas: []model.ContentFileMeta{{RelativePath: ".nvmrc", Digest: "sha1:nvmrc"}},
	}
	collected := buildStreamingGeneration(repoRoot, repo, "run-1", time.Date(2026, time.October, 24, 10, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	got := decodeRuntimeDeclarations(t, envelopes)
	if _, ok := got[".nvmrc#22"]; !ok || len(got) != 1 {
		t.Fatalf("delta declarations = %+v, want exactly .nvmrc#22", got)
	}
}

// TestStreamFactsEmitsNoRuntimeDeclarationsWhenAbsent proves the hook is a
// no-op for a repository with no pin file.
func TestStreamFactsEmitsNoRuntimeDeclarationsWhenAbsent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.go"), "package main\n")
	repo := testCollectorRepositoryMetadata(repoRoot)
	snapshot := RepositorySnapshot{
		FileCount:        1,
		ContentFileMetas: []model.ContentFileMeta{{RelativePath: "main.go", Digest: "sha1:main", Language: "go"}},
	}
	collected := buildStreamingGeneration(repoRoot, repo, "run-1", time.Date(2026, time.October, 24, 10, 30, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if n := len(factsByKind(envelopes, facts.RuntimeDeclarationVersionFactKind)); n != 0 {
		t.Fatalf("runtime_declaration.version count = %d, want 0", n)
	}
}

// TestStreamFactsSkipsAPinFileOverTheTokenCapAndLogsIt proves a pin file over
// the per-file token cap emits no declaration, a normal pin file beside it
// still emits, and the skip is logged WARN with the relative path, token count
// and cap. The skip is deterministic, so it does not mark the generation
// dropped.
func TestStreamFactsSkipsAPinFileOverTheTokenCapAndLogsIt(t *testing.T) {
	repoRoot := t.TempDir()
	huge := "nodejs" + strings.Repeat(" 20.1.0", runtimedeclaration.MaxTokensPerFile+1) + "\n"
	files := map[string]string{"big/.tool-versions": huge, ".nvmrc": "20\n"}
	var metas []model.ContentFileMeta
	for rel, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
		metas = append(metas, model.ContentFileMeta{RelativePath: rel, Digest: "sha1:" + rel})
	}
	snapshot := RepositorySnapshot{FileCount: len(metas), ContentFileMetas: metas}
	source, logs, _ := dropRecordingSource(t)
	collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
		repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-1", time.Date(2026, time.October, 24, 11, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	got := decodeRuntimeDeclarations(t, envelopes)
	if _, ok := got[".nvmrc#20"]; !ok || len(got) != 1 {
		t.Fatalf("declarations = %d, want only .nvmrc#20: the capped file must emit nothing", len(got))
	}
	if collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("a deterministic cap skip marked the generation dropped")
	}
	for _, want := range []string{
		"level=WARN", "msg=git_runtime_declaration_file_skipped", "relative_path=big/.tool-versions",
		"token_count=" + strconv.Itoa(runtimedeclaration.MaxTokensPerFile+1),
		"token_cap=" + strconv.Itoa(runtimedeclaration.MaxTokensPerFile),
		"byte_size=" + strconv.Itoa(len(huge)),
		"reason=token_cap_exceeded", "repository_id=repository:", "scope_id=git-repository-scope:", "generation_id=",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q lacks %q", logs.String(), want)
		}
	}
}

// TestStreamFactsSkipsAPinFileOverTheByteCapAndLogsIt proves a pin file over
// the per-file byte cap emits nothing even though it holds a single token, a
// normal pin file beside it still emits, and the WARN skip log carries the
// byte reason, size and cap, no token count (the file was never tokenized) and
// none of the file content.
func TestStreamFactsSkipsAPinFileOverTheByteCapAndLogsIt(t *testing.T) {
	repoRoot := t.TempDir()
	marker := "hostile-marker-content"
	huge := marker + strings.Repeat("x", runtimedeclaration.MaxBytesPerFile)
	files := map[string]string{"big/.nvmrc": huge, ".nvmrc": "20\n"}
	var metas []model.ContentFileMeta
	for rel, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
		metas = append(metas, model.ContentFileMeta{RelativePath: rel, Digest: "sha1:" + rel})
	}
	snapshot := RepositorySnapshot{FileCount: len(metas), ContentFileMetas: metas}
	source, logs, _ := dropRecordingSource(t)
	collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
		repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-1", time.Date(2026, time.October, 24, 11, 30, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	got := decodeRuntimeDeclarations(t, envelopes)
	if _, ok := got[".nvmrc#20"]; !ok || len(got) != 1 {
		t.Fatalf("declarations = %d, want only .nvmrc#20: the over-cap file must emit nothing", len(got))
	}
	if collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("a deterministic cap skip marked the generation dropped")
	}
	for _, want := range []string{
		"level=WARN", "msg=git_runtime_declaration_file_skipped", "relative_path=big/.nvmrc",
		"byte_size=" + strconv.Itoa(len(huge)),
		"byte_cap=" + strconv.Itoa(runtimedeclaration.MaxBytesPerFile),
		"reason=byte_cap_exceeded", "repository_id=repository:", "scope_id=git-repository-scope:", "generation_id=",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q lacks %q", logs.String(), want)
		}
	}
	for _, unwanted := range []string{"token_count=", "token_cap=", marker} {
		if strings.Contains(logs.String(), unwanted) {
			t.Errorf("byte-cap log %q must not carry %q", logs.String(), unwanted)
		}
	}
}

// TestStreamFactsSkipsManyOverCapPinFilesAndLogsEach proves the stream-level
// byte-cap path for many hostile pin files at once: 50 pin files of 1 MiB at
// different depths emit no fact, each yields one byte-cap WARN with its own
// byte_size, and an in-cap pin file beside them still emits. The retained
// candidate state is proven in the declaration package, where the map is
// reachable.
func TestStreamFactsSkipsManyOverCapPinFilesAndLogsEach(t *testing.T) {
	repoRoot := t.TempDir()
	const files, bodyBytes = 50, 1 << 20
	body := strings.Repeat("x", bodyBytes)
	metas := []model.ContentFileMeta{{RelativePath: ".nvmrc", Digest: "sha1:ok"}}
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".nvmrc"), "20\n")
	for i := 0; i < files; i++ {
		rel := strings.Repeat("d/", i+1) + ".nvmrc"
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
		metas = append(metas, model.ContentFileMeta{RelativePath: rel, Digest: "sha1:" + rel})
	}
	snapshot := RepositorySnapshot{FileCount: len(metas), ContentFileMetas: metas}
	source, logs, _ := dropRecordingSource(t)
	collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
		repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-1", time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	got := decodeRuntimeDeclarations(t, envelopes)
	if _, ok := got[".nvmrc#20"]; !ok || len(got) != 1 {
		t.Fatalf("declarations = %d, want only .nvmrc#20", len(got))
	}
	if n := strings.Count(logs.String(), "msg=git_runtime_declaration_file_skipped"); n != files {
		t.Fatalf("byte-cap skip logs = %d, want %d", n, files)
	}
	if n := strings.Count(logs.String(), "reason=byte_cap_exceeded"); n != files {
		t.Fatalf("byte_cap_exceeded logs = %d, want %d", n, files)
	}
	if n := strings.Count(logs.String(), "byte_size="+strconv.Itoa(bodyBytes)); n != files {
		t.Fatalf("logs with byte_size=%d = %d, want %d", bodyBytes, n, files)
	}
}
