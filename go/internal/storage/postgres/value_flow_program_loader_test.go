// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestValueFlowProgramInputLoaderSQLUsesActiveCompletedCodeCallsAndFunctionMetadata(t *testing.T) {
	for _, want := range []string{
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = acceptance.generation_id",
		"intent.projection_domain = 'code_calls'",
		"intent.completed_at IS NOT NULL",
		"pending_intent.completed_at IS NULL",
		"newer_generation.ingested_at > generation.ingested_at",
		"LEFT JOIN function_graph_ids AS caller_function",
		"LEFT JOIN function_graph_ids AS callee_function",
		"caller_function.uid = intent.payload->>'caller_entity_id'",
		"callee_function.function_id",
	} {
		if !strings.Contains(listValueFlowProgramCallEdgesSQL, want) &&
			!strings.Contains(listPendingValueFlowProgramInputsSQL, want) {
			t.Fatalf("value-flow program loader SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"package_import_path",
		"FROM content_entities",
		"source_kind",
		"source_label",
		"lang",
	} {
		if strings.Contains(listValueFlowProgramCallEdgesSQL, forbidden) ||
			strings.Contains(listValueFlowProgramSourcesSQL, forbidden) {
			t.Fatalf("value-flow program loader SQL must not contain %q", forbidden)
		}
	}
}

func TestValueFlowProgramInputLoaderBuildsBoundedProgramInput(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 18, 6, 45, 0, 0, time.UTC)
	caller := summary.NewFunctionID("repo-app", "example.com/app", "", "Handle")
	callee := summary.NewFunctionID("repo-lib", "example.com/lib", "", "Query")
	db := &valueFlowProgramLoaderDB{
		candidates: [][]any{{"scope-1", "repo-app", "run-1", "generation-1", now}},
		edges: [][]any{{
			"entity:caller", "entity:callee", "CALLS", "scip",
			"", string(caller),
			"", string(callee),
		}},
		summaries: map[string][]summary.SnapshotFunction{
			"repo-app": {{
				ID:      caller,
				Effects: summary.Effects{ParamToCallArg: []summary.CallArgFlow{{Callee: callee, Param: 0, Arg: 1}}},
				Version: "caller-version",
			}},
			"repo-lib": {{
				ID:      callee,
				Effects: summary.Effects{ParamToSink: []summary.ParamSink{{Param: 1, SinkKind: "sql"}}},
				Version: "callee-version",
			}},
		},
		sources: map[string][][]any{
			"repo-app": {{string(caller), 0, "http_request"}},
		},
	}
	store := NewValueFlowProgramInputStore(db)

	inputs, err := store.LoadPendingValueFlowProgramInputs(ctx, 10)
	if err != nil {
		t.Fatalf("LoadPendingValueFlowProgramInputs() error = %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	input := inputs[0]
	if got, want := input.RepositoryID, "repo-app"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if len(input.CallEdges) != 1 {
		t.Fatalf("CallEdges = %#v, want one edge", input.CallEdges)
	}
	if got := input.CallEdges[0].CallerFunctionID; got != caller {
		t.Fatalf("CallerFunctionID = %q, want %q", got, caller)
	}
	if got := input.CallEdges[0].CalleeFunctionID; got != callee {
		t.Fatalf("CalleeFunctionID = %q, want %q", got, callee)
	}
	program, stats := reducer.BuildValueFlowProgram(input)
	if stats.ProgramEdgeCount != 1 || stats.SinkCount != 1 {
		t.Fatalf("program=%#v stats=%#v, want one edge and one sink", program, stats)
	}
	if len(program.Sources) != 1 {
		t.Fatalf("program.Sources = %#v, want one source", program.Sources)
	}
	if got, want := program.Sources[0].Kind, "http_request"; got != want {
		t.Fatalf("program source kind = %q, want %q", got, want)
	}
}

func TestValueFlowProgramInputLoaderCountsMissingFunctionIdentity(t *testing.T) {
	ctx := context.Background()
	db := &valueFlowProgramLoaderDB{
		candidates: [][]any{{"scope-1", "repo-app", "run-1", "generation-1", time.Now().UTC()}},
		edges: [][]any{{
			"entity:caller", "entity:callee", "CALLS", "scip",
			"repo-app", "",
			"repo-lib", "repo-lib\x1fexample.com/lib\x1f\x1fQuery",
		}},
		summaries: map[string][]summary.SnapshotFunction{},
	}
	store := NewValueFlowProgramInputStore(db)

	inputs, err := store.LoadPendingValueFlowProgramInputs(ctx, 10)
	if err != nil {
		t.Fatalf("LoadPendingValueFlowProgramInputs() error = %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	if got, want := inputs[0].SkippedMissingIdentity, 1; got != want {
		t.Fatalf("SkippedMissingIdentity = %d, want %d", got, want)
	}
	if len(inputs[0].CallEdges) != 0 {
		t.Fatalf("CallEdges = %#v, want none without package identity", inputs[0].CallEdges)
	}
}

type valueFlowProgramLoaderDB struct {
	candidates [][]any
	edges      [][]any
	summaries  map[string][]summary.SnapshotFunction
	sources    map[string][][]any
}

func (db *valueFlowProgramLoaderDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM shared_projection_acceptance AS acceptance"):
		return &valueFlowProgramRows{data: db.candidates, idx: -1}, nil
	case strings.Contains(query, "LEFT JOIN function_graph_ids AS caller_function"):
		return &valueFlowProgramRows{data: db.edges, idx: -1}, nil
	case strings.Contains(query, "FROM function_summaries") && len(args) == 1:
		repo, _ := args[0].(string)
		functions := db.summaries[repo]
		rows := make([][]any, 0, len(functions))
		for _, fn := range functions {
			effects, err := json.Marshal(fn.Effects)
			if err != nil {
				return nil, err
			}
			rows = append(rows, []any{string(fn.ID), effects, fn.Version})
		}
		return &valueFlowProgramRows{data: rows, idx: -1}, nil
	case strings.Contains(query, "FROM function_sources") && len(args) == 1:
		repo, _ := args[0].(string)
		return &valueFlowProgramRows{data: db.sources[repo], idx: -1}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (db *valueFlowProgramLoaderDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return sharedIntentResult{}, nil
}

type valueFlowProgramRows struct {
	data [][]any
	idx  int
}

func (r *valueFlowProgramRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *valueFlowProgramRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	row := r.data[r.idx]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = val.(string)
		case *[]byte:
			*d = val.([]byte)
		case *time.Time:
			*d = val.(time.Time)
		case *int:
			*d = val.(int)
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *valueFlowProgramRows) Err() error   { return nil }
func (r *valueFlowProgramRows) Close() error { return nil }
