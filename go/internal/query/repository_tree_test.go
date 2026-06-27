// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func repositoryTreeFixtureFiles() []FileContent {
	return []FileContent{
		{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", LineCount: 10, Language: "markdown"},
		{RepoID: "repo-1", RelativePath: "cmd/app/main.go", CommitSHA: "abc123", LineCount: 50, Language: "go"},
		{RepoID: "repo-1", RelativePath: "cmd/app/util.go", CommitSHA: "abc123", LineCount: 20, Language: "go"},
		{RepoID: "repo-1", RelativePath: "internal/store/db.go", CommitSHA: "abc123", LineCount: 80, Language: "go"},
	}
}

func repositoryTreeHandler() *RepositoryHandler {
	return &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    repositoryTreeFixtureFiles(),
		},
	}
}

func decodeRepositoryTree(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return resp
}

func repositoryTreeEntries(t *testing.T, resp map[string]any) map[string]map[string]any {
	t.Helper()
	raw, ok := resp["entries"].([]any)
	if !ok {
		t.Fatalf("entries type = %T, want []any", resp["entries"])
	}
	byName := make(map[string]map[string]any, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("entry type = %T, want map[string]any", item)
		}
		byName[entry["name"].(string)] = entry
	}
	return byName
}

func requestRepositoryTree(t *testing.T, handler *RepositoryHandler, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestGetRepositoryTreeFiltersByLanguage(t *testing.T) {
	t.Parallel()

	// Recursive + language=go: only the three Go files, never the markdown README.
	w := requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree?recursive=true&language=go")
	entries := repositoryTreeEntries(t, decodeRepositoryTree(t, w))
	for _, name := range []string{"main.go", "util.go", "db.go"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("language=go listing missing %q; got %v", name, entries)
		}
	}
	if _, ok := entries["README.md"]; ok {
		t.Fatalf("language=go listing must exclude the markdown README; got %v", entries)
	}

	// A language with no matching files yields an empty listing, not a 404.
	w = requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree?recursive=true&language=rust")
	entries = repositoryTreeEntries(t, decodeRepositoryTree(t, w))
	if len(entries) != 0 {
		t.Fatalf("language=rust listing must be empty; got %v", entries)
	}

	// markdown matches only the README.
	w = requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree?recursive=true&language=markdown")
	entries = repositoryTreeEntries(t, decodeRepositoryTree(t, w))
	if _, ok := entries["README.md"]; !ok {
		t.Fatalf("language=markdown listing must include README.md; got %v", entries)
	}
	if _, ok := entries["main.go"]; ok {
		t.Fatalf("language=markdown listing must exclude Go files; got %v", entries)
	}
}

func TestGetRepositoryTreeListsOneLevelWithDirsAndFiles(t *testing.T) {
	t.Parallel()

	w := requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree")
	resp := decodeRepositoryTree(t, w)

	if got, want := resp["ref"], "abc123"; got != want {
		t.Fatalf("ref = %#v, want %#v", got, want)
	}
	if got, want := resp["path"], ""; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}

	entries := repositoryTreeEntries(t, resp)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 (cmd, internal, README.md)", len(entries))
	}

	cmd := entries["cmd"]
	if cmd == nil || cmd["type"] != "dir" {
		t.Fatalf("cmd entry = %#v, want a dir", cmd)
	}
	if got, want := cmd["path"], "cmd"; got != want {
		t.Fatalf("cmd.path = %#v, want %#v", got, want)
	}
	if got, want := cmd["child_count"], float64(2); got != want {
		t.Fatalf("cmd.child_count = %#v, want %#v", got, want)
	}

	internal := entries["internal"]
	if internal == nil || internal["type"] != "dir" {
		t.Fatalf("internal entry = %#v, want a dir", internal)
	}
	if got, want := internal["child_count"], float64(1); got != want {
		t.Fatalf("internal.child_count = %#v, want %#v", got, want)
	}

	readme := entries["README.md"]
	if readme == nil || readme["type"] != "file" {
		t.Fatalf("README.md entry = %#v, want a file", readme)
	}
	if got, want := readme["path"], "README.md"; got != want {
		t.Fatalf("README.md.path = %#v, want %#v", got, want)
	}
	if got, want := readme["size"], float64(10); got != want {
		t.Fatalf("README.md.size = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryTreeFiltersBySubpath(t *testing.T) {
	t.Parallel()

	w := requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree?path=cmd/app")
	resp := decodeRepositoryTree(t, w)

	if got, want := resp["path"], "cmd/app"; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
	entries := repositoryTreeEntries(t, resp)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (main.go, util.go)", len(entries))
	}
	main := entries["main.go"]
	if main == nil || main["type"] != "file" {
		t.Fatalf("main.go entry = %#v, want a file", main)
	}
	if got, want := main["path"], "cmd/app/main.go"; got != want {
		t.Fatalf("main.go.path = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryTreeServesSelectedIndexedBranch(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    repositoryTreeFixtureFiles(),
			repositoryRefs: []RepositoryRef{
				{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true},
			},
		},
	}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?ref=main")
	resp := decodeRepositoryTree(t, w)
	if got, want := resp["ref"], "abc123"; got != want {
		t.Fatalf("ref = %#v, want indexed head %#v", got, want)
	}
}

