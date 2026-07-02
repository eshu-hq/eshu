// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestIngestionStoreCommitScopeGenerationSkipsStreamingGCPRelationshipEvidenceForAccountScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 14, 30, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{
			queryResponses: []queueFakeRows{{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-orders","name":"order-gateway"}`)},
					{[]byte(`{"repo_id":"repo-payments","name":"payments-service"}`)},
				},
			}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "gcp:project:demo",
		SourceSystem:  "gcp",
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorGCP,
		PartitionKey:  "project:demo",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-gcp-account",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	envelopes := []facts.Envelope{{
		FactID:        "gcp-relationship-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.GCPCloudRelationshipFactKind,
		StableFactKey: "gcp-rel-1",
		ObservedAt:    generation.ObservedAt,
		Payload: map[string]any{
			"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway",
			"relationship_type":         "run_service_uses_secret",
			"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/payments-service",
			"support_state":             "supported",
		},
	}}

	err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testFactChannel(envelopes))
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	for _, execCall := range db.tx.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			t.Fatalf("account-scope GCP commit persisted streaming relationship evidence: %q", execCall.query)
		}
	}
}
