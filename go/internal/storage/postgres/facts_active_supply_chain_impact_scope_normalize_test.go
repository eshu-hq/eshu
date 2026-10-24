// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

func TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeSiblings(t *testing.T) {
	t.Parallel()

	// #5466 round-3 review F-6: scopeAnchorMatches
	// (go/internal/reducer/supplychain/core/scope.go) compares every
	// vulnerability.suppression scope anchor with strings.TrimSpace +
	// strings.EqualFold, so the suppression statement compares
	// lower(btrim(...)) of each scope anchor with the lowered filter lists,
	// while the exact top-level siblings serving other kinds stay exact.
	suppression := supplyChainImpactBranch(t, "vulnerability_suppression")
	for key, param := range map[string]factimpact.Param{
		"package_id":     factimpact.LowerPackageIDs,
		"purl":           factimpact.LowerPURLs,
		"cve_id":         factimpact.LowerCVEIDs,
		"subject_digest": factimpact.LowerSubjectDigests,
		"repository_id":  factimpact.LowerRepositoryIDs,
		"advisory_id":    factimpact.LowerAdvisoryIDs,
	} {
		placeholder := supplyChainImpactPlaceholder(t, suppression, param)
		want := "lower(btrim(fact.payload->'scope'->>'" + key + "', " + suppressionScopeTrimCharactersSQL + ")) = ANY(" + placeholder + ")"
		if !strings.Contains(suppression.SQL, want) {
			t.Fatalf("suppression statement missing %q:\n%s", want, suppression.SQL)
		}
	}
	for key, param := range map[string]factimpact.Param{
		"package_id":     factimpact.PackageIDs,
		"purl":           factimpact.PURLs,
		"cve_id":         factimpact.CVEIDs,
		"subject_digest": factimpact.SubjectDigests,
		"repository_id":  factimpact.RepositoryIDs,
	} {
		want := "fact.payload->>'" + key + "' = ANY(" + supplyChainImpactPlaceholder(t, suppression, param) + ")"
		if !strings.Contains(suppression.SQL, want) {
			t.Fatalf("suppression statement missing unchanged top-level sibling %q", want)
		}
	}
}

func TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeSiblings(t *testing.T) {
	t.Parallel()

	routed := &supplyChainImpactRoutingDB{}
	if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(), supplychaincore.SupplyChainImpactFactFilter{
		PackageIDs:     []string{"PKG:NPM/Left-Pad"},
		PURLs:          []string{" pkg:npm/left-pad@1.3.0 "},
		CVEIDs:         []string{"CVE-2026-1234"},
		SubjectDigests: []string{"SHA256:ABCDEF"},
		RepositoryIDs:  []string{"Repo://Acme/Api"},
		AdvisoryIDs:    []string{" GHSA-Demo-1111-2222 "},
	}); err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	calls := routed.callsFor("vulnerability_suppression")
	if len(calls) != 1 {
		t.Fatalf("suppression statement calls = %d, want 1 (calls %v)", len(calls), routed.labels())
	}
	for param, want := range map[factimpact.Param][]string{
		factimpact.LowerPackageIDs:     {"pkg:npm/left-pad"},
		factimpact.LowerPURLs:          {"pkg:npm/left-pad@1.3.0"},
		factimpact.LowerCVEIDs:         {"cve-2026-1234"},
		factimpact.LowerSubjectDigests: {"sha256:abcdef"},
		factimpact.LowerRepositoryIDs:  {"repo://acme/api"},
		factimpact.LowerAdvisoryIDs:    {"ghsa-demo-1111-2222"},
		factimpact.PackageIDs:          {"PKG:NPM/Left-Pad"},
	} {
		if got := boundList(t, calls[0], param); !slices.Equal(got, want) {
			t.Fatalf("param %d bound %v, want %v", param, got, want)
		}
	}
}
