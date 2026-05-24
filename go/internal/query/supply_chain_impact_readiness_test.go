package query

import (
	"reflect"
	"sort"
	"testing"
)

func TestBuildSupplyChainImpactReadinessClassifiesNotConfigured(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{},
	)
	if envelope.State != ReadinessStateNotConfigured {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateNotConfigured)
	}
	wantMissing := []string{MissingEvidenceAdvisorySources, MissingEvidenceOwnedPackages}
	sort.Strings(wantMissing)
	if !reflect.DeepEqual(envelope.MissingEvidence, wantMissing) {
		t.Fatalf("missing_evidence = %#v, want %#v", envelope.MissingEvidence, wantMissing)
	}
	if envelope.Freshness != FreshnessLabelUnknown {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelUnknown)
	}
	if envelope.Counts.FindingsReturned != 0 || envelope.Counts.FindingsTruncated {
		t.Fatalf("counts = %#v, want zero/false", envelope.Counts)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesEvidenceIncomplete(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateEvidenceIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, want owned_packages", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesReadyZeroFindings(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 3, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if envelope.Freshness != FreshnessLabelFresh {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelFresh)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", envelope.MissingEvidence)
	}
	if envelope.Counts.EvidenceFactsTotal != 15 {
		t.Fatalf("evidence_facts_total = %d, want 15", envelope.Counts.EvidenceFactsTotal)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesReadyWithFindings(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		[]SupplyChainImpactFindingResult{
			{FindingID: "finding-1", ImpactStatus: "affected_exact"},
			{FindingID: "finding-2", ImpactStatus: "possibly_affected"},
		},
		true,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyWithFindings)
	}
	if !envelope.Counts.FindingsTruncated {
		t.Fatal("findings_truncated = false, want true")
	}
	if got, want := envelope.Counts.FindingsByStatus["affected_exact"], 1; got != want {
		t.Fatalf("findings_by_status[affected_exact] = %d, want %d", got, want)
	}
	if got, want := envelope.Counts.FindingsByStatus["possibly_affected"], 1; got != want {
		t.Fatalf("findings_by_status[possibly_affected] = %d, want %d", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesTargetIncomplete(t *testing.T) {
	t.Parallel()

	// target_incomplete only fires when scope-relevant advisory evidence is
	// still missing; an in-flight snapshot for any source can flip the state
	// only when the scope has no advisory facts yet.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			TargetIncomplete:  true,
			IncompleteReasons: []string{"nvd_paging_in_progress"},
		},
	)
	if envelope.State != ReadinessStateTargetIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateTargetIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceTargetCollection) {
		t.Fatalf("missing_evidence = %#v, want target_collection_incomplete", envelope.MissingEvidence)
	}
	if got, want := envelope.IncompleteReasons, []string{"nvd_paging_in_progress"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("incomplete_reasons = %#v, want %#v", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessScopeGuardsTargetIncomplete(t *testing.T) {
	t.Parallel()

	// An in-flight snapshot for an unrelated source must NOT downgrade a
	// scope whose advisory evidence is already collected. Otherwise normal
	// staggered ingestion makes ready_zero_findings unreachable.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
			},
			TargetIncomplete:  true,
			IncompleteReasons: []string{"epss_refresh_in_progress"},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty for ready scope", envelope.MissingEvidence)
	}
	if len(envelope.IncompleteReasons) != 0 {
		t.Fatalf("incomplete_reasons = %#v, want empty for ready scope", envelope.IncompleteReasons)
	}
}

