// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// decodeDeclarationList decodes every runtime_declaration.version envelope in
// stream order, so a test can compare the exact fact list.
func decodeDeclarationList(t *testing.T, envelopes []facts.Envelope) []runtimedeclarationv1.Version {
	t.Helper()
	var out []runtimedeclarationv1.Version
	for _, env := range factsByKind(envelopes, facts.RuntimeDeclarationVersionFactKind) {
		v, err := factschema.DecodeRuntimeDeclarationVersion(factschema.Envelope{
			FactKind: env.FactKind, SchemaVersion: env.SchemaVersion, Payload: env.Payload,
		})
		if err != nil {
			t.Fatalf("decode %s: %v", env.StableFactKey, err)
		}
		out = append(out, v)
	}
	return out
}

func declarationLine(v runtimedeclarationv1.Version) string {
	return fmt.Sprintf("%s|%s|%s|%d.%d|%s|%s|%s|%s", v.RelativePath, v.ScopeDir, v.SourceKind, v.LineNumber, v.OrderIndex, v.Runtime, v.RawSpec, v.SpecKind, "")
}

// TestSnapshotRepositoryRuntimeDeclarationConformanceFixture is the
// conformance fixture for the runtime_declaration source list (#8815, #8818).
// It runs the real discovery, parser registry and fact stream over a monorepo
// that holds every source kind: version-pin files at the root and in a
// package, a root package.json and package.json files in three packages
// (different scope_dir values), go.mod files at the root and in a tool
// module, a root Dockerfile with an ARG-resolved base tag, a CI workflow with
// a setup-node version input, and dependency directories that must not emit.
// The expected list is the fixture.
func TestSnapshotRepositoryRuntimeDeclarationConformanceFixture(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		".nvmrc":                      "20.11.1\n",
		"package.json":                "{\n  \"name\": \"root\",\n  \"engines\": { \"node\": \">=18 <21\" },\n  \"packageManager\": \"pnpm@9.1.0+sha512.0123456789abcdef\"\n}\n",
		"go.mod":                      "module example.com/root\n\ngo 1.22.1\n\ntoolchain go1.22.3\n\nrequire example.com/dep v1.0.0\n\nreplace example.com/dep => ../dep\n",
		"main.go":                     "package main\n",
		"tools/go.mod":                "module example.com/tools\n\ngo 1.21\n",
		"tools/main.go":               "package main\n",
		"packages/web/package.json":   "{\"engines\":{\"node\":\"^20\",\"npm\":\"lts/*\"},\"volta\":{\"node\":\"20.11.1\",\"pnpm\":\"9.1.0\"}}\n",
		"packages/api/package.json":   "{\n  \"engines\": {\"node\": \"${NODE_VERSION}\"},\n  \"volta\": {\"yarn\": \"4.1.0\"}\n}\n",
		"packages/lib/package.json":   "{\"name\":\"lib\",\"engines\":{\"vscode\":\"^1.80.0\"},\"dependencies\":{\"left-pad\":\"1.3.0\"}}\n",
		"services/api/.tool-versions": "nodejs 20.11.1 18.19.0\n",
		"Dockerfile":                  "ARG NODE_VERSION=20.11.1\nFROM node:${NODE_VERSION} AS base\nFROM base\n",
		".github/workflows/ci.yml":    "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: 20\n",
		// Dependency directories: discovery prunes them, and the emitter refuses
		// them too.
		"node_modules/left-pad/package.json":         "{\"engines\":{\"node\":\">=0.4\"}}\n",
		"packages/web/node_modules/dep/package.json": "{\"engines\":{\"node\":\">=4\"}}\n",
		"vendor/github.com/acme/lib/package.json":    "{\"engines\":{\"node\":\">=6\"}}\n",
		"vendor/golang.org/x/mod/go.mod":             "module golang.org/x/mod\n\ngo 1.12\n",
		// Credential carriers and lock files never emit.
		".npmrc":            "//registry.example.com/:_authToken=secret\n",
		"package-lock.json": "{\"lockfileVersion\":3,\"engines\":{\"node\":\">=1\"}}\n",
	}
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	for rel, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
	}
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	observed := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return observed }}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	repo := testCollectorRepositoryMetadata(resolvedRepoRoot)
	collected := buildStreamingGeneration(resolvedRepoRoot, repo, "run-8815", observed, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	var got []string
	scopes := map[string]bool{}
	sourceKinds := map[string]bool{}
	for _, v := range decodeDeclarationList(t, envelopes) {
		if v.RepoID != repo.ID {
			t.Errorf("declaration %+v has repo_id %q, want %q", v, v.RepoID, repo.ID)
		}
		scopes[v.ScopeDir] = true
		sourceKinds[v.SourceKind] = true
		got = append(got, declarationLine(v))
	}
	sort.Strings(got)
	want := []string{
		".nvmrc|.|nvmrc|1.0|node|20.11.1|exact|",
		"go.mod|.|go_mod_go|3.0|go|1.22.1|exact|",
		"go.mod|.|go_mod_toolchain|5.0|go|go1.22.3|exact|",
		"package.json|.|package_json_engines|3.0|node|>=18 <21|range|",
		"package.json|.|package_json_package_manager|4.0|pnpm|9.1.0|exact|",
		"packages/api/package.json|packages/api|package_json_engines|2.0|node|${NODE_VERSION}|unresolved|",
		"packages/api/package.json|packages/api|package_json_volta|3.0|yarn|4.1.0|exact|",
		"packages/web/package.json|packages/web|package_json_engines|1.0|node|^20|range|",
		"packages/web/package.json|packages/web|package_json_engines|1.1|npm|lts/*|alias|",
		"packages/web/package.json|packages/web|package_json_volta|1.2|node|20.11.1|exact|",
		"packages/web/package.json|packages/web|package_json_volta|1.3|pnpm|9.1.0|exact|",
		"services/api/.tool-versions|services/api|tool_versions|1.0|node|20.11.1|exact|",
		"services/api/.tool-versions|services/api|tool_versions|1.1|node|18.19.0|exact|",
		"tools/go.mod|tools|go_mod_go|3.0|go|1.21|partial|",
		"Dockerfile|.|dockerfile_from|2.0|node|20.11.1|exact|",
		".github/workflows/ci.yml|.github/workflows|workflow_setup_version|7.0|node|20|partial|",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("declarations differ.\n got:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	for _, kind := range []string{
		"nvmrc", "tool_versions", "package_json_engines", "package_json_volta",
		"package_json_package_manager", "go_mod_go", "go_mod_toolchain",
		"dockerfile_from", "workflow_setup_version",
	} {
		if !sourceKinds[kind] {
			t.Errorf("fixture emitted no %q declaration", kind)
		}
	}
	if len(scopes) < 5 {
		t.Errorf("fixture scope_dir values = %v, want at least 5 distinct directories", scopes)
	}
}

// TestStreamFactsReadsManifestsOnBothContentBranchesAndDelta proves package.json
// and go.mod reach the emitter from the two-phase re-read path, the legacy
// in-memory path and a delta generation.
func TestStreamFactsReadsManifestsOnBothContentBranchesAndDelta(t *testing.T) {
	t.Parallel()

	const pkg = "{\"engines\":{\"node\":\">=18\"}}\n"
	const mod = "module m\ngo 1.22\n"
	observed := time.Date(2026, time.October, 25, 9, 0, 0, 0, time.UTC)

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "a", "package.json"), pkg)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "a", "go.mod"), mod)
	repo := testCollectorRepositoryMetadata(repoRoot)
	// The stream clears each meta as it reads it, so every snapshot gets its own.
	metas := func() []model.ContentFileMeta {
		return []model.ContentFileMeta{
			{RelativePath: "a/package.json", Digest: "sha1:p"}, {RelativePath: "a/go.mod", Digest: "sha1:g"},
		}
	}
	for name, snapshot := range map[string]RepositorySnapshot{
		"two-phase": {FileCount: 2, ContentFileMetas: metas()},
		"delta":     {FileCount: 2, Delta: true, ContentFileMetas: metas()},
		"legacy": {FileCount: 2, ContentFiles: []model.ContentFileSnapshot{
			{RelativePath: "a/package.json", Body: pkg, Digest: "sha1:p"}, {RelativePath: "a/go.mod", Body: mod, Digest: "sha1:g"},
		}},
	} {
		collected := buildStreamingGeneration(repoRoot, repo, "run-1", observed, snapshot, false, "")
		envelopes := drainFactChannel(collected.Facts)
		if got, want := collected.FactCount(), len(envelopes); got != want {
			t.Fatalf("%s: FactCount = %d, want %d", name, got, want)
		}
		list := decodeDeclarationList(t, envelopes)
		if len(list) != 2 || list[0].RelativePath != "a/go.mod" || list[1].RelativePath != "a/package.json" {
			t.Errorf("%s: declarations = %+v, want go.mod then package.json in path order", name, list)
		}
	}
}

