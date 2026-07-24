// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// repositoryAnchorSupplyChainImpactFactLoader is a scope-aware FactLoader fake
// for the #5464 repository-anchor tests. Like
// scanScopedSupplyChainImpactFactLoader (added for #5463),
// ListFacts/ListFactsByKind partition by (scopeID, generationID) so a base
// intent-scope load cannot accidentally return a fact production would only
// ever reach cross-scope. Unlike that fixture, ListActiveSupplyChainImpactFacts
// here is filter-dispatched (mirroring stubSupplyChainImpactFactLoader's
// activeForFilter), so a test can assert a reducer_container_image_identity
// fact is returned ONLY on the digest-seeded call the new #5464 load stage
// makes (loadSupplyChainImpactResolvedDigestEvidenceFacts), never on the
// intent's original (pre-digest) active-evidence pass. Placing the identity
// fact behind that dispatch — rather than in factsByScope, where the
// scope-blind stubSupplyChainImpactFactLoader's ListFactsByKind ignores the
// requested scope entirely and would return it regardless of stage
// ordering — is what proves the fix's cross-scope, digest-seeded resolution
// actually ran, instead of the identity fact merely being reachable by
// coincidence.
type repositoryAnchorSupplyChainImpactFactLoader struct {
	factsByScope    map[string][]facts.Envelope
	activeForFilter func(SupplyChainImpactFactFilter) []facts.Envelope
	activeFilters   []SupplyChainImpactFactFilter
	kindCalls       map[string][][]string
}

// ListOSPackageAdvisoryFactEnvelopes satisfies osPackageAdvisoryFactLoader
// with no cross-scope advisory targets: these tests inject the os_package
// fact directly through ListActiveSupplyChainImpactFacts instead, mirroring
// scanScopedSupplyChainImpactFactLoader's approach for the same reason (the
// #5464 join under test starts downstream of the os_package fact, not at how
// it arrives).
func (l *repositoryAnchorSupplyChainImpactFactLoader) ListOSPackageAdvisoryFactEnvelopes(
	_ context.Context,
	_ []string,
	_ int,
) ([]facts.Envelope, int, error) {
	return nil, 0, nil
}

func (l *repositoryAnchorSupplyChainImpactFactLoader) ListFacts(
	_ context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.factsByScope[scanScopedFactLoaderKey(scopeID, generationID)]...), nil
}

func (l *repositoryAnchorSupplyChainImpactFactLoader) ListFactsByKind(
	_ context.Context,
	scopeID string,
	generationID string,
	kinds []string,
) ([]facts.Envelope, error) {
	key := scanScopedFactLoaderKey(scopeID, generationID)
	if l.kindCalls == nil {
		l.kindCalls = map[string][][]string{}
	}
	l.kindCalls[key] = append(l.kindCalls[key], append([]string(nil), kinds...))
	var matched []facts.Envelope
	for _, envelope := range l.factsByScope[key] {
		if slices.Contains(kinds, envelope.FactKind) {
			matched = append(matched, envelope)
		}
	}
	return matched, nil
}

func (l *repositoryAnchorSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	l.activeFilters = append(l.activeFilters, filter)
	if l.activeForFilter == nil {
		return nil, nil
	}
	return append([]facts.Envelope(nil), l.activeForFilter(filter)...), nil
}

func repositoryAnchorDebianOSPackageFact(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		FactKind:     facts.VulnerabilityOSPackageFactKind,
		ScopeID:      scopeID,
		GenerationID: generationID,
		Payload: map[string]any{
			"distro":                 "debian",
			"distro_version":         "12",
			"package_manager":        "dpkg",
			"name":                   "openssl",
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "debian",
			"installed_version_raw":  "3.0.11-1~deb12u2",
			"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
		},
	}
}

// containerImageIdentityImpactFactWithSourceRepositoryIDs builds a
// reducer_container_image_identity fact carrying BOTH the OCI-registry-path
// repository_id (decoyRepositoryID — deliberately distinct from any repo id
// these tests expect the join to produce) and the git source_repository_ids
// the #5464 os_package join actually anchors on. #5464 STEP 1 found that
// repository_id is the OCI/container registry's OWN repository identifier
// (e.g. "oci-registry://ghcr.io/org/repo") and can never equal a
// workload/service/deployment-lane repositoryID, which is always the git
// "repository:..." entity id carried in source_repository_ids — so a decoy
// repository_id here proves the join reads the right field rather than
// merely tolerating an absent one.
func containerImageIdentityImpactFactWithSourceRepositoryIDs(
	factID string,
	digest string,
	decoyRepositoryID string,
	sourceRepositoryIDs ...string,
) facts.Envelope {
	envelope := containerImageIdentityImpactFact(factID, digest, decoyRepositoryID)
	envelope.Payload["source_repository_ids"] = append([]string(nil), sourceRepositoryIDs...)
	return envelope
}

