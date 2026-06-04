package reducer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testImpactPackageID     = "pkg:npm/example"
	testImpactPURL          = "pkg:npm/example@1.2.3"
	testImpactFixedPURL     = "pkg:npm/example@1.3.0"
	testImpactRepositoryID  = "repo://example/api"
	testImpactSubjectDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type stubSupplyChainImpactFactLoader struct {
	scopeFacts      []facts.Envelope
	active          []facts.Envelope
	activeCalls     [][]facts.Envelope
	activeForFilter func(SupplyChainImpactFactFilter) []facts.Envelope
	kindCalls       [][]string
	filters         []SupplyChainImpactFactFilter
}

func (s *stubSupplyChainImpactFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSupplyChainImpactFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	s.filters = append(s.filters, filter)
	if s.activeForFilter != nil {
		return append([]facts.Envelope(nil), s.activeForFilter(filter)...), nil
	}
	if len(s.activeCalls) > 0 {
		active := s.activeCalls[0]
		s.activeCalls = s.activeCalls[1:]
		return append([]facts.Envelope(nil), active...), nil
	}
	return append([]facts.Envelope(nil), s.active...), nil
}

type recordingSupplyChainImpactWriter struct {
	write SupplyChainImpactWrite
	calls int
}

func (w *recordingSupplyChainImpactWriter) WriteSupplyChainImpactFindings(
	_ context.Context,
	write SupplyChainImpactWrite,
) (SupplyChainImpactWriteResult, error) {
	w.calls++
	w.write = write
	return SupplyChainImpactWriteResult{
		CanonicalWrites: supplyChainImpactCanonicalWrites(write.Findings),
		FactsWritten:    len(write.Findings),
	}, nil
}

func TestBuildSupplyChainImpactFindingsClassifiesEvidencePaths(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		vulnerabilityEPSSFact("epss-1", "CVE-2026-0001", "0.71", "0.98"),
		vulnerabilityKEVFact("kev-1", "CVE-2026-0001"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),

		vulnerabilityCVEFact("cve-fixed", "CVE-2026-0002", 9.8),
		vulnerabilityAffectedPackageFact("affected-fixed", "CVE-2026-0002", "pkg:npm/fixed", "npm", "fixed", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-fixed", "pkg:npm/fixed", testImpactRepositoryID, "1.3.0"),

		vulnerabilityCVEFact("cve-unanchored", "CVE-2026-0003", 5.0),
		vulnerabilityAffectedPackageFact("affected-unanchored", "CVE-2026-0003", "pkg:npm/other", "npm", "other", "", "2.0.0"),

		vulnerabilityCVEFact("cve-unknown", "CVE-2026-0004", 7.5),
	})

	got := supplyChainImpactFindingsByCVE(findings)
	assertSupplyChainImpactStatus(t, got["CVE-2026-0001"], SupplyChainImpactAffectedExact)
	if got["CVE-2026-0001"].PriorityReason == "" || !got["CVE-2026-0001"].KnownExploited {
		t.Fatalf("CVE-2026-0001 priority signals missing: %#v", got["CVE-2026-0001"])
	}
	if got["CVE-2026-0001"].RuntimeReachability == "" {
		t.Fatalf("CVE-2026-0001 RuntimeReachability = blank, want package reachability")
	}
	assertSupplyChainImpactStatus(t, got["CVE-2026-0002"], SupplyChainImpactNotAffectedKnownFixed)
	if got["CVE-2026-0002"].RepositoryID == "" {
		t.Fatalf("CVE-2026-0002 RepositoryID = blank, want anchored known-fixed finding")
	}
	if _, ok := got["CVE-2026-0003"]; ok {
		t.Fatalf("CVE-2026-0003 produced an impact finding without owned package, repository, or image evidence: %#v", got["CVE-2026-0003"])
	}
	if _, ok := got["CVE-2026-0004"]; ok {
		t.Fatalf("CVE-2026-0004 produced an impact finding from source-only CVE evidence: %#v", got["CVE-2026-0004"])
	}
}

func TestBuildSupplyChainImpactFindingsDerivesImagePathFromSBOMAttachment(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, testImpactSubjectDigest)
	}
	if !strings.Contains(strings.Join(got.EvidencePath, " -> "), "sbom.component") {
		t.Fatalf("EvidencePath = %#v, want SBOM component path", got.EvidencePath)
	}
}

