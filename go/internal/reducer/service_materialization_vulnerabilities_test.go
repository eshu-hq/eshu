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

// affectedAdvisoryRecord is a fully durable advisory-affects-package record whose
// identity is the canonical advisory id and the affected package, never a
// per-scan evidence fact id.
func affectedAdvisoryRecord() ServiceVulnerabilityRecord {
	return ServiceVulnerabilityRecord{
		CanonicalID:       "GHSA-aaaa-bbbb-cccc",
		PackageEcosystem:  "npm",
		PackageName:       "left-pad",
		PrimaryAdvisoryID: "CVE-2026-0001",
		Severity:          "high",
		KEVListed:         false,
		EPSSScore:         "0.42",
		SourceConfidence:  "exact",
		SourceFreshness:   "active",
	}
}

func TestServiceVulnerabilityEvidenceKeyIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	record := affectedAdvisoryRecord()
	key := ServiceVulnerabilityEvidenceKey("svc-app", serviceVulnerabilityEvidenceIdentity(record))
	if !strings.HasPrefix(key, ServiceEvidenceFamilyVulnerabilities+":svc-app:") {
		t.Fatalf("vulnerabilities key %q is not the vulnerabilities:<service>:<identity> shape", key)
	}

	// Advisory evidence facts rotate a fact_id and a per-scan generation id every
	// ingest; keying on either would produce 100% false churn. The key must embed
	// none of: a fact_id, a generation/scan id.
	for _, generationBearing := range []string{"generation-a", "fact-id-1234", "gen-b", "scan-99"} {
		if strings.Contains(key, generationBearing) {
			t.Fatalf("vulnerabilities key %q must not embed generation-bearing token %q", key, generationBearing)
		}
	}

	again := ServiceVulnerabilityEvidenceKey("svc-app", serviceVulnerabilityEvidenceIdentity(record))
	if key != again {
		t.Fatalf("vulnerabilities identity not stable across calls: %q != %q", key, again)
	}
}

// TestServiceVulnerabilityEvidenceKeyStableAcrossGenerations is the anti-churn
// guard: the same logical advisory-affects-package row read from two different
// fact generations (only the per-scan evidence id differs; the durable canonical
// id and affected package do not) must produce the same key, so the FULL OUTER
// JOIN diff classifies it unchanged instead of churning every generation.
func TestServiceVulnerabilityEvidenceKeyStableAcrossGenerations(t *testing.T) {
	t.Parallel()

	genA := affectedAdvisoryRecord()
	genB := affectedAdvisoryRecord()
	keyA := ServiceVulnerabilityEvidenceKey("svc", serviceVulnerabilityEvidenceIdentity(genA))
	keyB := ServiceVulnerabilityEvidenceKey("svc", serviceVulnerabilityEvidenceIdentity(genB))
	if keyA != keyB {
		t.Fatalf("same durable advisory-package row must key identically across generations: %q != %q", keyA, keyB)
	}
}

func TestServiceVulnerabilityEvidenceKeyDistinguishesAdvisoryShape(t *testing.T) {
	t.Parallel()

	base := affectedAdvisoryRecord()
	differentAdvisory := base
	differentAdvisory.CanonicalID = "GHSA-zzzz-zzzz-zzzz"
	differentEcosystem := base
	differentEcosystem.PackageEcosystem = "pypi"
	differentPackage := base
	differentPackage.PackageName = "right-pad"

	keyBase := ServiceVulnerabilityEvidenceKey("svc", serviceVulnerabilityEvidenceIdentity(base))
	for name, variant := range map[string]ServiceVulnerabilityRecord{
		"advisory":  differentAdvisory,
		"ecosystem": differentEcosystem,
		"package":   differentPackage,
	} {
		if keyBase == ServiceVulnerabilityEvidenceKey("svc", serviceVulnerabilityEvidenceIdentity(variant)) {
			t.Fatalf("different %s must yield a distinct vulnerabilities identity", name)
		}
	}

	// The primary (human-facing) advisory id is observable, not identity: two rows
	// for the same canonical advisory + package must share one identity even when
	// the source-reported CVE/GHSA alias differs.
	aliasOnly := base
	aliasOnly.PrimaryAdvisoryID = "CVE-2026-9999"
	if keyBase != ServiceVulnerabilityEvidenceKey("svc", serviceVulnerabilityEvidenceIdentity(aliasOnly)) {
		t.Fatal("primary_advisory_id is observable and must not change the identity")
	}
}

func TestBuildServiceVulnerabilityEvidenceDropsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	evidence := buildServiceVulnerabilityEvidence([]ServiceVulnerabilityRecord{
		affectedAdvisoryRecord(),
		// Missing canonical id: cannot be keyed, dropped.
		{PackageEcosystem: "npm", PackageName: "x"},
		// Missing package ecosystem: dropped.
		{CanonicalID: "GHSA-1", PackageName: "x"},
		// Missing package name: dropped.
		{CanonicalID: "GHSA-1", PackageEcosystem: "npm"},
	})
	if len(evidence) != 1 {
		t.Fatalf("vulnerabilities evidence rows = %d, want 1 (only the fully-identified record)", len(evidence))
	}
	if len(evidence[0].Payload) == 0 {
		t.Fatal("vulnerabilities evidence row missing payload for hash classification")
	}
}

