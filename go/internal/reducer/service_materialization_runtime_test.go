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

func TestServiceRuntimeEvidenceKeyIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	instance := ServiceRuntimeInstance{
		PlatformKind: "kubernetes",
		Environment:  "prod",
		WorkloadRef:  "workload-instance:checkout:prod",
	}
	key := ServiceRuntimeEvidenceKey("svc-checkout", instance)
	if !strings.HasPrefix(key, ServiceEvidenceFamilyRuntime+":svc-checkout:") {
		t.Fatalf("runtime evidence key %q is not the runtime:<service>:... shape", key)
	}
	// The durable workload identity is the cluster/namespace/workload id, which
	// must not embed a resolution or materialization generation.
	for _, generationToken := range []string{"service-gen", "resolved-", "generation"} {
		if strings.Contains(key, generationToken) {
			t.Fatalf("runtime evidence key %q embeds a generation-bearing token %q", key, generationToken)
		}
	}
	again := ServiceRuntimeEvidenceKey("svc-checkout", instance)
	if key != again {
		t.Fatalf("runtime evidence key not stable across calls: %q != %q", key, again)
	}
}

func TestServiceRuntimeEvidenceKeyDistinguishesInstanceShape(t *testing.T) {
	t.Parallel()

	base := ServiceRuntimeInstance{PlatformKind: "kubernetes", Environment: "prod", WorkloadRef: "workload-instance:checkout:prod"}
	differentEnv := base
	differentEnv.Environment = "staging"
	differentEnv.WorkloadRef = "workload-instance:checkout:staging"
	differentPlatform := base
	differentPlatform.PlatformKind = "ecs"
	differentWorkload := base
	differentWorkload.WorkloadRef = "workload-instance:payments:prod"

	keyBase := ServiceRuntimeEvidenceKey("svc", base)
	if keyBase == ServiceRuntimeEvidenceKey("svc", differentEnv) {
		t.Fatal("different environment must yield a distinct runtime key")
	}
	if keyBase == ServiceRuntimeEvidenceKey("svc", differentPlatform) {
		t.Fatal("different platform kind must yield a distinct runtime key")
	}
	if keyBase == ServiceRuntimeEvidenceKey("svc", differentWorkload) {
		t.Fatal("different workload ref must yield a distinct runtime key")
	}
}

func TestBuildServiceRuntimeEvidenceFiltersAndDedupes(t *testing.T) {
	t.Parallel()

	evidence := buildServiceRuntimeEvidence([]ServiceRuntimeInstance{
		{PlatformKind: "kubernetes", Environment: "prod", WorkloadRef: "workload-instance:checkout:prod", Confidence: 0.9},
		{PlatformKind: "ecs", Environment: "staging", WorkloadRef: "workload-instance:checkout:staging"},
		// Duplicate identity must collapse to one row.
		{PlatformKind: "kubernetes", Environment: "prod", WorkloadRef: "workload-instance:checkout:prod", Confidence: 0.9},
		// An instance with no durable workload identity cannot be keyed and is dropped.
		{PlatformKind: "kubernetes", Environment: "prod", WorkloadRef: ""},
		// An instance with no platform/environment identity is dropped.
		{WorkloadRef: "workload-instance:checkout:prod"},
	})
	if len(evidence) != 2 {
		t.Fatalf("runtime evidence rows = %d, want 2 deduped durable instances", len(evidence))
	}
	for _, row := range evidence {
		if row.Identity == "" {
			t.Fatal("runtime evidence row missing stable identity")
		}
		if len(row.Payload) == 0 {
			t.Fatal("runtime evidence row missing payload for hash classification")
		}
	}
	if evidence[0].Identity >= evidence[1].Identity {
		t.Fatalf("runtime evidence must be ordered by identity: %#v", evidence)
	}
}

func TestServiceMaterializationWriterCommitsRuntimeFamily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-checkout",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
		},
		Runtime: []ServiceRuntimeEvidence{
			{Identity: "kubernetes|prod|workload-instance:checkout:prod", Payload: map[string]any{"platform_kind": "kubernetes"}},
		},
	})
	if err != nil {
		t.Fatalf("WriteServiceMaterialization() error = %v, want nil", err)
	}
	if !result.Committed {
		t.Fatal("first materialization should commit a new generation")
	}
	if result.EvidenceRows != 2 {
		t.Fatalf("EvidenceRows = %d, want 2 (ownership + runtime)", result.EvidenceRows)
	}

	var sawRuntime, sawOwnership bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		switch {
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyRuntime+":svc-checkout:"):
			sawRuntime = true
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":svc-checkout:"):
			sawOwnership = true
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !sawRuntime {
		t.Fatal("runtime-family snapshot row was not written")
	}
	if !sawOwnership {
		t.Fatal("ownership-family snapshot row regressed")
	}
}

