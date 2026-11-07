// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

const deltaUnkeyedGoImportPath = "github.com/acme/golden-delta-reader/app"

// deltaUnkeyedGoRepoEnvelope builds the repository fact of a delta generation
// whose changed Go files call same-package helpers in unchanged files.
func deltaUnkeyedGoRepoEnvelope() facts.Envelope {
	return facts.Envelope{
		FactKind: factload.FactKindRepository,
		Payload: map[string]any{
			"repo_id":              "repo-delta",
			"source_run_id":        "run-delta",
			"graph_id":             "repo-delta",
			"graph_kind":           "repository",
			"name":                 "repo-delta",
			"path":                 "/repo",
			"delta_generation":     true,
			"delta_relative_paths": []string{"app/main.go", "app/moved.go"},
		},
	}
}

// deltaUnkeyedGoFileEnvelope builds a changed-file fact whose parsed payload
// mirrors the tree-sitter Go engine: functions carry scip_symbol keys while
// same-package calls are keyless name-only edges (no callee_symbol), exactly
// as tests/fixtures/golden_delta_union parses.
func deltaUnkeyedGoFileEnvelope(relativePath string, functions []any, calls []any) facts.Envelope {
	return facts.Envelope{
		FactKind: factload.FactKindFile,
		Payload: map[string]any{
			"repo_id":             "repo-delta",
			"relative_path":       relativePath,
			"package_import_path": deltaUnkeyedGoImportPath,
			"parsed_file_data": map[string]any{
				"path":                relativePath,
				"package_import_path": deltaUnkeyedGoImportPath,
				"functions":           functions,
				"function_calls":      calls,
			},
		},
	}
}

// deltaUnkeyedGoFileEnvelopeWithImports mirrors deltaUnkeyedGoFileEnvelope
// for a changed file that carries imports, exactly as the tree-sitter Go
// engine emits them (#10301: plain imports record no alias).
func deltaUnkeyedGoFileEnvelopeWithImports(relativePath string, functions []any, calls []any, imports []any) facts.Envelope {
	env := deltaUnkeyedGoFileEnvelope(relativePath, functions, calls)
	env.Payload["parsed_file_data"].(map[string]any)["imports"] = imports
	return env
}

func deltaUnkeyedGoFunction(uid, name string, line, endLine int) map[string]any {
	return map[string]any{
		"name":                name,
		"line_number":         line,
		"end_line":            endLine,
		"uid":                 uid,
		"scip_symbol":         "scip-go gomod " + deltaUnkeyedGoImportPath + " " + name + "().",
		"package_import_path": deltaUnkeyedGoImportPath,
	}
}

func deltaUnkeyedGoCall(name string, line int) map[string]any {
	return map[string]any{
		"name":        name,
		"full_name":   name,
		"line_number": line,
		"lang":        "go",
	}
}

// keyFilteringSymbolLoader models the production symbol-definition contract:
// it returns only base-pool definitions whose scip_symbol was requested. A
// delta of keyless same-package calls requests nothing, so nothing resolves.
type keyFilteringSymbolLoader struct {
	primary []facts.Envelope
	base    []facts.Envelope

	requested [][]string
	calls     int
}

func (l *keyFilteringSymbolLoader) ListFacts(_ context.Context, _ string, _ string) ([]facts.Envelope, error) {
	return l.primary, nil
}

func (l *keyFilteringSymbolLoader) LoadActiveCodeCallSymbolDefinitionFacts(
	_ context.Context,
	symbolKeys []string,
) ([]facts.Envelope, error) {
	l.calls++
	l.requested = append(l.requested, symbolKeys)
	if len(symbolKeys) == 0 {
		return nil, nil
	}
	want := make(map[string]struct{}, len(symbolKeys))
	for _, key := range symbolKeys {
		want[key] = struct{}{}
	}
	var out []facts.Envelope
	for _, env := range l.base {
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		matched := false
		for _, fn := range payloadcore.MapSlice(fileData["functions"]) {
			if symbol, _ := fn["scip_symbol"].(string); symbol != "" {
				if _, ok := want[symbol]; ok {
					matched = true
					break
				}
			}
		}
		if matched {
			out = append(out, env)
		}
	}
	return out, nil
}

