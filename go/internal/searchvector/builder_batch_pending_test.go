// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuilderBatchUsesBatchedPendingVectorDocumentsWhenAvailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 12, 30, 0, 0, time.UTC)
	pendingDocs := make([]postgres.EshuSearchDocumentRow, 0, 4)
	vectors := map[string][]float64{}
	for scopeIndex, scopeID := range []string{"scope-a", "scope-b"} {
		for docIndex := 0; docIndex < 2; docIndex++ {
			docID := fmt.Sprintf("%s-doc-%d", scopeID, docIndex)
			if docIndex == 0 {
				docID = "shared-doc"
			}
			doc := searchDocument(
				docID,
				fmt.Sprintf("repo-%c", 'a'+scopeIndex),
				"Pending handler",
				fmt.Sprintf("handlers/%s/%d.go", scopeID, docIndex),
			)
			pendingDocs = append(pendingDocs, postgres.EshuSearchDocumentRow{
				ScopeID:      scopeID,
				GenerationID: fmt.Sprintf("gen-%c", 'a'+scopeIndex),
				Document:     doc,
			})
			vectors[searchhybrid.DocumentText(doc)] = []float64{float64(scopeIndex), float64(docIndex)}
		}
	}
	store := &recordingBatchPendingDocumentStore{
		recordingDocumentStore: recordingDocumentStore{pendingRows: pendingDocs},
	}
	values := &recordingVectorValueStore{}
	metadata := &recordingVectorMetadataStore{}

	result, err := Builder{
		Documents: store,
		Metadata:  metadata,
		Values:    values,
		Embedder:  &recordingEmbedder{dims: 2, vectors: vectors},
		Clock:     func() time.Time { return now },
	}.BuildBatch(context.Background(), []BuildRequest{
		{
			ScopeID:            "scope-a",
			GenerationID:       "gen-a",
			AfterDocumentID:    "scope-a-cursor",
			ProjectionRevision: 3,
			BuildFence:         7,
			RepoID:             "repo-a",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			Limit:              2,
		},
		{
			ScopeID:            "scope-b",
			GenerationID:       "gen-b",
			AfterDocumentID:    "scope-b-cursor",
			ProjectionRevision: 5,
			BuildFence:         11,
			RepoID:             "repo-b",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			Limit:              2,
		},
	})
	if err != nil {
		t.Fatalf("BuildBatch error = %v", err)
	}
	if got, want := result.DocumentCount, 4; got != want {
		t.Fatalf("result.DocumentCount = %d, want %d", got, want)
	}
	if got, want := len(store.batchFilters), 1; got != want {
		t.Fatalf("ListPendingVectorDocumentsForScopes calls = %d, want %d", got, want)
	}
	if got, want := len(store.pendingFilters), 0; got != want {
		t.Fatalf("per-scope ListPendingVectorDocuments calls = %d, want %d", got, want)
	}
	if got, want := len(store.filters), 0; got != want {
		t.Fatalf("fallback ListActiveDocuments calls = %d, want %d", got, want)
	}
	if got, want := store.batchFilters[0].Limit, 2; got != want {
		t.Fatalf("batch per-scope limit = %d, want %d", got, want)
	}
	if got, want := len(store.batchFilters[0].Scopes), 2; got != want {
		t.Fatalf("batch scope count = %d, want %d", got, want)
	}
	if got, want := store.batchFilters[0].Scopes[0].AfterDocumentID, "scope-a-cursor"; got != want {
		t.Fatalf("scope-a document cursor = %q, want %q", got, want)
	}
	if got, want := result.ScopeProgress, []BuildScopeProgress{
		{ScopeID: "scope-a", GenerationID: "gen-a", DocumentCount: 2, LastDocumentID: "shared-doc"},
		{ScopeID: "scope-b", GenerationID: "gen-b", DocumentCount: 2, LastDocumentID: "shared-doc"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ScopeProgress = %#v, want %#v", got, want)
	}
	if got, want := len(values.rows), 4; got != want {
		t.Fatalf("vector rows = %d, want %d", got, want)
	}
	rowByScopeAndDocument := map[string]postgres.EshuSearchVectorValue{}
	for _, row := range values.rows {
		rowByScopeAndDocument[row.ScopeID+"/"+row.DocumentID] = row
	}
	for _, want := range []struct {
		scopeID      string
		generationID string
		documentID   string
		revision     int64
		fence        int64
	}{
		{scopeID: "scope-a", generationID: "gen-a", documentID: "shared-doc", revision: 3, fence: 7},
		{scopeID: "scope-b", generationID: "gen-b", documentID: "shared-doc", revision: 5, fence: 11},
	} {
		row, ok := rowByScopeAndDocument[want.scopeID+"/"+want.documentID]
		if !ok {
			t.Fatalf("missing vector row for scope %q document %q", want.scopeID, want.documentID)
		}
		if row.GenerationID != want.generationID {
			t.Fatalf("vector row generation for scope %q document %q = %q, want %q", want.scopeID, want.documentID, row.GenerationID, want.generationID)
		}
		if row.ProjectionRevision != want.revision || row.BuildFence != want.fence {
			t.Fatalf("vector row guard for scope %q = revision %d fence %d, want revision %d fence %d",
				want.scopeID, row.ProjectionRevision, row.BuildFence, want.revision, want.fence)
		}
	}
	metadataByScope := make(map[string]postgres.EshuSearchVectorMetadata, len(metadata.rows))
	for _, row := range metadata.rows {
		metadataByScope[row.ScopeID] = row
	}
	if row := metadataByScope["scope-a"]; row.ProjectionRevision != 3 || row.BuildFence != 7 {
		t.Fatalf("scope-a metadata guard = revision %d fence %d, want revision 3 fence 7", row.ProjectionRevision, row.BuildFence)
	}
	if row := metadataByScope["scope-b"]; row.ProjectionRevision != 5 || row.BuildFence != 11 {
		t.Fatalf("scope-b metadata guard = revision %d fence %d, want revision 5 fence 11", row.ProjectionRevision, row.BuildFence)
	}
}

