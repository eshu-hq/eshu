// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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

func TestBuildSupplyChainImpactFindingsConnectsProviderAlertRuntimeContextFromRepositoryScope(t *testing.T) {
	t.Parallel()

	repoID := "repository:r_api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-runtime-repository-scope", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(172),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"dependency_scope":      "runtime",
		"relationship":          "transitive",
		"ghsa_ids":              []string{"GHSA-provider-runtime-scope"},
		"cve_ids":               []string{"CVE-2026-47141"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
		"severity":              "high",
		"updated_at":            "2026-05-26T12:00:00Z",
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-runtime-repository-scope", repoID, packageID, "package-lock.json")
	consumption.Payload["dependency_range"] = "3.0.3"
	consumption.Payload["dependency_scope"] = "runtime"
	workload := workloadIdentityRepositoryScopeImpactFact("workload-runtime-repository-scope", repoID, testImpactWorkloadID)

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		alert,
		consumption,
		workload,
	})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "workload-runtime-repository-scope")
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	assertContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
}

func TestBuildSupplyChainImpactFindingsConnectsProviderAlertRuntimeContextFromRelatedRepositoryScope(t *testing.T) {
	t.Parallel()

	repoID := "repository:r_api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-runtime-related-repository-scope", repoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(173),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"dependency_scope":      "runtime",
		"relationship":          "transitive",
		"ghsa_ids":              []string{"GHSA-provider-runtime-related-scope"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
		"severity":              "high",
		"updated_at":            "2026-05-26T12:00:00Z",
	})
	consumption := packageConsumptionCorrelationEnvelope("consume-runtime-related-repository-scope", repoID, packageID, "package-lock.json")
	consumption.Payload["dependency_range"] = "3.0.3"
	consumption.Payload["dependency_scope"] = "runtime"
	workload := workloadIdentityRepositoryScopeImpactFact("workload-runtime-related-repository-scope", repoID, testImpactWorkloadID)
	workload.ScopeID = "workload-identity-scope:temporary"
	workload.Payload["scope_id"] = workload.ScopeID

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		alert,
		consumption,
		workload,
	})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	assertContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
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
