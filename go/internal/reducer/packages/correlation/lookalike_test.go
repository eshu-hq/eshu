// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// A provider alert for "Serilog" must not match a manifest dependency spelled
// with a non-ASCII lookalike letter, while an ASCII case variant still matches
// (#9584). Go's strings.ToLower lowers U+0130 to "i" and U+212A to "k".
func TestSecurityAlertPackageNameMatchesRejectsNonASCIILookalikes(t *testing.T) {
	t.Parallel()

	alert := securityalert.ProviderSecurityAlert{SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
		Ecosystem:   "nuget",
		PackageName: "Serilog.Sinks.Kafka",
		PackageID:   "nuget://api.nuget.org/v3/index.json/serilog.sinks.kafka",
	}}
	tests := []struct {
		dependency string
		want       bool
	}{
		{"Serilog.Sinks.Kafka", true},
		{"SERILOG.SINKS.KAFKA", true},
		{"Ser\u0130log.Sinks.Kafka", false},
		{"Serilog.Sinks.\u212Aafka", false},
	}
	for _, test := range tests {
		if got := SecurityAlertPackageNameMatches(alert, test.dependency); got != test.want {
			t.Fatalf("SecurityAlertPackageNameMatches(%q) = %v, want %v", test.dependency, got, test.want)
		}
	}
}

// A Gemfile row spelled with a lookalike letter must not lend its requested
// range to the lockfile row of the real gem (#9584).
func TestRubyGemsLockfileRangeJoinRejectsNonASCIILookalike(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:gem/rails", "rubygems", "rails", "", observedAt),
		packageSourceRepositoryFact("repo-ruby-api", "ruby-api", "https://example.com/ruby-api.git", false, observedAt),
		packageManifestDependencyFactWithMetadata("repo-ruby-api", "ruby-api", "Gemfile", "Ra\u0130ls", "rubygems", "~> 1.0", observedAt,
			map[string]any{"section": "default", "dependency_scope": "runtime"}),
		packageManifestDependencyFactWithMetadata("repo-ruby-api", "ruby-api", "Gemfile.lock", "rails", "rubygems", "7.1.3", observedAt,
			map[string]any{"section": "gemfile.lock", "lockfile": true, "dependency_path": []any{"rails"}, "dependency_depth": 1, "direct_dependency": true}),
	})

	lockDecision, ok := packageConsumptionDecisionByPath(decisions, "Gemfile.lock")
	if !ok {
		t.Fatalf("Gemfile.lock decision missing: %#v", decisions)
	}
	if lockDecision.DependencyRange == "~> 1.0" {
		t.Fatalf("lockfile rails took the range of the lookalike Gemfile row %q", "Ra\u0130ls")
	}
	if _, ok := packageConsumptionDecisionByPath(decisions, "Gemfile"); ok {
		t.Fatalf("lookalike Gemfile row %q must not correlate to the rails registry package", "Ra\u0130ls")
	}
}

// The reverse direction of the alert match: an alert that names the lookalike
// must not match a manifest dependency on the real package (#9584).
func TestSecurityAlertForLookalikeDoesNotMatchRealPackage(t *testing.T) {
	t.Parallel()

	for _, lookalike := range []string{"Ser\u0130log", "\u212Aafka"} {
		alert := securityalert.ProviderSecurityAlert{SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
			Ecosystem:   "nuget",
			PackageName: lookalike,
			PackageID:   "nuget://api.nuget.org/v3/index.json/" + lookalike,
		}}
		real := strings.NewReplacer("\u0130", "I", "\u212A", "K").Replace(lookalike)
		if SecurityAlertPackageNameMatches(alert, real) {
			t.Fatalf("alert for %q matched manifest dependency %q", lookalike, real)
		}
		if !SecurityAlertPackageNameMatches(alert, lookalike) {
			t.Fatalf("alert for %q did not match the identically spelled dependency", lookalike)
		}
	}
}

// Registry identities keep a non-ASCII ecosystem spelling apart from the
// canonical ecosystem it imitates (#9584).
func TestRegistryIdentityEcosystemKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	identities := ExtractPackageRegistryIdentities([]facts.Envelope{
		packageRegistryPackageFact("pkg:pypi/requests", "PYP\u0130", "requests", "", observedAt),
		packageRegistryPackageFact("pkg:pypi/flask", "PYPI", "flask", "", observedAt),
	})
	got := map[string]string{}
	for _, identity := range identities {
		got[identity.PackageID] = identity.Ecosystem
	}
	if got["pkg:pypi/requests"] != "pyp\u0130" || got["pkg:pypi/flask"] != "pypi" {
		t.Fatalf("registry identity ecosystems = %#v, want pyp\u0130 kept and PYPI folded to pypi", got)
	}
}

// Only an ASCII spelling of sbt selects the sbt cross-version candidates; a
// long-s spelling keeps the plain dependency name (#9584).
func TestSBTNameCandidatesKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	if got := packageManifestDependencyNameCandidates(PackageManifestDependency{PackageManager: " SBT ", DependencyName: "cats-core"}); len(got) != 0 {
		t.Fatalf("sbt candidates with no cross version = %#v, want none", got)
	}
	if got := packageManifestDependencyNameCandidates(PackageManifestDependency{PackageManager: "\u017Fbt", DependencyName: "cats-core"}); len(got) == 0 {
		t.Fatalf("long-s sbt took the sbt path and returned no candidates")
	}
}