func TestBuildSupplyChainImpactReadinessClearsMissingOnReadyWithFindings(t *testing.T) {
	t.Parallel()

	// findings + missing-evidence reasons must not coexist in the envelope:
	// once the reducer admitted a finding, missing_evidence becomes
	// internally contradictory and is dropped.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		[]SupplyChainImpactFindingResult{
			{FindingID: "finding-1", ImpactStatus: "affected_exact"},
		},
		false,
		SupplyChainImpactReadinessSnapshot{},
	)
	if envelope.State != ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyWithFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty for ready_with_findings", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessUnavailable(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadinessUnavailable(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		[]SupplyChainImpactFindingResult{{FindingID: "finding-1", ImpactStatus: "affected_exact"}},
		true,
	)
	if envelope.State != ReadinessStateReadinessUnavailable {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadinessUnavailable)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceReadinessUnavailable) {
		t.Fatalf("missing_evidence = %#v, want readiness_unavailable", envelope.MissingEvidence)
	}
	if envelope.Counts.FindingsReturned != 1 {
		t.Fatalf("counts.findings_returned = %d, want 1 (findings page must survive)", envelope.Counts.FindingsReturned)
	}
	if !envelope.Counts.FindingsTruncated {
		t.Fatal("counts.findings_truncated = false, want true (carries the original page truncation)")
	}
}

func TestBuildSupplyChainImpactReadinessRejectsRepoOnlyRegistryAsOwnedPackages(t *testing.T) {
	t.Parallel()

	// package.registry data without a package_id anchor is global metadata,
	// not proof that the requested repository consumes packages. A
	// repository-anchored request with only registry evidence must still
	// surface MissingEvidenceOwnedPackages so the reviewer-flagged
	// "registry count suppresses missing owned packages" path stays closed.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 12, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateEvidenceIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, want owned_packages (registry alone cannot prove repo consumption)", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessAcceptsRegistryForPackageAnchor(t *testing.T) {
	t.Parallel()

	// When the caller anchors on a specific package_id, registry evidence
	// for that package IS owned-package proof; the reviewer fix must not
	// over-correct away the normal package-anchor flow.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{PackageID: "pkg:npm/example"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if readinessMissingContains(envelope.MissingEvidence, MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, must not include owned_packages for package-anchored scope", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessAggregatesFreshness(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 3, Freshness: FreshnessLabelStale},
				{Family: EvidenceFamilyVulnerabilityExploitability, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.Freshness != FreshnessLabelStale {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelStale)
	}
}

func TestBuildSupplyChainImpactReadinessNormalizesEvidenceSources(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyPackageRegistry, FactCount: 1},
				{Family: " "},
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 7},
			},
		},
	)
	want := []string{EvidenceFamilyPackageRegistry, EvidenceFamilyVulnerabilityAdvisory}
	got := make([]string, 0, len(envelope.EvidenceSources))
	for _, source := range envelope.EvidenceSources {
		got = append(got, source.Family)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence_sources = %#v, want %#v", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessExposesSourceSnapshotCacheMetadata(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
			SourceSnapshots: []SupplyChainImpactSourceSnapshot{
				{
					Source:               "first_epss",
					Ecosystem:            " ",
					CacheArtifactVersion: "vulnerability-source-cache.v1",
					SnapshotDigest:       "sha256:abc",
					LastUpdatedAt:        "2026-05-24T12:01:00Z",
					Freshness:            FreshnessLabelFresh,
					Complete:             true,
				},
				{Source: " "},
			},
		},
	)
	if len(envelope.SourceSnapshots) != 1 {
		t.Fatalf("source_snapshots = %#v, want one normalized snapshot", envelope.SourceSnapshots)
	}
	got := envelope.SourceSnapshots[0]
	if got.Source != "first_epss" {
		t.Fatalf("source = %q, want first_epss", got.Source)
	}
	if got.CacheArtifactVersion != "vulnerability-source-cache.v1" {
		t.Fatalf("cache_artifact_version = %q", got.CacheArtifactVersion)
	}
	if got.SnapshotDigest != "sha256:abc" {
		t.Fatalf("snapshot_digest = %q", got.SnapshotDigest)
	}
	if got.LastUpdatedAt != "2026-05-24T12:01:00Z" {
		t.Fatalf("last_updated_at = %q", got.LastUpdatedAt)
	}
	if got.Freshness != FreshnessLabelFresh {
		t.Fatalf("freshness = %q", got.Freshness)
	}
}
