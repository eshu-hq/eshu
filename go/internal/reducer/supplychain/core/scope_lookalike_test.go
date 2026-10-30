// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"testing"
)

// TestSuppressionPackageAnchorsKeepLookalikesApart is the #9584 suppression
// regression: strings.EqualFold treats U+212A (Kelvin sign) as k and U+017F
// (long s) as s, so a package-scoped suppression written for the real package
// hid findings on its lookalike. Package anchors fold ASCII letters only; an
// ASCII case variant still matches.
func TestSuppressionPackageAnchorsKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		scope     vulnerabilitySuppressionScope
		finding   SupplyChainImpactFinding
		wantMatch bool
		wantField string
	}{
		{
			name:      "Kelvin sign package id",
			scope:     vulnerabilitySuppressionScope{PackageID: "npm://registry.npmjs.org/kafka"},
			finding:   SupplyChainImpactFinding{PackageID: "npm://registry.npmjs.org/\u212Aafka"},
			wantField: "package_id",
		},
		{
			name:      "long s purl",
			scope:     vulnerabilitySuppressionScope{PURL: "pkg:pypi/requests"},
			finding:   SupplyChainImpactFinding{PURL: "pkg:pypi/reque\u017Fts"},
			wantField: "purl",
		},
		{
			name:      "Kelvin sign purl",
			scope:     vulnerabilitySuppressionScope{PURL: "pkg:npm/kafka@1.0.0"},
			finding:   SupplyChainImpactFinding{PURL: "pkg:npm/\u212Aafka@1.0.0"},
			wantField: "purl",
		},
		{
			name:      "lookalike suppression against the real package id",
			scope:     vulnerabilitySuppressionScope{PackageID: "npm://registry.npmjs.org/\u212Aafka"},
			finding:   SupplyChainImpactFinding{PackageID: "npm://registry.npmjs.org/kafka"},
			wantField: "package_id",
		},
		{
			name:      "lookalike suppression against the real purl",
			scope:     vulnerabilitySuppressionScope{PURL: "pkg:pypi/requeSTS\u017F"},
			finding:   SupplyChainImpactFinding{PURL: "pkg:pypi/requestss"},
			wantField: "purl",
		},
		{
			name:      "ASCII case variant package id still matches",
			scope:     vulnerabilitySuppressionScope{PackageID: "nuget://api.nuget.org/v3/index.json/Serilog"},
			finding:   SupplyChainImpactFinding{PackageID: "nuget://api.nuget.org/v3/index.json/serilog"},
			wantMatch: true,
		},
		{
			name:      "ASCII case variant purl still matches",
			scope:     vulnerabilitySuppressionScope{PURL: "pkg:PyPI/Requests"},
			finding:   SupplyChainImpactFinding{PURL: "pkg:pypi/requests"},
			wantMatch: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			suppression := vulnerabilitySuppression{SuppressionID: "s-9584", Scope: test.scope}
			if got := suppressionScopeMatchesFinding(test.finding, suppression); got != test.wantMatch {
				t.Fatalf("suppressionScopeMatchesFinding() = %t, want %t", got, test.wantMatch)
			}
			if got := suppressionAdjacent(test.finding, suppression); got != test.wantMatch {
				t.Fatalf("suppressionAdjacent() = %t, want %t", got, test.wantMatch)
			}
			reason := suppressionScopeMismatchReason(test.finding, suppression)
			if test.wantField != "" && !strings.Contains(reason, test.wantField+"=") {
				t.Fatalf("suppressionScopeMismatchReason() = %q, want a %s difference", reason, test.wantField)
			}
		})
	}
}
