// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// invokesCloudActionRepoEnvelope anchors the projection context for a repo so
// the cloud-action producer can resolve scope, source run, and acceptance unit.
func invokesCloudActionRepoEnvelope(repoID string) facts.Envelope {
	return facts.Envelope{
		FactKind: "repository",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       repoID,
			"source_run_id": "run-1",
		},
	}
}

// invokesCloudActionFileEnvelope builds a file fact envelope whose
// parsed_file_data carries one Function span and a function_calls slice. Each
// call map is passed through verbatim so a test can omit receiver_sdk_service or
// set an unmapped method name.
func invokesCloudActionFileEnvelope(
	repoID string,
	relativePath string,
	functions []map[string]any,
	calls []map[string]any,
) facts.Envelope {
	callSlice := make([]any, 0, len(calls))
	for _, call := range calls {
		callSlice = append(callSlice, call)
	}
	return facts.Envelope{
		FactKind: "file",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":           relativePath,
				"functions":      functions,
				"function_calls": callSlice,
			},
		},
	}
}

func buildInvokesCloudActionIntentsForTest(t *testing.T, envelopes []facts.Envelope) []SharedProjectionIntentRow {
	t.Helper()
	generationID := "gen-1"
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, generationID)
	index := buildCodeEntityIndex(envelopes)
	return buildInvokesCloudActionIntentRows(
		envelopes,
		index,
		contextByRepoID,
		time.Unix(0, 0).UTC(),
		invokesCloudActionEvidenceSource,
	)
}

// callerFunction returns a Function span large enough to contain a call at the
// given line.
func callerFunction(uid string) []map[string]any {
	return []map[string]any{
		{"name": "Handler", "uid": uid, "line_number": 1, "end_line": 100},
	}
}

func TestBuildInvokesCloudActionIntentRowsEmitsCatalogAction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-1"),
		invokesCloudActionFileEnvelope(
			"repo-1",
			"main.go",
			callerFunction("content-entity:handler"),
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
			},
		),
	}

	intents := buildInvokesCloudActionIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 INVOKES_CLOUD_ACTION intent, got %d", len(intents))
	}
	intent := intents[0]
	if intent.ProjectionDomain != DomainInvokesCloudAction {
		t.Fatalf("projection domain = %q, want %q", intent.ProjectionDomain, DomainInvokesCloudAction)
	}
	if got, want := payloadStr(intent.Payload, "function_id"), "content-entity:handler"; got != want {
		t.Fatalf("function_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "cloud_action"), "s3:putobject"; got != want {
		t.Fatalf("cloud_action = %q, want %q", got, want)
	}
	if got := payloadStr(intent.Payload, "action"); got != "" {
		t.Fatalf("payload must not set \"action\" (collides with the upsert discriminator); got %q", got)
	}
	if got, want := payloadStr(intent.Payload, "action_id"), "cloud-action:s3:putobject"; got != want {
		t.Fatalf("action_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(intent.Payload, "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	method := payloadStr(intent.Payload, "resolution_method")
	if !codeprovenance.Classified(method) {
		t.Fatalf("resolution_method = %q, want a classified provenance method", method)
	}
	if method != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want import_binding for an import-proven SDK call", method)
	}
}

// TestInvokesCloudActionUpsertIntentSurvivesFilterUpsertRows is the regression
// for the rc-10 root cause: the per-edge upsert intent must survive
// filterUpsertRows. The intent originally stored the cloud action under the
// payload "action" key (e.g. "s3:putobject"), but filterUpsertRows treats
// payload["action"] as the upsert/refresh/delete discriminator and skips any row
// whose action is not "upsert". That silently dropped every INVOKES_CLOUD_ACTION
// upsert (the intent still completed, no edge ever wrote), so the edge never
// materialized end to end.
func TestInvokesCloudActionUpsertIntentSurvivesFilterUpsertRows(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-1"),
		invokesCloudActionFileEnvelope(
			"repo-1",
			"main.go",
			callerFunction("content-entity:handler"),
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
			},
		),
	}

	intents := buildInvokesCloudActionIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 INVOKES_CLOUD_ACTION intent, got %d", len(intents))
	}

	rows := []SharedProjectionIntentRow{{Payload: intents[0].Payload}}
	kept := filterUpsertRows(rows)
	if len(kept) != 1 {
		t.Fatalf("filterUpsertRows dropped the invokes_cloud_action upsert intent (payload action=%q); the cloud action must not collide with the upsert discriminator",
			payloadStr(intents[0].Payload, "action"))
	}
}

func TestBuildInvokesCloudActionIntentRowsSkipsCallWithoutReceiverService(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-1"),
		invokesCloudActionFileEnvelope(
			"repo-1",
			"main.go",
			callerFunction("content-entity:handler"),
			[]map[string]any{
				// No receiver_sdk_service: cannot prove the call hits an SDK service.
				{"name": "PutObject", "line_number": 10},
			},
		),
	}

	intents := buildInvokesCloudActionIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no intent without receiver_sdk_service, got %d", len(intents))
	}
}

func TestBuildInvokesCloudActionIntentRowsSkipsUnmappedMethod(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-1"),
		invokesCloudActionFileEnvelope(
			"repo-1",
			"main.go",
			callerFunction("content-entity:handler"),
			[]map[string]any{
				// HeadObject is not in the mapping table.
				{"name": "HeadObject", "receiver_sdk_service": "s3", "line_number": 10},
			},
		),
	}

	intents := buildInvokesCloudActionIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no intent for an unmapped method, got %d", len(intents))
	}
}

func TestBuildInvokesCloudActionIntentRowsSkipsNonFunctionCaller(t *testing.T) {
	t.Parallel()

	// The call line falls outside every Function span, so the call's containing
	// entity resolves to the file root (a File, not a Function).
	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-1"),
		invokesCloudActionFileEnvelope(
			"repo-1",
			"main.go",
			[]map[string]any{
				{"name": "Handler", "uid": "content-entity:handler", "line_number": 1, "end_line": 5},
			},
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 50},
			},
		),
	}

	intents := buildInvokesCloudActionIntentsForTest(t, envelopes)
	if len(intents) != 0 {
		t.Fatalf("expected no intent when the caller is not a Function, got %d", len(intents))
	}
}

func TestBuildInvokesCloudActionIntentRowsDeduplicatesSameFunctionAction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-1"),
		invokesCloudActionFileEnvelope(
			"repo-1",
			"main.go",
			callerFunction("content-entity:handler"),
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 20},
			},
		),
	}

	intents := buildInvokesCloudActionIntentsForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected exactly 1 deduplicated intent for the same (function, action), got %d", len(intents))
	}
}

// TestInvokesCloudActionMappingNeverProducesNonCatalogAction is the
// correlation-truth guard: every (service, method) row in the mapping table must
// produce an action that is in the closed CAN_PERFORM catalog. A mapping row
// that resolved to a non-catalog action would let the producer fabricate an
// unjoinable CloudAction node.
func TestInvokesCloudActionMappingNeverProducesNonCatalogAction(t *testing.T) {
	t.Parallel()

	catalog := iamCanPerformCatalogByAction()
	for key, action := range cloudActionByServiceMethod {
		if _, ok := catalog[action]; !ok {
			t.Errorf("mapping %+v produces action %q which is not in the closed CAN_PERFORM catalog", key, action)
		}
	}
}
