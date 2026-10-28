// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// mavenOSVAffectedPackage carries the exact identity strings the OSV collector
// emitted on QA for a public Maven advisory (see the diagnosis note): the
// package_name is the bare artifact id, the purl uses a slash between group and
// artifact, and the package_id path is prefixed with the registry's "maven2".
func mavenOSVAffectedPackage() facts.Envelope {
	return facts.Envelope{
		FactID:   "affected-gson",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":            "CVE-2022-25647",
			"advisory_id":       "GHSA-4jrv-ppp4-jm57",
			"source":            "osv",
			"ecosystem":         "maven",
			"package_id":        "maven://repo.maven.apache.org/maven2/com.google.code.gson:gson",
			"purl":              "pkg:maven/com.google.code.gson/gson",
			"package_name":      "gson",
			"affected_versions": []any{"2.8.8"},
			"fixed_versions":    []any{"2.8.9"},
		},
	}
}

// mavenManifestEntityNameLoader models the exact-match predicate of
// listActivePackageManifestDependencyFactsWithSBTQuery:
// fact.payload->>'entity_name' = ANY($2). A manifest row is returned only when
// its entity_name equals one of the requested names.
type mavenManifestEntityNameLoader struct {
	manifestBackedSupplyChainImpactLoader
	rows           []facts.Envelope
	requestedNames []string
}

func (l *mavenManifestEntityNameLoader) ListActivePackageManifestDependencyFacts(
	_ context.Context,
	_ []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	l.requestedNames = append([]string(nil), packageNames...)
	var out []facts.Envelope
	for _, row := range l.rows {
		name, _ := row.Payload["entity_name"].(string)
		if slices.Contains(packageNames, name) {
			out = append(out, row)
		}
	}
	return out, nil
}

// RED: a Maven manifest dependency stores entity_name as "group:artifact" (QA:
// 91 active maven/gradle rows for three public advisories' packages). The
// requested-name set derived from the OSV advisory must include that form.
func TestManifestDependencyFilterIncludesMavenColonCoordinateFromOSVAdvisory(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactManifestDependencyFilter([]facts.Envelope{mavenOSVAffectedPackage()})
	const want = "com.google.code.gson:gson"
	if !slices.Contains(filter.PackageNames, want) {
		t.Fatalf("manifest dependency filter names = %q, want to contain %q", filter.PackageNames, want)
	}
}

// RED: end to end through Handle. The reducer anchors a Maven OSV finding on a
// manifest dependency row; if the row is never loaded, the finding has no owned
// anchor and appendSupplyChainImpactFinding drops it (of any status).
func TestSupplyChainImpactHandlerAnchorsMavenOSVFindingOnColonCoordinateManifestRow(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	loader := &mavenManifestEntityNameLoader{
		manifestBackedSupplyChainImpactLoader: manifestBackedSupplyChainImpactLoader{
			scopeFacts: []facts.Envelope{
				vulnerabilityCVEFact("cve-gson", "CVE-2022-25647", 7.5),
				mavenOSVAffectedPackage(),
			},
		},
		rows: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				testImpactRepositoryID, "api", "pom.xml",
				"com.google.code.gson:gson", "maven", "2.8.8", observedAt,
				map[string]any{"section": "dependencies", "direct_dependency": true},
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}

	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-impact-gson",
		ScopeID:      "vuln-intel://osv/maven/_batch?query=com.google.code.gson%3Agson%402.8.8",
		GenerationID: "generation-impact-gson",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1; requested manifest names = %q", got, strings.Join(loader.requestedNames, ", "))
	}
	if got := writer.write.Findings[0].RepositoryID; got != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got, testImpactRepositoryID)
	}
}

// Control: when the manifest row IS loaded (the loader ignores the name filter),
// the same advisory and the same dependency row join and produce an anchored
// finding. This isolates the defect to the requested-name set, not to the key
// join, version matching, or the owned-anchor gate.
func TestSupplyChainImpactHandlerJoinsMavenOSVFindingWhenManifestRowIsLoaded(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	loader := &manifestBackedSupplyChainImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-gson", "CVE-2022-25647", 7.5),
			mavenOSVAffectedPackage(),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				testImpactRepositoryID, "api", "pom.xml",
				"com.google.code.gson:gson", "maven", "2.8.8", observedAt,
				map[string]any{"section": "dependencies", "direct_dependency": true},
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}
	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID: "intent-impact-gson-control", ScopeID: "vuln-intel://osv/maven/_batch?query=gson",
		GenerationID: "generation-impact-gson", SourceSystem: "vulnerability_intelligence",
		Domain: reducercontract.DomainSupplyChainImpact, Cause: "vulnerability evidence observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1", got)
	}
	t.Logf("control finding: status=%s repo=%s observed=%s", writer.write.Findings[0].Status, writer.write.Findings[0].RepositoryID, writer.write.Findings[0].ObservedVersion)
}
