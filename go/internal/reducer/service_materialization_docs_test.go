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

func TestServiceDocumentationEvidenceKeyIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	record := ServiceDocumentationRecord{
		SourceSystem:   "confluence",
		SourceRecordID: "section:deploy",
		DocumentID:     "doc:runbook",
	}
	// The identity must be derived from the fact's generation-independent external
	// identity (source_system:source_record_id:document_id), never from a
	// generation-bearing fact_id.
	key := ServiceDocumentationEvidenceKey("svc-app", serviceDocumentationEvidenceIdentity(record))
	if !strings.HasPrefix(key, ServiceEvidenceFamilyDocs+":svc-app:") {
		t.Fatalf("docs evidence key %q is not the docs:<service>:<identity> shape", key)
	}

	// The fact_id digests the generation id (the documentation collectors embed
	// generation_id into FactID), so keying on it would produce 100% false churn.
	// The docs key must embed none of: a fact_id, a generation id.
	for _, generationBearing := range []string{"generation-a", "fact-id-1234", "gen-b"} {
		if strings.Contains(key, generationBearing) {
			t.Fatalf("docs evidence key %q must not embed a generation-bearing token %q", key, generationBearing)
		}
	}

	// Anti-churn guard: the same logical documentation fact must produce the same
	// key regardless of which fact generation surfaced it, so the FULL OUTER JOIN
	// diff classifies it unchanged instead of churning every generation.
	again := ServiceDocumentationEvidenceKey("svc-app", serviceDocumentationEvidenceIdentity(record))
	if key != again {
		t.Fatalf("docs identity not stable across calls: %q != %q", key, again)
	}
}

func TestServiceDocumentationEvidenceKeyDistinguishesRecordShape(t *testing.T) {
	t.Parallel()

	base := ServiceDocumentationRecord{
		SourceSystem:   "confluence",
		SourceRecordID: "section:deploy",
		DocumentID:     "doc:runbook",
	}
	differentDoc := base
	differentDoc.DocumentID = "doc:other"
	differentRecord := base
	differentRecord.SourceRecordID = "section:overview"
	differentSystem := base
	differentSystem.SourceSystem = "git_markdown"

	keyBase := ServiceDocumentationEvidenceKey("svc", serviceDocumentationEvidenceIdentity(base))
	keyDoc := ServiceDocumentationEvidenceKey("svc", serviceDocumentationEvidenceIdentity(differentDoc))
	keyRecord := ServiceDocumentationEvidenceKey("svc", serviceDocumentationEvidenceIdentity(differentRecord))
	keySystem := ServiceDocumentationEvidenceKey("svc", serviceDocumentationEvidenceIdentity(differentSystem))

	if keyBase == keyDoc {
		t.Fatal("different document id must yield a distinct docs identity")
	}
	if keyBase == keyRecord {
		t.Fatal("different source record id must yield a distinct docs identity")
	}
	if keyBase == keySystem {
		t.Fatal("different source system must yield a distinct docs identity")
	}
}

func TestBuildServiceDocumentationEvidenceDropsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	evidence := buildServiceDocumentationEvidence([]ServiceDocumentationRecord{
		{SourceSystem: "confluence", SourceRecordID: "section:a", DocumentID: "doc:1", FactKind: facts.DocumentationEntityMentionFactKind},
		// Missing document id: cannot be keyed, must be dropped rather than keyed
		// on an empty identity.
		{SourceSystem: "confluence", SourceRecordID: "section:b"},
		// Missing source record id: dropped.
		{SourceSystem: "confluence", DocumentID: "doc:2"},
	})
	if len(evidence) != 1 {
		t.Fatalf("docs evidence rows = %d, want 1 (only the fully-identified record)", len(evidence))
	}
	if len(evidence[0].Payload) == 0 {
		t.Fatal("docs evidence row missing payload for hash classification")
	}
}

func TestBuildServiceDocumentationEvidenceDeterministicAndDeduped(t *testing.T) {
	t.Parallel()

	record := ServiceDocumentationRecord{SourceSystem: "confluence", SourceRecordID: "section:a", DocumentID: "doc:1"}
	first := buildServiceDocumentationEvidence([]ServiceDocumentationRecord{record, record})
	if len(first) != 1 {
		t.Fatalf("duplicate documentation records must dedupe to one row, got %d", len(first))
	}

	unordered := buildServiceDocumentationEvidence([]ServiceDocumentationRecord{
		{SourceSystem: "confluence", SourceRecordID: "section:z", DocumentID: "doc:1"},
		{SourceSystem: "confluence", SourceRecordID: "section:a", DocumentID: "doc:1"},
	})
	if len(unordered) != 2 || unordered[0].Identity >= unordered[1].Identity {
		t.Fatalf("docs evidence must be ordered by identity: %#v", unordered)
	}
}

