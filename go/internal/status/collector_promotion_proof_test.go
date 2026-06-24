// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// promotionTestCatalog is a small deterministic catalog used to exercise every
// promotion lane without depending on the full default catalog.
func promotionTestCatalog() []status.CollectorCatalogEntry {
	return []status.CollectorCatalogEntry{
		{CollectorKind: "git", DisplayName: "Git", ClaimDriven: false, SourceScope: "repository"},
		{CollectorKind: "aws", DisplayName: "AWS Cloud", ClaimDriven: true, SourceScope: "account"},
		{CollectorKind: "jira", DisplayName: "Jira", ClaimDriven: true, SourceScope: "jira_site"},
		{CollectorKind: "pagerduty", DisplayName: "PagerDuty", ClaimDriven: true, SourceScope: "pagerduty_account"},
		{CollectorKind: "sbom_attestation", DisplayName: "SBOM Attestation", ClaimDriven: true, SourceScope: "sbom_attestation"},
		{CollectorKind: "grafana", DisplayName: "Grafana", ClaimDriven: true, SourceScope: "grafana"},
	}
}

func promotionScenarioReport(now time.Time) status.Report {
	return status.BuildReport(status.RawSnapshot{
		AsOf: now,
		Coordinator: &status.CoordinatorSnapshot{
			CollectorInstances: []status.CollectorInstanceSummary{
				{InstanceID: "collector-git", CollectorKind: "git", Mode: "snapshot", Enabled: true, ClaimsEnabled: false, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
				{InstanceID: "collector-aws", CollectorKind: "aws", Mode: "claim", Enabled: true, ClaimsEnabled: true, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
				{InstanceID: "collector-jira", CollectorKind: "jira", Mode: "claim", Enabled: true, ClaimsEnabled: true, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
				{InstanceID: "collector-pagerduty", CollectorKind: "pagerduty", Mode: "claim", Enabled: true, ClaimsEnabled: false, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
				{InstanceID: "collector-sbom", CollectorKind: "sbom_attestation", Mode: "claim", Enabled: false, ClaimsEnabled: false, DeactivatedAt: now.Add(-1 * time.Hour), LastObservedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
			},
		},
		AWSCloudScans: []status.AWSCloudScanStatus{
			{CollectorInstanceID: "collector-aws", AccountID: "acct", Region: "us-east-1", ServiceKind: "ec2", Status: "failed_terminal", CredentialFailed: true, FailureClass: "credential_denied", LastObservedAt: now.Add(-3 * time.Minute), UpdatedAt: now.Add(-3 * time.Minute)},
		},
		CollectorFactEvidence: []status.CollectorFactEvidence{
			{InstanceID: "collector-git", CollectorKind: "git", EvidenceSource: "source_facts", SourceSystems: []string{"git"}, ObservationCount: 12, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
			{InstanceID: "collector-git", CollectorKind: "git", EvidenceSource: "reducer_facts", ObservationCount: 9, LastObservedAt: now.Add(-1 * time.Minute), UpdatedAt: now.Add(-1 * time.Minute)},
			{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "source_facts", SourceSystems: []string{"jira"}, ObservationCount: 5, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
		},
	}, status.Options{})
}

func findPromotionProof(t *testing.T, proofs []status.CollectorPromotionProof, kind string) status.CollectorPromotionProof {
	t.Helper()
	for _, proof := range proofs {
		if proof.CollectorKind == kind {
			return proof
		}
	}
	t.Fatalf("no promotion proof for collector kind %q in %#v", kind, proofs)
	return status.CollectorPromotionProof{}
}

func TestCollectorPromotionProofsClassifiesEveryLane(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := promotionScenarioReport(now)
	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		Catalog:    promotionTestCatalog(),
		AsOf:       now,
		StaleAfter: 24 * time.Hour,
	})

	cases := map[string]string{
		"git":              status.CollectorPromotionImplemented,
		"aws":              status.CollectorPromotionFailed,
		"jira":             status.CollectorPromotionPartial,
		"pagerduty":        status.CollectorPromotionGated,
		"sbom_attestation": status.CollectorPromotionDisabled,
		"grafana":          status.CollectorPromotionUnsupported,
	}
	for kind, want := range cases {
		proof := findPromotionProof(t, proofs, kind)
		if proof.PromotionState != want {
			t.Errorf("collector %q promotion state = %q, want %q (blockers=%v)", kind, proof.PromotionState, want, proof.Blockers)
		}
	}

	git := findPromotionProof(t, proofs, "git")
	if git.ReducerReadback != status.CollectorReadbackAvailable {
		t.Errorf("git reducer readback = %q, want %q", git.ReducerReadback, status.CollectorReadbackAvailable)
	}
	if len(git.TelemetryHandles) == 0 {
		t.Errorf("git telemetry handles must not be empty")
	}

	jira := findPromotionProof(t, proofs, "jira")
	if jira.ReducerReadback != status.CollectorReadbackPending {
		t.Errorf("jira reducer readback = %q, want %q", jira.ReducerReadback, status.CollectorReadbackPending)
	}
	if len(jira.Blockers) == 0 {
		t.Errorf("partial jira proof must record a blocker")
	}

	grafana := findPromotionProof(t, proofs, "grafana")
	if grafana.InstanceID != "" {
		t.Errorf("no-instance grafana proof instance id = %q, want empty", grafana.InstanceID)
	}
	if grafana.ReducerReadback != status.CollectorReadbackUnavailable {
		t.Errorf("grafana reducer readback = %q, want %q", grafana.ReducerReadback, status.CollectorReadbackUnavailable)
	}
}

func TestCollectorPromotionProofsAreDeterministicAndSorted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := promotionScenarioReport(now)
	opts := status.CollectorPromotionOptions{Catalog: promotionTestCatalog(), AsOf: now, StaleAfter: 24 * time.Hour}

	first := status.CollectorPromotionProofs(report, opts)
	second := status.CollectorPromotionProofs(report, opts)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("promotion proofs not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	for i := 1; i < len(first); i++ {
		prev, cur := first[i-1], first[i]
		if prev.CollectorKind > cur.CollectorKind ||
			(prev.CollectorKind == cur.CollectorKind && prev.InstanceID > cur.InstanceID) {
			t.Fatalf("promotion proofs not sorted at %d: %q/%q then %q/%q", i, prev.CollectorKind, prev.InstanceID, cur.CollectorKind, cur.InstanceID)
		}
	}
}

func TestCollectorPromotionProofsTreatPartialHealthAsPartial(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	// A claim-driven collector with reducer readback but a partially completed
	// scan must never be promoted to implemented.
	report := status.BuildReport(status.RawSnapshot{
		AsOf: now,
		Coordinator: &status.CoordinatorSnapshot{
			CollectorInstances: []status.CollectorInstanceSummary{
				{InstanceID: "collector-vuln", CollectorKind: "vulnerability_intelligence", Mode: "claim", Enabled: true, ClaimsEnabled: true, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
			},
		},
		VulnerabilitySources: []status.VulnerabilitySourceState{
			{CollectorInstanceID: "collector-vuln", ScopeID: "scope", Source: "osv", TerminalStatus: "partial", UpdatedAt: now.Add(-2 * time.Minute)},
		},
		CollectorFactEvidence: []status.CollectorFactEvidence{
			{InstanceID: "collector-vuln", CollectorKind: "vulnerability_intelligence", EvidenceSource: "reducer_facts", ObservationCount: 4, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
		},
	}, status.Options{})

	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		Catalog:    []status.CollectorCatalogEntry{{CollectorKind: "vulnerability_intelligence", DisplayName: "Vuln", ClaimDriven: true, SourceScope: "vulnerability_intelligence"}},
		AsOf:       now,
		StaleAfter: 24 * time.Hour,
	})
	proof := findPromotionProof(t, proofs, "vulnerability_intelligence")
	if proof.PromotionState != status.CollectorPromotionPartial {
		t.Fatalf("partial-health collector promotion state = %q, want %q", proof.PromotionState, status.CollectorPromotionPartial)
	}
}

func TestCollectorPromotionProofsUnregisteredEvidenceIsPartialNotGated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	// Persisted fact evidence with no coordinator registration is unregistered;
	// it must not be reported as gated with a "claims disabled" blocker.
	report := status.BuildReport(status.RawSnapshot{
		AsOf: now,
		CollectorFactEvidence: []status.CollectorFactEvidence{
			{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "reducer_facts", ObservationCount: 3, LastObservedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
		},
	}, status.Options{})

	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		Catalog:    []status.CollectorCatalogEntry{{CollectorKind: "jira", DisplayName: "Jira", ClaimDriven: true, SourceScope: "jira_site"}},
		AsOf:       now,
		StaleAfter: 24 * time.Hour,
	})
	proof := findPromotionProof(t, proofs, "jira")
	if proof.PromotionState == status.CollectorPromotionGated {
		t.Fatalf("unregistered evidence must not be gated: %#v", proof)
	}
	if proof.PromotionState != status.CollectorPromotionPartial {
		t.Fatalf("unregistered claim-driven evidence promotion state = %q, want %q", proof.PromotionState, status.CollectorPromotionPartial)
	}
	for _, blocker := range proof.Blockers {
		if strings.Contains(blocker, "claims disabled") {
			t.Fatalf("unregistered collector blocker wrongly claims disabled registration: %q", blocker)
		}
	}
}

func TestCollectorPromotionProofsDoNotAliasRuntimeSlices(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := promotionScenarioReport(now)
	opts := status.CollectorPromotionOptions{Catalog: promotionTestCatalog(), AsOf: now, StaleAfter: 24 * time.Hour}

	proofs := status.CollectorPromotionProofs(report, opts)
	for i := range proofs {
		if len(proofs[i].EvidenceSources) > 0 {
			proofs[i].EvidenceSources[0] = "MUTATED"
		}
		if len(proofs[i].SourceSystems) > 0 {
			proofs[i].SourceSystems[0] = "MUTATED"
		}
	}

	// A fresh derivation must be unaffected by the mutation above.
	fresh := status.CollectorPromotionProofs(report, opts)
	for _, proof := range fresh {
		for _, source := range proof.EvidenceSources {
			if source == "MUTATED" {
				t.Fatalf("evidence sources alias shared runtime slice: %q", proof.CollectorKind)
			}
		}
		for _, system := range proof.SourceSystems {
			if system == "MUTATED" {
				t.Fatalf("source systems alias shared runtime slice: %q", proof.CollectorKind)
			}
		}
	}
}

func TestCollectorPromotionProofsMarkStaleEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := status.BuildReport(status.RawSnapshot{
		AsOf: now,
		Coordinator: &status.CoordinatorSnapshot{
			CollectorInstances: []status.CollectorInstanceSummary{
				{InstanceID: "collector-jira", CollectorKind: "jira", Mode: "claim", Enabled: true, ClaimsEnabled: true, LastObservedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
			},
		},
		CollectorFactEvidence: []status.CollectorFactEvidence{
			{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "reducer_facts", ObservationCount: 5, LastObservedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
		},
	}, status.Options{})

	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		Catalog:    []status.CollectorCatalogEntry{{CollectorKind: "jira", DisplayName: "Jira", ClaimDriven: true, SourceScope: "jira_site"}},
		AsOf:       now,
		StaleAfter: 24 * time.Hour,
	})
	proof := findPromotionProof(t, proofs, "jira")
	if proof.PromotionState != status.CollectorPromotionStale {
		t.Fatalf("stale jira promotion state = %q, want %q", proof.PromotionState, status.CollectorPromotionStale)
	}
}

func TestCollectorPromotionProofsHidePermissionScoped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := promotionScenarioReport(now)
	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		Catalog:          promotionTestCatalog(),
		AsOf:             now,
		StaleAfter:       24 * time.Hour,
		PermissionHidden: map[string]bool{"aws": true},
	})

	proof := findPromotionProof(t, proofs, "aws")
	if proof.PromotionState != status.CollectorPromotionPermissionHidden {
		t.Fatalf("hidden aws promotion state = %q, want %q", proof.PromotionState, status.CollectorPromotionPermissionHidden)
	}
	// Permission-hidden lanes must not leak instance, evidence, or failure detail.
	if proof.InstanceID != "" || len(proof.EvidenceSources) != 0 || len(proof.SourceSystems) != 0 || proof.Health != "" {
		t.Fatalf("permission-hidden aws proof leaked detail: %#v", proof)
	}
}