type recordingBatchPendingDocumentStore struct {
	recordingDocumentStore
	batchFilters []postgres.EshuSearchVectorDocumentBatchFilter
}

func (s *recordingBatchPendingDocumentStore) ListPendingVectorDocumentsForScopes(
	_ context.Context,
	filter postgres.EshuSearchVectorDocumentBatchFilter,
) ([]postgres.EshuSearchDocumentRow, error) {
	s.batchFilters = append(s.batchFilters, filter)
	return append([]postgres.EshuSearchDocumentRow(nil), s.pendingRows...), nil
}

func TestBuilderBatchRejectsMixedSourceKinds(t *testing.T) {
	t.Parallel()

	store := &recordingBatchPendingDocumentStore{}
	_, err := Builder{
		Documents: store,
		Metadata:  &recordingVectorMetadataStore{},
		Values:    &recordingVectorValueStore{},
		Embedder:  &recordingEmbedder{dims: 2},
	}.BuildBatch(context.Background(), []BuildRequest{
		{
			ScopeID:            "scope-a",
			GenerationID:       "gen-a",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			SourceKinds:        []searchdocs.SourceKind{searchdocs.SourceKindCodeEntity},
			Limit:              50,
		},
		{
			ScopeID:            "scope-b",
			GenerationID:       "gen-b",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			SourceKinds:        []searchdocs.SourceKind{searchdocs.SourceKind("runtime_entity")},
			Limit:              50,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "source kinds") {
		t.Fatalf("BuildBatch error = %v, want mixed source kinds rejection", err)
	}
	if got := len(store.batchFilters); got != 0 {
		t.Fatalf("batch pending reads = %d, want 0 after validation rejection", got)
	}
}

func TestBuilderBatchRejectsMissingGenerationForBatchedPendingStore(t *testing.T) {
	t.Parallel()

	store := &recordingBatchPendingDocumentStore{}
	_, err := Builder{
		Documents: store,
		Metadata:  &recordingVectorMetadataStore{},
		Values:    &recordingVectorValueStore{},
		Embedder:  &recordingEmbedder{dims: 2},
	}.BuildBatch(context.Background(), []BuildRequest{
		{
			ScopeID:            "scope-a",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			Limit:              50,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatalf("BuildBatch error = %v, want missing generation rejection", err)
	}
	if got := len(store.batchFilters); got != 0 {
		t.Fatalf("batch pending reads = %d, want 0 after validation rejection", got)
	}
}

func TestBuilderBatchCapsTailPageAtFiftyThousandDocuments(t *testing.T) {
	t.Parallel()

	store := &recordingBatchPendingDocumentStore{}
	_, err := Builder{
		Documents: store,
		Metadata:  &recordingVectorMetadataStore{},
		Values:    &recordingVectorValueStore{},
		Embedder:  &recordingEmbedder{dims: 2},
	}.BuildBatch(context.Background(), []BuildRequest{{
		ScopeID:            "scope-a",
		GenerationID:       "gen-a",
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              100000,
	}})
	if err != nil {
		t.Fatalf("BuildBatch error = %v", err)
	}
	if got, want := store.batchFilters[0].Limit, 50000; got != want {
		t.Fatalf("batch filter limit = %d, want %d", got, want)
	}
}