// TestCodeCallMaterializationHandlerDeltaUnkeyedSamePackageCalls is the live
// golden delta-reader regression (#10049 follow-up): the delta touches
// app/main.go (trailing comment, no move) and app/moved.go (caller moves), and
// both call same-package helpers defined in unchanged files. The tree-sitter
// Go parser emits those calls as keyless name-only edges, so the handler must
// still resolve them against the unchanged files' definitions and rewrite all
// four CALLS edges under a file-scoped refresh. It fails while the handler
// only requests symbol keys carried by the delta's own edges: nothing is
// requested, the three cross-file rows drop as unresolved callees, and the
// file-scoped refresh deletes their pre-existing edges.
func TestCodeCallMaterializationHandlerDeltaUnkeyedSamePackageCalls(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 10, 0, 0, 0, time.UTC)
	primary := []facts.Envelope{
		deltaUnkeyedGoRepoEnvelope(),
		deltaUnkeyedGoFileEnvelope("app/main.go",
			[]any{
				deltaUnkeyedGoFunction("entity:run", "deltaReaderRun", 9, 13),
				deltaUnkeyedGoFunction("entity:helper", "deltaReaderSameFileHelper", 15, 15),
			},
			[]any{
				deltaUnkeyedGoCall("deltaReaderSameFileHelper", 10),
				deltaUnkeyedGoCall("deltaReaderLedgerTotal", 11),
				deltaUnkeyedGoCall("deltaReaderReportLine", 12),
			},
		),
		deltaUnkeyedGoFileEnvelope("app/moved.go",
			[]any{
				deltaUnkeyedGoFunction("entity:moved-caller-new", "deltaReaderMovedCaller", 11, 13),
			},
			[]any{
				deltaUnkeyedGoCall("deltaReaderLedgerTotal", 12),
			},
		),
	}
	base := []facts.Envelope{
		deltaUnkeyedGoFileEnvelope("app/ledger.go",
			[]any{
				deltaUnkeyedGoFunction("entity:ledger-total", "deltaReaderLedgerTotal", 8, 10),
			},
			nil,
		),
		deltaUnkeyedGoFileEnvelope("app/report.go",
			[]any{
				deltaUnkeyedGoFunction("entity:report-line", "deltaReaderReportLine", 8, 10),
			},
			nil,
		),
	}
	loader := &keyFilteringSymbolLoader{primary: primary, base: base}
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: loader, IntentWriter: writer}

	intent := reducercontract.Intent{
		IntentID:     "intent-code-call-delta-unkeyed",
		ScopeID:      "scope-delta",
		GenerationID: "gen-delta",
		SourceSystem: "git",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		Cause:        "parser follow-up required",
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	}
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}

	var got []string
	for _, row := range writer.rows {
		if row.Payload["evidence_source"] != "parser/code-calls" || row.ProjectionDomain != reducercontract.DomainCodeCalls {
			continue
		}
		if row.Payload["intent_type"] == "repo_refresh" {
			continue
		}
		got = append(got,
			payloadcore.AnyToString(row.Payload["caller_entity_id"])+"->"+
				payloadcore.AnyToString(row.Payload["callee_entity_id"]))
	}
	sort.Strings(got)
	want := []string{
		"entity:moved-caller-new->entity:ledger-total",
		"entity:run->entity:helper",
		"entity:run->entity:ledger-total",
		"entity:run->entity:report-line",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("code-call edges = %#v, want %#v", got, want)
	}

	var refreshPaths []string
	for _, row := range writer.rows {
		if row.ProjectionDomain == reducercontract.DomainCodeCalls && row.Payload["intent_type"] == "repo_refresh" {
			refreshPaths = append(refreshPaths, payloadcore.SemanticPayloadStringSlice(row.Payload, "delta_file_paths")...)
		}
	}
	sort.Strings(refreshPaths)
	wantPaths := []string{"/repo/app/main.go", "/repo/app/moved.go"}
	if !reflect.DeepEqual(refreshPaths, wantPaths) {
		t.Fatalf("refresh delta_file_paths = %#v, want %#v", refreshPaths, wantPaths)
	}
}

