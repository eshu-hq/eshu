package reducer

// materialization_subduration_test.go provides TDD-first unit tests asserting
// that each long-pole materialization domain handler (issue #3624) populates:
//  1. Result.SubDurations — non-nil per-phase wall-time map with the expected
//     phase keys (durations only; emitted as sub_duration_<key>_seconds).
//  2. Result.SubSignals — non-nil diagnostic map carrying input_ready and
//     written_rows (counts/flags only; emitted as sub_signal_<key>, no
//     _seconds suffix).
//
// Per the issue's diagnostic contract, each domain is covered in three states:
//   - work-happened : input_ready==1.0 AND written_rows>0
//   - genuine-empty : input_ready==1.0 AND written_rows==0
//   - stall         : input_ready==0.0 (upstream data not ready)
//
// Genuine-empty (input present, zero rows) is only reachable on the
// writer-based domains (deployment_mapping, workload_identity): their writers
// run unconditionally and may return zero canonical writes. The two
// intent-emitting domains (inheritance_materialization, code_call_materialization)
// always emit at least one whole-scope refresh intent per repository once a
// projection context exists, so for them a present context implies
// written_rows>=1; their genuine-empty state is not reachable through Handle
// and is documented rather than asserted. The handlers still pass
// materializationDiagnosticSignals(true, 0) on the defensive
// len(intentRows)==0 branch so the signal stays correct if upstream behavior
// ever changes.
//
// assertWrittenRows reads Result.SubSignals["written_rows"] (NOT
// result.CanonicalWrites) so the missing-key defect the review flagged cannot
// pass green again.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ---------------------------------------------------------------------------
// deployment_mapping (PlatformMaterializationHandler)
// ---------------------------------------------------------------------------

// TestPlatformMaterializationHandlerSubDurationsWorkHappened asserts the
// work-happened state: input present (request has entity keys) and the writer
// reported canonical writes → input_ready==1, written_rows>0.
func TestPlatformMaterializationHandlerSubDurationsWorkHappened(t *testing.T) {
	t.Parallel()

	handler := PlatformMaterializationHandler{
		Writer: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{
				CanonicalWrites: 3,
				EvidenceSummary: "3 platform bindings written",
			},
		},
	}

	result := mustHandle(t, handler, platformIntent("intent-pm-work", []string{"platform:k8s:prod", "repo:svc-a"}))

	assertSubDurationsPresent(t, result, "deployment_mapping", []string{
		"platform_write", "phase_publish", "total",
	})
	assertInputReady(t, result, "deployment_mapping", 1.0)
	assertWrittenRows(t, result, "deployment_mapping", 3)
}

// TestPlatformMaterializationHandlerSubDurationsGenuineEmpty asserts the
// genuine-empty state: input present but the writer reported zero writes →
// input_ready==1, written_rows==0 (NOT a stall).
func TestPlatformMaterializationHandlerSubDurationsGenuineEmpty(t *testing.T) {
	t.Parallel()

	handler := PlatformMaterializationHandler{
		Writer: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{
				CanonicalWrites: 0,
				EvidenceSummary: "nothing to write",
			},
		},
	}

	result := mustHandle(t, handler, platformIntent("intent-pm-empty", []string{"platform:k8s:dev"}))

	assertInputReady(t, result, "deployment_mapping", 1.0)
	assertWrittenRows(t, result, "deployment_mapping", 0)
}

// ---------------------------------------------------------------------------
// workload_identity (WorkloadIdentityHandler)
// ---------------------------------------------------------------------------

// TestWorkloadIdentityHandlerSubDurationsWorkHappened asserts the
// work-happened state: input present and writer reported writes.
func TestWorkloadIdentityHandlerSubDurationsWorkHappened(t *testing.T) {
	t.Parallel()

	handler := WorkloadIdentityHandler{
		Writer: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{
				CanonicalID:      "canonical:workload/svc-a",
				CanonicalWrites:  5,
				EvidenceSummary:  "5 identity rows written",
				ReconciledScopes: 2,
			},
		},
	}

	result := mustHandle(t, handler, workloadIdentityIntent("intent-wi-work", []string{"workload:svc-a", "repo:svc-a"}))

	assertSubDurationsPresent(t, result, "workload_identity", []string{
		"graph_write", "phase_publish", "total",
	})
	assertInputReady(t, result, "workload_identity", 1.0)
	assertWrittenRows(t, result, "workload_identity", 5)
}

// TestWorkloadIdentityHandlerSubDurationsGenuineEmpty asserts the genuine-empty
// state: input present, writer reported zero writes → input_ready==1,
// written_rows==0.
func TestWorkloadIdentityHandlerSubDurationsGenuineEmpty(t *testing.T) {
	t.Parallel()

	handler := WorkloadIdentityHandler{
		Writer: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{
				CanonicalID:      "canonical:workload/svc-b",
				CanonicalWrites:  0,
				ReconciledScopes: 1,
			},
		},
	}

	result := mustHandle(t, handler, workloadIdentityIntent("intent-wi-empty", []string{"workload:svc-b"}))

	assertInputReady(t, result, "workload_identity", 1.0)
	assertWrittenRows(t, result, "workload_identity", 0)
}

// ---------------------------------------------------------------------------
// inheritance_materialization (InheritanceMaterializationHandler)
// ---------------------------------------------------------------------------

