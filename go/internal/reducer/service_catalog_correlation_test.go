// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type stubServiceCatalogCorrelationFactLoader struct {
	scopeFacts      []facts.Envelope
	activeRepos     []facts.Envelope
	kindCalls       [][]string
	repositoryCalls int
}

func (s *stubServiceCatalogCorrelationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubServiceCatalogCorrelationFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubServiceCatalogCorrelationFactLoader) ListActiveRepositoryFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.repositoryCalls++
	return append([]facts.Envelope(nil), s.activeRepos...), nil
}

type recordingServiceCatalogCorrelationWriter struct {
	write ServiceCatalogCorrelationWrite
	calls int
}

func (w *recordingServiceCatalogCorrelationWriter) WriteServiceCatalogCorrelations(
	_ context.Context,
	write ServiceCatalogCorrelationWrite,
) (ServiceCatalogCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return ServiceCatalogCorrelationWriteResult{
		FactsWritten: len(write.Decisions),
	}, nil
}

func TestBuildServiceCatalogCorrelationDecisionsClassifiesRepositoryEvidence(t *testing.T) {
	t.Parallel()

	decisions := BuildServiceCatalogCorrelationDecisions([]facts.Envelope{
		serviceCatalogEntityFact("entity-exact", "component:default/checkout", "Checkout"),
		serviceCatalogOwnershipFact("owner-exact", "component:default/checkout", "group:default/payments"),
		serviceCatalogRepositoryLinkFact("repo-link-exact", "component:default/checkout", "https://github.com/acme/checkout.git"),
		repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		serviceCatalogEntityFact("entity-unresolved", "component:default/docs-only", "Docs Only"),
		serviceCatalogEntityFact("entity-ambiguous", "component:default/shared", "Shared"),
		serviceCatalogRepositoryLinkFact("repo-link-ambiguous", "component:default/shared", "https://github.com/acme/shared.git"),
		repositoryFact("repo-shared-1", "shared-a", "https://github.com/acme/shared.git", false),
		repositoryFact("repo-shared-2", "shared-b", "git@github.com:acme/shared.git", false),
		serviceCatalogEntityFact("entity-rejected", "component:default/name-only", "Name Only"),
		serviceCatalogRepositoryLinkWithNameOnlyFact("repo-link-rejected", "component:default/name-only", "checkout"),
	})

	got := serviceCatalogDecisionsByEntity(decisions)
	assertServiceCatalogDecision(t, got["component:default/checkout"], ServiceCatalogCorrelationExact)
	if got["component:default/checkout"].RepositoryID != "repo-checkout" {
		t.Fatalf("exact RepositoryID = %q, want repo-checkout", got["component:default/checkout"].RepositoryID)
	}
	if got["component:default/checkout"].OwnerRef != "group:default/payments" {
		t.Fatalf("exact OwnerRef = %q, want group:default/payments", got["component:default/checkout"].OwnerRef)
	}
	if got["component:default/checkout"].ProvenanceOnly {
		t.Fatal("exact ProvenanceOnly = true, want false")
	}
	assertServiceCatalogDecision(t, got["component:default/docs-only"], ServiceCatalogCorrelationUnresolved)
	assertServiceCatalogDecision(t, got["component:default/shared"], ServiceCatalogCorrelationAmbiguous)
	if !slices.Equal(got["component:default/shared"].CandidateRepositoryIDs, []string{"repo-shared-1", "repo-shared-2"}) {
		t.Fatalf("ambiguous candidates = %v", got["component:default/shared"].CandidateRepositoryIDs)
	}
	assertServiceCatalogDecision(t, got["component:default/name-only"], ServiceCatalogCorrelationRejected)
}

