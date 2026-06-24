// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestServiceDeploymentEvidenceKeyIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	rel := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-checkout",
		TargetRepoID:     "repo-deploy",
		RelationshipType: relationships.RelDeploysFrom,
	}
	// The identity must be derived from the relationship's generation-independent
	// natural key, never from the generation-embedding resolved_id digest.
	key := ServiceDeploymentEvidenceKey("svc-checkout", serviceDeploymentEvidenceIdentity(rel))
	if !strings.HasPrefix(key, ServiceEvidenceFamilyDeployment+":svc-checkout:") {
		t.Fatalf("deployment evidence key %q is not the deployment:<service>:<identity> shape", key)
	}

	genA := relationships.ResolvedRelationshipID("gen-a", rel, 0)
	genB := relationships.ResolvedRelationshipID("gen-b", rel, 0)
	if genA == genB {
		t.Fatalf("resolved_id should embed the generation, but %q == %q", genA, genB)
	}
	if strings.Contains(key, genA) || strings.Contains(key, genB) {
		t.Fatalf("deployment evidence key %q must not embed a generation-bearing resolved_id", key)
	}

	// The same logical relationship must produce the same identity regardless of
	// generation, so the FULL OUTER JOIN diff can match updated/unchanged.
	again := ServiceDeploymentEvidenceKey("svc-checkout", serviceDeploymentEvidenceIdentity(rel))
	if key != again {
		t.Fatalf("identity not stable across calls: %q != %q", key, again)
	}
}

func TestServiceDeploymentEvidenceKeyDistinguishesRelationshipShape(t *testing.T) {
	t.Parallel()

	base := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: relationships.RelDeploysFrom,
	}
	differentTarget := base
	differentTarget.TargetRepoID = "repo-c"
	differentType := base
	differentType.RelationshipType = relationships.RelRunsOn

	keyBase := ServiceDeploymentEvidenceKey("svc", serviceDeploymentEvidenceIdentity(base))
	keyTarget := ServiceDeploymentEvidenceKey("svc", serviceDeploymentEvidenceIdentity(differentTarget))
	keyType := ServiceDeploymentEvidenceKey("svc", serviceDeploymentEvidenceIdentity(differentType))

	if keyBase == keyTarget {
		t.Fatal("different target repo must yield a distinct deployment identity")
	}
	if keyBase == keyType {
		t.Fatal("different relationship type must yield a distinct deployment identity")
	}
}

func TestBuildServiceDeploymentEvidenceFiltersToDeploymentTypes(t *testing.T) {
	t.Parallel()

	evidence := buildServiceDeploymentEvidence([]relationships.ResolvedRelationship{
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-deploy", RelationshipType: relationships.RelDeploysFrom, Confidence: 0.9},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-cfg", RelationshipType: relationships.RelDiscoversConfigIn},
		{SourceRepoID: "repo-svc", TargetEntityID: "platform-eks", RelationshipType: relationships.RelRunsOn},
		// Non-deployment relationships must be excluded from the deployment family.
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-lib", RelationshipType: relationships.RelDependsOn},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-mod", RelationshipType: relationships.RelUsesModule},
	})
	if len(evidence) != 3 {
		t.Fatalf("deployment evidence rows = %d, want 3 (DEPLOYS_FROM, DISCOVERS_CONFIG_IN, RUNS_ON)", len(evidence))
	}
	for _, row := range evidence {
		if row.Identity == "" {
			t.Fatal("deployment evidence row missing stable identity")
		}
		if len(row.Payload) == 0 {
			t.Fatal("deployment evidence row missing payload for hash classification")
		}
	}
}

func TestBuildServiceDeploymentEvidenceDeterministicAndDeduped(t *testing.T) {
	t.Parallel()

	rel := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-svc",
		TargetRepoID:     "repo-deploy",
		RelationshipType: relationships.RelDeploysFrom,
	}
	first := buildServiceDeploymentEvidence([]relationships.ResolvedRelationship{rel, rel})
	if len(first) != 1 {
		t.Fatalf("duplicate relationships must dedupe to one row, got %d", len(first))
	}

	unordered := buildServiceDeploymentEvidence([]relationships.ResolvedRelationship{
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-z", RelationshipType: relationships.RelDeploysFrom},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-a", RelationshipType: relationships.RelDeploysFrom},
	})
	if len(unordered) != 2 || unordered[0].Identity >= unordered[1].Identity {
		t.Fatalf("deployment evidence must be ordered by identity: %#v", unordered)
	}
}

