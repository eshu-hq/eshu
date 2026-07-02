// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchvector

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Test fakes and helpers for builder_test.go, split out for the 500-line cap.

type recordingDocumentStore struct {
	filter  postgres.EshuSearchDocumentFilter
	filters []postgres.EshuSearchDocumentFilter
	rows    []postgres.EshuSearchDocumentRow
	pages   [][]postgres.EshuSearchDocumentRow
}

func (s *recordingDocumentStore) ListActiveDocuments(
	_ context.Context,
	filter postgres.EshuSearchDocumentFilter,
) ([]postgres.EshuSearchDocumentRow, error) {
	s.filter = filter
	s.filters = append(s.filters, filter)
	if len(s.pages) > 0 {
		page := s.pages[0]
		s.pages = s.pages[1:]
		return append([]postgres.EshuSearchDocumentRow(nil), page...), nil
	}
	return append([]postgres.EshuSearchDocumentRow(nil), s.rows...), nil
}

type generationSwitchingDocumentStore struct {
	filters []postgres.EshuSearchDocumentFilter
	oldRows []postgres.EshuSearchDocumentRow
	newRows []postgres.EshuSearchDocumentRow
}

func (s *generationSwitchingDocumentStore) ListActiveDocuments(
	_ context.Context,
	filter postgres.EshuSearchDocumentFilter,
) ([]postgres.EshuSearchDocumentRow, error) {
	s.filters = append(s.filters, filter)
	if filter.GenerationID == "gen-old" {
		return pageSearchDocumentRows(s.oldRows, filter.Offset, filter.Limit), nil
	}
	if filter.GenerationID == "gen-new" {
		return pageSearchDocumentRows(s.newRows, filter.Offset, filter.Limit), nil
	}
	if filter.Offset == 0 {
		return pageSearchDocumentRows(s.oldRows, filter.Offset, filter.Limit), nil
	}
	return pageSearchDocumentRows(s.newRows, filter.Offset, filter.Limit), nil
}

func pageSearchDocumentRows(
	rows []postgres.EshuSearchDocumentRow,
	offset int,
	limit int,
) []postgres.EshuSearchDocumentRow {
	if offset >= len(rows) {
		return nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return append([]postgres.EshuSearchDocumentRow(nil), rows[offset:end]...)
}

type recordingVectorMetadataStore struct {
	rows    []postgres.EshuSearchVectorMetadata
	batches [][]postgres.EshuSearchVectorMetadata
}

func (s *recordingVectorMetadataStore) UpsertBatch(_ context.Context, rows []postgres.EshuSearchVectorMetadata) error {
	s.batches = append(s.batches, rows)
	s.rows = append(s.rows, rows...)
	return nil
}

type recordingVectorValueStore struct {
	rows    []postgres.EshuSearchVectorValue
	batches [][]postgres.EshuSearchVectorValue
}

func (s *recordingVectorValueStore) UpsertBatch(_ context.Context, rows []postgres.EshuSearchVectorValue) error {
	s.batches = append(s.batches, rows)
	s.rows = append(s.rows, rows...)
	return nil
}

type recordingEmbedder struct {
	dims    int
	err     error
	vectors map[string][]float64
	calls   []string
}

func (e *recordingEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	e.calls = append(e.calls, text)
	if e.err != nil {
		return nil, e.err
	}
	return append([]float64(nil), e.vectors[text]...), nil
}

func (e *recordingEmbedder) Dimensions() int {
	return e.dims
}

func searchDocument(id, repoID, title, path string) searchdocs.Document {
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindCodeEntity,
		Title:       title,
		Path:        path,
		ContextText: "bounded context",
		Labels:      []string{"go", "handler"},
		GraphHandles: []searchdocs.GraphHandle{{
			Kind: "content_entity",
			ID:   id,
		}},
		TruthScope: searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
		Freshness:  searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

func sameVector(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
