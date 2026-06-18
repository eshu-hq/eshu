package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

type stubCodeFunctionSummaryLoader struct {
	effects map[summary.FunctionID]summary.Effects
}

func (l stubCodeFunctionSummaryLoader) LoadCodeFunctionSummaryEffects(
	context.Context, string, string,
) (map[summary.FunctionID]summary.Effects, error) {
	return l.effects, nil
}

type recordingCodeFunctionSummaryWriter struct {
	calls     int
	previous  summary.Snapshot
	snapshot  summary.Snapshot
	updatedAt time.Time
}

func (w *recordingCodeFunctionSummaryWriter) LoadSnapshot(context.Context) (summary.Snapshot, error) {
	return w.previous, nil
}

func (w *recordingCodeFunctionSummaryWriter) UpsertSnapshot(_ context.Context, snap summary.Snapshot, updatedAt time.Time) error {
	w.calls++
	w.snapshot = snap
	w.updatedAt = updatedAt
	return nil
}

func codeFunctionSummaryIntent() Intent {
	return Intent{
		IntentID:     "intent-summary-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeFunctionSummary,
	}
}

// TestCodeFunctionSummaryHandlerPersistsVersionedSnapshot proves the handler
// loads the raw Effects, recomputes content versions through summary.Store, and
// upserts a versioned snapshot.
func TestCodeFunctionSummaryHandlerPersistsVersionedSnapshot(t *testing.T) {
	t.Parallel()

	loader := stubCodeFunctionSummaryLoader{effects: map[summary.FunctionID]summary.Effects{
		summary.FunctionID("repo-1\x1fpkg\x1f\x1fview"): {SourceToReturn: []string{"http_request"}},
		summary.FunctionID("repo-1\x1fpkg\x1f\x1fquery"): {ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}}},
	}}
	writer := &recordingCodeFunctionSummaryWriter{}
	at := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: loader,
		Writer: writer,
		Now:    func() time.Time { return at },
	}

	result, err := handler.Handle(context.Background(), codeFunctionSummaryIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.calls != 1 || len(writer.snapshot.Functions) != 2 {
		t.Fatalf("snapshot not persisted: calls=%d functions=%d", writer.calls, len(writer.snapshot.Functions))
	}
	if writer.updatedAt != at {
		t.Fatalf("updatedAt = %v, want %v", writer.updatedAt, at)
	}
	for _, fn := range writer.snapshot.Functions {
		if fn.Version == "" {
			t.Fatalf("function %q persisted without a content version", fn.ID)
		}
	}
	if result.CanonicalWrites != 2 || result.Status != ResultStatusSucceeded {
		t.Fatalf("result = %+v, want 2 canonical writes succeeded", result)
	}
}

// TestCodeFunctionSummaryHandlerRebuildsDeltaFromDurableSnapshot proves a
// delta generation keeps unchanged callees in the versioning store before
// persisting the updated caller snapshot.
func TestCodeFunctionSummaryHandlerRebuildsDeltaFromDurableSnapshot(t *testing.T) {
	t.Parallel()

	callerID := summary.FunctionID("repo-1\x1fpkg\x1f\x1fhandler")
	calleeID := summary.FunctionID("repo-1\x1fpkg\x1f\x1fvalidate")
	previousStore := summary.NewStore()
	previousStore.Upsert(map[summary.FunctionID]summary.Effects{
		calleeID: {ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "authz"}}},
		callerID: {ParamToCallArg: []summary.CallArgFlow{{Callee: calleeID, Param: 0, Arg: 0}}},
	})
	writer := &recordingCodeFunctionSummaryWriter{previous: previousStore.Snapshot()}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{effects: map[summary.FunctionID]summary.Effects{
			callerID: {
				ParamToCallArg: []summary.CallArgFlow{{Callee: calleeID, Param: 0, Arg: 0}},
				SourceToReturn: []string{"http_request"},
			},
		}},
		Writer: writer,
	}

	if _, err := handler.Handle(context.Background(), codeFunctionSummaryIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(writer.snapshot.Functions) != 2 {
		t.Fatalf("snapshot functions = %d, want previous callee plus updated caller", len(writer.snapshot.Functions))
	}
	if _, ok := summary.Load(writer.snapshot).Version(calleeID); !ok {
		t.Fatalf("snapshot dropped unchanged callee %q", calleeID)
	}
}

// TestCodeFunctionSummaryHandlerRejectsWrongDomain proves the handler refuses an
// intent for another domain.
func TestCodeFunctionSummaryHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{},
		Writer: &recordingCodeFunctionSummaryWriter{},
	}
	intent := codeFunctionSummaryIntent()
	intent.Domain = DomainDataLineage
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle accepted a non-summary domain")
	}
}

func TestImplementedDefaultDomainDefinitionsOmitsCodeFunctionSummaryWithoutWriter(t *testing.T) {
	t.Parallel()

	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeFunctionSummaryLoader: stubCodeFunctionSummaryLoader{},
	})
	for _, def := range definitions {
		if def.Domain == DomainCodeFunctionSummary {
			t.Fatalf("code_function_summary registered without writer; want omitted")
		}
	}
}

func TestNewDefaultRegistryAcceptsCodeFunctionSummaryWhenWired(t *testing.T) {
	t.Parallel()

	registry, err := NewDefaultRegistry(DefaultHandlers{
		CodeFunctionSummaryLoader: stubCodeFunctionSummaryLoader{},
		CodeFunctionSummaryWriter: &recordingCodeFunctionSummaryWriter{},
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry returned error with summary wired: %v", err)
	}
	def, ok := registry.Definition(DomainCodeFunctionSummary)
	if !ok {
		t.Fatal("code_function_summary not registered when wired")
	}
	if _, ok := def.Handler.(CodeFunctionSummaryMaterializationHandler); !ok {
		t.Fatalf("handler type = %T, want CodeFunctionSummaryMaterializationHandler", def.Handler)
	}
}
