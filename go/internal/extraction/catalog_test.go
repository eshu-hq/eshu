// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extraction

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestCatalogProfilesAreComplete(t *testing.T) {
	seen := make(map[scope.CollectorKind]struct{}, len(catalog))
	for _, profile := range catalog {
		if err := profile.Validate(); err != nil {
			t.Errorf("catalog profile %q invalid: %v", profile.Family, err)
		}
		if profile.DisplayName == "" {
			t.Errorf("catalog profile %q has no display name", profile.Family)
		}
		if _, dup := seen[profile.Family]; dup {
			t.Errorf("catalog lists family %q more than once", profile.Family)
		}
		seen[profile.Family] = struct{}{}
	}
}

func TestCatalogClassifications(t *testing.T) {
	rows := Catalog()
	if len(rows) != len(catalog) {
		t.Fatalf("Catalog returned %d rows, want %d", len(rows), len(catalog))
	}

	byFamily := make(map[scope.CollectorKind]Readiness, len(rows))
	counts := make(map[Classification]int)
	for _, row := range rows {
		byFamily[row.Family] = row
		counts[row.Classification]++
		if !row.Classification.Valid() {
			t.Errorf("family %q has invalid classification %q", row.Family, row.Classification)
		}
		if row.Rationale == "" {
			t.Errorf("family %q has empty rationale", row.Family)
		}
	}

	wantClass := map[scope.CollectorKind]Classification{
		scope.CollectorGit:             KeepInTree,
		scope.CollectorTerraformState:  KeepInTree,
		scope.CollectorAWS:             KeepInTree,
		scope.CollectorGCP:             KeepInTree,
		scope.CollectorAzure:           KeepInTree,
		scope.CollectorKubernetesLive:  KeepInTree,
		scope.CollectorSBOMAttestation: KeepInTree,
		scope.CollectorPagerDuty:       ExternalReady,
		scope.CollectorJira:            Blocked,
		scope.CollectorGrafana:         Blocked,
	}
	for family, want := range wantClass {
		got, ok := byFamily[family]
		if !ok {
			t.Fatalf("catalog missing expected family %q", family)
		}
		if got.Classification != want {
			t.Errorf("family %q classification = %q, want %q", family, got.Classification, want)
		}
	}

	// The catalog must exercise the keep-in-tree, blocked, and external-ready
	// classifications against real collector families. PagerDuty is the first
	// and only external_ready family (#8863 cutover, #10619 in-tree removal);
	// no family is an extraction candidate right now.
	if counts[KeepInTree] == 0 || counts[Blocked] == 0 || counts[ExternalReady] == 0 {
		t.Fatalf("catalog classification coverage = %+v, want keep_in_tree, blocked, and external_ready all present", counts)
	}
	if counts[ExternalReady] != 1 {
		t.Errorf("catalog has %d external_ready entries, want exactly 1 (pagerduty)", counts[ExternalReady])
	}
}

func TestCatalogPagerDutyIsExternalReady(t *testing.T) {
	// PagerDuty runs out of tree as its default path since the #8863
	// satellite cutover, and #10619 removed the in-tree command, source
	// package, and image build, so it must classify as external_ready.
	got, ok := Lookup(scope.CollectorPagerDuty)
	if !ok {
		t.Fatal("Lookup(pagerduty) not found in catalog")
	}
	if got.Classification != ExternalReady {
		t.Fatalf("PagerDuty classification = %q, want external_ready", got.Classification)
	}
	for _, criterion := range got.Criteria {
		if criterion.State != Met {
			t.Errorf("PagerDuty criterion %s = %q, want met (boundary proof complete)", criterion.Criterion, criterion.State)
		}
	}
}

func TestCatalogBlockedFamiliesReportBlockers(t *testing.T) {
	got, ok := Lookup(scope.CollectorJira)
	if !ok {
		t.Fatal("Lookup(jira) not found in catalog")
	}
	if got.Classification != Blocked {
		t.Fatalf("Jira classification = %q, want blocked", got.Classification)
	}
	if len(got.Blockers) == 0 {
		t.Fatal("blocked family Jira reported no blockers")
	}
	for _, blocker := range got.Blockers {
		if blocker.State != Unmet {
			t.Errorf("blocker %s state = %q, want unmet", blocker.Criterion, blocker.State)
		}
	}
}

func TestCatalogTracksSBOMAttestation(t *testing.T) {
	// The SBOM subject digest is a join key the core projection consumes,
	// so the family must be tracked as correlation-critical until the
	// architecture gate for a producer split passes.
	got, ok := Lookup(scope.CollectorSBOMAttestation)
	if !ok {
		t.Fatal("Lookup(sbom_attestation) not found in catalog")
	}
	if got.Classification != KeepInTree {
		t.Fatalf("SBOM attestation classification = %q, want keep_in_tree", got.Classification)
	}
}

func TestLookupUnknownFamily(t *testing.T) {
	if _, ok := Lookup(scope.CollectorKind("does_not_exist")); ok {
		t.Fatal("Lookup(unknown) = true, want false")
	}
}