func TestBuildSupplyChainImpactFindingsRequiresAffectedVersionForExactImpact(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.RuntimeReachability != jsTSPackageAPIMissingEvidence {
		t.Fatalf("RuntimeReachability = %q, want JS/TS package API missing evidence", got.RuntimeReachability)
	}
	if got.Confidence != "partial" || got.Reachability == nil || got.Reachability.Confidence != "unknown" {
		t.Fatalf("impact/reachability confidence = %q/%#v, want separate partial impact and unknown reachability", got.Confidence, got.Reachability)
	}
	if !stringSliceContains(got.MissingEvidence, jsTSParserOrSCIPMissingReason) {
		t.Fatalf("MissingEvidence = %#v, want JS/TS parser or SCIP package API gap", got.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactFindingsUsesProviderAlertWithOwnedLockfileEvidence(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example/api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-1", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(168),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"dependency_scope":      "runtime",
		"relationship":          "transitive",
		"ghsa_ids":              []string{"GHSA-provider-0001"},
		"cve_ids":               []string{"CVE-2026-47138"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
		"severity":              "high",
		"cvss":                  map[string]any{"score": 8.1, "vector": "CVSS:3.1/AV:N/AC:L"},
		"updated_at":            "2026-05-26T12:00:00Z",
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-1", repoID, packageID, "package-lock.json")
	consumption.Payload["dependency_range"] = "3.0.3"
	consumption.Payload["dependency_path"] = []string{"ajv", "fast-uri"}
	consumption.Payload["dependency_depth"] = 2
	consumption.Payload["direct_dependency"] = false
	consumption.Payload["dependency_scope"] = "runtime"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, consumption})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.CVEID != "CVE-2026-47138" || got.AdvisoryID != "GHSA-provider-0001" {
		t.Fatalf("advisory identity = (%q, %q), want provider CVE and GHSA", got.CVEID, got.AdvisoryID)
	}
	if got.ObservedVersion != "3.0.3" || got.RequestedRange != "3.0.3" {
		t.Fatalf("version evidence = observed %q requested %q, want lockfile version", got.ObservedVersion, got.RequestedRange)
	}
	if got.VulnerableRange != "< 3.1.0" || got.FixedVersion != "3.1.0" {
		t.Fatalf("advisory range/fix = %q/%q, want provider vulnerable range and patched version", got.VulnerableRange, got.FixedVersion)
	}
	if !strings.Contains(strings.Join(got.EvidencePath, " -> "), facts.SecurityAlertRepositoryAlertFactKind) {
		t.Fatalf("EvidencePath = %#v, want provider alert evidence", got.EvidencePath)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want %q", got.DetectionProfile, DetectionProfilePrecise)
	}
}

func TestBuildSupplyChainImpactFindingsConnectsProviderAlertRuntimeContext(t *testing.T) {
	t.Parallel()

	repoID := "repo://github/example/api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-runtime-context", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(171),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"dependency_scope":      "runtime",
		"relationship":          "transitive",
		"ghsa_ids":              []string{"GHSA-provider-runtime"},
		"cve_ids":               []string{"CVE-2026-47140"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
		"severity":              "high",
		"updated_at":            "2026-05-26T12:00:00Z",
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-runtime-context", repoID, packageID, "package-lock.json")
	consumption.Payload["dependency_range"] = "3.0.3"
	consumption.Payload["dependency_scope"] = "runtime"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		alert,
		consumption,
		workloadIdentityImpactFact("workload-runtime-context", repoID, testImpactWorkloadID),
		serviceCatalogCorrelationImpactFact(
			"catalog-runtime-context",
			repoID,
			testImpactServiceID,
			testImpactWorkloadID,
			string(ServiceCatalogCorrelationExact),
			"matches",
			false,
		),
	})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.ServiceIDs, testImpactServiceID)
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "workload-runtime-context")
	assertContainsString(t, got.EvidenceFactIDs, "catalog-runtime-context")
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
}

func TestBuildSupplyChainImpactFindingsResolvesProviderAlertRepositoryScope(t *testing.T) {
	t.Parallel()

	providerRepoID := "security-alert:github:acme/api"
	canonicalRepoID := "repository:r_api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-provider-repo", providerRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(169),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "packages/client/package-lock.json",
		"dependency_scope":      "runtime",
		"relationship":          "transitive",
		"ghsa_ids":              []string{"GHSA-provider-0002"},
		"cve_ids":               []string{"CVE-2026-47139"},
		"vulnerable_range":      "<= 3.1.1",
		"patched_version":       "3.1.2",
		"severity":              "high",
		"updated_at":            "2026-05-26T12:00:00Z",
	})
	consumption := packageConsumptionCorrelationEnvelope(
		"consume-provider-repo",
		canonicalRepoID,
		packageID,
		"packages/client/package-lock.json",
	)
	consumption.Payload["repository_name"] = "api"
	consumption.Payload["dependency_range"] = "3.0.3"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, consumption})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	if got.RepositoryID != canonicalRepoID {
		t.Fatalf("RepositoryID = %q, want canonical repository id %q", got.RepositoryID, canonicalRepoID)
	}
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
}

