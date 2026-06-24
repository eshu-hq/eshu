// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

func TestCodeFunctionSummaryHandlerReplacesFullSnapshotPruningMissingFunctions(t *testing.T) {
	t.Parallel()

	keepID := summary.FunctionID("repo-1\x1fpkg\x1f\x1fkeep")
	staleID := summary.FunctionID("repo-1\x1fpkg\x1f\x1fstale")
	otherRepoID := summary.FunctionID("repo-2\x1fpkg\x1f\x1fkeep")
	previous := summary.NewStore()
	previous.Upsert(map[summary.FunctionID]summary.Effects{
		keepID:      {ParamToReturn: []int{0}},
		staleID:     {ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}}},
		otherRepoID: {SourceToReturn: []string{"http_request"}},
	})
	writer := &recordingCodeFunctionSummaryWriter{previous: previous.Snapshot()}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{effects: map[summary.FunctionID]summary.Effects{
			keepID: {ParamToReturn: []int{0}},
		}},
		Writer: writer,
	}
	intent := codeFunctionSummaryIntent()
	intent.Payload = map[string]any{"repo_id": "repo-1", "full_snapshot": true}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.replaceCalls != 1 || writer.upsertCalls != 0 {
		t.Fatalf("writer calls = replace %d upsert %d, want replace only", writer.replaceCalls, writer.upsertCalls)
	}
	if writer.replaceRepo != "repo-1" {
		t.Fatalf("replace repo = %q, want repo-1", writer.replaceRepo)
	}
	got := summary.Load(writer.replaceSnapshot)
	if _, ok := got.Version(keepID); !ok {
		t.Fatalf("replace snapshot missing current function %q", keepID)
	}
	if _, ok := got.Version(staleID); ok {
		t.Fatalf("replace snapshot kept stale same-repo function %q", staleID)
	}
	if _, ok := got.Version(otherRepoID); ok {
		t.Fatalf("replace snapshot included unrelated repo function %q", otherRepoID)
	}
}

func TestCodeFunctionSummaryHandlerPreservesDeltaNoDeleteBehavior(t *testing.T) {
	t.Parallel()

	changedID := summary.FunctionID("repo-1\x1fpkg\x1f\x1fchanged")
	unchangedID := summary.FunctionID("repo-1\x1fpkg\x1f\x1funchanged")
	previous := summary.NewStore()
	previous.Upsert(map[summary.FunctionID]summary.Effects{
		changedID:   {ParamToReturn: []int{0}},
		unchangedID: {ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}}},
	})
	writer := &recordingCodeFunctionSummaryWriter{previous: previous.Snapshot()}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{effects: map[summary.FunctionID]summary.Effects{
			changedID: {ParamToReturn: []int{1}},
		}},
		Writer: writer,
	}
	intent := codeFunctionSummaryIntent()
	intent.Payload = map[string]any{"repo_id": "repo-1"}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.upsertCalls != 1 || writer.replaceCalls != 0 {
		t.Fatalf("writer calls = upsert %d replace %d, want upsert only", writer.upsertCalls, writer.replaceCalls)
	}
	if _, ok := summary.Load(writer.snapshot).Version(unchangedID); !ok {
		t.Fatalf("delta upsert dropped unchanged function %q", unchangedID)
	}
}

func TestCodeFunctionSummaryHandlerReplacesEmptyFullSnapshot(t *testing.T) {
	t.Parallel()

	previous := summary.NewStore()
	previous.Upsert(map[summary.FunctionID]summary.Effects{
		"repo-1\x1fpkg\x1f\x1fstale": {ParamToReturn: []int{0}},
	})
	writer := &recordingCodeFunctionSummaryWriter{previous: previous.Snapshot()}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader: stubCodeFunctionSummaryLoader{},
		Writer: writer,
		Now:    func() time.Time { return time.Date(2026, time.June, 18, 2, 0, 0, 0, time.UTC) },
	}
	intent := codeFunctionSummaryIntent()
	intent.Payload = map[string]any{"repo_id": "repo-1", "full_snapshot": true}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.replaceCalls != 1 || len(writer.replaceSnapshot.Functions) != 0 {
		t.Fatalf("replace calls/snapshot = %d/%#v, want empty repo replacement", writer.replaceCalls, writer.replaceSnapshot)
	}
}

func TestCodeFunctionSummaryHandlerReplacesCompanionStoresOnEmptyFullSnapshot(t *testing.T) {
	t.Parallel()

	sourceWriter := &recordingCodeFunctionSourceWriter{}
	graphIDWriter := &recordingCodeFunctionGraphIDWriter{}
	handler := CodeFunctionSummaryMaterializationHandler{
		Loader:        stubCodeFunctionSummaryLoader{},
		Writer:        &recordingCodeFunctionSummaryWriter{},
		SourceLoader:  stubCodeFunctionSourceLoader{},
		SourceWriter:  sourceWriter,
		GraphIDLoader: stubCodeFunctionGraphIDLoader{},
		GraphIDWriter: graphIDWriter,
	}
	intent := codeFunctionSummaryIntent()
	intent.Payload = map[string]any{"repo_id": "repo-1", "full_snapshot": true}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if sourceWriter.calls != 1 || len(sourceWriter.repos) != 1 || sourceWriter.repos[0] != "repo-1" {
		t.Fatalf("source replacement calls = %+v, want one empty repo-1 replacement", sourceWriter)
	}
	if len(sourceWriter.sets) != 1 || len(sourceWriter.sets[0]) != 0 {
		t.Fatalf("source replacement set = %+v, want empty", sourceWriter.sets)
	}
	if graphIDWriter.calls != 1 || len(graphIDWriter.repos) != 1 || graphIDWriter.repos[0] != "repo-1" {
		t.Fatalf("graph-id replacement calls = %+v, want one empty repo-1 replacement", graphIDWriter)
	}
	if len(graphIDWriter.sets) != 1 || len(graphIDWriter.sets[0]) != 0 {
		t.Fatalf("graph-id replacement set = %+v, want empty", graphIDWriter.sets)
	}
}