// TestSupplyChainImpactHandlerResolvesRepositoryFromContainerImageIdentityDigest
// is the positive regression guard for issue #5464: it proves the additive
// load stage (loadSupplyChainImpactResolvedDigestEvidenceFacts) re-runs the
// active-evidence reader seeded with the digest the scanner-analysis-scope
// stage resolves, so reducer_container_image_identity for that digest is
// loaded and the join in classifySupplyChainImpactPackage stamps
// finding.RepositoryID from it — which then lets the finding reach
// workload/service/environment context, all of which
// matchingSupplyChainWorkloads/Services and applySupplyChainRuntimeContext's
// deployment lookup require a non-blank RepositoryID for. The identity fact
// is placed behind the fake's filter-dispatch (not a scope map entry), so it
// is reachable ONLY on the digest-seeded call; before the fix, RepositoryID
// stayed blank end-to-end and none of workload/service/environment context
// would resolve.
func TestSupplyChainImpactHandlerResolvesRepositoryFromContainerImageIdentityDigest(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl-repo-anchor"
		intentGenerationID = "generation-intel-5464"
		scanScopeID        = "scan-target-debian-app-os-package-repo-anchor"
		scanGenerationID   = "generation-scan-5464"
		repositoryID       = "github.com/example/repo-anchor-app"
		workloadID         = "workload-repo-anchor"
		serviceID          = "service-repo-anchor"
		environment        = "production"
		imageRef           = "registry.example/repo-anchor-app@" + testScannerAnalysisImageDigest
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-repo-anchor", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)
	// repository_id deliberately carries a decoy OCI-registry-path value
	// (#5464 STEP 1): the join under test MUST read source_repository_ids,
	// never repository_id, so a passing test here proves the right field won.
	identity := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-repo-anchor", testScannerAnalysisImageDigest,
		"oci-registry://registry.example/repo-anchor-app", repositoryID,
	)

	loader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-repo-anchor",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-repo-anchor",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				workloadIdentityImpactFact("workload-repo-anchor-1", repositoryID, workloadID),
				serviceCatalogCorrelationImpactFact(
					"service-repo-anchor-1",
					repositoryID,
					serviceID,
					workloadID,
					string(ServiceCatalogCorrelationExact),
					"",
					false,
				),
				cicdRunCorrelationImpactFact(
					"deployment-repo-anchor-1",
					testScannerAnalysisImageDigest,
					imageRef,
					repositoryID,
					environment,
					string(CICDRunCorrelationExact),
				),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
	}
	// The FIRST call to ListActiveSupplyChainImpactFacts always serves the
	// os_package fact, regardless of what the round-0 filter already carries
	// (the cicdRunCorrelationImpactFact fixture above contributes the same
	// digest/repository into that very first filter, since it loads through
	// the intent's own base scope-fact stage before any active-evidence call
	// runs). This mirrors production and scanScopedSupplyChainImpactFactLoader:
	// os_package arrives once, up front; only a LATER call — the one the new
	// #5464 digest-seeded stage makes after the scanner-analysis-scope stage
	// resolves SubjectDigest — is dispatched on the digest to decide whether
	// reducer_container_image_identity is returned. Serving the identity fact
	// on round 0 instead would let it arrive regardless of stage ordering and
	// mask the bug this test guards against.
	osPackageServed := false
	loader.activeForFilter = func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		if !osPackageServed {
			osPackageServed = true
			return []facts.Envelope{osPackage}
		}
		if slices.Contains(filter.SubjectDigests, testScannerAnalysisImageDigest) {
			return []facts.Envelope{identity}
		}
		return nil
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-repo-anchor",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1: %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.RepositoryID != repositoryID {
		t.Fatalf(
			"RepositoryID = %q, want %q resolved from reducer_container_image_identity by the scanned digest %q",
			got.RepositoryID, repositoryID, testScannerAnalysisImageDigest,
		)
	}
	assertContainsString(t, got.EvidenceFactIDs, "identity-repo-anchor")
	assertContainsString(t, got.EvidencePath, containerImageIdentityFactKind)
	assertContainsString(t, got.WorkloadIDs, workloadID)
	assertContainsString(t, got.ServiceIDs, serviceID)
	assertContainsString(t, got.Environments, environment)

	if got, want := result.SubSignals["resolved_digest_evidence_facts"], float64(1); got != want {
		t.Fatalf("SubSignals[resolved_digest_evidence_facts] = %v, want %v", got, want)
	}
}

