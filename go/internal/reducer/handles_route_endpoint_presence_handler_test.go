// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// TestWorkloadMaterializationHandlerPublishesEndpointRepoPathPresence proves the
// workload handler publishes property-keyed (repo_id, path) Endpoint presence
// after the endpoint nodes commit, so the handles_route gate can see them
// (#2809). A nil presence writer is a no-op, leaving the hot workload path
// byte-identical.
func TestWorkloadMaterializationHandlerPublishesEndpointRepoPathPresence(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:         "repo-service-api",
				RepoName:       "service-api",
				Classification: "service",
				Confidence:     0.96,
				Provenance:     []string{"dockerfile_runtime"},
				APIEndpoints: []APIEndpointSignal{
					{Path: "/widgets", Methods: []string{"get"}, SourceKinds: []string{"openapi"}},
				},
			},
		},
	}
	presenceWriter := &recordingPresenceWriter{}
	handler := WorkloadMaterializationHandler{
		FactLoader:             &stubFactLoader{},
		InputLoader:            inputLoader,
		Materializer:           NewWorkloadMaterializer(&recordingCypherExecutor{}),
		EndpointPresenceWriter: presenceWriter,
	}

	intent := Intent{
		IntentID:        "intent-wm-endpoint-presence",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "endpoints ready",
		EntityKeys:      []string{"repo-service-api"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(presenceWriter.upserts) == 0 {
		t.Fatal("no endpoint repo/path presence upsert recorded")
	}
	wantUID := apiEndpointRepoPathPresenceKey("repo-service-api", "/widgets")
	found := false
	for _, batch := range presenceWriter.upserts {
		for _, row := range batch {
			if row.Keyspace == GraphProjectionKeyspaceAPIEndpointRepoPath && row.UID == wantUID {
				found = true
				if row.ScopeID != "scope-service" {
					t.Fatalf("presence scope = %q, want scope-service", row.ScopeID)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no presence row for uid %q in %+v", wantUID, presenceWriter.upserts)
	}
}

func TestWorkloadMaterializationHandlerNilPresenceWriterNoOp(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:         "repo-service-api",
				RepoName:       "service-api",
				Classification: "service",
				Confidence:     0.96,
				Provenance:     []string{"dockerfile_runtime"},
				APIEndpoints: []APIEndpointSignal{
					{Path: "/widgets", Methods: []string{"get"}, SourceKinds: []string{"openapi"}},
				},
			},
		},
	}
	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		InputLoader:  inputLoader,
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
		// EndpointPresenceWriter nil
	}

	intent := Intent{
		IntentID:        "intent-wm-nil-presence",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "endpoints ready",
		EntityKeys:      []string{"repo-service-api"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v (nil presence writer must be a no-op)", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0; nil presence writer changed materialization behavior")
	}
}