func TestBuildServiceCatalogCorrelationDecisionsAdmitsRepoLocalDescriptorScope(t *testing.T) {
	t.Parallel()

	decisions := BuildServiceCatalogCorrelationDecisions([]facts.Envelope{
		serviceCatalogEntityFactWithScope(
			"entity-local",
			"git-repository-scope:repo-checkout",
			"component:default/checkout",
			"Checkout",
		),
		serviceCatalogEntityFactWithScope(
			"entity-stale",
			"git-repository-scope:repo-archived",
			"component:default/archived",
			"Archived",
		),
		serviceCatalogEntityFact("entity-unscoped", "component:default/unscoped", "Unscoped"),
		repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		repositoryFact("repo-archived", "archived", "https://github.com/acme/archived.git", true),
	})

	got := serviceCatalogDecisionsByEntity(decisions)
	local := got["component:default/checkout"]
	assertServiceCatalogDecision(t, local, ServiceCatalogCorrelationExact)
	if got, want := local.RepositoryID, "repo-checkout"; got != want {
		t.Fatalf("local RepositoryID = %q, want %q", got, want)
	}
	if got, want := local.ServiceID, "component:default/checkout"; got != want {
		t.Fatalf("local ServiceID = %q, want %q", got, want)
	}
	if local.WorkloadID != "" {
		t.Fatalf("local WorkloadID = %q, want empty without workload proof", local.WorkloadID)
	}
	if local.ProvenanceOnly {
		t.Fatal("local ProvenanceOnly = true, want false")
	}
	if got, want := local.Reason, "repo-local catalog descriptor scope matches canonical repository identity"; got != want {
		t.Fatalf("local Reason = %q, want %q", got, want)
	}
	if !slices.Equal(local.EvidenceFactIDs, []string{"entity-local", "repo-checkout"}) {
		t.Fatalf("local EvidenceFactIDs = %#v, want entity and repository facts", local.EvidenceFactIDs)
	}

	stale := got["component:default/archived"]
	assertServiceCatalogDecision(t, stale, ServiceCatalogCorrelationStale)
	if !slices.Equal(stale.CandidateRepositoryIDs, []string{"repo-archived"}) {
		t.Fatalf("stale candidates = %v, want repo-archived", stale.CandidateRepositoryIDs)
	}
	if !slices.Equal(stale.EvidenceFactIDs, []string{"entity-stale", "repo-archived"}) {
		t.Fatalf("stale EvidenceFactIDs = %#v, want entity and tombstoned repository facts", stale.EvidenceFactIDs)
	}

	unscoped := got["component:default/unscoped"]
	assertServiceCatalogDecision(t, unscoped, ServiceCatalogCorrelationUnresolved)
	if unscoped.RepositoryID != "" {
		t.Fatalf("unscoped RepositoryID = %q, want empty", unscoped.RepositoryID)
	}
}

func TestBuildServiceCatalogCorrelationDecisionsKeepsProviderScopesSeparate(t *testing.T) {
	t.Parallel()

	const entityRef = "component:default/checkout"
	decisions := BuildServiceCatalogCorrelationDecisions([]facts.Envelope{
		serviceCatalogEntityProviderFact("entity-backstage", "backstage", entityRef, "Checkout"),
		serviceCatalogOwnershipProviderFact("owner-backstage", "backstage", entityRef, "group:default/payments"),
		serviceCatalogRepositoryLinkProviderFact("repo-link-backstage", "backstage", entityRef, "https://github.com/acme/checkout.git"),
		serviceCatalogEntityProviderFact("entity-cortex", "cortex", entityRef, "Checkout"),
		serviceCatalogOwnershipProviderFact("owner-cortex", "cortex", entityRef, "team:checkout"),
		serviceCatalogRepositoryLinkNameProviderFact("repo-link-cortex", "cortex", entityRef, "checkout"),
		repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
	})

	got := serviceCatalogDecisionsByProviderEntity(decisions)
	if gotLen, want := len(got), 2; gotLen != want {
		t.Fatalf("provider/entity decisions = %d, want %d: %#v", gotLen, want, decisions)
	}
	backstage := got["backstage|"+entityRef]
	assertServiceCatalogDecision(t, backstage, ServiceCatalogCorrelationExact)
	if backstage.RepositoryID != "repo-checkout" {
		t.Fatalf("backstage RepositoryID = %q, want repo-checkout", backstage.RepositoryID)
	}
	if backstage.OwnerRef != "group:default/payments" {
		t.Fatalf("backstage OwnerRef = %q, want group:default/payments", backstage.OwnerRef)
	}
	cortex := got["cortex|"+entityRef]
	assertServiceCatalogDecision(t, cortex, ServiceCatalogCorrelationRejected)
	if cortex.OwnerRef != "team:checkout" {
		t.Fatalf("cortex OwnerRef = %q, want team:checkout", cortex.OwnerRef)
	}
}

