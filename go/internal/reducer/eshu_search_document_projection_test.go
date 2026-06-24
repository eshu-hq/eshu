// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestProjectSearchDocumentsCuratesEachLane(t *testing.T) {
	t.Parallel()

	indexed := time.Date(2026, time.June, 12, 0, 0, 0, 0, time.UTC)
	input := SearchDocumentProjectionInput{
		ContentEntities: []searchdocs.ContentEntity{
			{EntityID: "e-1", RepoID: "repo-1", EntityType: "Function", EntityName: "Handle", RelativePath: "h.go", SourceCache: "func Handle() {}", IndexedAt: indexed},
		},
		ContentFiles: []searchdocs.ContentFile{
			{RepoID: "repo-1", RelativePath: "main.go", Language: "go", Content: "package main", IndexedAt: indexed},
		},
		RuntimeSummaries: []searchdocs.RuntimeSummary{
			{ID: "svc-1", RepoID: "repo-1", Title: "billing", Summary: "billing service", ServiceID: "svc-1", UpdatedAt: indexed},
		},
	}

	projection := ProjectSearchDocuments(input)

	if got, want := projection.Summary.Considered, 3; got != want {
		t.Fatalf("considered = %d, want %d", got, want)
	}
	if got, want := projection.Summary.Included, 3; got != want {
		t.Fatalf("included = %d, want %d", got, want)
	}
	if got := len(projection.Documents); got != 3 {
		t.Fatalf("documents = %d, want 3", got)
	}
	for kind, want := range map[searchdocs.SourceKind]int{
		searchdocs.SourceKindCodeEntity:     1,
		searchdocs.SourceKindRepositoryFile: 1,
		searchdocs.SourceKindRuntimeSummary: 1,
	} {
		if got := projection.Summary.IncludedBySourceKind[kind]; got != want {
			t.Errorf("included[%s] = %d, want %d", kind, got, want)
		}
	}
	// Documents must be ordered by ID for idempotent writes.
	for i := 1; i < len(projection.Documents); i++ {
		if projection.Documents[i-1].ID > projection.Documents[i].ID {
			t.Fatalf("documents not sorted by ID: %q before %q", projection.Documents[i-1].ID, projection.Documents[i].ID)
		}
	}
}

func TestProjectSearchDocumentsDropsSensitiveAndExcluded(t *testing.T) {
	t.Parallel()

	input := SearchDocumentProjectionInput{
		ContentEntities: []searchdocs.ContentEntity{
			{EntityID: "secret-1", RepoID: "repo-1", EntityType: "Const", EntityName: "Key", SourceCache: "const api_key = \"abc\""}, // sensitive
			{EntityID: "", RepoID: "repo-1", EntityType: "Function", EntityName: "NoID"},                                             // missing handle
			{EntityID: "dash-1", RepoID: "repo-1", EntityType: "DashboardAsset"},                                                     // excluded kind
		},
		ContentFiles: []searchdocs.ContentFile{
			{RepoID: "repo-1", RelativePath: "ok.go", Content: "package ok"},
		},
	}

	projection := ProjectSearchDocuments(input)

	if got, want := projection.Summary.Included, 1; got != want {
		t.Fatalf("included = %d, want %d", got, want)
	}
	if got := projection.Summary.SkippedByReason[searchdocs.ReasonSensitiveContext]; got != 1 {
		t.Errorf("sensitive skipped = %d, want 1", got)
	}
	if got := projection.Summary.SkippedByReason[searchdocs.ReasonMissingStableHandle]; got != 1 {
		t.Errorf("missing-handle skipped = %d, want 1", got)
	}
	if got := projection.Summary.SkippedByReason[searchdocs.ReasonExcludedSourceKind]; got != 1 {
		t.Errorf("excluded-kind skipped = %d, want 1", got)
	}
	// No included document may carry sensitive context.
	for _, doc := range projection.Documents {
		if doc.ID == "searchdoc:content_entity:secret-1" {
			t.Fatalf("sensitive document leaked into projection: %q", doc.ID)
		}
	}
}

// BenchmarkProjectSearchDocuments measures curation throughput for a bounded
// per-generation source set, providing performance evidence for the new
// projection stage. The core is pure CPU work with no I/O.
func BenchmarkProjectSearchDocuments(b *testing.B) {
	const lane = 500
	entities := make([]searchdocs.ContentEntity, 0, lane)
	files := make([]searchdocs.ContentFile, 0, lane)
	for i := 0; i < lane; i++ {
		id := "e-" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		entities = append(entities, searchdocs.ContentEntity{
			EntityID: id, RepoID: "repo-1", EntityType: "Function", EntityName: "Fn", RelativePath: "f.go", SourceCache: "func Fn() {}",
		})
		files = append(files, searchdocs.ContentFile{
			RepoID: "repo-1", RelativePath: "f" + id + ".go", Language: "go", Content: "package main",
		})
	}
	input := SearchDocumentProjectionInput{ContentEntities: entities, ContentFiles: files}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		projection := ProjectSearchDocuments(input)
		if projection.Summary.Considered != 2*lane {
			b.Fatalf("considered = %d, want %d", projection.Summary.Considered, 2*lane)
		}
	}
}

func TestProjectSearchDocumentsEmptyInput(t *testing.T) {
	t.Parallel()

	projection := ProjectSearchDocuments(SearchDocumentProjectionInput{})
	if projection.Summary.Considered != 0 || projection.Summary.Included != 0 {
		t.Fatalf("empty input produced counts: %+v", projection.Summary)
	}
	if len(projection.Documents) != 0 {
		t.Fatalf("empty input produced %d documents", len(projection.Documents))
	}
}