func TestCollectorPromotionProofsMarkFixtureOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := promotionScenarioReport(now)
	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		Catalog:     promotionTestCatalog(),
		AsOf:        now,
		StaleAfter:  24 * time.Hour,
		FixtureOnly: map[string]bool{"git": true},
	})

	proof := findPromotionProof(t, proofs, "git")
	if !proof.FixtureOnly {
		t.Fatalf("git proof FixtureOnly = false, want true")
	}
	if proof.PromotionState == status.CollectorPromotionImplemented {
		t.Fatalf("fixture-only lane must not be promoted to implemented: %#v", proof)
	}
}

func TestRenderJSONIncludesPromotionProofsOnlyForPresentCollectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	empty := status.BuildReport(status.RawSnapshot{AsOf: now}, status.DefaultOptions())
	body, err := status.RenderJSON(empty)
	if err != nil {
		t.Fatalf("RenderJSON(empty) error = %v", err)
	}
	if strings.Contains(string(body), "collector_promotion_proofs") {
		t.Fatalf("empty report must omit collector_promotion_proofs: %s", body)
	}

	populated := promotionScenarioReport(now)
	body, err = status.RenderJSON(populated)
	if err != nil {
		t.Fatalf("RenderJSON(populated) error = %v", err)
	}
	if !strings.Contains(string(body), "collector_promotion_proofs") {
		t.Fatalf("populated report must include collector_promotion_proofs: %s", body)
	}
	// The global status surface reports only present collectors, never the full
	// default fleet, so an unconfigured family must not appear.
	if strings.Contains(string(body), "\"collector_kind\": \"kubernetes_live\"") {
		t.Fatalf("global status must not enumerate absent collector families: %s", body)
	}
}

