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

type manifestBackedSupplyChainImpactLoader struct {
	scopeFacts           []facts.Envelope
	activeFacts          []facts.Envelope
	repositoryFacts      []facts.Envelope
	manifestFacts        []facts.Envelope
	jvmReachabilityFacts []facts.Envelope
	repositoryCalls      int
	manifestCalls        int
	jvmReachabilityCalls int
	jvmFilters           []JVMReachabilityFactFilter
	manifestEcosystem    []string
	manifestNames        []string
	kindCalls            [][]string
	filters              []SupplyChainImpactFactFilter
}

func (s *manifestBackedSupplyChainImpactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	s.filters = append(s.filters, filter)
	return append([]facts.Envelope(nil), s.activeFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListActiveRepositoryFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.repositoryCalls++
	return append([]facts.Envelope(nil), s.repositoryFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListActivePackageManifestDependencyFacts(
	_ context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	s.manifestCalls++
	s.manifestEcosystem = append([]string(nil), ecosystems...)
	s.manifestNames = append([]string(nil), packageNames...)
	return append([]facts.Envelope(nil), s.manifestFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListActiveJVMReachabilityFacts(
	_ context.Context,
	filter JVMReachabilityFactFilter,
) ([]facts.Envelope, error) {
	s.jvmReachabilityCalls++
	s.jvmFilters = append(s.jvmFilters, filter)
	return append([]facts.Envelope(nil), s.jvmReachabilityFacts...), nil
}

func TestSupplyChainImpactHandlerUsesManifestDependencyBeforeRegistryCorrelation(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC)
	loader := &manifestBackedSupplyChainImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-undici", "CVE-2026-0001", 7.5),
			vulnerabilityAffectedPackageFact(
				"affected-undici",
				"CVE-2026-0001",
				"npm://registry.npmjs.org/undici",
				"npm",
				"undici",
				"6.23.0",
				"6.23.1",
			),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				testImpactRepositoryID,
				"api",
				"package-lock.json",
				"undici",
				"npm",
				"6.23.0",
				observedAt,
				map[string]any{
					"section":           "package-lock",
					"lockfile":          true,
					"dependency_path":   []any{"fetch-client", "undici"},
					"dependency_depth":  2,
					"direct_dependency": false,
				},
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact-undici",
		ScopeID:      "vuln-intel://osv/npm/undici@6.23.0",
		GenerationID: "generation-impact-undici",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.manifestCalls != 1 {
		t.Fatalf("ListActivePackageManifestDependencyFacts() calls = %d, want 1", loader.manifestCalls)
	}
	if got, want := strings.Join(loader.manifestEcosystem, ","), "npm"; got != want {
		t.Fatalf("manifest ecosystems = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.manifestNames, ","), "undici"; got != want {
		t.Fatalf("manifest names = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	finding := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
	if finding.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", finding.RepositoryID, testImpactRepositoryID)
	}
	if finding.ObservedVersion != "6.23.0" {
		t.Fatalf("ObservedVersion = %q, want lockfile version 6.23.0", finding.ObservedVersion)
	}
	if !strings.Contains(strings.Join(finding.EvidencePath, " -> "), factKindContentEntity) {
		t.Fatalf("EvidencePath = %#v, want content_entity source dependency evidence", finding.EvidencePath)
	}
}

func TestSupplyChainImpactHandlerUsesRepositoryScopedSecurityAlertLockfileEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC)
	providerRepoID := "security-alert:github:acme/api"
	canonicalRepoID := "repository:r_api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	loader := &manifestBackedSupplyChainImpactLoader{
		scopeFacts: []facts.Envelope{
			securityAlertEnvelope("alert-provider-lockfile", providerRepoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(172),
				"provider_state":        "open",
				"package_id":            packageID,
				"ecosystem":             "npm",
				"package_name":          "fast-uri",
				"manifest_path":         "package-lock.json",
				"dependency_scope":      "runtime",
				"relationship":          "transitive",
				"ghsa_ids":              []string{"GHSA-provider-9670"},
				"cve_ids":               []string{"CVE-2026-9670"},
				"vulnerable_range":      "< 3.1.0",
				"patched_version":       "3.1.0",
				"severity":              "high",
				"updated_at":            "2026-05-26T12:00:00Z",
			}),
		},
		activeFacts: []facts.Envelope{
			workloadIdentityImpactFact("workload-provider-lockfile", canonicalRepoID, "workload:api"),
			serviceCatalogCorrelationImpactFact(
				"catalog-provider-lockfile",
				canonicalRepoID,
				"service:api",
				"workload:api",
				string(ServiceCatalogCorrelationExact),
				"matches",
				false,
			),
		},
		repositoryFacts: []facts.Envelope{
			packageSourceRepositoryFact(
				canonicalRepoID,
				"api",
				"https://github.com/acme/api",
				false,
				observedAt,
			),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				canonicalRepoID,
				"",
				"package-lock.json",
				"fast-uri",
				"npm",
				"3.0.3",
				observedAt.Add(time.Minute),
				map[string]any{
					"section":           "package-lock",
					"lockfile":          true,
					"dependency_scope":  "runtime",
					"dependency_path":   []any{"ajv", "fast-uri"},
					"dependency_depth":  2,
					"direct_dependency": false,
				},
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-provider-lockfile",
		ScopeID:      providerRepoID,
		GenerationID: "security-alert-generation-967",
		SourceSystem: "security_alert",
		Domain:       DomainSupplyChainImpact,
		Cause:        "provider security alert evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.repositoryCalls != 1 {
		t.Fatalf("ListActiveRepositoryFacts() calls = %d, want 1", loader.repositoryCalls)
	}
	if loader.manifestCalls != 1 {
		t.Fatalf("ListActivePackageManifestDependencyFacts() calls = %d, want 1", loader.manifestCalls)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d: %#v", got, want, writer.write.Findings)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	finding := writer.write.Findings[0]
	if finding.RepositoryID != canonicalRepoID {
		t.Fatalf("RepositoryID = %q, want canonical repository id %q", finding.RepositoryID, canonicalRepoID)
	}
	assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
	if finding.ObservedVersion != "3.0.3" {
		t.Fatalf("ObservedVersion = %q, want lockfile version 3.0.3", finding.ObservedVersion)
	}
	assertContainsString(t, finding.WorkloadIDs, "workload:api")
	assertContainsString(t, finding.ServiceIDs, "service:api")
	assertContainsString(t, finding.EvidenceFactIDs, "workload-provider-lockfile")
	assertContainsString(t, finding.EvidenceFactIDs, "catalog-provider-lockfile")
	assertNotContainsString(t, finding.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, finding.MissingEvidence, "service evidence missing")
	if !strings.Contains(strings.Join(finding.EvidencePath, " -> "), factKindContentEntity) {
		t.Fatalf("EvidencePath = %#v, want manifest dependency evidence", finding.EvidencePath)
	}
}

func TestBuildSupplyChainImpactFindingsUsesProviderAlertManifestDependencyEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	providerRepoID := "security-alert:github:acme/api"
	canonicalRepoID := "repository:r_api"
	packageID := "npm://registry.npmjs.org/fast-uri"
	alert := securityAlertEnvelope("alert-provider-manifest", providerRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(173),
		"provider_state":        "open",
		"package_id":            packageID,
		"ecosystem":             "npm",
		"package_name":          "fast-uri",
		"manifest_path":         "package-lock.json",
		"ghsa_ids":              []string{"GHSA-provider-9671"},
		"cve_ids":               []string{"CVE-2026-9671"},
		"vulnerable_range":      "< 3.1.0",
		"patched_version":       "3.1.0",
	})
	repository := packageSourceRepositoryFact(
		canonicalRepoID,
		"api",
		"https://github.com/acme/api",
		false,
		observedAt,
	)
	dependency := packageManifestDependencyFactWithMetadata(
		canonicalRepoID,
		"",
		"package-lock.json",
		"fast-uri",
		"npm",
		"3.0.3",
		observedAt.Add(time.Minute),
		map[string]any{
			"section":          "package-lock",
			"lockfile":         true,
			"dependency_scope": "runtime",
		},
	)

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, repository, dependency})

	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d: %#v", got, want, findings)
	}
	if findings[0].RepositoryID != canonicalRepoID {
		t.Fatalf("RepositoryID = %q, want canonical repository id %q", findings[0].RepositoryID, canonicalRepoID)
	}
	assertSupplyChainImpactStatus(t, findings[0], SupplyChainImpactAffectedExact)
}