func TestBuildServiceVulnerabilityEvidenceDeterministicAndDeduped(t *testing.T) {
	t.Parallel()

	record := affectedAdvisoryRecord()
	first := buildServiceVulnerabilityEvidence([]ServiceVulnerabilityRecord{record, record})
	if len(first) != 1 {
		t.Fatalf("duplicate advisory rows must dedupe to one row, got %d", len(first))
	}

	high := affectedAdvisoryRecord()
	high.PackageName = "z-pkg"
	low := affectedAdvisoryRecord()
	low.PackageName = "a-pkg"
	unordered := buildServiceVulnerabilityEvidence([]ServiceVulnerabilityRecord{high, low})
	if len(unordered) != 2 || unordered[0].Identity >= unordered[1].Identity {
		t.Fatalf("vulnerabilities evidence must be ordered by identity: %#v", unordered)
	}
}

func TestServiceVulnerabilityEvidencePayloadExcludesGenerationFields(t *testing.T) {
	t.Parallel()

	payload := serviceVulnerabilityEvidencePayload(affectedAdvisoryRecord())
	for _, forbidden := range []string{"fact_id", "generation_id", "scan_id", "evidence_fact_id"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("vulnerabilities payload must not carry generation-bearing field %q: %#v", forbidden, payload)
		}
	}
	if payload["canonical_id"] != "GHSA-aaaa-bbbb-cccc" || payload["package_name"] != "left-pad" {
		t.Fatalf("vulnerabilities payload missing durable identity fields: %#v", payload)
	}
}

func TestServiceMaterializationWriterCommitsVulnerabilitiesFamily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: func() time.Time { return now }}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-app",
		Ownership: []ServiceOwnershipEvidence{
			{OwnerRef: "team-payments", Payload: map[string]any{"tier": "gold"}},
		},
		Vulnerabilities: []ServiceVulnerabilityEvidence{
			{
				Identity: serviceVulnerabilityEvidenceIdentity(affectedAdvisoryRecord()),
				Payload:  serviceVulnerabilityEvidencePayload(affectedAdvisoryRecord()),
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
		t.Fatalf("EvidenceRows = %d, want 2 (ownership + vulnerabilities)", result.EvidenceRows)
	}

	var sawVulnerabilities, sawOwnership bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		switch {
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyVulnerabilities+":svc-app:"):
			sawVulnerabilities = true
		case strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyOwnership+":svc-app:"):
			sawOwnership = true
		}
		if strings.Contains(row.evidenceKey, result.GenerationID) {
			t.Fatalf("evidence key %q embeds the generation id; identity must be generation-independent", row.evidenceKey)
		}
	}
	if !sawVulnerabilities {
		t.Fatal("vulnerabilities-family snapshot row was not written")
	}
	if !sawOwnership {
		t.Fatal("ownership-family snapshot row regressed")
	}
}

func TestServiceMaterializationWriterTombstonesRetiredVulnerability(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	result, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID: "svc-a",
		Vulnerabilities: []ServiceVulnerabilityEvidence{
			{Identity: "keep", Payload: map[string]any{"severity": "high"}},
			{Identity: "gone", Retired: true},
		},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	var tombstoned bool
	for _, row := range store.snapshotsFor(result.GenerationID) {
		if row.evidenceKey == ServiceVulnerabilityEvidenceKey("svc-a", "gone") {
			tombstoned = row.tombstone
		}
	}
	if !tombstoned {
		t.Fatal("retired vulnerabilities evidence must be written as a tombstone row, never silently absent")
	}
}

func TestServiceMaterializationVulnerabilitiesChangeFlipsGeneration(t *testing.T) {
	t.Parallel()

	store := newFakeServiceMaterializationStore()
	writer := PostgresServiceMaterializationWriter{DB: store, Now: time.Now}

	first, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:       "svc-a",
		Vulnerabilities: []ServiceVulnerabilityEvidence{{Identity: "row-1", Payload: map[string]any{"severity": "high"}}},
	})
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	// Identical advisory evidence is a no-op (anti-churn across re-materializations).
	same, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:       "svc-a",
		Vulnerabilities: []ServiceVulnerabilityEvidence{{Identity: "row-1", Payload: map[string]any{"severity": "high"}}},
	})
	if err != nil {
		t.Fatalf("idempotent write error = %v", err)
	}
	if same.Committed || same.GenerationID != first.GenerationID {
		t.Fatalf("identical advisory evidence must be a no-op: committed=%v gen=%q->%q", same.Committed, first.GenerationID, same.GenerationID)
	}
	// A changed advisory payload (for example a severity escalation) must flip the
	// generation.
	changed, err := writer.WriteServiceMaterialization(context.Background(), ServiceMaterializationWrite{
		ServiceID:       "svc-a",
		Vulnerabilities: []ServiceVulnerabilityEvidence{{Identity: "row-1", Payload: map[string]any{"severity": "critical"}}},
	})
	if err != nil {
		t.Fatalf("changed write error = %v", err)
	}
	if !changed.Committed || changed.GenerationID == first.GenerationID {
		t.Fatalf("changed advisory payload must commit a new generation, got committed=%v gen=%q", changed.Committed, changed.GenerationID)
	}
}

