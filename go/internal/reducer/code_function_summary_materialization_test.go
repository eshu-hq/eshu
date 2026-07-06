// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// stubCodeFunctionSummaryLoader returns raw code_function_summary envelopes
// built from its effects/graphIDs fields; the handler decodes them through the
// typed contracts seam. The graphIDs field lets a test attach a graph_uid to a
// function id so the graph-id view (which reads the same facts) resolves it.
type stubCodeFunctionSummaryLoader struct {
	effects  map[summary.FunctionID]summary.Effects
	graphIDs map[summary.FunctionID]string
}

func (l stubCodeFunctionSummaryLoader) LoadCodeFunctionSummaryFacts(
	context.Context, string, string,
) ([]facts.Envelope, error) {
	return l.summaryEnvelopes(), nil
}

// summaryEnvelopes builds one code_function_summary envelope per FunctionID in
// effects (union'd with graphIDs so a graph-id-only function still emits a
// fact), carrying the reconstructed effect lists a real payload would hold.
func (l stubCodeFunctionSummaryLoader) summaryEnvelopes() []facts.Envelope {
	ids := make(map[summary.FunctionID]struct{})
	for id := range l.effects {
		ids[id] = struct{}{}
	}
	for id := range l.graphIDs {
		ids[id] = struct{}{}
	}
	envelopes := make([]facts.Envelope, 0, len(ids))
	for id := range ids {
		payload := map[string]any{"function_id": string(id)}
		if uid, ok := l.graphIDs[id]; ok {
			payload["graph_uid"] = uid
		}
		eff := l.effects[id]
		if len(eff.ParamToReturn) > 0 {
			ret := make([]any, 0, len(eff.ParamToReturn))
			for _, p := range eff.ParamToReturn {
				ret = append(ret, float64(p))
			}
			payload["param_to_return"] = ret
		}
		if len(eff.SourceToReturn) > 0 {
			src := make([]any, 0, len(eff.SourceToReturn))
			for _, s := range eff.SourceToReturn {
				src = append(src, s)
			}
			payload["source_to_return"] = src
		}
		if len(eff.ParamToSink) > 0 {
			sinks := make([]any, 0, len(eff.ParamToSink))
			for _, s := range eff.ParamToSink {
				sinks = append(sinks, map[string]any{"param": float64(s.Param), "sink_kind": s.SinkKind})
			}
			payload["param_to_sink"] = sinks
		}
		if len(eff.ParamToCallArg) > 0 {
			flows := make([]any, 0, len(eff.ParamToCallArg))
			for _, f := range eff.ParamToCallArg {
				flows = append(flows, map[string]any{"callee": string(f.Callee), "param": float64(f.Param), "arg": float64(f.Arg)})
			}
			payload["param_to_call_arg"] = flows
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "summary:" + string(id),
			FactKind: facts.CodeFunctionSummaryFactKind,
			Payload:  payload,
		})
	}
	return envelopes
}

type recordingCodeFunctionSummaryWriter struct {
	calls           int
	upsertCalls     int
	replaceCalls    int
	previous        summary.Snapshot
	snapshot        summary.Snapshot
	replaceSnapshot summary.Snapshot
	replaceRepo     string
	updatedAt       time.Time
}

func (w *recordingCodeFunctionSummaryWriter) LoadSnapshot(context.Context) (summary.Snapshot, error) {
	return w.previous, nil
}

func (w *recordingCodeFunctionSummaryWriter) UpsertSnapshot(_ context.Context, snap summary.Snapshot, updatedAt time.Time) error {
	w.calls++
	w.upsertCalls++
	w.snapshot = snap
	w.updatedAt = updatedAt
	return nil
}

func (w *recordingCodeFunctionSummaryWriter) ReplaceSnapshot(
	_ context.Context,
	repo string,
	snap summary.Snapshot,
	updatedAt time.Time,
) error {
	w.calls++
	w.replaceCalls++
	w.replaceRepo = repo
	w.replaceSnapshot = snap
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
		summary.FunctionID("repo-1\x1fpkg\x1f\x1fview"):  {SourceToReturn: []string{"http_request"}},
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
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeFunctionSummaryLoader: stubCodeFunctionSummaryLoader{},
		},
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
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			CodeFunctionSummaryLoader: stubCodeFunctionSummaryLoader{},
			CodeFunctionSummaryWriter: &recordingCodeFunctionSummaryWriter{},
		},
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

// stubCodeFunctionSourceLoader returns raw code_function_source envelopes built
// from its sources slice; the handler decodes them through the typed contracts
// seam.
type stubCodeFunctionSourceLoader struct {
	sources []interproc.Source
}

