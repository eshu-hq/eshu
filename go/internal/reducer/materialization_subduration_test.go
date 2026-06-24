// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	"testing"
	"time"
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

// TestInheritanceMaterializationHandlerSubDurationsContextPresentNoEntities
// asserts the genuine-empty state: a repository fact with source_run_id builds a
// projection context (input present) but the facts carry no inheritance Class
// content_entity rows, so ExtractInheritanceRows returns empty repoIDs. This
// reaches the context-present/no-entities branch → input_ready==1,
// written_rows==0, succeeded — genuine empty work, NOT an ordering stall.
func TestInheritanceMaterializationHandlerSubDurationsContextPresentNoEntities(t *testing.T) {
	t.Parallel()

	handler := InheritanceMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: inheritanceContextOnlyEnvelopes()},
		IntentWriter: &recordingInheritanceIntentWriter{},
	}

	result := mustHandle(t, handler, inheritanceIntent("intent-im-context-only"))

	if result.Status != ResultStatusSucceeded {
		t.Fatalf("inheritance_materialization: Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	assertSubDurationsPresent(t, result, "inheritance_materialization", []string{
		"load_facts", "total",
	})
	assertInputReady(t, result, "inheritance_materialization", 1.0)
	assertWrittenRows(t, result, "inheritance_materialization", 0)
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
