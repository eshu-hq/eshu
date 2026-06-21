package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// fakePagedSearchDocLoader streams a fixed list of pages into the handler. It
// records how many times each page was delivered so tests can assert the
// handler consumes the loader in bounded increments rather than one shot.
type fakePagedSearchDocLoader struct {
	pages []SearchDocumentProjectionInput
	err   error
	calls int
}

func (f *fakePagedSearchDocLoader) StreamSearchDocumentSources(
	_ context.Context,
	_ string,
	_ string,
	page func(SearchDocumentProjectionInput) error,
) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	for _, p := range f.pages {
		if err := page(p); err != nil {
			return err
		}
	}
	return nil
}

// capturingSearchDocWriter records every InsertPage and the single Finalize so
// tests can prove the handler writes incrementally and retires exactly once.
type capturingSearchDocWriter struct {
	begins        int
	insertedPages [][]searchdocs.Document
	finalizeCalls int
	beginErr      error
	insertErr     error
	finalizeErr   error
	result        EshuSearchDocumentWriteResult
	beginArg      EshuSearchDocumentWriteBegin
}

func (w *capturingSearchDocWriter) BeginEshuSearchDocumentWrite(
	_ context.Context,
	begin EshuSearchDocumentWriteBegin,
) (SearchDocumentWriteSession, error) {
	w.begins++
	w.beginArg = begin
	if w.beginErr != nil {
		return nil, w.beginErr
	}
	return &capturingSearchDocWriteSession{parent: w}, nil
}

type capturingSearchDocWriteSession struct{ parent *capturingSearchDocWriter }

func (s *capturingSearchDocWriteSession) InsertPage(_ context.Context, docs []searchdocs.Document) error {
	if s.parent.insertErr != nil {
		return s.parent.insertErr
	}
	// Copy so later page reuse by the caller cannot mutate captured state.
	captured := make([]searchdocs.Document, len(docs))
	copy(captured, docs)
	s.parent.insertedPages = append(s.parent.insertedPages, captured)
	return nil
}

