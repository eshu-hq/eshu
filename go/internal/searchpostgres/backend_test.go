// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchpostgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBackendSearchProjectsBoundedContentRows(t *testing.T) {
	t.Parallel()

	store := &fakeContentStore{
		files: []postgres.FileContentRow{{
			RepoID:       "repo-checkout",
			RelativePath: "cmd/api/main.go",
			Content:      "package main\nfunc main() { startCheckoutAPI() }",
			Language:     "go",
			ArtifactType: "source",
		}},
		entities: []postgres.EntityContentRow{{
			EntityID:     "content-entity:checkout-start",
			RepoID:       "repo-checkout",
			RelativePath: "cmd/api/main.go",
			EntityType:   "function",
			EntityName:   "startCheckoutAPI",
			StartLine:    12,
			EndLine:      20,
			Language:     "go",
			ArtifactType: "source",
			SourceCache:  "func startCheckoutAPI() { listenAndServe() }",
		}},
	}
	backend := Backend{Store: store}

	candidates, err := backend.Search(context.Background(), boundedKeywordRequest())
	if err != nil {
		t.Fatalf("Backend.Search returned error: %v", err)
	}
	if got, want := store.fileLimit, 3; got != want {
		t.Fatalf("file limit = %d, want %d", got, want)
	}
	if got, want := store.entityLimit, 3; got != want {
		t.Fatalf("entity limit = %d, want %d", got, want)
	}
	if got, want := store.fileRepoID, "repo-checkout"; got != want {
		t.Fatalf("file repo id = %q, want %q", got, want)
	}
	if got, want := store.entityRepoID, "repo-checkout"; got != want {
		t.Fatalf("entity repo id = %q, want %q", got, want)
	}
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}

	entityCandidate := candidates[0]
	if got, want := entityCandidate.Document.ID, "searchdoc:content_entity:content-entity:checkout-start"; got != want {
		t.Fatalf("entity candidate document id = %q, want %q", got, want)
	}
	assertDerivedContentDocument(t, entityCandidate.Document)
	if got, want := entityCandidate.Metadata["backend"], "postgres_content_search"; got != want {
		t.Fatalf("entity backend metadata = %q, want %q", got, want)
	}
	if got, want := entityCandidate.Metadata["source_table"], "content_entities"; got != want {
		t.Fatalf("entity source table metadata = %q, want %q", got, want)
	}

	fileCandidate := candidates[1]
	if got, want := fileCandidate.Document.ID, "searchdoc:content_file:repo-checkout:cmd/api/main.go"; got != want {
		t.Fatalf("file candidate document id = %q, want %q", got, want)
	}
	assertDerivedContentDocument(t, fileCandidate.Document)
	if got, want := fileCandidate.Metadata["source_table"], "content_files"; got != want {
		t.Fatalf("file source table metadata = %q, want %q", got, want)
	}
	if !(entityCandidate.Score > fileCandidate.Score && fileCandidate.Score > 0) {
		t.Fatalf("scores = entity %.3f, file %.3f; want deterministic positive rank scores", entityCandidate.Score, fileCandidate.Score)
	}
}

func TestBackendSearchRequiresKeywordMode(t *testing.T) {
	t.Parallel()

	req := boundedKeywordRequest()
	req.Mode = searchbench.ModeHybrid

	_, err := (Backend{Store: &fakeContentStore{}}).Search(context.Background(), req)
	if !errorContains(err, "keyword") {
		t.Fatalf("Backend.Search error = %v, want keyword-mode rejection", err)
	}
}

func TestBackendSearchRequiresRepositoryScope(t *testing.T) {
	t.Parallel()

	req := boundedKeywordRequest()
	req.Scope = searchretrieval.Scope{ServiceID: "service:checkout"}

	_, err := (Backend{Store: &fakeContentStore{}}).Search(context.Background(), req)
	if !errorContains(err, "repository") {
		t.Fatalf("Backend.Search error = %v, want repository-scope rejection", err)
	}
}