func TestServiceMaterializationWriterCommitsDeploymentFamily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-checkout",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
		},
		Deployment: []ServiceDeploymentEvidence{
			{Identity: "deploys_from|repo-svc|repo-deploy||", Payload: map[string]any{"relationship_type": "DEPLOYS_FROM"}},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceMaterialization() error = %v, want nil", err)
	}
	if !result.Committed {
		t.Fatal("first materialization should commit a new generation")
	}
	if result.EvidenceRows != 2 {
		t.Fatalf("EvidenceRows = %d, want 2 (ownership + deployment)", result.EvidenceRows)
	}

	var sawDeployment, sawOwnership bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		switch {
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDeployment+":svc-checkout:"):
			sawDeployment = true
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":svc-checkout:"):
			sawOwnership = true
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !sawDeployment {
		t.Fatal("deployment-family snapshot row was not written")
	}
	if !sawOwnership {
		t.Fatal("ownership-family snapshot row regressed")
	}
}

func TestServiceMaterializationWriterTombstonesRetiredDeployment(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Deployment: []ServiceDeploymentEvidence{
			{Identity: "keep", Payload: map[string]any{"relationship_type": "DEPLOYS_FROM"}},
			{Identity: "gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceDeploymentEvidenceKey("svc-a", "gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired deployment evidence must be written as a tombstone row, never silently absent")
	}
}

// serviceTypedCatalogEntityFact builds a catalog entity fact carrying an explicit
// service_id, so the correlation decision admits a non-empty ServiceID and the
// per-service materialization (ownership + deployment) runs.
func serviceTypedCatalogEntityFact(factID, entityRef, displayName string) facts.Envelope {
	envelope := serviceCatalogEntityFact(factID, entityRef, displayName)
	envelope.Payload["entity_type"] = "service"
	envelope.Payload["service_id"] = "svc-checkout"
	return envelope
}

// fakeRepoScopedResolvedLoader returns deployment relationships keyed by repo.
type fakeRepoScopedResolvedLoader struct {
	byRepo map[string][]relationships.ResolvedRelationship
	calls  int
}

func (f *fakeRepoScopedResolvedLoader) GetResolvedRelationshipsForRepos(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, error) {
	f.calls++
	out := []relationships.ResolvedRelationship{}
	for _, repoID := range repoIDs {
		out = append(out, f.byRepo[repoID]...)
	}
	return out, nil
}

func TestServiceCatalogHandlerCommitsDeploymentFamilyWhenWired(t *testing.T) {
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
	deploymentLoader := &fakeRepoScopedResolvedLoader{
		byRepo: map[string][]relationships.ResolvedRelationship{
			"repo-checkout": {
				{SourceRepoID: "repo-checkout", TargetRepoID: "repo-deploy", RelationshipType: relationships.RelDeploysFrom, Confidence: 0.9},
			},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   loader,
		Writer:                       &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter:        PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		DeploymentRelationshipLoader: deploymentLoader,
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
	if deploymentLoader.calls != 1 {
		t.Fatalf("deployment loader calls = %d, want 1 bounded load", deploymentLoader.calls)
	}

	var deploymentRows int
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDeployment+":") {
				deploymentRows++
			}
		}
	}
	if deploymentRows == 0 {
		t.Fatal("expected at least one deployment-family snapshot row after correlation with a deployment relationship")
	}
}

func TestServiceCatalogHandlerSkipsDeploymentFamilyWhenLoaderNil(t *testing.T) {
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
		// DeploymentRelationshipLoader intentionally nil: Stage-1 ownership-only path.
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
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDeployment+":") {
				t.Fatal("deployment family must not materialize when no loader is wired")
			}
		}
	}
}

func TestServiceMaterializationDeploymentChangeFlipsGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:  "svc-a",
		Deployment: []ServiceDeploymentEvidence{{Identity: "rel-1", Payload: map[string]any{"confidence": 0.5}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	// Identical deployment evidence is a no-op.
	same, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:  "svc-a",
		Deployment: []ServiceDeploymentEvidence{{Identity: "rel-1", Payload: map[string]any{"confidence": 0.5}}},
	})
	if err != nil {
		t.Fatalf("idempotent write error = %v", err)
	}
	if same.Committed || same.GenerationID != first.GenerationID {
		t.Fatalf("identical deployment evidence must be a no-op: committed=%v gen=%q->%q", same.Committed, first.GenerationID, same.GenerationID)
	}
	// A changed deployment payload must flip the generation.
	changed, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:  "svc-a",
		Deployment: []ServiceDeploymentEvidence{{Identity: "rel-1", Payload: map[string]any{"confidence": 0.9}}},
	})
	if err != nil {
		t.Fatalf("changed write error = %v", err)
	}
	if !changed.Committed || changed.GenerationID == first.GenerationID {
		t.Fatalf("changed deployment payload must commit a new generation, got committed=%v gen=%q", changed.Committed, changed.GenerationID)
	}
}
