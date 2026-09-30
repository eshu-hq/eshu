// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/eshusearch"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

type generationCheckPageLoader struct{}

func (generationCheckPageLoader) StreamSearchDocumentSources(
	_ context.Context, _, _ string, page func(eshusearch.SearchDocumentProjectionInput) error,
) error {
	return page(eshusearch.SearchDocumentProjectionInput{ContentFiles: []searchdocs.ContentFile{
		{RepoID: "repo-1", RelativePath: "main.go", Content: "package main"},
	}})
}

type generationCheckWriter struct{}

func (generationCheckWriter) BeginEshuSearchDocumentWrite(
	context.Context, eshusearch.EshuSearchDocumentWriteBegin,
) (eshusearch.SearchDocumentWriteSession, error) {
	return generationCheckSession{}, nil
}

type generationCheckSession struct{}

func (generationCheckSession) InsertPage(context.Context, []searchdocs.Document) error { return nil }

func (generationCheckSession) Finalize(context.Context) (eshusearch.EshuSearchDocumentWriteResult, error) {
	return eshusearch.EshuSearchDocumentWriteResult{}, nil
}
func (generationCheckSession) Cancel(context.Context) error { return nil }

// TestSearchDocumentDomainHandlerReceivesGenerationCheck proves the registry
// hands SearchDocumentHandlers.EshuSearchDocumentGenerationCheck to the handler
// it registers (#7458): the check runs before a page and before Finalize, and
// leaving it unset is a Handle error rather than a silently disabled fence.
func TestSearchDocumentDomainHandlerReceivesGenerationCheck(t *testing.T) {
	t.Parallel()

	calls := 0
	registry, err := NewDefaultRegistry(DefaultHandlers{SearchDocumentHandlers: SearchDocumentHandlers{
		EshuSearchDocumentSourceLoader: generationCheckPageLoader{},
		EshuSearchDocumentWriter:       generationCheckWriter{},
		EshuSearchDocumentGenerationCheck: func(context.Context, string, string) (bool, error) {
			calls++
			return true, nil
		},
	}})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v", err)
	}
	definition, ok := registry.Definition(eshusearch.DomainEshuSearchDocument)
	if !ok {
		t.Fatal("eshu search document domain not registered")
	}
	intent := Intent{IntentID: "i", ScopeID: "scope-1", GenerationID: "gen-1", SourceSystem: "git", Domain: eshusearch.DomainEshuSearchDocument}
	if _, err := definition.Handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("generation check calls = %d, want 2 (one page plus the pre-Finalize check)", calls)
	}

	unset, err := NewDefaultRegistry(DefaultHandlers{SearchDocumentHandlers: SearchDocumentHandlers{
		EshuSearchDocumentSourceLoader: generationCheckPageLoader{},
		EshuSearchDocumentWriter:       generationCheckWriter{},
	}})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() without check error = %v", err)
	}
	definition, _ = unset.Definition(eshusearch.DomainEshuSearchDocument)
	if _, err := definition.Handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() with no generation check succeeded, want a construction error")
	}
}