func TestServiceMaterializationWriterTombstonesRetiredRuntime(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Runtime: []ServiceRuntimeEvidence{
			{Identity: "keep", Payload: map[string]any{"platform_kind": "kubernetes"}},
			{Identity: "gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceRuntimeEvidenceKeyFromIdentity("svc-a", "gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired runtime evidence must be written as a tombstone row, never silently absent")
	}
}

func TestServiceMaterializationRuntimeChangeFlipsGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Runtime:   []ServiceRuntimeEvidence{{Identity: "inst-1", Payload: map[string]any{"confidence": 0.5}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	same, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Runtime:   []ServiceRuntimeEvidence{{Identity: "inst-1", Payload: map[string]any{"confidence": 0.5}}},
	})
	if err != nil {
		t.Fatalf("idempotent write error = %v", err)
	}
	if same.Committed || same.GenerationID != first.GenerationID {
		t.Fatalf("identical runtime evidence must be a no-op: committed=%v gen=%q->%q", same.Committed, first.GenerationID, same.GenerationID)
	}
	changed, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Runtime:   []ServiceRuntimeEvidence{{Identity: "inst-1", Payload: map[string]any{"confidence": 0.9}}},
	})
	if err != nil {
		t.Fatalf("changed write error = %v", err)
	}
	if !changed.Committed || changed.GenerationID == first.GenerationID {
		t.Fatalf("changed runtime payload must commit a new generation, got committed=%v gen=%q", changed.Committed, changed.GenerationID)
	}
}

// fakeRepoScopedRuntimeLoader returns runtime instances keyed by repo.
type fakeRepoScopedRuntimeLoader struct {
	byRepo map[string][]ServiceRuntimeInstance
	calls  int
}

func (f *fakeRepoScopedRuntimeLoader) GetRuntimeInstancesForRepos(
	_ context.Context,
	repoIDs []string,
) (map[string][]ServiceRuntimeInstance, error) {
	f.calls++
	out := map[string][]ServiceRuntimeInstance{}
	for _, repoID := range repoIDs {
		out[repoID] = f.byRepo[repoID]
	}
	return out, nil
}

func TestServiceCatalogHandlerCommitsRuntimeFamilyWhenWired(t *testing.T) {
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
	runtimeLoader := &fakeRepoScopedRuntimeLoader{
		byRepo: map[string][]ServiceRuntimeInstance{
			"repo-checkout": {
				{PlatformKind: "kubernetes", Environment: "prod", WorkloadRef: "workload-instance:checkout:prod", Confidence: 0.9},
			},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:            loader,
		Writer:                &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter: PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		RuntimeInstanceLoader: runtimeLoader,
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
	if runtimeLoader.calls != 1 {
		t.Fatalf("runtime loader calls = %d, want 1 bounded load", runtimeLoader.calls)
	}

	var runtimeRows int
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyRuntime+":") {
				runtimeRows++
			}
		}
	}
	if runtimeRows == 0 {
		t.Fatal("expected at least one runtime-family snapshot row after correlation with a runtime instance")
	}
}

func TestServiceCatalogHandlerSkipsRuntimeFamilyWhenLoaderNil(t *testing.T) {
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
		// RuntimeInstanceLoader intentionally nil: ownership/deployment-only path.
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
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyRuntime+":") {
				t.Fatal("runtime family must not materialize when no loader is wired")
			}
		}
	}
}

func TestServiceRuntimeInstancePayloadExcludesGenerationFields(t *testing.T) {
	t.Parallel()

	payload := serviceRuntimeEvidencePayload(ServiceRuntimeInstance{
		PlatformKind: "kubernetes",
		PlatformName: "prod",
		Environment:  "prod",
		WorkloadRef:  "workload-instance:checkout:prod",
		WorkloadName: "checkout",
		Confidence:   0.8,
	})
	for _, forbidden := range []string{"generation_id", "resolved_id", "instance_id"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("runtime payload must not carry generation-bearing field %q: %#v", forbidden, payload)
		}
	}
	if payload["platform_kind"] != "kubernetes" || payload["environment"] != "prod" {
		t.Fatalf("runtime payload missing durable identity fields: %#v", payload)
	}
	// The payload must hash identically for the same durable instance.
	if facts.StableID("x", payload) == "" {
		t.Fatal("runtime payload must be stably hashable")
	}
}
