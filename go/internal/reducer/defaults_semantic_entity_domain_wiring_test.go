// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
)

// TestNewDefaultRuntimeRegistersSemanticEntityMaterializationWhenWriterPresent
// drives NewDefaultRuntime's DomainSemanticEntityMaterialization wiring
// end-to-end. It stays in the reducer root rather than moving with the
// semantic_entity family to semanticentity (issue #6061), because it
// exercises the root's own runtime/registry construction across several
// families' writers, not semanticentity's internals alone.
func TestNewDefaultRuntimeRegistersSemanticEntityMaterializationWhenWriterPresent(t *testing.T) {
	t.Parallel()

	runtime, err := NewDefaultRuntime(DefaultHandlers{
		WorkloadIdentityWriter: &recordingWorkloadIdentityWriter{
			result: WorkloadIdentityWriteResult{CanonicalWrites: 1},
		},
		CloudAssetResolutionWriter: &recordingCloudAssetResolutionWriter{
			result: CloudAssetResolutionWriteResult{CanonicalWrites: 1},
		},
		PlatformMaterializationWriter: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
		},
		SemanticEntityWriter: &recordingSemanticEntityWriter{
			result: semanticentity.SemanticEntityWriteResult{CanonicalWrites: 1},
		},
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactKind: "repository",
					Payload: map[string]any{
						"repo_id": "repo-1",
					},
				},
				{
					FactKind: "content_entity",
					SourceRef: facts.Ref{
						SourceURI:    "/repo/src/Logged.java",
						SourceSystem: "git",
					},
					Payload: map[string]any{
						"repo_id":       "repo-1",
						"entity_id":     "annotation-1",
						"relative_path": "src/Logged.java",
						"entity_type":   "Annotation",
						"entity_name":   "Logged",
						"language":      "java",
						"start_line":    12,
						"end_line":      12,
						"entity_metadata": map[string]any{
							"kind":        "applied",
							"target_kind": "method_declaration",
						},
					},
				},
			},
		},
		CodeCallIntentWriter: &recordingCodeCallIntentWriter{},
	})
	if err != nil {
		t.Fatalf("NewDefaultRuntime() error = %v, want nil", err)
	}

	_, err = runtime.Execute(context.Background(), Intent{
		IntentID:     "intent-semantic-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "semantic entity follow-up",
		Status:       IntentStatusClaimed,
		EnqueuedAt:   time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("runtime.Execute(semantic_entity_materialization) error = %v, want nil", err)
	}
}