func TestCollectorPromotionProofsDefaultCatalogCoversFullFleet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	report := status.BuildReport(status.RawSnapshot{AsOf: now}, status.DefaultOptions())
	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{AsOf: now})

	if len(proofs) != len(status.KnownCollectorKinds()) {
		t.Fatalf("default catalog produced %d proofs, want %d", len(proofs), len(status.KnownCollectorKinds()))
	}
	for _, proof := range proofs {
		if proof.PromotionState != status.CollectorPromotionUnsupported {
			t.Errorf("collector %q with no evidence = %q, want %q", proof.CollectorKind, proof.PromotionState, status.CollectorPromotionUnsupported)
		}
	}
}

func TestDefaultCollectorCatalogCoversEveryCollectorKind(t *testing.T) {
	t.Parallel()

	catalog := status.DefaultCollectorCatalog()
	if len(catalog) == 0 {
		t.Fatal("default collector catalog is empty")
	}
	seen := map[string]bool{}
	for _, entry := range catalog {
		if entry.CollectorKind == "" {
			t.Fatalf("catalog entry missing collector kind: %#v", entry)
		}
		if entry.DisplayName == "" {
			t.Fatalf("catalog entry %q missing display name", entry.CollectorKind)
		}
		if seen[entry.CollectorKind] {
			t.Fatalf("catalog entry %q duplicated", entry.CollectorKind)
		}
		seen[entry.CollectorKind] = true
	}
	for _, kind := range status.KnownCollectorKinds() {
		if !seen[kind] {
			t.Errorf("default catalog missing known collector kind %q", kind)
		}
	}
}
