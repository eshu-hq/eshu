// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWorkloadMaterializationPublishesRepoDependencyFenceOnEverySuccessPath(t *testing.T) {
	for _, tc := range []struct {
		name          string
		withCandidate bool
	}{
		{name: "zero candidates"},
		{name: "materialized candidate", withCandidate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, time.September, 15, 17, 0, 0, 0, time.UTC)
			envelopes := []facts.Envelope{{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-payments",
					"name":     "payments",
				},
				ObservedAt: now,
			}}
			if tc.withCandidate {
				envelopes = append(envelopes, facts.Envelope{
					FactID:   "fact-file",
					FactKind: "file",
					Payload: map[string]any{
						"repo_id": "repo-payments",
						"parsed_file_data": map[string]any{
							"k8s_resources": []any{map[string]any{
								"name": "payments", "kind": "Deployment", "namespace": "production",
							}},
						},
					},
					ObservedAt: now,
				})
			}
			publisher := &recordingGraphProjectionPhasePublisher{}
			handler := WorkloadMaterializationHandler{
				FactLoader:     &stubFactLoader{envelopes: envelopes},
				Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
				PhasePublisher: publisher,
			}
			intent := Intent{
				IntentID:     "workload-fenced",
				ScopeID:      "scope-payments",
				GenerationID: "gen-1",
				SourceSystem: "git",
				Domain:       DomainWorkloadMaterialization,
				EntityKeys:   []string{"repo:payments"},
				Payload: map[string]any{
					RepoDependencyReadinessFencePayloadKey:  "fence-123",
					RepoDependencyReadinessRepoIDPayloadKey: "repo-payments",
				},
				Status:      IntentStatusPending,
				EnqueuedAt:  now,
				AvailableAt: now,
			}

			if _, err := handler.Handle(context.Background(), intent); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			wantKey := workloadMaterializationRepoReadinessKey(
				"scope-payments", "repo-payments", "gen-1",
			)
			wantKey.SourceRunID = RepoDependencyReadinessFenceSourceRunID("fence-123")
			if !publishedReadinessKey(publisher, wantKey) {
				t.Fatalf("token-scoped readiness row %+v not published; calls=%+v", wantKey, publisher.calls)
			}
		})
	}
}
