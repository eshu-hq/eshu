// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

func TestBuildSupplyChainImpactReadinessClassifiesNotConfigured(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{},
	)
	if envelope.State != packet.ReadinessStateNotConfigured {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateNotConfigured)
	}
	wantMissing := []string{packet.MissingEvidenceAdvisorySources, packet.MissingEvidenceOwnedPackages}
	sort.Strings(wantMissing)
	if !reflect.DeepEqual(envelope.MissingEvidence, wantMissing) {
		t.Fatalf("missing_evidence = %#v, want %#v", envelope.MissingEvidence, wantMissing)
	}
	if envelope.Freshness != packet.FreshnessLabelUnknown {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, packet.FreshnessLabelUnknown)
	}
	if envelope.Counts.FindingsReturned != 0 || envelope.Counts.FindingsTruncated {
		t.Fatalf("counts = %#v, want zero/false", envelope.Counts)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesEvidenceIncomplete(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateEvidenceIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, want owned_packages", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesReadyZeroFindings(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 3, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 3, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyZeroFindings)
	}
	if envelope.Freshness != packet.FreshnessLabelFresh {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, packet.FreshnessLabelFresh)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", envelope.MissingEvidence)
	}
	if envelope.Counts.EvidenceFactsTotal != 18 {
		t.Fatalf("evidence_facts_total = %d, want 18", envelope.Counts.EvidenceFactsTotal)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesStaleAdvisoryAsIncomplete(t *testing.T) {
	t.Parallel()

	// Stale advisory evidence is missing evidence for the current scanner
	// answer, even when owned package and package-registry joins are fresh.
	// API and MCP callers must not receive ready_zero_findings for a
	// zero-finding page backed by stale advisory metadata.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: packet.FreshnessLabelStale},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 3, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 3, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateEvidenceIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %#v, want advisory_sources for stale advisory metadata", envelope.MissingEvidence)
	}
	if envelope.Freshness != packet.FreshnessLabelStale {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, packet.FreshnessLabelStale)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesReadyWithFindings(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{CVEID: "CVE-2026-0001"},
		[]packet.FindingResult{
			{FindingID: "finding-1", Status: "affected_exact"},
			{FindingID: "finding-2", Status: "possibly_affected"},
		},
		true,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyWithFindings)
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
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			TargetIncomplete:  true,
			IncompleteReasons: []string{"nvd_paging_in_progress"},
		},
	)
	if envelope.State != packet.ReadinessStateTargetIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateTargetIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceTargetCollection) {
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
	envelope := packet.BuildReadiness(
		packet.TargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
			},
			TargetIncomplete:  true,
			IncompleteReasons: []string{"epss_refresh_in_progress"},
		},
	)
	if envelope.State != packet.ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyZeroFindings)
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
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		[]packet.FindingResult{
			{FindingID: "finding-1", Status: "affected_exact"},
		},
		false,
		packet.ReadinessSnapshot{},
	)
	if envelope.State != packet.ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyWithFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty for ready_with_findings", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessUnavailable(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadinessUnavailable(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		[]packet.FindingResult{{FindingID: "finding-1", Status: "affected_exact"}},
		true,
	)
	if envelope.State != packet.ReadinessStateReadinessUnavailable {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadinessUnavailable)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceReadinessUnavailable) {
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
	// surface packet.MissingEvidenceOwnedPackages so the reviewer-flagged
	// "registry count suppresses missing owned packages" path stays closed.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 12, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateEvidenceIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, want owned_packages (registry alone cannot prove repo consumption)", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessAcceptsRegistryForPackageAnchor(t *testing.T) {
	t.Parallel()

	// When the caller anchors on a specific package_id, registry evidence
	// for that package IS owned-package proof; the reviewer fix must not
	// over-correct away the normal package-anchor flow.
	envelope := packet.BuildReadiness(
		packet.TargetScope{PackageID: "pkg:npm/example"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyZeroFindings)
	}
	if packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, must not include owned_packages for package-anchored scope", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessAggregatesFreshness(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 3, Freshness: packet.FreshnessLabelStale},
				{Family: packet.EvidenceFamilyVulnerabilityExploitability, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.Freshness != packet.FreshnessLabelStale {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, packet.FreshnessLabelStale)
	}
}