func BenchmarkAddManifestDependencySupplyChainConsumption(b *testing.B) {
	observedAt := time.Date(2026, 5, 26, 8, 30, 0, 0, time.UTC)
	affectedPackages := make(map[string][]supplyChainAffectedPackage, 1)
	envelopes := make([]facts.Envelope, 0, 200)
	for i := 0; i < 200; i++ {
		packageName := "package-" + strings.Repeat("x", i%8) + string(rune('a'+i%26))
		packageID := "npm://registry.npmjs.org/" + packageName
		affectedPackages["CVE-2026-0001"] = append(affectedPackages["CVE-2026-0001"], supplyChainAffectedPackage{
			factID:    "affected-" + packageName,
			cveID:     "CVE-2026-0001",
			packageID: packageID,
			ecosystem: "npm",
			name:      packageName,
		})
		envelopes = append(envelopes, packageManifestDependencyFactWithMetadata(
			testImpactRepositoryID,
			"api",
			"package-lock.json",
			packageName,
			"npm",
			"1.0.0",
			observedAt,
			map[string]any{"lockfile": true},
		))
	}

	b.ReportAllocs()
	for b.Loop() {
		index := &supplyChainImpactIndex{
			affectedPackages: affectedPackages,
			consumption:      map[string][]supplyChainPackageConsumption{},
		}
		addManifestDependencySupplyChainConsumption(index, envelopes)
	}
}
