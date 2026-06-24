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

func TestServiceDependencyEvidenceKeyIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	rel := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-app",
		TargetRepoID:     "repo-lib",
		RelationshipType: relationships.RelDependsOn,
	}
	// The identity must be derived from the relationship's generation-independent
	// natural key, never from the generation-embedding resolved_id digest.
	key := ServiceDependencyEvidenceKey("svc-app", serviceDependencyEvidenceIdentity(rel))
	if !strings.HasPrefix(key, ServiceEvidenceFamilyDependencies+":svc-app:") {
		t.Fatalf("dependency evidence key %q is not the dependencies:<service>:<identity> shape", key)
	}

	// resolved_id embeds the resolution generation id (relationships/models.go
	// ResolvedRelationshipID digests generationID), so keying on it would produce
	// 100% false churn. Prove the resolved_id changes across generations and that
	// the dependency key embeds neither generation's resolved_id.
	genA := relationships.ResolvedRelationshipID("gen-a", rel, 0)
	genB := relationships.ResolvedRelationshipID("gen-b", rel, 0)
	if genA == genB {
		t.Fatalf("resolved_id should embed the generation, but %q == %q", genA, genB)
	}
	if strings.Contains(key, genA) || strings.Contains(key, genB) {
		t.Fatalf("dependency evidence key %q must not embed a generation-bearing resolved_id", key)
	}

	// Anti-churn guard: the same logical relationship must produce the same key
	// regardless of which resolution generation surfaced it, so the FULL OUTER
	// JOIN diff classifies it unchanged instead of churning every generation.
	again := ServiceDependencyEvidenceKey("svc-app", serviceDependencyEvidenceIdentity(rel))
	if key != again {
		t.Fatalf("dependency identity not stable across calls: %q != %q", key, again)
	}
}

func TestServiceDependencyEvidenceKeyDistinguishesRelationshipShape(t *testing.T) {
	t.Parallel()

	base := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: relationships.RelDependsOn,
	}
	differentTarget := base
	differentTarget.TargetRepoID = "repo-c"
	differentType := base
	differentType.RelationshipType = relationships.RelUsesModule

	keyBase := ServiceDependencyEvidenceKey("svc", serviceDependencyEvidenceIdentity(base))
	keyTarget := ServiceDependencyEvidenceKey("svc", serviceDependencyEvidenceIdentity(differentTarget))
	keyType := ServiceDependencyEvidenceKey("svc", serviceDependencyEvidenceIdentity(differentType))

	if keyBase == keyTarget {
		t.Fatal("different target repo must yield a distinct dependency identity")
	}
	if keyBase == keyType {
		t.Fatal("different relationship type must yield a distinct dependency identity")
	}
}

func TestBuildServiceDependencyEvidenceFiltersToDependencyTypes(t *testing.T) {
	t.Parallel()

	evidence := buildServiceDependencyEvidence([]relationships.ResolvedRelationship{
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-lib", RelationshipType: relationships.RelDependsOn, Confidence: 0.9},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-mod", RelationshipType: relationships.RelUsesModule},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-cfg", RelationshipType: relationships.RelReadsConfigFrom},
		// Deployment relationships must be excluded from the dependencies family.
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-deploy", RelationshipType: relationships.RelDeploysFrom},
		{SourceRepoID: "repo-svc", TargetEntityID: "platform-eks", RelationshipType: relationships.RelRunsOn},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-discover", RelationshipType: relationships.RelDiscoversConfigIn},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-prov", RelationshipType: relationships.RelProvisionsDependencyFor},
	})
	if len(evidence) != 3 {
		t.Fatalf("dependency evidence rows = %d, want 3 (DEPENDS_ON, USES_MODULE, READS_CONFIG_FROM)", len(evidence))
	}
	for _, row := range evidence {
		if row.Identity == "" {
			t.Fatal("dependency evidence row missing stable identity")
		}
		if len(row.Payload) == 0 {
			t.Fatal("dependency evidence row missing payload for hash classification")
		}
	}
}

func TestBuildServiceDependencyEvidenceDeterministicAndDeduped(t *testing.T) {
	t.Parallel()

	rel := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-svc",
		TargetRepoID:     "repo-lib",
		RelationshipType: relationships.RelDependsOn,
	}
	first := buildServiceDependencyEvidence([]relationships.ResolvedRelationship{rel, rel})
	if len(first) != 1 {
		t.Fatalf("duplicate relationships must dedupe to one row, got %d", len(first))
	}

	unordered := buildServiceDependencyEvidence([]relationships.ResolvedRelationship{
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-z", RelationshipType: relationships.RelDependsOn},
		{SourceRepoID: "repo-svc", TargetRepoID: "repo-a", RelationshipType: relationships.RelDependsOn},
	})
	if len(unordered) != 2 || unordered[0].Identity >= unordered[1].Identity {
		t.Fatalf("dependency evidence must be ordered by identity: %#v", unordered)
	}
}