func TestBuildSupplyChainImpactReadinessNormalizesEvidenceSources(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 1},
				{Family: " "},
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 7},
			},
		},
	)
	want := []string{packet.EvidenceFamilyPackageRegistry, packet.EvidenceFamilyVulnerabilityAdvisory}
	got := make([]string, 0, len(envelope.EvidenceSources))
	for _, source := range envelope.EvidenceSources {
		got = append(got, source.Family)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence_sources = %#v, want %#v", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesUnsupportedEcosystem(t *testing.T) {
	t.Parallel()

	// Eshu observed an owned dependency in an ecosystem the matcher cannot
	// resolve (no advisory matched and no finding emitted). Readiness must
	// surface this as unsupported, not as ready_zero_findings, so callers
	// cannot mistake "we cannot match this" for "clean".
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 3, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 3},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateUnsupported)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceUnsupportedTargets) {
		t.Fatalf("missing_evidence = %#v, want unsupported_targets", envelope.MissingEvidence)
	}
	if len(envelope.UnsupportedTargets) != 1 ||
		envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindEcosystem ||
		envelope.UnsupportedTargets[0].Count != 3 {
		t.Fatalf("unsupported_targets = %#v, want one ecosystem entry with count=3", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesUnsupportedPackageManagerFile(t *testing.T) {
	t.Parallel()

	// Eshu parsed a package-manager file but recorded an unsupported lockfile
	// feature; readiness must surface the observation as unsupported instead
	// of admitting clean evidence.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{
					TargetKind:     packet.UnsupportedTargetKindPackageManagerFile,
					Reason:         "lockfile_unsupported_feature",
					LockfileFlavor: "yarn",
					FeatureToken:   "patch",
					Count:          1,
				},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateUnsupported)
	}
	if len(envelope.UnsupportedTargets) != 1 ||
		envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindPackageManagerFile ||
		envelope.UnsupportedTargets[0].FeatureToken != "patch" {
		t.Fatalf("unsupported_targets = %#v, want one package_manager_file entry with feature=patch", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesUnsupportedDependencySource(t *testing.T) {
	t.Parallel()

	// VCS/path/local dependency evidence is observed target evidence, but it
	// is not registry-resolvable package consumption. Surface it as an
	// unsupported target with a stable reason code instead of letting the
	// scope look clean or merely absent.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{
					TargetKind:   packet.UnsupportedTargetKindDependencySource,
					Reason:       "vcs_dependency_unsupported",
					Ecosystem:    "pypi",
					FeatureToken: "vcs_dependency",
					Count:        1,
				},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateUnsupported)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceUnsupportedTargets) {
		t.Fatalf("missing_evidence = %#v, want unsupported_targets", envelope.MissingEvidence)
	}
	if len(envelope.UnsupportedTargets) != 1 ||
		envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindDependencySource ||
		envelope.UnsupportedTargets[0].Reason != "vcs_dependency_unsupported" ||
		envelope.UnsupportedTargets[0].FeatureToken != "vcs_dependency" {
		t.Fatalf("unsupported_targets = %#v, want one dependency_source vcs row", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesUnsupportedSBOMTarget(t *testing.T) {
	t.Parallel()

	// An SBOM document was observed but the parser flagged unsupported_field
	// or malformed_document; readiness must surface that the subject digest
	// has unsupported target evidence so callers do not mistake "nothing
	// matched" for "no SBOM evidence".
	envelope := packet.BuildReadiness(
		packet.TargetScope{SubjectDigest: "sha256:deadbeef"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyContainerImageIdentity, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindSBOMTarget, Reason: "unsupported_field", Count: 2},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateUnsupported)
	}
	if len(envelope.UnsupportedTargets) != 1 ||
		envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindSBOMTarget {
		t.Fatalf("unsupported_targets = %#v, want one sbom_target entry", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesMissingSBOMOrImageEvidence(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{SubjectDigest: "sha256:missing"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateEvidenceIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceSBOMOrImage) {
		t.Fatalf("missing_evidence = %#v, want sbom_or_image_evidence", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesPackageRegistryMetadataTooLarge(t *testing.T) {
	t.Parallel()

	// The package-registry collector observed the requested package but the
	// metadata document exceeded the configured byte limit. That is an
	// explicit source coverage gap, not a clean zero-finding result.
	envelope := packet.BuildReadiness(
		packet.TargetScope{PackageID: "pkg:npm/oversized"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{
					TargetKind: packet.UnsupportedTargetKindPackageRegistryMetadata,
					Reason:     "metadata_too_large",
					Ecosystem:  "npm",
					Count:      1,
				},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateUnsupported)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceUnsupportedTargets) {
		t.Fatalf("missing_evidence = %#v, want unsupported_targets", envelope.MissingEvidence)
	}
	if len(envelope.UnsupportedTargets) != 1 ||
		envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindPackageRegistryMetadata ||
		envelope.UnsupportedTargets[0].Reason != "metadata_too_large" ||
		envelope.UnsupportedTargets[0].Ecosystem != "npm" {
		t.Fatalf("unsupported_targets = %#v, want one package_registry_metadata metadata_too_large entry", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesUnsupportedImageTarget(t *testing.T) {
	t.Parallel()

	// A container image was observed but no supported analyzer matched the
	// image content; readiness must surface unsupported instead of admitting
	// the image as covered.
	envelope := packet.BuildReadiness(
		packet.TargetScope{SubjectDigest: "sha256:cafefade"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyContainerImageIdentity, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindImageTarget, Reason: "image_analyzer_unsupported", Count: 1},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateUnsupported)
	}
	if len(envelope.UnsupportedTargets) != 1 ||
		envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindImageTarget {
		t.Fatalf("unsupported_targets = %#v, want one image_target entry", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessUnsupportedOutranksReadyZeroFindings(t *testing.T) {
	t.Parallel()

	// Accuracy guard from the Copilot review thread: even when a scope has
	// matchable advisory + owned-package evidence and the reducer admitted
	// zero findings (a clean ready_zero_findings shape on the matchable
	// side), the presence of observed unsupported target evidence MUST
	// outrank ready_zero_findings. Otherwise callers would see
	// "ready_zero_findings" while there is real coverage Eshu cannot match,
	// which is exactly the "clean" misread the unsupported state is
	// supposed to prevent.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 3, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 2},
			},
		},
	)
	if envelope.State != packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q (unsupported must outrank ready_zero_findings when observed coverage gap exists)", envelope.State, packet.ReadinessStateUnsupported)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceUnsupportedTargets) {
		t.Fatalf("missing_evidence = %#v, want unsupported_targets", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessUnsupportedDropsEntriesWithoutReason(t *testing.T) {
	t.Parallel()

	// Companion guard for the Copilot OpenAPI review: every surfaced
	// unsupported_target row MUST carry a stable reason code because the
	// schema requires it. Producer rows with a blank reason are dropped
	// during normalization so the envelope cannot publish a contract
	// violation, and a scope with only-blank-reason entries falls back to
	// the non-unsupported classification.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindSBOMTarget, Reason: "  ", Count: 1},
			},
		},
	)
	if len(envelope.UnsupportedTargets) != 0 {
		t.Fatalf("unsupported_targets = %#v, want empty (blank reasons must be dropped)", envelope.UnsupportedTargets)
	}
	if envelope.State == packet.ReadinessStateUnsupported {
		t.Fatalf("state = %q, must not be unsupported when no normalized targets remain", envelope.State)
	}
}

func TestBuildSupplyChainImpactReadinessUnsupportedDoesNotCollapseMissingEvidence(t *testing.T) {
	t.Parallel()

	// A scope with normal missing-evidence (no owned-package facts, no
	// unsupported observations) must remain evidence_incomplete with
	// missing_evidence=owned_packages. It MUST NOT slide into unsupported,
	// otherwise callers cannot tell "we never collected this" from "we
	// observed something but cannot match it".
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q (missing-evidence must not be unsupported)", envelope.State, packet.ReadinessStateEvidenceIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, want owned_packages", envelope.MissingEvidence)
	}
	if packet.ReadinessMissingContains(envelope.MissingEvidence, packet.MissingEvidenceUnsupportedTargets) {
		t.Fatalf("missing_evidence = %#v, must NOT carry unsupported_targets without unsupported evidence", envelope.MissingEvidence)
	}
	if len(envelope.UnsupportedTargets) != 0 {
		t.Fatalf("unsupported_targets = %#v, want empty when no unsupported producer fired", envelope.UnsupportedTargets)
	}
}

func TestBuildSupplyChainImpactReadinessUnsupportedSurfacesAlongsideFindings(t *testing.T) {
	t.Parallel()

	// Unsupported target evidence is additive: when findings exist, the state
	// stays ready_with_findings (the reducer did decide) but unsupported
	// target counts remain visible so operators can see hidden coverage gaps
	// without being told the result is clean for unsupported families.
	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		[]packet.FindingResult{
			{FindingID: "finding-1", Status: "affected_exact"},
		},
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 2},
			},
		},
	)
	if envelope.State != packet.ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyWithFindings)
	}
	if len(envelope.UnsupportedTargets) != 1 {
		t.Fatalf("unsupported_targets = %#v, want one entry surfaced even with findings", envelope.UnsupportedTargets)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty on ready_with_findings", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessUnsupportedNormalizesAndSortsTargets(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			UnsupportedTargets: []packet.UnsupportedTarget{
				{TargetKind: packet.UnsupportedTargetKindSBOMTarget, Reason: "unsupported_field", Count: 2},
				{TargetKind: " "},
				{TargetKind: packet.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 1},
				{TargetKind: packet.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 1},
			},
		},
	)
	if got := len(envelope.UnsupportedTargets); got != 2 {
		t.Fatalf("unsupported_targets count = %d, want 2 (blank dropped, duplicates merged)", got)
	}
	// Deterministic order: ecosystem before sbom_target by target_kind.
	if envelope.UnsupportedTargets[0].TargetKind != packet.UnsupportedTargetKindEcosystem ||
		envelope.UnsupportedTargets[1].TargetKind != packet.UnsupportedTargetKindSBOMTarget {
		t.Fatalf("unsupported_targets order = %#v, want ecosystem then sbom_target", envelope.UnsupportedTargets)
	}
	// Duplicates collapse with counts summed.
	if got := envelope.UnsupportedTargets[0].Count; got != 2 {
		t.Fatalf("ecosystem count = %d, want 2 (duplicate dedup sums)", got)
	}
}

func TestBuildSupplyChainImpactReadinessExposesSourceSnapshotCacheMetadata(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
			SourceSnapshots: []packet.SourceSnapshot{
				{
					Source:               "first_epss",
					Ecosystem:            " ",
					CacheArtifactVersion: "vulnerability-source-cache.v1",
					SnapshotDigest:       "sha256:abc",
					LastUpdatedAt:        "2026-05-24T12:01:00Z",
					Freshness:            packet.FreshnessLabelFresh,
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
	if got.Freshness != packet.FreshnessLabelFresh {
		t.Fatalf("freshness = %q", got.Freshness)
	}
}
