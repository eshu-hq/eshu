// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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

// languageMetadataSharedPath, languageMetadataSharedName, and
// languageMetadataSharedStart forward to querytestutil. The values moved
// there for #6642 so package language's repository_match_key_test.go can
// share the identical collision fixture; these consts keep this file's
// callers unchanged.
const (
	languageMetadataSharedPath  = querytestutil.LanguageMetadataSharedPath
	languageMetadataSharedName  = querytestutil.LanguageMetadataSharedName
	languageMetadataSharedStart = querytestutil.LanguageMetadataSharedStart
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
	search querycontract.LanguageEntitySearch,
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
func languageMetadataCollisionSeeds(omitRepoID bool) []querytestutil.GraphGrantSeed {
	seeds := make([]querytestutil.GraphGrantSeed, 0, 2)
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
		seeds = append(seeds, querytestutil.GraphGrantSeed{RepoID: repoID, Row: row})
	}
	return seeds
}

func runLanguageMetadataCollisionQuery(t *testing.T, omitRepoID bool) []any {
	t.Helper()

	handler := &LanguageQueryHandler{
		Neo4j: &querytestutil.EvaluatingRepositoryGraph{
			Seeds:             languageMetadataCollisionSeeds(omitRepoID),
			RepositoryAlias:   "r",
			RepositoryColumns: repositoryProjectedColumns(),
		},
		Content: &languageMetadataCollisionStore{omitRepoID: omitRepoID},
		Profile: ProfileLocalAuthoritative,
	}
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantOtherRepo})
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

// TestLanguageResultRepositoryMatchKeySeparatesRepositories moved to
// language_query_repository_match_key_test.go (#6642): it calls
// languageResultRepositoryMatchKey directly, a language-family-unexported
// free function, so it belongs in a family-owned white-box test file.