func TestBuildSupplyChainImpactFindingsSkipsAmbiguousProviderRepositoryScope(t *testing.T) {
	t.Parallel()

	providerRepoID := "security-alert:github:acme/api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-provider-ambiguous", providerRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(170),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "packages/client/package-lock.json",
		"ghsa_ids":              []string{"GHSA-provider-0003"},
		"vulnerable_range":      "<= 3.1.1",
		"patched_version":       "3.1.2",
	})
	firstConsumption := packageConsumptionCorrelationEnvelope(
		"consume-provider-ambiguous-1",
		"repository:r_api_1",
		packageID,
		"packages/client/package-lock.json",
	)
	firstConsumption.Payload["repository_name"] = "api"
	firstConsumption.Payload["dependency_range"] = "3.0.3"
	secondConsumption := packageConsumptionCorrelationEnvelope(
		"consume-provider-ambiguous-2",
		"repository:r_api_2",
		packageID,
		"packages/client/package-lock.json",
	)
	secondConsumption.Payload["repository_name"] = "api"
	secondConsumption.Payload["dependency_range"] = "3.0.3"

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, firstConsumption, secondConsumption})

	if got := len(findings); got != 0 {
		t.Fatalf("len(findings) = %d, want no impact finding for ambiguous repository scope: %#v", got, findings)
	}
}

func TestSupplyChainImpactHandlerLoadsActiveEvidenceAndWritesFindings(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		},
		active: []facts.Envelope{
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), strings.Join(supplyChainImpactFactKinds(), ","); got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[0].PackageIDs, ","), testImpactPackageID; got != want {
		t.Fatalf("active package IDs = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestSupplyChainImpactHandlerKeepsProviderAlertsRepositoryScoped(t *testing.T) {
	t.Parallel()

	providerRepoID := "security-alert:github:acme/api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-provider-scope", providerRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(171),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"repository_name":       "api",
		"cve_ids":               []string{"CVE-2026-47140"},
		"ghsa_ids":              []string{"GHSA-provider-0004"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
	})
	unrelatedConsumption := packageConsumptionCorrelationEnvelope(
		"consume-other-repo",
		"repository:r_other",
		packageID,
		"package-lock.json",
	)
	unrelatedConsumption.Payload["repository_name"] = "other"
	unrelatedConsumption.Payload["dependency_range"] = "3.0.3"
	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{alert},
		active: []facts.Envelope{
			vulnerabilityCVEFact("cve-provider-scope", "CVE-2026-47140", 7.5),
			vulnerabilityAffectedPackageFact(
				"affected-provider-scope",
				"CVE-2026-47140",
				packageID,
				"npm",
				"fast-uri",
				"< 3.1.0",
				"3.1.0",
			),
			unrelatedConsumption,
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-provider-scope",
		ScopeID:      providerRepoID,
		GenerationID: "generation-provider-scope",
		SourceSystem: "security_alert",
		Domain:       DomainSupplyChainImpact,
		Cause:        "provider security alert evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := len(writer.write.Findings); got != 0 {
		t.Fatalf("len(writer.write.Findings) = %d, want no cross-repository provider-alert findings: %#v", got, writer.write.Findings)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 without same-repository dependency evidence", result.CanonicalWrites)
	}
}