func (s *capturingSearchDocWriteSession) Finalize(_ context.Context) (EshuSearchDocumentWriteResult, error) {
	s.parent.finalizeCalls++
	if s.parent.finalizeErr != nil {
		return EshuSearchDocumentWriteResult{}, s.parent.finalizeErr
	}
	return s.parent.result, nil
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

	loader := &fakePagedSearchDocLoader{pages: []SearchDocumentProjectionInput{{
		ContentEntities: []searchdocs.ContentEntity{
			{EntityID: "e-1", RepoID: "repo-1", EntityType: "Function", EntityName: "Handle", SourceCache: "func Handle(){}"},
			{EntityID: "secret", RepoID: "repo-1", EntityType: "Const", SourceCache: "password = x"}, // dropped
		},
		ContentFiles: []searchdocs.ContentFile{
			{RepoID: "repo-1", RelativePath: "main.go", Content: "package main"},
		},
	}}}
	writer := &capturingSearchDocWriter{result: EshuSearchDocumentWriteResult{CanonicalWrites: 2, Retired: 1}}
	handler := EshuSearchDocumentHandler{Loader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if loader.calls != 1 || writer.begins != 1 || writer.finalizeCalls != 1 {
		t.Fatalf("loader=%d begins=%d finalize=%d, want 1/1/1", loader.calls, writer.begins, writer.finalizeCalls)
	}
	if len(writer.insertedPages) != 1 {
		t.Fatalf("inserted pages = %d, want 1", len(writer.insertedPages))
	}
	// The writer must receive only the curated (non-sensitive) documents.
	if got := len(writer.insertedPages[0]); got != 2 {
		t.Fatalf("written documents = %d, want 2 (sensitive dropped)", got)
	}
	if result.Status != ResultStatusSucceeded {
		t.Errorf("status = %v, want succeeded", result.Status)
	}
	if result.CanonicalWrites != 2 {
		t.Errorf("canonical writes = %d, want 2", result.CanonicalWrites)
	}
	for _, doc := range writer.insertedPages[0] {
		if strings.Contains(doc.ID, "secret") {
			t.Fatalf("sensitive document leaked to writer: %q", doc.ID)
		}
	}
	if !strings.Contains(result.EvidenceSummary, "considered=3") || !strings.Contains(result.EvidenceSummary, "included=2") {
		t.Errorf("evidence summary = %q", result.EvidenceSummary)
	}
	if writer.beginArg.ScopeID != "scope-1" || writer.beginArg.GenerationID != "gen-1" {
		t.Errorf("begin scope/generation = %q/%q", writer.beginArg.ScopeID, writer.beginArg.GenerationID)
	}
}

// TestEshuSearchDocumentHandlerStreamsBoundedPages proves the handler projects
// and writes one page at a time (insert called per loader page) and runs the
// authoritative retire exactly once via Finalize, with aggregated evidence
// counts equal to the sum across pages. This is the #3440 streaming regression:
// an unbounded single-shot load+write would insert once with everything.
func TestEshuSearchDocumentHandlerStreamsBoundedPages(t *testing.T) {
	t.Parallel()

	loader := &fakePagedSearchDocLoader{pages: []SearchDocumentProjectionInput{
		{ContentEntities: []searchdocs.ContentEntity{
			{EntityID: "e-1", RepoID: "repo-1", EntityType: "Function", EntityName: "A", SourceCache: "func A(){}"},
		}},
		{ContentEntities: []searchdocs.ContentEntity{
			{EntityID: "e-2", RepoID: "repo-1", EntityType: "Function", EntityName: "B", SourceCache: "func B(){}"},
			{EntityID: "secret", RepoID: "repo-1", EntityType: "Const", SourceCache: "password = x"}, // dropped
		}},
		{ContentFiles: []searchdocs.ContentFile{
			{RepoID: "repo-1", RelativePath: "main.go", Content: "package main"},
		}},
	}}
	writer := &capturingSearchDocWriter{result: EshuSearchDocumentWriteResult{CanonicalWrites: 3, Retired: 4}}
	handler := EshuSearchDocumentHandler{Loader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), searchDocIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if writer.begins != 1 || writer.finalizeCalls != 1 {
		t.Fatalf("begins=%d finalize=%d, want 1 begin and exactly one finalize", writer.begins, writer.finalizeCalls)
	}
	if len(writer.insertedPages) != 3 {
		t.Fatalf("insert page calls = %d, want 3 (one per loader page)", len(writer.insertedPages))
	}
	// Page sizes prove incremental projection: 1 doc, 1 doc (secret dropped), 1 doc.
	wantSizes := []int{1, 1, 1}
	for i, page := range writer.insertedPages {
		if len(page) != wantSizes[i] {
			t.Fatalf("inserted page %d size = %d, want %d", i, len(page), wantSizes[i])
		}
	}
	// Aggregated evidence: considered across all pages = 1 + 2 + 1 = 4, included = 3.
	if !strings.Contains(result.EvidenceSummary, "considered=4") || !strings.Contains(result.EvidenceSummary, "included=3") {
		t.Errorf("aggregated evidence summary = %q, want considered=4 included=3", result.EvidenceSummary)
	}
	if !strings.Contains(result.EvidenceSummary, "written=3") || !strings.Contains(result.EvidenceSummary, "retired=4") {
		t.Errorf("evidence summary missing finalize counts: %q", result.EvidenceSummary)
	}
	if result.CanonicalWrites != 3 {
		t.Errorf("canonical writes = %d, want 3 (from finalize)", result.CanonicalWrites)
	}
}

func TestEshuSearchDocumentHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := EshuSearchDocumentHandler{Loader: &fakePagedSearchDocLoader{}, Writer: &capturingSearchDocWriter{}}
	intent := searchDocIntent()
	intent.Domain = DomainWorkloadIdentity
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for wrong domain")
	}
}

func TestEshuSearchDocumentHandlerPropagatesLoaderError(t *testing.T) {
	t.Parallel()

	handler := EshuSearchDocumentHandler{Loader: &fakePagedSearchDocLoader{err: errors.New("load boom")}, Writer: &capturingSearchDocWriter{}}
	if _, err := handler.Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected loader error to propagate")
	}
}

func TestEshuSearchDocumentHandlerPropagatesWriterError(t *testing.T) {
	t.Parallel()

	loader := &fakePagedSearchDocLoader{pages: []SearchDocumentProjectionInput{{
		ContentFiles: []searchdocs.ContentFile{{RepoID: "repo-1", RelativePath: "main.go", Content: "package main"}},
	}}}
	handler := EshuSearchDocumentHandler{Loader: loader, Writer: &capturingSearchDocWriter{insertErr: errors.New("write boom")}}
	if _, err := handler.Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected writer error to propagate")
	}
}

func TestEshuSearchDocumentHandlerPropagatesFinalizeError(t *testing.T) {
	t.Parallel()

	handler := EshuSearchDocumentHandler{
		Loader: &fakePagedSearchDocLoader{},
		Writer: &capturingSearchDocWriter{finalizeErr: errors.New("finalize boom")},
	}
	if _, err := handler.Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected finalize error to propagate")
	}
}

func TestEshuSearchDocumentHandlerRequiresDependencies(t *testing.T) {
	t.Parallel()

	if _, err := (EshuSearchDocumentHandler{}).Handle(context.Background(), searchDocIntent()); err == nil {
		t.Fatal("expected error when loader and writer are nil")
	}
}