func TestBuildServiceCatalogCorrelationDecisionsPrefersStrongestRepositoryMatch(t *testing.T) {
	t.Parallel()

	const entityRef = "component:default/checkout"
	decisions := BuildServiceCatalogCorrelationDecisions([]facts.Envelope{
		serviceCatalogEntityFact("entity-checkout", entityRef, "Checkout"),
		serviceCatalogRepositoryLinkFact("repo-link-derived", entityRef, "git@github.com:acme/checkout.git"),
		serviceCatalogRepositoryIDLinkFact("repo-link-id", entityRef, "repo-checkout"),
		repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
	})

	got := serviceCatalogDecisionsByEntity(decisions)[entityRef]
	assertServiceCatalogDecision(t, got, ServiceCatalogCorrelationExact)
	if got.Reason != "catalog repository id matches canonical repository identity" {
		t.Fatalf("Reason = %q, want repository-id match reason", got.Reason)
	}
}

func TestServiceCatalogCorrelationHandlerLoadsActiveRepositoriesAndWritesFacts(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogRepositoryLinkFact("repo-link", "component:default/checkout", "https://github.com/acme/checkout.git"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
	writer := &recordingServiceCatalogCorrelationWriter{}
	handler := ServiceCatalogCorrelationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteServiceCatalogCorrelations() calls = %d, want 1", writer.calls)
	}
	if loader.repositoryCalls != 1 {
		t.Fatalf("ListActiveRepositoryFacts() calls = %d, want 1", loader.repositoryCalls)
	}
	if got, want := loader.kindCalls[0], serviceCatalogCorrelationFactKinds(); !slices.Equal(got, want) {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := result.Domain, DomainServiceCatalogCorrelation; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(writer.write.Decisions), 1; got != want {
		t.Fatalf("decisions = %d, want %d", got, want)
	}
}

func TestPostgresServiceCatalogCorrelationWriterPersistsReducerFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresServiceCatalogCorrelationWriter{DB: db}

	result, err := writer.WriteServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationWrite{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
		Decisions: []ServiceCatalogCorrelationDecision{
			{
				Provider:        "backstage",
				EntityRef:       "component:default/checkout",
				EntityType:      "component",
				DisplayName:     "Checkout",
				RepositoryID:    "repo-checkout",
				OwnerRef:        "group:default/payments",
				Lifecycle:       "production",
				Tier:            "tier_1",
				Outcome:         ServiceCatalogCorrelationExact,
				Reason:          "catalog repository link matches repository remote after git URL canonicalization",
				ProvenanceOnly:  false,
				EvidenceFactIDs: []string{"entity", "repo-link", "repo-checkout"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceCatalogCorrelations() error = %v, want nil", err)
	}
	if result.FactsWritten != 1 {
		t.Fatalf("FactsWritten = %d, want 1", result.FactsWritten)
	}
	if got, want := db.execs[0].args[3], serviceCatalogCorrelationFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	payload := unmarshalServiceCatalogCorrelationPayload(t, db.execs[0].args[14])
	if got, want := payload["entity_ref"], "component:default/checkout"; got != want {
		t.Fatalf("entity_ref = %#v, want %q", got, want)
	}
	if got, want := payload["outcome"], string(ServiceCatalogCorrelationExact); got != want {
		t.Fatalf("outcome = %#v, want %q", got, want)
	}
	if got, want := payload["repository_id"], "repo-checkout"; got != want {
		t.Fatalf("repository_id = %#v, want %q", got, want)
	}
}

func serviceCatalogDecisionsByEntity(
	decisions []ServiceCatalogCorrelationDecision,
) map[string]ServiceCatalogCorrelationDecision {
	out := make(map[string]ServiceCatalogCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.EntityRef] = decision
	}
	return out
}

func serviceCatalogDecisionsByProviderEntity(
	decisions []ServiceCatalogCorrelationDecision,
) map[string]ServiceCatalogCorrelationDecision {
	out := make(map[string]ServiceCatalogCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.Provider+"|"+decision.EntityRef] = decision
	}
	return out
}

func assertServiceCatalogDecision(
	t *testing.T,
	decision ServiceCatalogCorrelationDecision,
	want ServiceCatalogCorrelationOutcome,
) {
	t.Helper()
	if decision.Outcome != want {
		t.Fatalf("decision[%s].Outcome = %q, want %q; reason=%s", decision.EntityRef, decision.Outcome, want, decision.Reason)
	}
}

func serviceCatalogEntityFact(factID, entityRef, displayName string) facts.Envelope {
	return serviceCatalogEntityProviderFact(factID, "backstage", entityRef, displayName)
}

func serviceCatalogEntityFactWithScope(factID, scopeID, entityRef, displayName string) facts.Envelope {
	envelope := serviceCatalogEntityFact(factID, entityRef, displayName)
	envelope.ScopeID = scopeID
	envelope.Payload["entity_type"] = "service"
	return envelope
}

func serviceCatalogEntityProviderFact(factID, provider, entityRef, displayName string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogEntityFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":     provider,
			"entity_ref":   entityRef,
			"entity_type":  "component",
			"display_name": displayName,
			"lifecycle":    "production",
			"tier":         "tier_1",
		},
	}
}

