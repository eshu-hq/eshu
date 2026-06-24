package reducer

// materialization_subduration_helpers_test.go holds the real fact builders and
// assertion helpers shared by materialization_subduration_test.go (issue
// #3624). It is split out so both files stay under the 500-line limit.

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ---------------------------------------------------------------------------
// real fact builders (work-happened / genuine-empty / stall)
// ---------------------------------------------------------------------------

// inheritanceWorkEnvelopes returns a repository fact plus a parent/child class
// pair (child declares the parent as a base) so the inheritance Handle path
// builds a projection context AND emits durable intent rows. The repository
// fact carries source_run_id so buildCodeCallProjectionContexts (shared with
// the inheritance handler) yields a non-empty context.
func inheritanceWorkEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"source_run_id": "run-1",
				"graph_id":      "repo-1",
				"graph_kind":    "repository",
				"name":          "repo-1",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
				"file_path":   "/src/parent.py",
				"path":        "/src/parent.py",
				"language":    "python",
				"start_line":  1,
				"end_line":    30,
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_child",
				"entity_type": "Class",
				"entity_name": "ChildClass",
				"file_path":   "/src/child.py",
				"path":        "/src/child.py",
				"language":    "python",
				"start_line":  10,
				"end_line":    50,
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}
}

// inheritanceContextOnlyEnvelopes returns ONLY a repository fact carrying
// source_run_id so buildCodeCallProjectionContexts yields a non-empty
// projection context, but NO inheritance Class content_entity rows, so
// ExtractInheritanceRows returns empty repoIDs. This exercises the
// context-present/no-entities branch: genuine empty work (input_ready=1,
// written_rows=0), distinct from an ordering stall (no context at all).
func inheritanceContextOnlyEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-im",
				"source_run_id": "run-im",
				"graph_id":      "repo-im",
				"graph_kind":    "repository",
				"name":          "im-test-repo",
			},
		},
	}
}

// codeCallWorkEnvelopes returns a repository fact plus caller/callee file facts
// producing at least one code-call edge, so the code_call Handle path emits
// durable intent rows (refresh + edge).
func codeCallWorkEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"graph_id":      "repo-a",
				"graph_kind":    "repository",
				"name":          "repo-a",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "caller.py",
				"parsed_file_data": map[string]any{
					"path": "caller.py",
					"functions": []any{
						map[string]any{"name": "handle", "line_number": 3, "uid": "entity:handle"},
					},
					"function_calls_scip": []any{
						map[string]any{
							"caller_file":   "caller.py",
							"caller_line":   3,
							"caller_symbol": "pkg/caller#handle().",
							"callee_file":   "callee.py",
							"callee_line":   1,
							"callee_symbol": "pkg/callee#callee().",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "callee.py",
				"parsed_file_data": map[string]any{
					"path": "callee.py",
					"functions": []any{
						map[string]any{"name": "callee", "line_number": 1, "uid": "entity:callee"},
					},
				},
			},
		},
	}
}

// codeCallContextOnlyEnvelopes returns ONLY a repository fact with
// source_run_id: a projection context is built (input present) but there are no
// file/edge facts, so extraction yields zero edges and zero intent rows. This
// exercises the genuine-empty path (input_ready=1, written_rows=0).
func codeCallContextOnlyEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-ccm",
				"source_run_id": "run-ccm",
				"graph_id":      "repo-ccm",
				"graph_kind":    "repository",
				"name":          "ccm-test-repo",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// assertion + driver helpers
// ---------------------------------------------------------------------------

// mustHandle runs a handler and fails the test on error, returning the Result.
func mustHandle(t *testing.T, handler interface {
	Handle(context.Context, Intent) (Result, error)
}, intent Intent) Result {
	t.Helper()
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() unexpected error: %v", err)
	}
	return result
}

// assertSubDurationsPresent fails if SubDurations is nil or any required key is
// absent. These are duration phases only.
func assertSubDurationsPresent(t *testing.T, result Result, domain string, keys []string) {
	t.Helper()
	if result.SubDurations == nil {
		t.Fatalf("%s: SubDurations must not be nil", domain)
	}
	for _, k := range keys {
		if _, ok := result.SubDurations[k]; !ok {
			t.Fatalf("%s: SubDurations missing key %q; got keys: %v", domain, k, mapKeys(result.SubDurations))
		}
	}
}

// assertInputReady fails if SubSignals is nil, lacks "input_ready", or its value
// differs from want.
func assertInputReady(t *testing.T, result Result, domain string, want float64) {
	t.Helper()
	if result.SubSignals == nil {
		t.Fatalf("%s: SubSignals must not be nil", domain)
	}
	got, ok := result.SubSignals[diagnosticSignalInputReady]
	if !ok {
		t.Fatalf("%s: SubSignals missing %q; got keys: %v", domain, diagnosticSignalInputReady, mapKeys(result.SubSignals))
	}
	if got != want {
		t.Fatalf("%s: input_ready = %v, want %v", domain, got, want)
	}
}

// assertWrittenRows fails if SubSignals["written_rows"] != want. It reads the
// SIGNAL, not result.CanonicalWrites, so a missing written_rows key fails.
func assertWrittenRows(t *testing.T, result Result, domain string, want int) {
	t.Helper()
	if result.SubSignals == nil {
		t.Fatalf("%s: SubSignals must not be nil", domain)
	}
	got, ok := result.SubSignals[diagnosticSignalWrittenRows]
	if !ok {
		t.Fatalf("%s: SubSignals missing %q; got keys: %v", domain, diagnosticSignalWrittenRows, mapKeys(result.SubSignals))
	}
	if got != float64(want) {
		t.Fatalf("%s: written_rows = %v, want %d", domain, got, want)
	}
}

// assertWrittenRowsGreater fails if SubSignals["written_rows"] <= threshold.
func assertWrittenRowsGreater(t *testing.T, result Result, domain string, threshold int) {
	t.Helper()
	if result.SubSignals == nil {
		t.Fatalf("%s: SubSignals must not be nil", domain)
	}
	got, ok := result.SubSignals[diagnosticSignalWrittenRows]
	if !ok {
		t.Fatalf("%s: SubSignals missing %q; got keys: %v", domain, diagnosticSignalWrittenRows, mapKeys(result.SubSignals))
	}
	if got <= float64(threshold) {
		t.Fatalf("%s: written_rows = %v, want > %d", domain, got, threshold)
	}
}

func mapKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