func TestSupplyChainImpactHandlerLoadsActiveEvidenceFromPackageIdentity(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			packageRegistryPackageImpactFact("package-1", testImpactPackageID),
		},
		activeCalls: [][]facts.Envelope{
			{
				vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
				packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
			},
			{
				vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			},
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "package-registry:npm:example",
		GenerationID: "generation-package",
		SourceSystem: "package_registry",
		Domain:       DomainSupplyChainImpact,
		Cause:        "package registry identity observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := strings.Join(loader.filters[0].PackageIDs, ","), testImpactPackageID; got != want {
		t.Fatalf("active package IDs = %q, want %q", got, want)
	}
	if got, want := len(loader.filters), 2; got != want {
		t.Fatalf("active evidence loads = %d, want %d", got, want)
	}
	if got, want := strings.Join(loader.filters[1].CVEIDs, ","), "CVE-2026-0001"; got != want {
		t.Fatalf("follow-up CVE IDs = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	assertSupplyChainImpactStatus(t, writer.write.Findings[0], SupplyChainImpactAffectedExact)
}

func TestSupplyChainImpactFilterUsesRiskSignalCVEIDs(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactFilter([]facts.Envelope{
		vulnerabilityEPSSFact("epss-1", "CVE-2026-0001", "0.71", "0.98"),
		vulnerabilityKEVFact("kev-1", "CVE-2026-0002"),
	})

	if got, want := strings.Join(filter.CVEIDs, ","), "CVE-2026-0001,CVE-2026-0002"; got != want {
		t.Fatalf("CVEIDs = %q, want %q", got, want)
	}
}

func TestSupplyChainImpactStableFactKeyIncludesRepository(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
	}
	left := supplyChainImpactStableFactKey(write, SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0001",
		PackageID:     testImpactPackageID,
		RepositoryID:  "repo://example/api",
		SubjectDigest: testImpactSubjectDigest,
	})
	right := supplyChainImpactStableFactKey(write, SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0001",
		PackageID:     testImpactPackageID,
		RepositoryID:  "repo://example/worker",
		SubjectDigest: testImpactSubjectDigest,
	})
	if left == right {
		t.Fatalf("stable fact keys collapsed distinct repositories: %q", left)
	}
	if !strings.Contains(left, ":repo://example/api:") {
		t.Fatalf("stable fact key = %q, want repository identity segment", left)
	}
}

func TestSupplyChainImpactStableFactKeyIgnoresSourceScopeGeneration(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:            "CVE-2026-0001",
		PackageID:        testImpactPackageID,
		ObservedVersion:  "1.2.3",
		RequestedRange:   "^1.2.0",
		Status:           SupplyChainImpactAffectedExact,
		RepositoryID:     testImpactRepositoryID,
		SubjectDigest:    testImpactSubjectDigest,
		FixedVersion:     "1.3.0",
		MatchReason:      "npm_semver_affected_range",
		Remediation:      SupplyChainImpactRemediation{FirstPatchedVersion: "1.3.0"},
		EvidenceFactIDs:  []string{"cve-1", "affected-1", "consume-1"},
		EvidencePath:     []string{facts.VulnerabilityCVEFactKind, facts.VulnerabilityAffectedPackageFactKind},
		DetectionProfile: DetectionProfilePrecise,
	}
	writes := []SupplyChainImpactWrite{
		{ScopeID: "vuln-intel://osv/npm/example", GenerationID: "generation-vulnerability-1"},
		{ScopeID: "vuln-intel://osv/npm/example", GenerationID: "generation-vulnerability-2"},
		{ScopeID: "package-registry:npm:example", GenerationID: "generation-package-1"},
	}
	wantKey := supplyChainImpactStableFactKey(writes[0], finding)
	wantFindingID := supplyChainImpactFindingID(finding)
	for _, write := range writes {
		gotKey := supplyChainImpactStableFactKey(write, finding)
		if gotKey != wantKey {
			t.Fatalf("stable fact key differs by source scope/generation: got=%q want=%q for %#v", gotKey, wantKey, write)
		}
		if gotID := supplyChainImpactFindingID(finding); gotID != wantFindingID {
			t.Fatalf("finding id differs by source scope/generation: got=%q want=%q", gotID, wantFindingID)
		}
		if strings.Contains(gotKey, "generation-") || strings.Contains(gotKey, "vuln-intel://") || strings.Contains(gotKey, "package-registry:") {
			t.Fatalf("stable fact key = %q, want canonical logical identity without source scope/generation", gotKey)
		}
	}
}