func serviceCatalogOwnershipFact(factID, entityRef, ownerRef string) facts.Envelope {
	return serviceCatalogOwnershipProviderFact(factID, "backstage", entityRef, ownerRef)
}

func serviceCatalogOwnershipProviderFact(factID, provider, entityRef, ownerRef string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogOwnershipFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":   provider,
			"entity_ref": entityRef,
			"owner_ref":  ownerRef,
		},
	}
}

func serviceCatalogRepositoryLinkFact(factID, entityRef, repositoryURL string) facts.Envelope {
	return serviceCatalogRepositoryLinkProviderFact(factID, "backstage", entityRef, repositoryURL)
}

func serviceCatalogRepositoryLinkProviderFact(factID, provider, entityRef, repositoryURL string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogRepositoryLinkFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":       provider,
			"entity_ref":     entityRef,
			"repository_url": repositoryURL,
		},
	}
}

func serviceCatalogRepositoryIDLinkFact(factID, entityRef, repositoryID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogRepositoryLinkFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":      "backstage",
			"entity_ref":    entityRef,
			"repository_id": repositoryID,
		},
	}
}

func serviceCatalogRepositoryLinkWithNameOnlyFact(factID, entityRef, repositoryName string) facts.Envelope {
	return serviceCatalogRepositoryLinkNameProviderFact(factID, "backstage", entityRef, repositoryName)
}

func serviceCatalogRepositoryLinkNameProviderFact(factID, provider, entityRef, repositoryName string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.ServiceCatalogRepositoryLinkFactKind,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":        provider,
			"entity_ref":      entityRef,
			"repository_name": repositoryName,
		},
	}
}

func repositoryFact(factID, name, remoteURL string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      factID,
		FactKind:    factKindRepository,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"repo_id":    factID,
			"name":       name,
			"remote_url": remoteURL,
		},
	}
}

func unmarshalServiceCatalogCorrelationPayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload): %v", err)
	}
	return payload
}