func TestServiceDocumentationEvidencePayloadExcludesGenerationFields(t *testing.T) {
	t.Parallel()

	payload := serviceDocumentationEvidencePayload(ServiceDocumentationRecord{
		SourceSystem:    "confluence",
		SourceRecordID:  "section:deploy",
		DocumentID:      "doc:runbook",
		FactKind:        facts.DocumentationClaimCandidateFactKind,
		SourceURI:       "https://wiki/runbook#deploy",
		ObservationHash: "hash-1",
	})
	for _, forbidden := range []string{"fact_id", "generation_id"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("docs payload must not carry generation-bearing field %q: %#v", forbidden, payload)
		}
	}
	if payload["source_system"] != "confluence" || payload["document_id"] != "doc:runbook" {
		t.Fatalf("docs payload missing durable identity fields: %#v", payload)
	}
}

func TestServiceMaterializationWriterCommitsDocsFamily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-app",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
		},
		Docs: []ServiceDocumentationEvidence{
			{Identity: "confluence:section:deploy:doc:runbook", Payload: map[string]any{"fact_kind": facts.DocumentationEntityMentionFactKind}},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceMaterialization() error = %v, want nil", err)
	}
	if !result.Committed {
		t.Fatal("first materialization should commit a new generation")
	}
	if result.EvidenceRows != 2 {
		t.Fatalf("EvidenceRows = %d, want 2 (ownership + docs)", result.EvidenceRows)
	}

	var sawDocs, sawOwnership bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		switch {
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDocs+":svc-app:"):
			sawDocs = true
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":svc-app:"):
			sawOwnership = true
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !sawDocs {
		t.Fatal("docs-family snapshot row was not written")
	}
	if !sawOwnership {
		t.Fatal("ownership-family snapshot row regressed")
	}
}

func TestServiceMaterializationWriterTombstonesRetiredDoc(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Docs: []ServiceDocumentationEvidence{
			{Identity: "keep", Payload: map[string]any{"fact_kind": "x"}},
			{Identity: "gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceDocumentationEvidenceKey("svc-a", "gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired docs evidence must be written as a tombstone row, never silently absent")
	}
}

func TestServiceMaterializationDocsChangeFlipsGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Docs:      []ServiceDocumentationEvidence{{Identity: "rec-1", Payload: map[string]any{"observation_hash": "h1"}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	// Identical docs evidence is a no-op (anti-churn across re-materializations).
	same, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Docs:      []ServiceDocumentationEvidence{{Identity: "rec-1", Payload: map[string]any{"observation_hash": "h1"}}},
	})
	if err != nil {
		t.Fatalf("idempotent write error = %v", err)
	}
	if same.Committed || same.GenerationID != first.GenerationID {
		t.Fatalf("identical docs evidence must be a no-op: committed=%v gen=%q->%q", same.Committed, first.GenerationID, same.GenerationID)
	}
	// A changed docs payload must flip the generation.
	changed, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Docs:      []ServiceDocumentationEvidence{{Identity: "rec-1", Payload: map[string]any{"observation_hash": "h2"}}},
	})
	if err != nil {
		t.Fatalf("changed write error = %v", err)
	}
	if !changed.Committed || changed.GenerationID == first.GenerationID {
		t.Fatalf("changed docs payload must commit a new generation, got committed=%v gen=%q", changed.Committed, changed.GenerationID)
	}
}

// fakeServiceScopedDocsLoader returns documentation records keyed by service id.
type fakeServiceScopedDocsLoader struct {
	byService map[string][]ServiceDocumentationRecord
	calls     int
}

func (f *fakeServiceScopedDocsLoader) GetDocumentationEvidenceForServices(
	_ context.Context,
	serviceIDs []string,
) (map[string][]ServiceDocumentationRecord, error) {
	f.calls++
	out := map[string][]ServiceDocumentationRecord{}
	for _, serviceID := range serviceIDs {
		out[serviceID] = f.byService[serviceID]
	}
	return out, nil
}

func TestServiceCatalogHandlerCommitsDocsFamilyWhenWired(t *testing.T) {
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
	// The correlated service id is the service-typed entity's service_id payload.
	docsLoader := &fakeServiceScopedDocsLoader{
		byService: map[string][]ServiceDocumentationRecord{
			"svc-checkout": {
				{SourceSystem: "confluence", SourceRecordID: "section:deploy", DocumentID: "doc:runbook", FactKind: facts.DocumentationEntityMentionFactKind},
			},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                  loader,
		Writer:                      &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter:       PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		DocumentationEvidenceLoader: docsLoader,
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
	if docsLoader.calls != 1 {
		t.Fatalf("docs loader calls = %d, want 1 bounded load", docsLoader.calls)
	}

	var docsRows int
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDocs+":") {
				docsRows++
			}
		}
	}
	if docsRows == 0 {
		t.Fatal("expected at least one docs-family snapshot row after correlation with a referencing documentation fact")
	}
}

func TestServiceCatalogHandlerSkipsDocsFamilyWhenLoaderNil(t *testing.T) {
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
		// DocumentationEvidenceLoader intentionally nil: prior-families-only path.
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
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDocs+":") {
				t.Fatal("docs family must not materialize when no loader is wired")
			}
		}
	}
}