// TestStreamFactsNeverCarriesAManifestCredentialIntoAFactOrALog is the seeded
// violation for the manifest sources: a credential-shaped value in engines,
// volta and packageManager of a package.json, and in the go and toolchain
// directives of a go.mod, must not reach a fact payload or a log line. The
// safe neighbours still emit, and the rejection is logged by count.
func TestStreamFactsNeverCarriesAManifestCredentialIntoAFactOrALog(t *testing.T) {
	// Built by concatenation: GitHub push protection blocks the whole literal.
	token := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	repoRoot := t.TempDir()
	files := map[string]string{
		"package.json": `{"engines":{"node":"` + token + `","npm":"^10"},"volta":{"yarn":"` + token + `"},"packageManager":"pnpm@` + token + `"}`,
		"go.mod":       "module m\ngo " + token + "\ntoolchain go1.22.3\n",
	}
	var metas []model.ContentFileMeta
	for rel, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
		metas = append(metas, model.ContentFileMeta{RelativePath: rel, Digest: "sha1:" + rel})
	}
	snapshot := RepositorySnapshot{FileCount: len(metas), ContentFileMetas: metas}
	source, logs, _ := dropRecordingSource(t)
	collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
		repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-1", time.Date(2026, time.October, 25, 10, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	list := decodeDeclarationList(t, envelopes)
	if len(list) != 2 {
		t.Fatalf("declarations = %+v, want engines.npm and the toolchain only", list)
	}
	for _, env := range factsByKind(envelopes, facts.RuntimeDeclarationVersionFactKind) {
		if strings.Contains(fmt.Sprint(env.Payload), token) {
			t.Fatalf("a runtime_declaration fact carries the credential-shaped value: %v", env.Payload)
		}
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("a log line carries the credential-shaped value: %s", logs.String())
	}
	for _, want := range []string{
		"msg=git_runtime_declaration_values_rejected", "reason=value_rejected", "relative_path=package.json",
		"rejected_count=3", "relative_path=go.mod", "rejected_count=1",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q lacks %q", logs.String(), want)
		}
	}
	if collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load() {
		t.Fatal("a deterministic value rejection marked the generation dropped")
	}
}