func (l stubCodeFunctionSourceLoader) LoadCodeFunctionSourceFacts(context.Context, string, string) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(l.sources))
	for _, src := range l.sources {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "source:" + string(src.Port.Func),
			FactKind: facts.CodeFunctionSourceFactKind,
			Payload: map[string]any{
				"function_id": string(src.Port.Func),
				"kind":        src.Kind,
				"param_index": float64(src.Port.Slot.Index),
			},
		})
	}
	return envelopes, nil
}

type recordingCodeFunctionSourceWriter struct {
	calls int
	repos []string
	sets  [][]interproc.Source
}

func (w *recordingCodeFunctionSourceWriter) ReplaceSources(
	_ context.Context,
	repo string,
	sources []interproc.Source,
	_ time.Time,
) error {
	w.calls++
	w.repos = append(w.repos, repo)
	w.sets = append(w.sets, sources)
	return nil
}

// TestCodeFunctionSummaryHandlerPersistsSourcesWhenWired proves the handler also
// persists param-level sources when the optional source loader/writer are set.
func TestCodeFunctionSummaryHandlerPersistsSourcesWhenWired(t *testing.T) {
	t.Parallel()
	srcWriter := &recordingCodeFunctionSourceWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{},
		Writer: &recordingCodeFunctionSummaryWriter{},
		SourceLoader: stubCodeFunctionSourceLoader{sources: []interproc.Source{
			{Port: interproc.Port{Func: "repo-1\x1fpkg\x1f\x1fhandle", Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0}}, Kind: "http_request"},
		}},
		SourceWriter: srcWriter,
	}
	if _, err := handler.Handle(context.Background(), codeFunctionSummaryIntent()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if srcWriter.calls != 1 || len(srcWriter.sets) != 1 ||
		len(srcWriter.sets[0]) != 1 || srcWriter.sets[0][0].Kind != "http_request" {
		t.Fatalf("sources not persisted: %+v", srcWriter)
	}
}

// TestCodeFunctionSummaryHandlerReplacesEmptySourceSnapshot proves a generation
// with summaries but no current source ports still clears the repo's durable
// source snapshot.
func TestCodeFunctionSummaryHandlerReplacesEmptySourceSnapshot(t *testing.T) {
	t.Parallel()
	srcWriter := &recordingCodeFunctionSourceWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{effects: map[summary.FunctionID]summary.Effects{
			"repo-1\x1fpkg\x1f\x1fhandle": {ParamToReturn: []int{0}},
		}},
		Writer:       &recordingCodeFunctionSummaryWriter{},
		SourceLoader: stubCodeFunctionSourceLoader{},
		SourceWriter: srcWriter,
	}

	if _, err := handler.Handle(context.Background(), codeFunctionSummaryIntent()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if srcWriter.calls != 1 || len(srcWriter.repos) != 1 || srcWriter.repos[0] != "repo-1" {
		t.Fatalf("source replacement calls = %+v, want one repo-1 replacement", srcWriter)
	}
	if len(srcWriter.sets) != 1 || len(srcWriter.sets[0]) != 0 {
		t.Fatalf("source replacement set = %+v, want empty", srcWriter.sets)
	}
}

// TestCodeFunctionSummaryHandlerSkipsSourcesWhenUnwired proves source persistence
// is skipped (no panic) when the optional source loader/writer are absent.
func TestCodeFunctionSummaryHandlerSkipsSourcesWhenUnwired(t *testing.T) {
	t.Parallel()
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{},
		Writer: &recordingCodeFunctionSummaryWriter{},
	}
	if _, err := handler.Handle(context.Background(), codeFunctionSummaryIntent()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
}

// stubCodeFunctionGraphIDLoader returns raw code_function_summary envelopes
// carrying a graph_uid per FunctionID in ids; the handler derives the
// FunctionID->uid map from them (the same facts the summary loader reads). An
// empty-string uid is emitted as a present-but-empty graph_uid so the
// unresolved-id-clears-stale-mapping behavior is preserved.
type stubCodeFunctionGraphIDLoader struct {
	ids map[summary.FunctionID]string
}

func (l stubCodeFunctionGraphIDLoader) LoadCodeFunctionGraphIDFacts(context.Context, string, string) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(l.ids))
	for id, uid := range l.ids {
		payload := map[string]any{"function_id": string(id)}
		if uid != "" {
			payload["graph_uid"] = uid
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "summary:" + string(id),
			FactKind: facts.CodeFunctionSummaryFactKind,
			Payload:  payload,
		})
	}
	return envelopes, nil
}