// fakeRepoScopedVulnerabilityLoader returns supply-chain advisory records keyed by
// canonical repository id, recording the exact repository id set it was asked for
// so a test can prove the load is bounded, once, and repository-scoped.
type fakeRepoScopedVulnerabilityLoader struct {
	byRepo    map[string][]ServiceVulnerabilityRecord
	calls     int
	lastRepos []string
}

func (f *fakeRepoScopedVulnerabilityLoader) GetSupplyChainAdvisoriesForRepos(
	_ context.Context,
	repoIDs []string,
) (map[string][]ServiceVulnerabilityRecord, error) {
	f.calls++
	f.lastRepos = append([]string(nil), repoIDs...)
	out := map[string][]ServiceVulnerabilityRecord{}
	for _, repoID := range repoIDs {
		out[repoID] = f.byRepo[repoID]
	}
	return out, nil
}

func TestServiceCatalogHandlerCommitsVulnerabilitiesFamilyWhenWired(t *testing.T) {
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
	// Correlation truth: the advisory is attributed to the service ONLY because its
	// repository (repo-checkout) carries a supply-chain impact finding.
	vulnerabilityLoader := &fakeRepoScopedVulnerabilityLoader{
		byRepo: map[string][]ServiceVulnerabilityRecord{
			"repo-checkout": {affectedAdvisoryRecord()},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                  loader,
		Writer:                      &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter:       PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		VulnerabilityEvidenceLoader: vulnerabilityLoader,
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
	if vulnerabilityLoader.calls != 1 {
		t.Fatalf("vulnerability loader calls = %d, want 1 bounded load", vulnerabilityLoader.calls)
	}
	if len(vulnerabilityLoader.lastRepos) != 1 || vulnerabilityLoader.lastRepos[0] != "repo-checkout" {
		t.Fatalf("repo-scoped load = %v, want [repo-checkout]", vulnerabilityLoader.lastRepos)
	}

	var vulnerabilityRows int
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyVulnerabilities+":") {
				vulnerabilityRows++
			}
		}
	}
	if vulnerabilityRows == 0 {
		t.Fatal("expected at least one vulnerabilities-family snapshot row after correlation with advisory evidence")
	}
}

// TestServiceCatalogHandlerAttributesVulnerabilityOnlyViaRepositoryFinding is the
// correlation-truth negative case: an advisory finding that belongs to a DIFFERENT
// repository must never be attributed to a service whose repository carries no
// finding. The only durable join is service -> repository -> finding; a service
// gets an advisory only through its own repository's impact finding.
func TestServiceCatalogHandlerAttributesVulnerabilityOnlyViaRepositoryFinding(t *testing.T) {
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
	// The finding lives on a different repository, so it must NOT attach to the
	// checkout service. Loading by repo-checkout returns nothing for repo-other.
	vulnerabilityLoader := &fakeRepoScopedVulnerabilityLoader{
		byRepo: map[string][]ServiceVulnerabilityRecord{
			"repo-other": {affectedAdvisoryRecord()},
		},
	}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                  loader,
		Writer:                      &recordingServiceCatalogCorrelationWriter{},
		MaterializationWriter:       PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		VulnerabilityEvidenceLoader: vulnerabilityLoader,
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
	if len(vulnerabilityLoader.lastRepos) != 1 || vulnerabilityLoader.lastRepos[0] != "repo-checkout" {
		t.Fatalf("repo-scoped load = %v, want [repo-checkout]", vulnerabilityLoader.lastRepos)
	}
	for _, rows := range materialization.snapshots {
		for _, row := range rows {
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyVulnerabilities+":") {
				t.Fatal("advisory finding on a different repository must not attach to this service")
			}
		}
	}
}

func TestServiceCatalogHandlerSkipsVulnerabilitiesFamilyWhenLoaderNil(t *testing.T) {
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
		// VulnerabilityEvidenceLoader intentionally nil: prior-families-only path.
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
			if strings.HasPrefix(row.evidenceKey, ServiceEvidenceFamilyVulnerabilities+":") {
				t.Fatal("vulnerabilities family must not materialize when no loader is wired")
			}
		}
	}
}
