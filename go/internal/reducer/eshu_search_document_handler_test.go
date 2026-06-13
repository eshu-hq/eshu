package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

type fakeSearchDocLoader struct {
	input SearchDocumentProjectionInput
	err   error
	calls int
}

func (f *fakeSearchDocLoader) LoadSearchDocumentSources(_ context.Context, _ string, _ string) (SearchDocumentProjectionInput, error) {
	f.calls++
	return f.input, f.err
}

type capturingSearchDocWriter struct {
	write  EshuSearchDocumentWrite
	result EshuSearchDocumentWriteResult
	err    error
	calls  int
}

func (w *capturingSearchDocWriter) WriteEshuSearchDocuments(_ context.Context, write EshuSearchDocumentWrite) (EshuSearchDocumentWriteResult, error) {
	w.calls++
	w.write = write
	return w.result, w.err
}

func searchDocIntent() Intent {
	return Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Domain:       DomainEshuSearchDocument,
	}
}

func TestEshuSearchDocumentHandlerProjectsAndWrites(t *testing.T) {
	t.Parallel()

	loader := &fakeSearchDocLoader{input: SearchDocumentProjectionInput{
		ContentEntities: []searchdocs.ContentEntity{
			{EntityID: "e-1", RepoID: "repo-1", EntityType: "Function", EntityName: "Handle", SourceCache: "func Handle(){}"},
			{EntityID: "secret", RepoID: "repo-1", EntityType: "Const", SourceCache: "password = x"}, // dropped
		},
		ContentFiles: []searchdocs.ContentFile{
			{RepoID: "repo-1", RelativePath: "main.go", Content: "package main"},
		},
	}}
	writer := &capturingSearchDocWriter{result: EshuSearchDocumentWriteResult{CanonicalWrites: 2, Retired: 1}}
	handler := EshuSearchDocumentHandler{Loader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if loader.calls != 1 || writer.calls != 1 {
		t.Fatalf("loader calls=%d writer calls=%d, want 1/1", loader.calls, writer.calls)
	}
	if result.Status != ResultStatusSucceeded {
		t.Errorf("status = %v, want succeeded", result.Status)
	}
	if result.CanonicalWrites != 2 {
		t.Errorf("canonical writes = %d, want 2", result.CanonicalWrites)
	}
	// The writer must receive only the curated (non-sensitive) documents.
	if got := len(writer.write.Documents); got != 2 {
		t.Fatalf("written documents = %d, want 2 (sensitive dropped)", got)
	}
	for _, doc := range writer.write.Documents {
		if strings.Contains(doc.ID, "secret") {
			t.Fatalf("sensitive document leaked to writer: %q", doc.ID)
		}
	}
	if !strings.Contains(result.EvidenceSummary, "considered=3") || !strings.Contains(result.EvidenceSummary, "included=2") {
		t.Errorf("evidence summary = %q", result.EvidenceSummary)
	}
	if writer.write.ScopeID != "scope-1" || writer.write.GenerationID != "gen-1" {
		t.Errorf("write scope/generation = %q/%q", writer.write.ScopeID, writer.write.GenerationID)
	}
}

func TestEshuSearchDocumentHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := EshuSearchDocumentHandler{Loader: &fakeSearchDocLoader{}, Writer: &capturingSearchDocWriter{}}
	intent := searchDocIntent()
	intent.Domain = DomainWorkloadIdentity
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for wrong domain")
	}
}

func TestEshuSearchDocumentHandlerPropagatesLoaderError(t *testing.T) {
	t.Parallel()

	handler := EshuSearchDocumentHandler{Loader: &fakeSearchDocLoader{err: errors.New("load boom")}, Writer: &capturingSearchDocWriter{}}
	if _, err := handler.Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected loader error to propagate")
	}
}

func TestEshuSearchDocumentHandlerPropagatesWriterError(t *testing.T) {
	t.Parallel()

	handler := EshuSearchDocumentHandler{Loader: &fakeSearchDocLoader{}, Writer: &capturingSearchDocWriter{err: errors.New("write boom")}}
	if _, err := handler.Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected writer error to propagate")
	}
}

func TestEshuSearchDocumentHandlerRequiresDependencies(t *testing.T) {
	t.Parallel()

	if _, err := (EshuSearchDocumentHandler{}).Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected error when loader and writer are nil")
	}
}