func TestBackendSearchSkipsExcludedContentDocuments(t *testing.T) {
	t.Parallel()

	store := &fakeContentStore{
		files: []postgres.FileContentRow{{
			RepoID:       "repo-checkout",
			RelativePath: "secrets.txt",
			Content:      "AWS_SECRET_ACCESS_KEY=not-a-real-key",
		}},
	}

	candidates, err := (Backend{Store: store}).Search(context.Background(), boundedKeywordRequest())
	if err != nil {
		t.Fatalf("Backend.Search returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("len(candidates) = %d, want excluded sensitive document skipped", len(candidates))
	}
}

func TestBackendSearchPropagatesStoreError(t *testing.T) {
	t.Parallel()

	store := &fakeContentStore{fileErr: errors.New("postgres unavailable")}

	_, err := (Backend{Store: store}).Search(context.Background(), boundedKeywordRequest())
	if !errorContains(err, "file content") || !errorContains(err, "postgres unavailable") {
		t.Fatalf("Backend.Search error = %v, want file-content store context", err)
	}
}

func boundedKeywordRequest() searchretrieval.Request {
	return searchretrieval.Request{
		QueryID: "postgres-baseline-001",
		Query:   "checkout api startup",
		Scope: searchretrieval.Scope{
			RepoID: "repo-checkout",
		},
		Mode:    searchbench.ModeKeyword,
		Limit:   2,
		Timeout: 50 * time.Millisecond,
	}
}

func assertDerivedContentDocument(t *testing.T, doc searchdocs.Document) {
	t.Helper()
	if got, want := doc.TruthScope.Level, searchdocs.TruthLevelDerived; got != want {
		t.Fatalf("document truth level = %q, want %q", got, want)
	}
	if got, want := doc.TruthScope.Basis, searchdocs.TruthBasisContentIndex; got != want {
		t.Fatalf("document truth basis = %q, want %q", got, want)
	}
	if got, want := doc.Freshness.State, searchdocs.FreshnessFresh; got != want {
		t.Fatalf("document freshness = %q, want %q", got, want)
	}
	if got, want := doc.AccessScope.RepoID, "repo-checkout"; got != want {
		t.Fatalf("document access repo = %q, want %q", got, want)
	}
	if len(doc.GraphHandles) == 0 {
		t.Fatal("document graph handles are empty")
	}
}

type fakeContentStore struct {
	fileRepoID   string
	fileLimit    int
	entityRepoID string
	entityLimit  int
	files        []postgres.FileContentRow
	entities     []postgres.EntityContentRow
	fileErr      error
	entityErr    error
}

func (store *fakeContentStore) SearchFileContent(
	_ context.Context,
	query string,
	repoID string,
	limit int,
) ([]postgres.FileContentRow, error) {
	store.fileRepoID = repoID
	store.fileLimit = limit
	if store.fileErr != nil {
		return nil, store.fileErr
	}
	return trimFiles(store.files, query, limit), nil
}

func (store *fakeContentStore) SearchEntityContent(
	_ context.Context,
	query string,
	repoID string,
	limit int,
) ([]postgres.EntityContentRow, error) {
	store.entityRepoID = repoID
	store.entityLimit = limit
	if store.entityErr != nil {
		return nil, store.entityErr
	}
	return trimEntities(store.entities, query, limit), nil
}

func trimFiles(rows []postgres.FileContentRow, query string, limit int) []postgres.FileContentRow {
	out := make([]postgres.FileContentRow, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Content), strings.ToLower(query)) || query != "" {
			out = append(out, row)
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

func trimEntities(rows []postgres.EntityContentRow, query string, limit int) []postgres.EntityContentRow {
	out := make([]postgres.EntityContentRow, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.SourceCache), strings.ToLower(query)) || query != "" {
			out = append(out, row)
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

func errorContains(err error, needle string) bool {
	return err != nil && strings.Contains(err.Error(), needle)
}
