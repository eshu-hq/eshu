// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// edgeWriterSparseCase drives one EdgeWriter domain with only the identity
// keys its row builder requires, so every optional payload field is absent.
type edgeWriterSparseCase struct {
	name     string
	domain   string
	payloads []map[string]any
}

// edgeWriterSparseCases covers every domain EdgeWriter.buildRowMap routes.
// Each payload carries the builder's required keys and nothing optional, which
// is the input shape that exposes a conditionally omitted row key (#6782).
var edgeWriterSparseCases = []edgeWriterSparseCase{
	{
		name:   "repo dependency with a bare evidence artifact",
		domain: reducer.DomainRepoDependency,
		payloads: []map[string]any{
			{
				"repo_id":           "repo-a",
				"target_repo_id":    "repo-b",
				"relationship_type": "DEPLOYS_FROM",
				// A non-GitHub-Actions artifact: no start_line, end_line,
				// commit_sha, ref_value, or ref_pinned.
				"evidence_artifacts": []any{
					map[string]any{"evidence_kind": "HELM_VALUES_REFERENCE", "path": "values.yaml"},
				},
			},
			{
				"repo_id":            "repo-a",
				"target_repo_id":     "repo-c",
				"relationship_type":  "DEPLOYS_FROM",
				"evidence_artifacts": []any{map[string]any{"evidence_kind": "ARGOCD_APP_SOURCE", "path": "app.yaml", "environment": "prod"}},
			},
			{"repo_id": "repo-a", "target_repo_id": "repo-d"},
			{"repo_id": "repo-a", "platform_id": "platform-x", "relationship_type": "RUNS_ON"},
		},
	},
	{
		name:     "workload dependency",
		domain:   reducer.DomainWorkloadDependency,
		payloads: []map[string]any{{"workload_id": "w-1", "target_workload_id": "w-2"}},
	},
	{
		name:   "code calls without call_kind",
		domain: reducer.DomainCodeCalls,
		payloads: []map[string]any{
			{"caller_entity_id": "f-1", "callee_entity_id": "f-2"},
			{"caller_entity_id": "f-1", "callee_entity_id": "f-3", "caller_entity_type": "Function", "callee_entity_type": "Function"},
			{"caller_entity_id": "f-1", "callee_entity_id": "c-1", "relationship_type": "REFERENCES"},
			{"caller_entity_id": "f-1", "callee_entity_id": "c-2", "relationship_type": "REFERENCES", "caller_entity_type": "Function", "callee_entity_type": "Class"},
			{"caller_entity_id": "f-1", "callee_entity_id": "c-3", "relationship_type": "INSTANTIATES"},
			{"caller_entity_id": "f-1", "callee_entity_id": "c-4", "relationship_type": "INSTANTIATES", "caller_entity_type": "Function", "callee_entity_type": "Class"},
			{"source_entity_id": "c-5", "target_entity_id": "c-6", "relationship_type": "USES_METACLASS"},
		},
	},
	{
		name:   "inheritance",
		domain: reducer.DomainInheritanceEdges,
		payloads: []map[string]any{
			{"child_entity_id": "c-1", "parent_entity_id": "c-2"},
			{"child_entity_id": "c-1", "parent_entity_id": "c-3", "child_entity_type": "Class", "parent_entity_type": "Class"},
			{"child_entity_id": "c-1", "parent_entity_id": "i-1", "relationship_type": "IMPLEMENTS"},
			{"child_entity_id": "m-1", "parent_entity_id": "m-2", "relationship_type": "OVERRIDES"},
			{"child_entity_id": "a-1", "parent_entity_id": "a-2", "relationship_type": "ALIASES"},
		},
	},
	{
		name:   "documentation",
		domain: reducer.DomainDocumentationEdges,
		payloads: []map[string]any{
			{"section_uid": "s-1", "target_entity_id": "e-1"},
			{"section_uid": "s-2", "target_entity_id": "w-1", "target_kind": "workload"},
		},
	},
	{
		name:     "rationale",
		domain:   reducer.DomainRationaleEdges,
		payloads: []map[string]any{{"rationale_uid": "r-1", "target_entity_id": "e-1"}},
	},
	{
		name:   "sql relationships",
		domain: reducer.DomainSQLRelationships,
		payloads: []map[string]any{
			{"source_entity_id": "q-1", "target_entity_id": "t-1", "relationship_type": "QUERIES_TABLE"},
			{"source_entity_id": "t-1", "target_entity_id": "col-1", "relationship_type": "HAS_COLUMN"},
			{"source_entity_id": "tr-1", "target_entity_id": "t-1", "relationship_type": "TRIGGERS"},
			{"source_entity_id": "tr-1", "target_entity_id": "fn-1", "relationship_type": "EXECUTES"},
		},
	},
	{
		name:   "shell exec",
		domain: reducer.DomainShellExec,
		payloads: []map[string]any{
			{"source_entity_id": "f-1", "target_entity_id": "s-1", "repo_id": "repo-a", "source_path": "run.sh"},
		},
	},
	{
		name:   "deployable unit correlation",
		domain: reducer.DomainDeployableUnitEdges,
		payloads: []map[string]any{{
			"repo_id": "repo-a", "deployment_repo_id": "repo-b", "deployable_unit_key": "unit",
			"correlation_key": "corr", "relationship_type": "DEPLOYS_FROM",
		}},
	},
	{
		name:     "handles route",
		domain:   reducer.DomainHandlesRoute,
		payloads: []map[string]any{{"function_entity_id": "f-1", "repo_id": "repo-a", "path": "/health"}},
	},
	{
		name:     "runs in",
		domain:   reducer.DomainRunsIn,
		payloads: []map[string]any{{"function_id": "f-1", "repo_id": "repo-a"}},
	},
	{
		name:     "invokes cloud action",
		domain:   reducer.DomainInvokesCloudAction,
		payloads: []map[string]any{{"function_id": "f-1", "cloud_action": "s3:GetObject", "action_id": "act-1"}},
	},
	{
		name:   "codeowners ownership",
		domain: reducer.DomainCodeownersOwnershipEdges,
		payloads: []map[string]any{
			{"repo_id": "repo-a", "owner_ref": "@team", "pattern": "*", "source_path": "CODEOWNERS"},
		},
	},
	{
		name:   "submodule pin without pinned_sha",
		domain: reducer.DomainSubmodulePinEdges,
		payloads: []map[string]any{
			{"parent_repo_id": "repo-a", "resolved_repo_id": "repo-b", "submodule_path": "vendor/b"},
		},
	},
}