func TestGetRepositoryTreeServesSelectedIndexedCommitSHA(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    repositoryTreeFixtureFiles(),
			repositoryRefs: []RepositoryRef{
				{Name: "release", Kind: "branch", HeadSHA: "def456"},
			},
		},
	}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?ref=abc123")
	resp := decodeRepositoryTree(t, w)
	if got, want := resp["ref"], "abc123"; got != want {
		t.Fatalf("ref = %#v, want indexed head %#v", got, want)
	}
}

func TestGetRepositoryTreeRejectsUnindexedSelectedBranch(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    repositoryTreeFixtureFiles(),
			repositoryRefs: []RepositoryRef{
				{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true},
				{Name: "release", Kind: "branch", HeadSHA: "def456"},
			},
		},
	}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?ref=release")
	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryTreeRejectsSelectedBranchWhenRefsUnavailable(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    repositoryTreeFixtureFiles(),
		},
	}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?ref=main")
	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryTreeRecursiveReturnsFullSubtree(t *testing.T) {
	t.Parallel()

	w := requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree?recursive=true")
	resp := decodeRepositoryTree(t, w)
	entries := repositoryTreeEntries(t, resp)

	for _, want := range []string{"cmd/app/main.go", "cmd/app/util.go", "internal/store/db.go", "README.md"} {
		found := false
		for _, entry := range entries {
			if entry["path"] == want && entry["type"] == "file" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("recursive tree missing file %q; entries = %#v", want, entries)
		}
	}
	// Intermediate directories are also reported in recursive mode.
	for _, want := range []string{"cmd", "cmd/app", "internal", "internal/store"} {
		found := false
		for _, entry := range entries {
			if entry["path"] == want && entry["type"] == "dir" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("recursive tree missing dir %q; entries = %#v", want, entries)
		}
	}
}

func TestGetRepositoryTreeUnknownRepositoryReturns404(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{Content: fakePortContentStore{}}
	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-ghost/tree")
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryTreeUnknownPathReturns404(t *testing.T) {
	t.Parallel()

	w := requestRepositoryTree(t, repositoryTreeHandler(), "/api/v0/repositories/repo-1/tree?path=does/not/exist")
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryTreeEmptyRepositoryReturnsEmptyEntries(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		},
	}
	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree")
	resp := decodeRepositoryTree(t, w)
	entries, ok := resp["entries"].([]any)
	if !ok {
		t.Fatalf("entries type = %T, want []any", resp["entries"])
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 for an indexed repository with no files", len(entries))
	}
}

func TestGetRepositoryTree_LocalLightweightReturnsTree(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Profile: ProfileLocalLightweight,
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    repositoryTreeFixtureFiles(),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/tree", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	handler.Mount(mux)
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var env ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if env.Truth == nil {
		t.Fatal("truth envelope is nil")
	}
	if got, want := env.Truth.Capability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth capability = %s, want %s", got, want)
	}
	if got, want := string(env.Truth.Profile), string(ProfileLocalLightweight); got != want {
		t.Fatalf("truth profile = %s, want %s", got, want)
	}
	if got, want := string(env.Truth.Level), string(TruthLevelDerived); got != want {
		t.Fatalf("truth level = %s, want %s", got, want)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("json.Marshal env.Data: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("json.Unmarshal data: %v", err)
	}
	if _, ok := data["entries"]; !ok {
		t.Fatal("response missing entries")
	}
}
