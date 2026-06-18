package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

type stubValueFlowSummaryReader struct{ snap summary.Snapshot }

func (r stubValueFlowSummaryReader) LoadSnapshot(context.Context) (summary.Snapshot, error) {
	return r.snap, nil
}

type stubValueFlowSourceReader struct{ sources []interproc.Source }

func (r stubValueFlowSourceReader) LoadSources(context.Context) ([]interproc.Source, error) {
	return r.sources, nil
}

type stubValueFlowGraphIDReader struct{ ids map[summary.FunctionID]string }

func (r stubValueFlowGraphIDReader) LoadGraphIDs(context.Context) (map[summary.FunctionID]string, error) {
	return r.ids, nil
}

const (
	repoAHandle = summary.FunctionID("repo-a\x1fpkg\x1f\x1fhandle")
	repoBQuery  = summary.FunctionID("repo-b\x1fpkg\x1f\x1fquery")
)

// TestCodeValueFlowFixpointProjectsCrossRepoEdge proves the headline cross-repo
// composition: a request source entering handle in repo A flows through the call
// into query in repo B, reaching a SQL sink, and is projected as a single
// TAINT_FLOWS_TO edge between the two repos' Function uids.
func TestCodeValueFlowFixpointProjectsCrossRepoEdge(t *testing.T) {
	t.Parallel()

	snap := summary.Snapshot{Functions: []summary.SnapshotFunction{
		{ID: repoAHandle, Version: "v1", Effects: summary.Effects{
			ParamToCallArg: []summary.CallArgFlow{{Callee: repoBQuery, Param: 0, Arg: 0}},
		}},
		{ID: repoBQuery, Version: "v1", Effects: summary.Effects{
			ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}},
		}},
	}}
	sources := []interproc.Source{{
		Port: interproc.Port{Func: interproc.FunctionID(repoAHandle), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0}},
		Kind: "http_request",
	}}
	graphIDs := map[summary.FunctionID]string{
		repoAHandle: "uid-handle",
		repoBQuery:  "uid-query",
	}
	writer := &recordingCodeInterprocEvidenceWriter{}
	handler := CodeValueFlowFixpointHandler{
		SummaryReader: stubValueFlowSummaryReader{snap: snap},
		SourceReader:  stubValueFlowSourceReader{sources: sources},
		GraphIDReader: stubValueFlowGraphIDReader{ids: graphIDs},
		Writer:        writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID: "i-1", ScopeID: "scope-b", GenerationID: "gen-1", Domain: DomainCodeValueFlowFixpoint,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if writer.retractCalls != 1 || writer.retractEvidence != codeValueFlowFixpointSource {
		t.Fatalf("expected one scoped retract for the fixpoint source, got %d (%q)", writer.retractCalls, writer.retractEvidence)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 {
		t.Fatalf("expected 1 cross-repo edge written, got %d calls / %d rows", writer.writeCalls, len(writer.writtenRows))
	}
	row := writer.writtenRows[0]
	if row["source_function_uid"] != "uid-handle" || row["sink_function_uid"] != "uid-query" || row["sink_kind"] != "sql" {
		t.Fatalf("cross-repo edge endpoints wrong: %+v", row)
	}
	if result.CanonicalWrites != 1 || result.Status != ResultStatusSucceeded {
		t.Fatalf("result = %+v, want 1 canonical write succeeded", result)
	}
}

// TestCodeValueFlowFixpointDropsUnresolvedUID proves a finding whose endpoint has
// no resolved graph uid draws no edge (no phantom node).
func TestCodeValueFlowFixpointDropsUnresolvedUID(t *testing.T) {
	t.Parallel()

	snap := summary.Snapshot{Functions: []summary.SnapshotFunction{
		{ID: repoAHandle, Version: "v1", Effects: summary.Effects{
			ParamToCallArg: []summary.CallArgFlow{{Callee: repoBQuery, Param: 0, Arg: 0}},
		}},
		{ID: repoBQuery, Version: "v1", Effects: summary.Effects{
			ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}},
		}},
	}}
	sources := []interproc.Source{{
		Port: interproc.Port{Func: interproc.FunctionID(repoAHandle), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0}},
		Kind: "http_request",
	}}
	// Only the source uid resolves; the sink uid is missing.
	graphIDs := map[summary.FunctionID]string{repoAHandle: "uid-handle"}
	writer := &recordingCodeInterprocEvidenceWriter{}
	handler := CodeValueFlowFixpointHandler{
		SummaryReader: stubValueFlowSummaryReader{snap: snap},
		SourceReader:  stubValueFlowSourceReader{sources: sources},
		GraphIDReader: stubValueFlowGraphIDReader{ids: graphIDs},
		Writer:        writer,
	}
	if _, err := handler.Handle(context.Background(), Intent{ScopeID: "scope-b", GenerationID: "gen-1", Domain: DomainCodeValueFlowFixpoint}); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("unresolved sink uid must draw no edge, got %d writes", writer.writeCalls)
	}
}

// TestCodeValueFlowFixpointRejectsWrongDomain proves the handler refuses another
// domain's intent.
func TestCodeValueFlowFixpointRejectsWrongDomain(t *testing.T) {
	t.Parallel()
	handler := CodeValueFlowFixpointHandler{
		SummaryReader: stubValueFlowSummaryReader{},
		SourceReader:  stubValueFlowSourceReader{},
		GraphIDReader: stubValueFlowGraphIDReader{},
		Writer:        &recordingCodeInterprocEvidenceWriter{},
	}
	if _, err := handler.Handle(context.Background(), Intent{Domain: DomainDataLineage}); err == nil {
		t.Fatal("Handle accepted a non-fixpoint domain")
	}
}

// TestNewDefaultRegistryAcceptsCodeValueFlowFixpointWhenWired proves the fixpoint
// domain registers (ownership-valid) when its readers and the evidence writer are
// wired, and is omitted otherwise.
func TestNewDefaultRegistryAcceptsCodeValueFlowFixpointWhenWired(t *testing.T) {
	t.Parallel()
	omitted, err := NewDefaultRegistry(DefaultHandlers{
		CodeValueFlowSummaryReader: stubValueFlowSummaryReader{},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry error: %v", err)
	}
	if _, ok := omitted.Definition(DomainCodeValueFlowFixpoint); ok {
		t.Fatal("fixpoint registered without all readers + writer")
	}
	wired, err := NewDefaultRegistry(DefaultHandlers{
		CodeValueFlowSummaryReader:  stubValueFlowSummaryReader{},
		CodeValueFlowSourceReader:   stubValueFlowSourceReader{},
		CodeValueFlowGraphIDReader:  stubValueFlowGraphIDReader{},
		CodeInterprocEvidenceWriter: &recordingCodeInterprocEvidenceWriter{},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry error: %v", err)
	}
	def, ok := wired.Definition(DomainCodeValueFlowFixpoint)
	if !ok {
		t.Fatal("fixpoint not registered when fully wired")
	}
	if _, ok := def.Handler.(CodeValueFlowFixpointHandler); !ok {
		t.Fatalf("handler type = %T", def.Handler)
	}
}