// TestSupplyChainImpactHandlerRetainsOSPackageFindingWithUnresolvedDigest is
// the negative regression guard for #5464: when the scanned digest resolves
// (SubjectDigest is stamped from scanner_worker.analysis) but NO
// reducer_container_image_identity fact exists anywhere in the generation for
// that digest, RepositoryID MUST stay blank — the join must never invent a
// repository anchor — and no workload/service/environment context can
// resolve. The finding must still be RETAINED under the #5463 owned-anchor
// rule (a vendor-matched os_package is its own anchor even with no
// repository).
func TestSupplyChainImpactHandlerRetainsOSPackageFindingWithUnresolvedDigest(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID         = "vuln-intel:debian:openssl-repo-anchor-unresolved"
		intentGenerationID    = "generation-intel-5464-unresolved"
		scanScopeID           = "scan-target-debian-app-os-package-repo-anchor-unresolved"
		scanGenerationID      = "generation-scan-5464-unresolved"
		unrelatedRepositoryID = "github.com/example/unrelated-repo"
		unrelatedWorkloadID   = "workload-unrelated"
		imageRef              = "registry.example/repo-anchor-app-unresolved@" + testScannerAnalysisImageDigest
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-repo-anchor-unresolved", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)

	loader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-repo-anchor-unresolved",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-repo-anchor-unresolved",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				// Tied to an unrelated repository, proving an empty
				// finding.RepositoryID never accidentally matches every
				// workload regardless of repository.
				workloadIdentityImpactFact("workload-unrelated-1", unrelatedRepositoryID, unrelatedWorkloadID),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
	}
	osPackageServed := false
	loader.activeForFilter = func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		if !osPackageServed {
			osPackageServed = true
			return []facts.Envelope{osPackage}
		}
		// No reducer_container_image_identity fact exists for this digest
		// anywhere in the active generation.
		return nil
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-repo-anchor-unresolved",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1 (retained via the owned-anchor rule): %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty: an unresolvable digest must never invent a repository anchor", got.RepositoryID)
	}
	if len(got.WorkloadIDs) != 0 {
		t.Fatalf("WorkloadIDs = %#v, want none without a resolved repository", got.WorkloadIDs)
	}
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want none without a resolved repository", got.ServiceIDs)
	}
	if len(got.Environments) != 0 {
		t.Fatalf("Environments = %#v, want none without a resolved repository", got.Environments)
	}
	if got.SubjectDigest != testScannerAnalysisImageDigest {
		t.Fatalf("SubjectDigest = %q, want %q (the scanner analysis digest resolves independently of the repository join)",
			got.SubjectDigest, testScannerAnalysisImageDigest)
	}
	assertContainsString(t, got.EvidencePath, facts.VulnerabilityOSPackageFactKind)
}

// TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity
// is the precedence regression guard for #5464: a finding that already has a
// consumption-derived RepositoryID (set earlier in
// classifySupplyChainImpactPackage from reducer_package_consumption_correlation,
// a manifest-level anchor) must keep it, not be overwritten by a
// digest-resolved reducer_container_image_identity repository. The
// consumption path is per-package manifest evidence; an image identity only
// tells you which repository built the scanned image, which can be shared
// across many unrelated packages, so it is treated as lower precedence and
// only fills a currently-blank RepositoryID.
func TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID           = "vuln-intel:debian:openssl-repo-anchor-precedence"
		intentGenerationID      = "generation-intel-5464-precedence"
		scanScopeID             = "scan-target-debian-app-os-package-repo-anchor-precedence"
		scanGenerationID        = "generation-scan-5464-precedence"
		consumptionRepositoryID = "github.com/example/consumption-anchor-app"
		imageRepositoryID       = "github.com/example/image-anchor-app"
		imageRef                = "registry.example/precedence-app@" + testScannerAnalysisImageDigest
		affectedPackageID       = "pkg:deb/debian/openssl"
	)

	osPackage := repositoryAnchorDebianOSPackageFact("dpkg-os-openssl-repo-anchor-precedence", scanScopeID, scanGenerationID)
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)
	identity := containerImageIdentityImpactFactWithSourceRepositoryIDs(
		"identity-precedence", testScannerAnalysisImageDigest,
		"oci-registry://registry.example/precedence-app", imageRepositoryID,
	)

	loader := &repositoryAnchorSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-repo-anchor-precedence",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-repo-anchor-precedence",
					"CVE-2026-5464",
					"debian",
					"DSA-2026-5464",
					affectedPackageID,
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
				packageConsumptionFactWithRange("consume-repo-anchor-precedence", affectedPackageID, consumptionRepositoryID, ""),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
	}
	osPackageServed := false
	loader.activeForFilter = func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		if !osPackageServed {
			osPackageServed = true
			return []facts.Envelope{osPackage}
		}
		if slices.Contains(filter.SubjectDigests, testScannerAnalysisImageDigest) {
			return []facts.Envelope{identity}
		}
		return nil
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-repo-anchor-precedence",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1: %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.RepositoryID != consumptionRepositoryID {
		t.Fatalf(
			"RepositoryID = %q, want the consumption-derived %q to win over the image-identity-derived %q",
			got.RepositoryID, consumptionRepositoryID, imageRepositoryID,
		)
	}
	assertNotContainsString(t, got.EvidenceFactIDs, "identity-precedence")
	assertNotContainsString(t, got.EvidencePath, containerImageIdentityFactKind)
}