// TestEdgeWriterSparseRowsCarryEveryReferencedKey drives every EdgeWriter
// domain with payloads that omit every optional field and asserts each routed
// row carries every key its UNWIND statement reads (#6782). It failed on the
// evidence-artifact rows (start_line, end_line, commit_sha, ref_value,
// ref_pinned), the code-call routes (call_kind), and the PINS_SUBMODULE route
// (pinned_sha) before those builders sent explicit nils.
func TestEdgeWriterSparseRowsCarryEveryReferencedKey(t *testing.T) {
	t.Parallel()

	for _, tc := range edgeWriterSparseCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			writer := NewEdgeWriter(executor, 0)
			rows := make([]reducer.SharedProjectionIntentRow, 0, len(tc.payloads))
			for index, payload := range tc.payloads {
				rows = append(rows, reducer.SharedProjectionIntentRow{
					IntentID:     tc.name + "-" + string(rune('a'+index)),
					RepositoryID: "repo-a",
					GenerationID: "gen-1",
					Payload:      payload,
				})
			}
			if _, err := writer.WriteEdges(context.Background(), tc.domain, rows, "test/sparse"); err != nil {
				t.Fatalf("WriteEdges() error = %v", err)
			}
			if checked := assertUnwindRowsCarryReferencedKeys(t, executor.calls); checked < len(tc.payloads) {
				t.Fatalf("checked %d rows, want at least %d (a payload was dropped as unroutable)", checked, len(tc.payloads))
			}
		})
	}
}