// TestStreamFactsReportsAnUnreadableAndAnOverCapManifest proves a malformed
// package.json and a package.json over the manifest cap emit nothing and are
// logged with their reason, while a good manifest beside them still emits.
func TestStreamFactsReportsAnUnreadableAndAnOverCapManifest(t *testing.T) {
	repoRoot := t.TempDir()
	huge := "{\"engines\":{\"node\":\"18\"},\"pad\":\"" + strings.Repeat("x", 1<<20) + "\"}"
	files := map[string]string{
		"ok/package.json":      "{\"engines\":{\"node\":\"18\"}}",
		"broken/package.json":  "{\"engines\":{\"node\":\"18\"",
		"huge/package.json":    huge,
		"deep/package.json":    strings.Repeat("[", 5000),
		"nothing/package.json": "{\"name\":\"x\"}",
		"empty/package.json":   "",
		"hostile/go.mod":       strings.Repeat("\x00", 100),
		"nothing/go.mod":       "module m\n",
	}
	var metas []model.ContentFileMeta
	for rel, body := range files {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), body)
		metas = append(metas, model.ContentFileMeta{RelativePath: rel, Digest: "sha1:" + rel})
	}
	snapshot := RepositorySnapshot{FileCount: len(metas), ContentFileMetas: metas}
	source, logs, _ := dropRecordingSource(t)
	collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
		repoRoot, testCollectorRepositoryMetadata(repoRoot), "run-1", time.Date(2026, time.October, 25, 11, 0, 0, 0, time.UTC), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	list := decodeDeclarationList(t, envelopes)
	if len(list) != 1 || list[0].RelativePath != "ok/package.json" {
		t.Fatalf("declarations = %+v, want only ok/package.json", list)
	}
	for _, want := range []string{
		"reason=manifest_unparseable", "relative_path=broken/package.json", "relative_path=deep/package.json",
		"reason=byte_cap_exceeded", "relative_path=huge/package.json", fmt.Sprintf("byte_cap=%d", 1<<20),
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log %q lacks %q", logs.String(), want)
		}
	}
	for _, unwanted := range []string{"nothing/", "empty/", "hostile/", "xxxx", "engines"} {
		if strings.Contains(logs.String(), unwanted) {
			t.Errorf("log %q must not mention %q", logs.String(), unwanted)
		}
	}
	if n := strings.Count(logs.String(), "msg=git_runtime_declaration_file_skipped"); n != 3 {
		t.Errorf("skip logs = %d, want 3 (broken, deep, huge)", n)
	}
}