type recordingCodeFunctionGraphIDWriter struct {
	calls int
	repos []string
	sets  []map[summary.FunctionID]string
}

func (w *recordingCodeFunctionGraphIDWriter) ReplaceGraphIDs(
	_ context.Context,
	repo string,
	ids map[summary.FunctionID]string,
	_ time.Time,
) error {
	w.calls++
	w.repos = append(w.repos, repo)
	w.sets = append(w.sets, ids)
	return nil
}

// TestCodeFunctionSummaryHandlerPersistsGraphIDsWhenWired proves the handler also
// persists the FunctionID->uid map when the optional graph-id loader/writer are set.
func TestCodeFunctionSummaryHandlerPersistsGraphIDsWhenWired(t *testing.T) {
	t.Parallel()
	gidWriter := &recordingCodeFunctionGraphIDWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader:        stubCodeFunctionSummaryLoader{},
		Writer:        &recordingCodeFunctionSummaryWriter{},
		GraphIDLoader: stubCodeFunctionGraphIDLoader{ids: map[summary.FunctionID]string{"repo-1\x1fpkg\x1f\x1fview": "uid-view"}},
		GraphIDWriter: gidWriter,
	}
	if _, err := handler.Handle(context.Background(), codeFunctionSummaryIntent()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if gidWriter.calls != 1 || len(gidWriter.sets) != 1 ||
		gidWriter.sets[0]["repo-1\x1fpkg\x1f\x1fview"] != "uid-view" {
		t.Fatalf("graph ids not persisted: %+v", gidWriter)
	}
}

// TestCodeFunctionSummaryHandlerReplacesUnresolvedGraphIDs proves unresolved
// graph uids still reach the writer so stale mappings are cleared.
func TestCodeFunctionSummaryHandlerReplacesUnresolvedGraphIDs(t *testing.T) {
	t.Parallel()
	gidWriter := &recordingCodeFunctionGraphIDWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{effects: map[summary.FunctionID]summary.Effects{
			"repo-1\x1fpkg\x1f\x1fview": {ParamToReturn: []int{0}},
		}},
		Writer: &recordingCodeFunctionSummaryWriter{},
		GraphIDLoader: stubCodeFunctionGraphIDLoader{ids: map[summary.FunctionID]string{
			"repo-1\x1fpkg\x1f\x1fview": "",
		}},
		GraphIDWriter: gidWriter,
	}

	if _, err := handler.Handle(context.Background(), codeFunctionSummaryIntent()); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if gidWriter.calls != 1 || len(gidWriter.repos) != 1 || gidWriter.repos[0] != "repo-1" {
		t.Fatalf("graph-id replacement calls = %+v, want one repo-1 replacement", gidWriter)
	}
	if len(gidWriter.sets) != 1 || gidWriter.sets[0]["repo-1\x1fpkg\x1f\x1fview"] != "" {
		t.Fatalf("graph-id replacement set = %+v, want unresolved id retained", gidWriter.sets)
	}
}

type recordingValueFlowFixpointProjector struct {
	calls        int
	scopeID      string
	generationID string
	result       ValueFlowFixpointProjectionResult
}

func (p *recordingValueFlowFixpointProjector) ProjectValueFlowFixpointEvidence(
	_ context.Context,
	scopeID string,
	generationID string,
) (ValueFlowFixpointProjectionResult, error) {
	p.calls++
	p.scopeID = scopeID
	p.generationID = generationID
	return p.result, nil
}

// TestCodeFunctionSummaryHandlerProjectsFixpointAfterPersistence proves the
// summary-driven TAINT_FLOWS_TO projection is ordered after the summary/source/id
// stores have been updated, not queued as a racing direct interproc intent.
func TestCodeFunctionSummaryHandlerProjectsFixpointAfterPersistence(t *testing.T) {
	t.Parallel()

	projector := &recordingValueFlowFixpointProjector{
		result: ValueFlowFixpointProjectionResult{FindingCount: 1, GraphRows: 1},
	}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader:                  stubCodeFunctionSummaryLoader{},
		Writer:                  &recordingCodeFunctionSummaryWriter{},
		ValueFlowFixpointWriter: projector,
	}

	result, err := handler.Handle(context.Background(), codeFunctionSummaryIntent())
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if projector.calls != 1 || projector.scopeID != "scope-1" || projector.generationID != "gen-1" {
		t.Fatalf("fixpoint projector not called with intent scope: %+v", projector)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want fixpoint write counted", result.CanonicalWrites)
	}
}