// TestCodeCallMaterializationHandlerDeltaImportBearingSamePackageCalls is the
// #10301 residual the import-free derivation left behind: the moved caller
// lives in a file WITH a plain import (no dot-import), so its keyless
// same-package call must still resolve against the unchanged file's
// definition instead of dropping while the file-scoped refresh retracts the
// old edge. A companion dot-imported file proves the fail-safe side: its bare
// call still derives nothing.
func TestCodeCallMaterializationHandlerDeltaImportBearingSamePackageCalls(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 7, 10, 0, 0, 0, time.UTC)
	primary := []facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":              "repo-delta",
				"source_run_id":        "run-delta",
				"graph_id":             "repo-delta",
				"graph_kind":           "repository",
				"name":                 "repo-delta",
				"path":                 "/repo",
				"delta_generation":     true,
				"delta_relative_paths": []string{"app/plain.go", "app/dotted.go"},
			},
		},
		deltaUnkeyedGoFileEnvelopeWithImports("app/plain.go",
			[]any{
				deltaUnkeyedGoFunction("entity:plain-caller", "deltaReaderPlainCaller", 11, 13),
			},
			[]any{
				deltaUnkeyedGoCall("deltaReaderLedgerTotal", 12),
			},
			[]any{
				map[string]any{"name": "fmt", "line_number": 4, "lang": "go"},
			},
		),
		deltaUnkeyedGoFileEnvelopeWithImports("app/dotted.go",
			[]any{
				deltaUnkeyedGoFunction("entity:dotted-caller", "deltaReaderDottedCaller", 11, 13),
			},
			[]any{
				// ReportLine is defined only in the unchanged report.go,
				// which no derived key loads: the dot import means this
				// bare call may belong to example.com/other, so it must
				// stay unresolved rather than guess same-package.
				deltaUnkeyedGoCall("deltaReaderReportLine", 12),
			},
			[]any{
				map[string]any{"name": "example.com/other", "alias": ".", "line_number": 4, "lang": "go"},
			},
		),
	}
	base := []facts.Envelope{
		deltaUnkeyedGoFileEnvelope("app/ledger.go",
			[]any{
				deltaUnkeyedGoFunction("entity:ledger-total", "deltaReaderLedgerTotal", 8, 10),
			},
			nil,
		),
		deltaUnkeyedGoFileEnvelope("app/report.go",
			[]any{
				deltaUnkeyedGoFunction("entity:report-line", "deltaReaderReportLine", 8, 10),
			},
			nil,
		),
	}
	loader := &keyFilteringSymbolLoader{primary: primary, base: base}
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: loader, IntentWriter: writer}

	intent := reducercontract.Intent{
		IntentID:     "intent-code-call-delta-import-bearing",
		ScopeID:      "scope-delta",
		GenerationID: "gen-delta",
		SourceSystem: "git",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		Cause:        "parser follow-up required",
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	}
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}

	var got []string
	for _, row := range writer.rows {
		if row.Payload["evidence_source"] != "parser/code-calls" || row.ProjectionDomain != reducercontract.DomainCodeCalls {
			continue
		}
		if row.Payload["intent_type"] == "repo_refresh" {
			continue
		}
		got = append(got,
			payloadcore.AnyToString(row.Payload["caller_entity_id"])+"->"+
				payloadcore.AnyToString(row.Payload["callee_entity_id"]))
	}
	sort.Strings(got)
	want := []string{
		"entity:plain-caller->entity:ledger-total",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("code-call edges = %#v, want %#v", got, want)
	}

	// The overlay load must have requested only the provably same-package
	// callee: the plain-imported file's call derives its key, while the
	// dot-imported file's call derives nothing.
	var requested []string
	for _, keys := range loader.requested {
		requested = append(requested, keys...)
	}
	sort.Strings(requested)
	wantRequested := []string{"scip-go gomod " + deltaUnkeyedGoImportPath + " deltaReaderLedgerTotal()."}
	if !reflect.DeepEqual(requested, wantRequested) {
		t.Fatalf("requested symbol keys = %#v, want %#v", requested, wantRequested)
	}
}
