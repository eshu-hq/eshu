// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func repositoryContentHandler(files []FileContent) *RepositoryHandler {
	return &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    files,
		},
	}
}

func requestRepositoryContent(t *testing.T, handler *RepositoryHandler, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func decodeRepositoryContent(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
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

func TestGetRepositoryContentReturnsUtf8File(t *testing.T) {
	t.Parallel()

	content := "# Title\nhello\n"
	handler := repositoryContentHandler([]FileContent{
		{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", LineCount: 2, Language: "markdown", Content: content},
	})

	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=README.md")
	resp := decodeRepositoryContent(t, w)

	if got, want := resp["path"], "README.md"; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
	if got, want := resp["ref"], "abc123"; got != want {
		t.Fatalf("ref = %#v, want %#v", got, want)
	}
	if got, want := resp["encoding"], "utf-8"; got != want {
		t.Fatalf("encoding = %#v, want %#v", got, want)
	}
	if got, want := resp["content"], content; got != want {
		t.Fatalf("content = %#v, want %#v", got, want)
	}
	if got, want := resp["size"], float64(len(content)); got != want {
		t.Fatalf("size = %#v, want %#v", got, want)
	}
	if got, want := resp["language"], "markdown"; got != want {
		t.Fatalf("language = %#v, want %#v", got, want)
	}
	if got := resp["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
}

func TestGetRepositoryContentServesSelectedIndexedBranch(t *testing.T) {
	t.Parallel()

	content := "# Title\nhello\n"
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles: []FileContent{
				{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", Content: content},
			},
			repositoryRefs: []RepositoryRef{
				{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true},
			},
		},
	}

	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=README.md&ref=main")
	resp := decodeRepositoryContent(t, w)
	if got, want := resp["ref"], "abc123"; got != want {
		t.Fatalf("ref = %#v, want indexed head %#v", got, want)
	}
	if got, want := resp["content"], content; got != want {
		t.Fatalf("content = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryContentRejectsUnindexedSelectedBranch(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles: []FileContent{
				{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", Content: "# Title\n"},
			},
			repositoryRefs: []RepositoryRef{
				{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true},
				{Name: "release", Kind: "branch", HeadSHA: "def456"},
			},
		},
	}

	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=README.md&ref=release")
	if got, want := w.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryContentRejectsUnknownSourceBackedRef(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles: []FileContent{
				{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", Content: "# Title\n"},
			},
			repositoryRefs: []RepositoryRef{
				{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true},
			},
		},
	}

	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=README.md&ref=release")
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryContentRequiresPathParam(t *testing.T) {
	t.Parallel()

	handler := repositoryContentHandler([]FileContent{
		{RepoID: "repo-1", RelativePath: "README.md", Content: "x"},
	})
	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content")
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryContentMissingFileReturns404(t *testing.T) {
	t.Parallel()

	handler := repositoryContentHandler([]FileContent{
		{RepoID: "repo-1", RelativePath: "README.md", Content: "x"},
	})
	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=does/not/exist.go")
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryContentUnknownRepoReturns404(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{Content: fakePortContentStore{}}
	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-ghost/content?path=README.md")
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestGetRepositoryContentTruncatesLargeFile(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("a", repositoryContentMaxBytes+100)
	handler := repositoryContentHandler([]FileContent{
		{RepoID: "repo-1", RelativePath: "big.txt", Content: content},
	})

	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=big.txt")
	resp := decodeRepositoryContent(t, w)

	if got := resp["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true", got)
	}
	if got, want := resp["size"], float64(len(content)); got != want {
		t.Fatalf("size = %#v, want full byte size %#v", got, want)
	}
	returned, ok := resp["content"].(string)
	if !ok {
		t.Fatalf("content type = %T, want string", resp["content"])
	}
	if len(returned) > repositoryContentMaxBytes {
		t.Fatalf("returned content = %d bytes, want <= cap %d", len(returned), repositoryContentMaxBytes)
	}
}

func TestGetRepositoryContent_LocalLightweightReturnsContentWithDegradedTruth(t *testing.T) {
	t.Parallel()

	content := "# Title\nhello\n"
	handler := &RepositoryHandler{
		Profile: ProfileLocalLightweight,
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles: []FileContent{
				{RepoID: "repo-1", RelativePath: "README.md", CommitSHA: "abc123", LineCount: 2, Language: "markdown", Content: content},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/content?path=README.md", nil)
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
	if got, want := string(env.Truth.Profile), string(ProfileLocalLightweight); got != want {
		t.Fatalf("truth profile = %s, want %s", got, want)
	}

	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("json.Marshal env.Data: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("json.Unmarshal data: %v", err)
	}
	if got, want := data["content"], content; got != want {
		t.Fatalf("content = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryContentBase64EncodesBinary(t *testing.T) {
	t.Parallel()

	binary := string([]byte{0xff, 0xfe, 0xfd, 0x00})
	handler := repositoryContentHandler([]FileContent{
		{RepoID: "repo-1", RelativePath: "bin.dat", Content: binary},
	})

	w := requestRepositoryContent(t, handler, "/api/v0/repositories/repo-1/content?path=bin.dat")
	resp := decodeRepositoryContent(t, w)

	if got, want := resp["encoding"], "base64"; got != want {
		t.Fatalf("encoding = %#v, want %#v", got, want)
	}
	encoded, ok := resp["content"].(string)
	if !ok {
		t.Fatalf("content type = %T, want string", resp["content"])
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != binary {
		t.Fatalf("decoded = %#v, want %#v", string(decoded), binary)
	}
}
