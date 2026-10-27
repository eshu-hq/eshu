// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// osvMavenAdvisory builds an affected-package fact in the shape the OSV
// collector emits for a Maven coordinate: a bare artifact package_name, a slash
// purl, and a package_id whose path starts with the registry's maven2 segment.
// Empty purl or packageID leave that field out.
func osvMavenAdvisory(artifact, purl, packageID string) facts.Envelope {
	payload := map[string]any{
		"cve_id":            "CVE-2026-9286",
		"advisory_id":       "GHSA-9286-test",
		"source":            "osv",
		"ecosystem":         "maven",
		"package_name":      artifact,
		"affected_versions": []any{"2.8.8"},
		"fixed_versions":    []any{"2.8.9"},
	}
	if purl != "" {
		payload["purl"] = purl
	}
	if packageID != "" {
		payload["package_id"] = packageID
	}
	return facts.Envelope{FactID: "affected-" + artifact, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

func runMavenImpact(t *testing.T, advisory facts.Envelope, rows ...facts.Envelope) (*mavenManifestEntityNameLoader, []SupplyChainImpactFinding) {
	t.Helper()
	loader := &mavenManifestEntityNameLoader{
		manifestBackedSupplyChainImpactLoader: manifestBackedSupplyChainImpactLoader{
			scopeFacts: []facts.Envelope{vulnerabilityCVEFact("cve-9286", "CVE-2026-9286", 7.5), advisory},
		},
		rows: rows,
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}
	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID: "intent-9286", ScopeID: "vuln-intel://osv/maven/_batch", GenerationID: "generation-9286",
		SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact,
		Cause: "vulnerability evidence observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return loader, writer.write.Findings
}

func mavenManifestRow(manager, path, name string) facts.Envelope {
	return packageManifestDependencyFactWithMetadata(
		testImpactRepositoryID, "api", path, name, manager, "2.8.8",
		time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC),
		map[string]any{"section": "dependencies", "direct_dependency": true},
	)
}

// Positive and negative cases for the OSV Maven anchor (issue #9286), all
// through Handle against a loader that matches entity_name exactly like the
// SQL predicate does.
func TestSupplyChainImpactMavenOSVAnchorMatrix(t *testing.T) {
	t.Parallel()

	const (
		purl      = "pkg:maven/com.google.code.gson/gson"
		packageID = "maven://repo.maven.apache.org/maven2/com.google.code.gson:gson"
	)
	tests := []struct {
		name     string
		advisory facts.Envelope
		row      facts.Envelope
		want     int
	}{
		{"maven pom row anchors", osvMavenAdvisory("gson", purl, packageID), mavenManifestRow("maven", "pom.xml", "com.google.code.gson:gson"), 1},
		{"gradle row with the same coordinates anchors", osvMavenAdvisory("gson", purl, packageID), mavenManifestRow("gradle", "build.gradle", "com.google.code.gson:gson"), 1},
		{"purl alone anchors", osvMavenAdvisory("gson", purl, ""), mavenManifestRow("maven", "pom.xml", "com.google.code.gson:gson"), 1},
		{"package id alone anchors", osvMavenAdvisory("gson", "", packageID), mavenManifestRow("maven", "pom.xml", "com.google.code.gson:gson"), 1},
		{
			"stored mixed case anchors when the advisory keeps the case",
			osvMavenAdvisory("Core-API", "pkg:maven/Org.Example/Core-API", ""),
			mavenManifestRow("maven", "pom.xml", "Org.Example:Core-API"), 1,
		},
		{"different group with the same artifact does not anchor", osvMavenAdvisory("gson", purl, packageID), mavenManifestRow("maven", "pom.xml", "org.other:gson"), 0},
		{"same group with a different artifact does not anchor", osvMavenAdvisory("gson", purl, packageID), mavenManifestRow("maven", "pom.xml", "com.google.code.gson:gson-extras"), 0},
		{"artifact-only advisory with no group never anchors to a coordinate", osvMavenAdvisory("gson", "", ""), mavenManifestRow("maven", "pom.xml", "com.google.code.gson:gson"), 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			loader, findings := runMavenImpact(t, test.advisory, test.row)
			if len(findings) != test.want {
				t.Fatalf("len(Findings) = %d, want %d; requested names = %q", len(findings), test.want, loader.requestedNames)
			}
			if test.want == 1 && findings[0].RepositoryID != testImpactRepositoryID {
				t.Fatalf("RepositoryID = %q, want %q", findings[0].RepositoryID, testImpactRepositoryID)
			}
		})
	}
}

// The requested-name set carries the coordinate and no registry-path-prefixed
// name that no manifest row can match (issue #9286).
func TestManifestDependencyFilterMavenOSVNamesAreCoordinateShaped(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactManifestDependencyFilter([]facts.Envelope{osvMavenAdvisory(
		"gson", "pkg:maven/com.google.code.gson/gson",
		"maven://repo.maven.apache.org/maven2/com.google.code.gson:gson",
	)})
	if !slices.Contains(filter.PackageNames, "com.google.code.gson:gson") {
		t.Fatalf("names = %q, want the group:artifact coordinate", filter.PackageNames)
	}
	for _, name := range filter.PackageNames {
		if name == "maven2/com.google.code.gson:gson" || name == "maven2/com.google.code.gson/gson" {
			t.Fatalf("names = %q, must not carry the maven2 registry segment", filter.PackageNames)
		}
	}
}