func TestServiceDependencyEvidencePayloadExcludesGenerationFields(t *testing.T) {
	t.Parallel()

	payload := serviceDependencyEvidencePayload(relationships.ResolvedRelationship{
		SourceRepoID:     "repo-svc",
		TargetRepoID:     "repo-lib",
		RelationshipType: relationships.RelDependsOn,
		Confidence:       0.8,
		ResolutionSource: relationships.ResolutionSourceInferred,
	})
	for _, forbidden := range []string{"generation_id", "resolved_id"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("dependency payload must not carry generation-bearing field %q: %#v", forbidden, payload)
		}
	}
	if payload["relationship_type"] != "DEPENDS_ON" || payload["target_repo_id"] != "repo-lib" {
		t.Fatalf("dependency payload missing durable identity fields: %#v", payload)
	}
}

func TestServiceMaterializationWriterCommitsDependenciesFamily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-app",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
		},
		Dependencies: []ServiceDependencyEvidence{
			{Identity: "depends_on|repo-svc|repo-lib||", Payload: map[string]any{"relationship_type": "DEPENDS_ON"}},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceMaterialization() error = %v, want nil", err)
	}
	if !result.Committed {
		t.Fatal("first materialization should commit a new generation")
	}
	if result.EvidenceRows != 2 {
		t.Fatalf("EvidenceRows = %d, want 2 (ownership + dependencies)", result.EvidenceRows)
	}

	var sawDependency, sawOwnership bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		switch {
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDependencies+":svc-app:"):
			sawDependency = true
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":svc-app:"):
			sawOwnership = true
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !sawDependency {
		t.Fatal("dependencies-family snapshot row was not written")
	}
	if !sawOwnership {
		t.Fatal("ownership-family snapshot row regressed")
	}
}

func TestServiceMaterializationWriterTombstonesRetiredDependency(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Dependencies: []ServiceDependencyEvidence{
			{Identity: "keep", Payload: map[string]any{"relationship_type": "DEPENDS_ON"}},
			{Identity: "gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceDependencyEvidenceKey("svc-a", "gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired dependency evidence must be written as a tombstone row, never silently absent")
	}
}

func TestServiceMaterializationDependencyChangeFlipsGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:    "svc-a",
		Dependencies: []ServiceDependencyEvidence{{Identity: "rel-1", Payload: map[string]any{"confidence": 0.5}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	// Identical dependency evidence is a no-op.
	same, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:    "svc-a",
		Dependencies: []ServiceDependencyEvidence{{Identity: "rel-1", Payload: map[string]any{"confidence": 0.5}}},
	})
	if err != nil {
		t.Fatalf("idempotent write error = %v", err)
	}
	if same.Committed || same.GenerationID != first.GenerationID {
		t.Fatalf("identical dependency evidence must be a no-op: committed=%v gen=%q->%q", same.Committed, first.GenerationID, same.GenerationID)
	}
	// A changed dependency payload must flip the generation.
	changed, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:    "svc-a",
		Dependencies: []ServiceDependencyEvidence{{Identity: "rel-1", Payload: map[string]any{"confidence": 0.9}}},
	})
	if err != nil {
		t.Fatalf("changed write error = %v", err)
	}
	if !changed.Committed || changed.GenerationID == first.GenerationID {
		t.Fatalf("changed dependency payload must commit a new generation, got committed=%v gen=%q", changed.Committed, changed.GenerationID)
	}
}

func TestServiceCatalogHandlerCommitsDependenciesFamilyWhenWired(t *testing.T) {
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
	// A single resolved-relationship loader feeds both the deployment and the
	// dependencies family; the handler partitions by relationship type.
	relationshipLoader := &fakeRepoScopedResolvedLoader{
		byRepo: map[string][]relationships.ResolvedRelationship{
			"repo-checkout": {
				{SourceRepoID: "repo-checkout", TargetRepoID: "repo-deploy", RelationshipType: relationships.RelDeploysFrom, Confidence: 0.9},
				{SourceRepoID: "repo-checkout", TargetRepoID: "repo-lib", RelationshipType: relationships.RelDependsOn, Confidence: 0.8},
				{SourceRepoID: "repo-checkout", TargetRepoID: "repo-mod", RelationshipType: relationships.RelUsesModule, Confidence: 0.7},
			},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   loader,
		Writer:                       &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter:        PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		DeploymentRelationshipLoader: relationshipLoader,
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
	// Both families must come from ONE bounded load, not two.
	if relationshipLoader.calls != 1 {
		t.Fatalf("relationship loader calls = %d, want 1 bounded load feeding both families", relationshipLoader.calls)
	}

	var dependencyRows, deploymentRows int
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDependencies+":") {
				dependencyRows++
			}
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDeployment+":") {
				deploymentRows++
			}
		}
	}
	if dependencyRows == 0 {
		t.Fatal("expected at least one dependencies-family snapshot row after correlation with a dependency relationship")
	}
	if deploymentRows == 0 {
		t.Fatal("deployment family must still materialize from the same loaded relationships")
	}
}

func TestServiceCatalogHandlerSkipsDependenciesFamilyWhenLoaderNil(t *testing.T) {
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
		// DeploymentRelationshipLoader intentionally nil: ownership-only path.
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
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyDependencies+":") {
				t.Fatal("dependencies family must not materialize when no loader is wired")
			}
		}
	}
}
