// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestServiceMaterializationWriterScopesLineageByIngestionScope is the #6475
// regression: two ingestion scopes that correlate the same service id must
// each keep their own active generation. Before the fix the second scope's
// commit either collided on the generation id (identical evidence) or
// superseded the first scope's active generation (changed evidence).
func TestServiceMaterializationWriterScopesLineageByIngestionScope(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}
	evidence := []ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "gold"}}}

	scopeA, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		IntentID: "intent-a", ScopeID: "scope-a", ServiceID: "svc-shared", Ownership: evidence,
	})
	if err != nil {
		t.Fatalf("scope A write error = %v", err)
	}
	scopeB, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		IntentID: "intent-b", ScopeID: "scope-b", ServiceID: "svc-shared", Ownership: evidence,
	})
	if err != nil {
		t.Fatalf("scope B write error = %v", err)
	}

	if !scopeA.Committed || !scopeB.Committed {
		t.Fatalf("Committed = (A %v, B %v), want both true: identical evidence in another scope is a new lineage, not a no-op", scopeA.Committed, scopeB.Committed)
	}
	if scopeA.GenerationID == scopeB.GenerationID {
		t.Fatalf("scopes A and B share generation id %q; the scope must be part of the generation identity", scopeA.GenerationID)
	}
	if len(scopeB.SupersededIDs) != 0 {
		t.Fatalf("scope B superseded %v, want nothing: it must never retire scope A's generation", scopeB.SupersededIDs)
	}
	assertFakeScopeActive(t, store, "scope-a", "svc-shared", scopeA.GenerationID)
	assertFakeScopeActive(t, store, "scope-b", "svc-shared", scopeB.GenerationID)

	// A changed write in scope A supersedes scope A's generation only.
	changedA, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		IntentID: "intent-a2", ScopeID: "scope-a", ServiceID: "svc-shared",
		Ownership: []ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "platinum"}}},
	})
	if err != nil {
		t.Fatalf("scope A changed write error = %v", err)
	}
	if len(changedA.SupersededIDs) != 1 || changedA.SupersededIDs[0] != scopeA.GenerationID {
		t.Fatalf("scope A changed write superseded %v, want exactly [%s]", changedA.SupersededIDs, scopeA.GenerationID)
	}
	assertFakeScopeActive(t, store, "scope-a", "svc-shared", changedA.GenerationID)
	assertFakeScopeActive(t, store, "scope-b", "svc-shared", scopeB.GenerationID)
}

func TestServiceMaterializationGenerationIDFoldsScope(t *testing.T) {
	t.Parallel()

	write := ServiceMaterializationWrite{
		ScopeID:   "scope-a",
		ServiceID: "svc-a",
		Ownership: []ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "gold"}}},
	}
	other := write
	other.ScopeID = "scope-b"
	if ServiceMaterializationGenerationID(write) == ServiceMaterializationGenerationID(other) {
		t.Fatal("generation id is unchanged by the scope; two scopes would collide on the generation_id primary key")
	}
	repeat := write
	repeat.IntentID = "a-different-intent"
	if ServiceMaterializationGenerationID(write) != ServiceMaterializationGenerationID(repeat) {
		t.Fatal("generation id depends on the intent id; a repeat of the same evidence in the same scope must stay a no-op")
	}
}

func TestServiceMaterializationWriterRequiresScopeID(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}
	if _, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Ownership: []ServiceOwnershipEvidence{{OwnerRef: "team-a"}},
	}); err == nil {
		t.Fatal("WriteServiceMaterialization() error = nil, want non-nil for an empty scope_id")
	}
	if len(store.generations) != 0 {
		t.Fatalf("a scope-less write reached the store: %d generation(s)", len(store.generations))
	}
}

// TestServiceCatalogHandlerThreadsIntentScopeIntoLineage proves the handler
// writes each generation under the claimed intent's ingestion scope.
func TestServiceCatalogHandlerThreadsIntentScopeIntoLineage(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceTypedCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogOwnershipFact("ownership", "component:default/checkout", "team-payments"),
			serviceCatalogRepositoryIDLinkFact("repo-link", "component:default/checkout", "repo-checkout"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
	store := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:            loader,
		Writer:                &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter: PostgresServiceMaterializationWriter{DB: store, Now: time.Now},
	}
	const scopeID = "service-catalog-manifest://repo-checkout/catalog-info.yaml"
	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      scopeID,
		GenerationID: "generation-service-catalog",
		Domain:       reducercontract.DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(store.generations) == 0 {
		t.Fatal("handler committed no generation; the fixture no longer exercises the lineage write")
	}
	for id, gen := range store.generations {
		if gen.scopeID != scopeID {
			t.Errorf("generation %s scope_id = %q, want the intent scope %q", id, gen.scopeID, scopeID)
		}
	}
}

func assertFakeScopeActive(t *testing.T, store *fakeServiceMaterializationStore, scopeID, serviceID, want string) {
	t.Helper()
	var active []string
	for id, gen := range store.generations {
		if gen.scopeID == scopeID && gen.serviceID == serviceID && gen.status == ServiceMaterializationStatusActive {
			active = append(active, id)
		}
	}
	if len(active) != 1 || active[0] != want {
		t.Fatalf("active generations for (%s, %s) = %v, want exactly [%s]", scopeID, serviceID, active, want)
	}
}
