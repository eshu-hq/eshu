// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// appliedRoutingRecord is a fully durable applied-slot incident routing record
// whose EvidenceID is the source fact's generation-INDEPENDENT StableFactKey, not
// the generation-bearing envelope FactID.
func appliedRoutingRecord() ServiceIncidentRecord {
	return ServiceIncidentRecord{
		Provider:           "pagerduty",
		ProviderIncidentID: "PINC-1",
		Slot:               "applied_routing",
		EvidenceKind:       facts.IncidentRoutingAppliedPagerDutyResourceFactKind,
		EvidenceID:         facts.IncidentRoutingAppliedPagerDutyResourceFactKind + ":applied_pagerduty_resource:service:PSVC",
		TruthLabel:         "exact",
		ProviderObjectID:   "PSVC",
		DeclaredMatchState: "match",
		RedactionState:     "none",
	}
}

func TestServiceIncidentEvidenceKeyIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	record := appliedRoutingRecord()
	key := ServiceIncidentEvidenceKey("svc-app", serviceIncidentEvidenceIdentity(record))
	if !strings.HasPrefix(key, ServiceEvidenceFamilyIncidents+":svc-app:") {
		t.Fatalf("incidents evidence key %q is not the incidents:<service>:<identity> shape", key)
	}

	// The applied/live routing fact's envelope FactID digests generation_id (the
	// collectors embed generation_id into FactID), and the applied fact carries a
	// per-run state_generation_id. Keying on either would produce 100% false churn.
	// The incidents key must embed none of: a fact_id, a generation id, a
	// state_generation_id.
	for _, generationBearing := range []string{"generation-a", "fact-id-1234", "gen-b", "state-gen-99"} {
		if strings.Contains(key, generationBearing) {
			t.Fatalf("incidents evidence key %q must not embed generation-bearing token %q", key, generationBearing)
		}
	}

	again := ServiceIncidentEvidenceKey("svc-app", serviceIncidentEvidenceIdentity(record))
	if key != again {
		t.Fatalf("incidents identity not stable across calls: %q != %q", key, again)
	}
}

// TestServiceIncidentEvidenceKeyStableAcrossGenerations is the anti-churn guard:
// the same logical routing row read from two different fact generations (only the
// generation-bearing FactID differs; the durable StableFactKey does not) must
// produce the same incidents key, so the FULL OUTER JOIN diff classifies it
// unchanged instead of churning every generation.
func TestServiceIncidentEvidenceKeyStableAcrossGenerations(t *testing.T) {
	t.Parallel()

	genA := appliedRoutingRecord()
	genB := appliedRoutingRecord()
	// Simulate the read model surfacing the same durable routing row from a later
	// fact generation: the durable identity fields are unchanged. (If the loader
	// regressed to keying EvidenceID on the envelope FactID, this row would carry a
	// new EvidenceID per generation and the keys would diverge.)
	keyA := ServiceIncidentEvidenceKey("svc", serviceIncidentEvidenceIdentity(genA))
	keyB := ServiceIncidentEvidenceKey("svc", serviceIncidentEvidenceIdentity(genB))
	if keyA != keyB {
		t.Fatalf("same durable routing row must key identically across generations: %q != %q", keyA, keyB)
	}
}

func TestServiceIncidentEvidenceKeyDistinguishesRoutingShape(t *testing.T) {
	t.Parallel()

	base := appliedRoutingRecord()
	differentSlot := base
	differentSlot.Slot = "live_routing"
	differentIncident := base
	differentIncident.ProviderIncidentID = "PINC-2"
	differentEvidence := base
	differentEvidence.EvidenceID = base.EvidenceID + ":other"
	differentKind := base
	differentKind.EvidenceKind = facts.IncidentRoutingObservedPagerDutyServiceFactKind
	differentProvider := base
	differentProvider.Provider = "opsgenie"

	keyBase := ServiceIncidentEvidenceKey("svc", serviceIncidentEvidenceIdentity(base))
	for name, variant := range map[string]ServiceIncidentRecord{
		"slot":     differentSlot,
		"incident": differentIncident,
		"evidence": differentEvidence,
		"kind":     differentKind,
		"provider": differentProvider,
	} {
		if keyBase == ServiceIncidentEvidenceKey("svc", serviceIncidentEvidenceIdentity(variant)) {
			t.Fatalf("different %s must yield a distinct incidents identity", name)
		}
	}
}

func TestBuildServiceIncidentEvidenceDropsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	evidence := buildServiceIncidentEvidence([]ServiceIncidentRecord{
		appliedRoutingRecord(),
		// Missing evidence id (the durable StableFactKey was unavailable, only the
		// generation-bearing FactID existed): cannot be keyed, must be dropped rather
		// than keyed on a generation-bearing or empty identity.
		{Provider: "pagerduty", ProviderIncidentID: "PINC-9", Slot: "applied_routing", EvidenceKind: "x"},
		// Missing provider incident id: dropped.
		{Provider: "pagerduty", Slot: "applied_routing", EvidenceKind: "x", EvidenceID: "k"},
		// Missing slot: dropped.
		{Provider: "pagerduty", ProviderIncidentID: "PINC-9", EvidenceKind: "x", EvidenceID: "k"},
	})
	if len(evidence) != 1 {
		t.Fatalf("incidents evidence rows = %d, want 1 (only the fully-identified record)", len(evidence))
	}
	if len(evidence[0].Payload) == 0 {
		t.Fatal("incidents evidence row missing payload for hash classification")
	}
}