func TestPostgresSupplyChainImpactWriterPersistsSignalsWithoutPriorityCollapse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 16, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSupplyChainImpactWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), SupplyChainImpactWrite{
		IntentID:     "intent-impact",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Cause:        "vulnerability evidence observed",
		Findings: []SupplyChainImpactFinding{
			{
				CVEID:               "CVE-2026-0001",
				PackageID:           testImpactPackageID,
				PURL:                testImpactPURL,
				ObservedVersion:     "1.2.3",
				RequestedRange:      "^1.2.0",
				FixedVersion:        "1.3.0",
				MatchReason:         "npm_semver_affected_range",
				Status:              SupplyChainImpactAffectedExact,
				Confidence:          "exact",
				CVSSScore:           9.8,
				EPSSProbability:     "0.71",
				EPSSPercentile:      "0.98",
				KnownExploited:      true,
				RuntimeReachability: "package_manifest",
				RepositoryID:        testImpactRepositoryID,
				CanonicalWrites:     1,
				EvidenceFactIDs:     []string{"cve-1", "affected-1", "version-1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 || result.FactsWritten != 1 {
		t.Fatalf("result = %#v, want one canonical fact", result)
	}
	payload := unmarshalSupplyChainImpactPayload(t, db.execs[0].args[14])
	if got, want := payload["impact_status"], string(SupplyChainImpactAffectedExact); got != want {
		t.Fatalf("impact_status = %#v, want %#v", got, want)
	}
	if got, want := payload["requested_range"], "^1.2.0"; got != want {
		t.Fatalf("requested_range = %#v, want %#v", got, want)
	}
	if got, want := payload["match_reason"], "npm_semver_affected_range"; got != want {
		t.Fatalf("match_reason = %#v, want %#v", got, want)
	}
	if _, exists := payload["priority"]; exists {
		t.Fatalf("payload must keep risk signals separate instead of emitting opaque priority: %#v", payload)
	}
	if got, want := payload["known_exploited"], true; got != want {
		t.Fatalf("known_exploited = %#v, want %#v", got, want)
	}
}

func supplyChainImpactFindingsByCVE(findings []SupplyChainImpactFinding) map[string]SupplyChainImpactFinding {
	out := make(map[string]SupplyChainImpactFinding, len(findings))
	for _, finding := range findings {
		out[finding.CVEID] = finding
	}
	return out
}

func assertSupplyChainImpactStatus(
	t *testing.T,
	finding SupplyChainImpactFinding,
	status SupplyChainImpactStatus,
) {
	t.Helper()
	if finding.Status != status {
		t.Fatalf("Status = %q, want %q for %#v", finding.Status, status, finding)
	}
}

func unmarshalSupplyChainImpactPayload(t *testing.T, raw any) map[string]any {
	t.Helper()

	var payload map[string]any
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal payload: %v", err)
	}
	return payload
}

func vulnerabilityCVEFact(factID string, cveID string, cvssScore float64) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":     cveID,
			"cvss_score": cvssScore,
			"aliases":    []any{cveID},
		},
	}
}

func vulnerabilityAffectedPackageFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	affectedVersion string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":            cveID,
			"package_id":        packageID,
			"ecosystem":         ecosystem,
			"package_name":      name,
			"affected_versions": []any{affectedVersion},
			"fixed_versions":    []any{fixedVersion},
		},
	}
}

func vulnerabilityEPSSFact(factID string, cveID string, probability string, percentile string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityEPSSScoreFactKind,
		Payload: map[string]any{
			"cve_id":      cveID,
			"probability": probability,
			"percentile":  percentile,
			"score_date":  "2026-05-16",
		},
	}
}

func vulnerabilityKEVFact(factID string, cveID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityKnownExploitedFactKind,
		Payload: map[string]any{
			"cve_id":     cveID,
			"date_added": "2026-05-16",
		},
	}
}

func packageVersionFact(factID string, packageID string, purl string, version string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.PackageRegistryPackageVersionFactKind,
		Payload: map[string]any{
			"package_id": packageID,
			"purl":       purl,
			"version":    version,
		},
	}
}

func packageRegistryPackageImpactFact(factID string, packageID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.PackageRegistryPackageFactKind,
		Payload: map[string]any{
			"package_id":      packageID,
			"ecosystem":       "npm",
			"raw_name":        "example",
			"normalized_name": "example",
		},
	}
}

func sbomComponentImpactFact(factID string, documentID string, purl string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMComponentFactKind,
		Payload: map[string]any{
			"document_id": documentID,
			"purl":        purl,
			"name":        "example",
			"version":     "1.2.3",
		},
	}
}

func sbomAttachmentImpactFact(factID string, documentID string, subjectDigest string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: sbomAttestationAttachmentFactKind,
		Payload: map[string]any{
			"document_id":       documentID,
			"subject_digest":    subjectDigest,
			"attachment_status": "attached_verified",
			"canonical_writes":  1,
		},
	}
}

func containerImageIdentityImpactFact(factID string, digest string, repositoryID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: containerImageIdentityFactKind,
		Payload: map[string]any{
			"digest":           digest,
			"repository_id":    repositoryID,
			"canonical_writes": 1,
		},
	}
}