// TestEdgeWriterSparseOptionalKeysAreNil pins the value sent for an absent
// optional key: nil, not "". Readers treat a present empty string as a real
// value (a `call_kind` of "", a `ref_value` that gates `ref_pinned`), so the
// fix must not trade the NornicDB junk token for an empty string.
func TestEdgeWriterSparseOptionalKeysAreNil(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		domain  string
		payload map[string]any
		keys    []string
	}{
		{
			name:    "code call",
			domain:  reducer.DomainCodeCalls,
			payload: map[string]any{"caller_entity_id": "f-1", "callee_entity_id": "f-2"},
			keys:    []string{"call_kind"},
		},
		{
			name:    "submodule pin",
			domain:  reducer.DomainSubmodulePinEdges,
			payload: map[string]any{"parent_repo_id": "repo-a", "resolved_repo_id": "repo-b", "submodule_path": "vendor/b"},
			keys:    []string{"pinned_sha"},
		},
		{
			name:   "evidence artifact",
			domain: reducer.DomainRepoDependency,
			payload: map[string]any{
				"repo_id": "repo-a", "target_repo_id": "repo-b", "relationship_type": "DEPLOYS_FROM",
				"evidence_artifacts": []any{map[string]any{"evidence_kind": "HELM_VALUES_REFERENCE", "path": "values.yaml"}},
			},
			keys: []string{"start_line", "end_line", "commit_sha", "ref_value", "ref_pinned"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executor := &recordingExecutor{}
			writer := NewEdgeWriter(executor, 0)
			rows := []reducer.SharedProjectionIntentRow{{IntentID: "i-1", RepositoryID: "repo-a", Payload: tc.payload}}
			if _, err := writer.WriteEdges(context.Background(), tc.domain, rows, "test/sparse"); err != nil {
				t.Fatalf("WriteEdges() error = %v", err)
			}
			found := 0
			for _, call := range executor.calls {
				rowsOut, _ := unwindParamRows(call.Parameters["rows"])
				for _, row := range rowsOut {
					if _, relevant := row[tc.keys[0]]; !relevant {
						continue
					}
					found++
					for _, key := range tc.keys {
						if value, present := row[key]; !present || value != nil {
							t.Fatalf("%s = %#v (present=%v), want an explicit nil", key, value, present)
						}
					}
				}
			}
			if found == 0 {
				t.Fatalf("no routed row carried %s", tc.keys[0])
			}
		})
	}
}

// TestCodeInterprocEvidenceRowsCarryEveryReferencedKey covers the
// TAINT_FLOWS_TO writer. The reducer's row builder
// (taint.ExtractInterprocEvidenceRows) adds why_trail_json and
// why_trail_truncated only when a trail exists, so a finding without a trail
// reached NornicDB with the literal text "row.why_trail_json" stored on the
// edge (#6782). The writer now sends both keys, nil when absent.
func TestCodeInterprocEvidenceRowsCarryEveryReferencedKey(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := sourcecypher.NewCodeInterprocEvidenceWriter(executor, 0)
	rows := []map[string]any{{
		"uid":                 "flow-1",
		"source_function_uid": "fn-src",
		"sink_function_uid":   "fn-sink",
		"relative_path":       "app/handler.go",
		"sink_kind":           "sql",
		"source_kind":         "http",
		"confidence":          0.9,
		"cloud":               "",
	}}
	if err := writer.WriteCodeInterprocEvidence(context.Background(), rows, "scope-1", "gen-1", "reducer/code-interproc"); err != nil {
		t.Fatalf("WriteCodeInterprocEvidence() error = %v", err)
	}
	if checked := assertUnwindRowsCarryReferencedKeys(t, executor.calls); checked != 1 {
		t.Fatalf("checked %d rows, want 1", checked)
	}
	written, _ := unwindParamRows(executor.calls[0].Parameters["rows"])
	for _, key := range []string{"why_trail_json", "why_trail_truncated"} {
		if value, present := written[0][key]; !present || value != nil {
			t.Fatalf("%s = %#v (present=%v), want an explicit nil", key, value, present)
		}
	}
	if _, present := rows[0]["why_trail_json"]; present {
		t.Fatal("writer mutated the caller's row map")
	}
}