func TestBuildServiceIncidentEvidenceDeterministicAndDeduped(t *testing.T) {
	t.Parallel()

	record := appliedRoutingRecord()
	first := buildServiceIncidentEvidence([]ServiceIncidentRecord{record, record})
	if len(first) != 1 {
		t.Fatalf("duplicate incident routing rows must dedupe to one row, got %d", len(first))
	}

	high := appliedRoutingRecord()
	high.Slot = "z_slot"
	low := appliedRoutingRecord()
	low.Slot = "a_slot"
	unordered := buildServiceIncidentEvidence([]ServiceIncidentRecord{high, low})
	if len(unordered) != 2 || unordered[0].Identity >= unordered[1].Identity {
		t.Fatalf("incidents evidence must be ordered by identity: %#v", unordered)
	}
}

func TestServiceIncidentEvidencePayloadExcludesGenerationFields(t *testing.T) {
	t.Parallel()

	payload := serviceIncidentEvidencePayload(appliedRoutingRecord())
	for _, forbidden := range []string{"fact_id", "generation_id", "state_generation_id"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("incidents payload must not carry generation-bearing field %q: %#v", forbidden, payload)
		}
	}
	if payload["provider"] != "pagerduty" || payload["slot"] != "applied_routing" {
		t.Fatalf("incidents payload missing durable identity fields: %#v", payload)
	}
}

func TestServiceMaterializationWriterCommitsIncidentsFamily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-app",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
		},
		Incidents: []ServiceIncidentEvidence{
			{
				Identity: serviceIncidentEvidenceIdentity(appliedRoutingRecord()),
				Payload:  serviceIncidentEvidencePayload(appliedRoutingRecord()),
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceMaterialization() error = %v, want nil", err)
	}
	if !result.Committed {
		t.Fatal("first materialization should commit a new generation")
	}
	if result.EvidenceRows != 2 {
		t.Fatalf("EvidenceRows = %d, want 2 (ownership + incidents)", result.EvidenceRows)
	}

	var sawIncidents, sawOwnership bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		switch {
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyIncidents+":svc-app:"):
			sawIncidents = true
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":svc-app:"):
			sawOwnership = true
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !sawIncidents {
		t.Fatal("incidents-family snapshot row was not written")
	}
	if !sawOwnership {
		t.Fatal("ownership-family snapshot row regressed")
	}
}

func TestServiceMaterializationWriterTombstonesRetiredIncident(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Incidents: []ServiceIncidentEvidence{
			{Identity: "keep", Payload: map[string]any{"truth_label": "exact"}},
			{Identity: "gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceIncidentEvidenceKey("svc-a", "gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired incidents evidence must be written as a tombstone row, never silently absent")
	}
}

func TestServiceMaterializationIncidentsChangeFlipsGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Incidents: []ServiceIncidentEvidence{{Identity: "row-1", Payload: map[string]any{"declared_match_state": "match"}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	// Identical incidents evidence is a no-op (anti-churn across re-materializations).
	same, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Incidents: []ServiceIncidentEvidence{{Identity: "row-1", Payload: map[string]any{"declared_match_state": "match"}}},
	})
	if err != nil {
		t.Fatalf("idempotent write error = %v", err)
	}
	if same.Committed || same.GenerationID != first.GenerationID {
		t.Fatalf("identical incidents evidence must be a no-op: committed=%v gen=%q->%q", same.Committed, first.GenerationID, same.GenerationID)
	}
	// A changed incidents payload (drift) must flip the generation.
	changed, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Incidents: []ServiceIncidentEvidence{{Identity: "row-1", Payload: map[string]any{"declared_match_state": "drifted"}}},
	})
	if err != nil {
		t.Fatalf("changed write error = %v", err)
	}
	if !changed.Committed || changed.GenerationID == first.GenerationID {
		t.Fatalf("changed incidents payload must commit a new generation, got committed=%v gen=%q", changed.Committed, changed.GenerationID)
	}
}

// fakeServiceScopedIncidentLoader returns incident routing records keyed by Eshu
// catalog service id.
type fakeServiceScopedIncidentLoader struct {
	byService map[string][]ServiceIncidentRecord
	calls     int
}

func (f *fakeServiceScopedIncidentLoader) GetIncidentEvidenceForServices(
	_ context.Context,
	serviceIDs []string,
) (map[string][]ServiceIncidentRecord, error) {
	f.calls++
	out := map[string][]ServiceIncidentRecord{}
	for _, serviceID := range serviceIDs {
		out[serviceID] = f.byService[serviceID]
	}
	return out, nil
}

func TestServiceCatalogHandlerCommitsIncidentsFamilyWhenWired(t *testing.T) {
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
	incidentLoader := &fakeServiceScopedIncidentLoader{
		byService: map[string][]ServiceIncidentRecord{
			"svc-checkout": {appliedRoutingRecord()},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:             loader,
		Writer:                 &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter:  PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		IncidentEvidenceLoader: incidentLoader,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if incidentLoader.calls != 1 {
		t.Fatalf("incident loader calls = %d, want 1 bounded load", incidentLoader.calls)
	}

	var incidentRows int
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyIncidents+":") {
				incidentRows++
			}
		}
	}
	if incidentRows == 0 {
		t.Fatal("expected at least one incidents-family snapshot row after correlation with routing evidence")
	}
}

func TestServiceCatalogHandlerSkipsIncidentsFamilyWhenLoaderNil(t *testing.T) {
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
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:            loader,
		Writer:                &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter: PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		// IncidentEvidenceLoader intentionally nil: prior-families-only path.
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "gen",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyIncidents+":") {
				t.Fatal("incidents family must not materialize when no loader is wired")
			}
		}
	}
}
