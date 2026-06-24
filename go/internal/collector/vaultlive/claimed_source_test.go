// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceUsesWorkflowGenerationAndFence(t *testing.T) {
	t.Parallel()

	scopeID, err := VaultScopeID("vault-a", "admin")
	if err != nil {
		t.Fatalf("VaultScopeID() error = %v", err)
	}
	source, err := NewClaimedSource(ClaimedSourceConfig{
		Config: Config{
			CollectorInstanceID: "vault-live-primary",
			RedactionKey:        testRedactionKey(t),
			Targets: []ClusterTarget{{
				VaultClusterID: "vault-a",
				Namespace:      "admin",
				SourceURI:      "https://vault.example.com",
			}},
		},
		ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		Clock:         fixedClock(),
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	item := workflow.WorkItem{
		WorkItemID:          "wi",
		RunID:               "run",
		CollectorKind:       scope.CollectorVaultLive,
		CollectorInstanceID: "vault-live-primary",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "vault_live:generation-from-workflow",
		GenerationID:        "vault_live:generation-from-workflow",
		CurrentFencingToken: 42,
		Status:              workflow.WorkItemStatusClaimed,
		CreatedAt:           time.Unix(1700000000, 0).UTC(),
		UpdatedAt:           time.Unix(1700000000, 0).UTC(),
	}

	gen, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%v err=%v, want claimed generation", ok, err)
	}
	if gen.Scope.ScopeID != scopeID || gen.Scope.CollectorKind != scope.CollectorVaultLive {
		t.Fatalf("scope = %+v", gen.Scope)
	}
	if gen.Generation.GenerationID != item.GenerationID || gen.Generation.TriggerKind != scope.TriggerKindSnapshot {
		t.Fatalf("generation = %+v, want workflow generation %q", gen.Generation, item.GenerationID)
	}
	for env := range gen.Facts {
		if env.FactKind == facts.VaultAuthMountFactKind {
			if env.FencingToken != item.CurrentFencingToken {
				t.Fatalf("fact fencing token = %d, want %d", env.FencingToken, item.CurrentFencingToken)
			}
			return
		}
	}
	t.Fatal("NextClaimed() emitted no vault_auth_mount fact")
}

func TestClaimedSourceScopeMetadataDoesNotExposeRawNamespace(t *testing.T) {
	t.Parallel()

	scopeID, err := VaultScopeID("vault-a", "admin/platform")
	if err != nil {
		t.Fatalf("VaultScopeID() error = %v", err)
	}
	source, err := NewClaimedSource(ClaimedSourceConfig{
		Config: Config{
			CollectorInstanceID: "vault-live-primary",
			RedactionKey:        testRedactionKey(t),
			Targets: []ClusterTarget{{
				VaultClusterID: "vault-a",
				Namespace:      "admin/platform",
			}},
		},
		ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		Clock:         fixedClock(),
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	gen, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "wi",
		RunID:               "run",
		CollectorKind:       scope.CollectorVaultLive,
		CollectorInstanceID: "vault-live-primary",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "vault_live:generation-from-workflow",
		GenerationID:        "vault_live:generation-from-workflow",
		CurrentFencingToken: 42,
		Status:              workflow.WorkItemStatusClaimed,
		CreatedAt:           time.Unix(1700000000, 0).UTC(),
		UpdatedAt:           time.Unix(1700000000, 0).UTC(),
	})
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%v err=%v, want claimed generation", ok, err)
	}
	if raw := gen.Scope.Metadata["namespace"]; raw != "" {
		t.Fatalf("scope metadata exposed raw namespace %q", raw)
	}
	if got := gen.Scope.Metadata["namespace_present"]; got != "true" {
		t.Fatalf("scope metadata namespace_present = %q, want true", got)
	}
}

func TestClaimedSourceRejectsMissingFenceBeforeClientCall(t *testing.T) {
	t.Parallel()

	scopeID, err := VaultScopeID("vault-a", "admin")
	if err != nil {
		t.Fatalf("VaultScopeID() error = %v", err)
	}
	factory := &fakeClientFactory{client: snapshotFixtureClient()}
	source, err := NewClaimedSource(ClaimedSourceConfig{
		Config: Config{
			CollectorInstanceID: "vault-live-primary",
			RedactionKey:        testRedactionKey(t),
			Targets:             []ClusterTarget{{VaultClusterID: "vault-a", Namespace: "admin"}},
		},
		ClientFactory: factory,
		Clock:         fixedClock(),
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "wi",
		RunID:               "run",
		CollectorKind:       scope.CollectorVaultLive,
		CollectorInstanceID: "vault-live-primary",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "vault_live:generation-from-workflow",
		GenerationID:        "vault_live:generation-from-workflow",
		Status:              workflow.WorkItemStatusClaimed,
		CreatedAt:           time.Unix(1700000000, 0).UTC(),
		UpdatedAt:           time.Unix(1700000000, 0).UTC(),
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want missing-fence error")
	}
	if factory.calls != 0 {
		t.Fatalf("client factory calls = %d, want 0 before fence validation", factory.calls)
	}
}

func TestClaimedSourceRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(ClaimedSourceConfig{
		Config: Config{
			CollectorInstanceID: "vault-live-primary",
			RedactionKey:        testRedactionKey(t),
			Targets:             []ClusterTarget{{VaultClusterID: "vault-a"}},
		},
		ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorGrafana,
		CollectorInstanceID: "vault-live-primary",
		SourceSystem:        "grafana",
		ScopeID:             "grafana:scope",
		GenerationID:        "grafana:generation",
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want collector-kind rejection")
	}
}