// TestInheritanceMaterializationHandlerSubDurationsWorkHappened asserts the
// work-happened state using a real fact set (repository + parent/child content
// entities) that produces durable intent rows.
func TestInheritanceMaterializationHandlerSubDurationsWorkHappened(t *testing.T) {
	t.Parallel()

	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: inheritanceWorkEnvelopes()},
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	result := mustHandle(t, handler, inheritanceIntent("intent-im-work"))

	assertSubDurationsPresent(t, result, "inheritance_materialization", []string{
		"load_facts", "build_intents", "upsert_intents", "total",
	})
	assertInputReady(t, result, "inheritance_materialization", 1.0)
	assertWrittenRowsGreater(t, result, "inheritance_materialization", 0)
}

// TestInheritanceMaterializationHandlerSubDurationsStall asserts the stall
// state: empty facts → no repository context → input_ready==0, written_rows==0.
func TestInheritanceMaterializationHandlerSubDurationsStall(t *testing.T) {
	t.Parallel()

	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: nil},
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	result := mustHandle(t, handler, inheritanceIntent("intent-im-stall"))

	assertSubDurationsPresent(t, result, "inheritance_materialization", []string{
		"load_facts", "total",
	})
	assertInputReady(t, result, "inheritance_materialization", 0.0)
	assertWrittenRows(t, result, "inheritance_materialization", 0)
}

// ---------------------------------------------------------------------------
// code_call_materialization (CodeCallMaterializationHandler)
// ---------------------------------------------------------------------------

// TestCodeCallMaterializationHandlerSubDurationsWorkHappened asserts the
// work-happened state using a real fact set that produces intent rows.
func TestCodeCallMaterializationHandlerSubDurationsWorkHappened(t *testing.T) {
	t.Parallel()

	handler := CodeCallMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: codeCallWorkEnvelopes()},
		IntentWriter: &recordingCodeCallIntentWriter{},
	}

	result := mustHandle(t, handler, codeCallIntent("intent-ccm-work"))

	assertSubDurationsPresent(t, result, "code_call_materialization", []string{
		"load_facts", "build_context", "extract_rows", "build_intents", "upsert_intents", "total",
	})
	assertInputReady(t, result, "code_call_materialization", 1.0)
	assertWrittenRowsGreater(t, result, "code_call_materialization", 0)
}

// TestCodeCallMaterializationHandlerSubDurationsContextOnlyRefresh asserts the
// input-present path with no code-call edges: a repository fact with
// source_run_id builds a projection context, which always emits exactly one
// whole-scope refresh intent per repo. So input_ready==1 and written_rows==1
// (the refresh). This is why the genuine-empty (input present, zero rows) state
// is NOT reachable for the intent-emitting domains — a present context always
// yields a refresh intent.
func TestCodeCallMaterializationHandlerSubDurationsContextOnlyRefresh(t *testing.T) {
	t.Parallel()

	handler := CodeCallMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: codeCallContextOnlyEnvelopes()},
		IntentWriter: &recordingCodeCallIntentWriter{},
	}

	result := mustHandle(t, handler, codeCallIntent("intent-ccm-context-only"))

	assertInputReady(t, result, "code_call_materialization", 1.0)
	// One repo with context → exactly one refresh intent, zero edge intents.
	assertWrittenRows(t, result, "code_call_materialization", 1)
}

// TestCodeCallMaterializationHandlerSubDurationsStall asserts the stall state:
// empty facts → no projection context → input_ready==0, written_rows==0.
func TestCodeCallMaterializationHandlerSubDurationsStall(t *testing.T) {
	t.Parallel()

	handler := CodeCallMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: nil},
		IntentWriter: &recordingCodeCallIntentWriter{},
	}

	result := mustHandle(t, handler, codeCallIntent("intent-ccm-stall"))

	assertSubDurationsPresent(t, result, "code_call_materialization", []string{
		"load_facts", "build_context", "total",
	})
	assertInputReady(t, result, "code_call_materialization", 0.0)
	assertWrittenRows(t, result, "code_call_materialization", 0)
}

// ---------------------------------------------------------------------------
// intent constructors
// ---------------------------------------------------------------------------

func fixedTestTime() time.Time {
	return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
}

func platformIntent(id string, entityKeys []string) Intent {
	now := fixedTestTime()
	return Intent{
		IntentID:        id,
		ScopeID:         "scope-pm",
		GenerationID:    "gen-pm",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "test",
		EntityKeys:      entityKeys,
		RelatedScopeIDs: []string{"scope-pm"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusClaimed,
	}
}

func workloadIdentityIntent(id string, entityKeys []string) Intent {
	now := fixedTestTime()
	return Intent{
		IntentID:        id,
		ScopeID:         "scope-wi",
		GenerationID:    "gen-wi",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "test",
		EntityKeys:      entityKeys,
		RelatedScopeIDs: []string{"scope-wi-2"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusClaimed,
	}
}

func inheritanceIntent(id string) Intent {
	now := fixedTestTime()
	return Intent{
		IntentID:        id,
		ScopeID:         "scope-im",
		GenerationID:    "gen-im",
		SourceSystem:    "git",
		Domain:          DomainInheritanceMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo:myrepo"},
		RelatedScopeIDs: []string{"scope-im"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusClaimed,
	}
}

func codeCallIntent(id string) Intent {
	now := fixedTestTime()
	return Intent{
		IntentID:        id,
		ScopeID:         "scope-ccm",
		GenerationID:    "gen-ccm",
		SourceSystem:    "git",
		Domain:          DomainCodeCallMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo:myrepo"},
		RelatedScopeIDs: []string{"scope-ccm"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusClaimed,
	}
}

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
