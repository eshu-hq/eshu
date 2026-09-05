// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"
)

// #5167 code-family batch 2a review round 2, finding 2.
//
// enrichLanguageResultsWithContentMetadata builds metadataByKey from the
// content rows and then looks each graph row up in it. The key was file path,
// label, name and start line -- no repository component -- so two repositories
// the SAME caller was granted that share all four (a fork, a vendored copy, a
// generated file two services both carry) collided in that map: the last
// content row written won, and both graph rows were enriched with it. The grant
// binding does not help here, because both repositories are inside the grant;
// this is a cross-repository correctness bug within one tenant's own answer.
//
// The fix puts the repository on both sides of the key. These tests seed one
// caller granted two repositories whose rows agree on every old key component
// and differ only in repo_id, and assert each row keeps its own repository's
// metadata.

const (
	languageMetadataSharedPath  = "internal/auth/session.go"
	languageMetadataSharedName  = "SharedKeyProbe"
	languageMetadataSharedStart = 10
)

// languageMetadataCollisionStore returns one content row per repository, both
// carrying the same path/type/name/start line and each carrying a docstring
// naming its own repository.
type languageMetadataCollisionStore struct {
	fakePortContentStore
	// omitRepoID drops repo_id from the content rows, standing in for a store
	// or projection that cannot attribute a row to a repository. Rows without
	// one must still only ever match graph rows without one.
	omitRepoID bool
}

func (s *languageMetadataCollisionStore) SearchEntitiesByLanguageAndTypeForAccess(
	_ context.Context,
	search languageEntitySearch,
) ([]EntityContent, error) {
	rows := make([]EntityContent, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		row := EntityContent{
			EntityID:     repoID + "#" + languageMetadataSharedName,
			RepoID:       repoID,
			RelativePath: languageMetadataSharedPath,
			EntityType:   search.EntityType,
			EntityName:   languageMetadataSharedName,
			Language:     search.Language,
			StartLine:    languageMetadataSharedStart,
			EndLine:      20,
			Metadata:     map[string]any{"docstring": languageMetadataDocstring(repoID)},
		}
		if s.omitRepoID {
			row.RepoID = ""
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func languageMetadataDocstring(repoID string) string {
	return "content metadata from " + repoID
}

// languageMetadataCollisionSeeds are the two graph rows the fake returns: same
// path, label, name and start line, different repository.
func languageMetadataCollisionSeeds(omitRepoID bool) []graphGrantSeed {
	seeds := make([]graphGrantSeed, 0, 2)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		row := map[string]any{
			"entity_id":  repoID + "#" + languageMetadataSharedName,
			"name":       languageMetadataSharedName,
			"labels":     []any{"Function"},
			"file_path":  languageMetadataSharedPath,
			"repo_id":    repoID,
			"repo_name":  repoID,
			"language":   "go",
			"start_line": int64(languageMetadataSharedStart),
			"end_line":   int64(20),
		}
		if omitRepoID {
			delete(row, "repo_id")
		}
		seeds = append(seeds, graphGrantSeed{repoID: repoID, row: row})
	}
	return seeds
}

func runLanguageMetadataCollisionQuery(t *testing.T, omitRepoID bool) []any {
	t.Helper()

	handler := &LanguageQueryHandler{
		Neo4j: &evaluatingRepositoryGraph{
			seeds:             languageMetadataCollisionSeeds(omitRepoID),
			repositoryAlias:   "r",
			repositoryColumns: repositoryProjectedColumns(),
		},
		Content: &languageMetadataCollisionStore{omitRepoID: omitRepoID},
		Profile: ProfileLocalAuthoritative,
	}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantOtherRepo})
	rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody("function"), &auth)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := decodeEnvelopeData(t, rec.Body.Bytes())
	rows, ok := data["results"].([]any)
	if !ok {
		t.Fatalf("results = %#v, want an array: %s", data["results"], rec.Body.String())
	}
	if len(rows) != 2 {
		t.Fatalf("results carried %d row(s), want 2 (one per granted repository): %s", len(rows), rec.Body.String())
	}
	return rows
}

// TestLanguageQueryMetadataDoesNotCrossGrantedRepositories is the regression
// test: with the repository out of the merge key, both rows come back carrying
// whichever repository's content row was written to the map last.
func TestLanguageQueryMetadataDoesNotCrossGrantedRepositories(t *testing.T) {
	t.Parallel()

	for _, row := range runLanguageMetadataCollisionQuery(t, false) {
		result, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("result row = %#v, want an object", row)
		}
		repoID, _ := result["repo_id"].(string)
		metadata, ok := result["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("row for %q has no merged metadata: %#v", repoID, result)
		}
		got, _ := metadata["docstring"].(string)
		if want := languageMetadataDocstring(repoID); got != want {
			t.Fatalf("row for %q merged docstring %q, want %q: the merge key crosses repositories", repoID, got, want)
		}
	}
}

// TestLanguageQueryMetadataKeyFallsBackWhenNoRepositoryIsKnown pins the other
// side of the key change: a row that carries no repository must still merge
// against a content row that carries none either, rather than losing its
// metadata entirely.
func TestLanguageQueryMetadataKeyFallsBackWhenNoRepositoryIsKnown(t *testing.T) {
	t.Parallel()

	merged := 0
	for _, row := range runLanguageMetadataCollisionQuery(t, true) {
		result, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("result row = %#v, want an object", row)
		}
		metadata, ok := result["metadata"].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := metadata["docstring"].(string); ok {
			merged++
		}
	}
	if merged == 0 {
		t.Fatal("no row merged content metadata when neither side carries a repository; the fallback key does not match")
	}
}

// TestLanguageResultRepositoryMatchKeySeparatesRepositories is the unit-level statement of
// the same rule, so a future reader can see the key contract without running a
// route.
func TestLanguageResultRepositoryMatchKeySeparatesRepositories(t *testing.T) {
	t.Parallel()

	left := languageResultRepositoryMatchKey(codeGrantGrantedRepo, languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	right := languageResultRepositoryMatchKey(codeGrantOtherRepo, languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	if left == right {
		t.Fatalf("two repositories sharing path/label/name/start line produced the same key %q", left)
	}
	unattributedLeft := languageResultRepositoryMatchKey("", languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	unattributedRight := languageResultRepositoryMatchKey("", languageMetadataSharedPath, "Function", languageMetadataSharedName, languageMetadataSharedStart)
	if unattributedLeft != unattributedRight {
		t.Fatal("two rows that both carry no repository must share a key")
	}
	if unattributedLeft == left {
		t.Fatalf("a row with no repository shares key %q with one inside a repository", left)
	}
}
