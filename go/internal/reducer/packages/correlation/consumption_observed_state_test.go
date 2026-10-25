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

// TestExtractPackageManifestDependenciesHonorsAnUnresolvedState (#8988): a row
// a parser marked dependency_resolution_state=unresolved never yields an
// observed version, whatever its text looks like to the exact-version check.
// The Gradle map form keeps a colon-bearing value ("1.0:jdk8"), and the Gradle
// reader rejects a single '!' and an interior '+' that the reducer's
// cross-ecosystem check accepts; an sbt row whose version is an identifier the
// parser could not resolve keeps that identifier as its text. A resolved or
// partial row, and a row from an ecosystem that emits no state, keep the
// observed version the exact-version check yields.
func TestExtractPackageManifestDependenciesHonorsAnUnresolvedState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		packageManager string
		value          string
		state          string
		lockfile       bool
		want           string
	}{
		{"gradle map form colon unresolved", "gradle", "1.0:jdk8", "unresolved", false, ""},
		{"gradle single bang unresolved", "gradle", "1.0!", "unresolved", false, ""},
		{"gradle interior plus unresolved", "gradle", "1.0+build.1", "unresolved", false, ""},
		{"gradle exact resolved", "gradle", "1.0", "resolved", false, "1.0"},
		{"gradle exact partial", "gradle", "1.0", "partial", false, "1.0"},
		{"sbt identifier unresolved", "sbt", "someIdent", "unresolved", false, ""},
		{"sbt exact partial scope placeholder", "sbt", "2.3.4-RC1", "partial", false, "2.3.4-RC1"},
		{"sbt exact resolved", "sbt", "2.3.4-RC1", "resolved", false, "2.3.4-RC1"},
		{"maven exact resolved", "maven", "5.0.0-M1", "resolved", false, "5.0.0-M1"},
		{"maven property unresolved", "maven", "${v}", "unresolved", false, ""},
		{"npm without a state", "npm", "1.0.0+build.1", "", false, "1.0.0+build.1"},
		{"cargo lockfile without a state", "cargo", "1.0.0+build.1", "", true, "1.0.0+build.1"},
		{"nuget lockfile without a state", "nuget", "13.0.3", "", true, "13.0.3"},
		// The exact-version backstop refuses ':' for every ecosystem (a ':' means
		// notation text leaked into the version), so a deb epoch has no observed
		// version whether or not the parser emits a state: the answer is
		// unchanged by the state gate.
		{"deb epoch without a state", "deb", "1:2.3-1", "", false, ""},
	}
	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			metadata := map[string]any{"section": "dependencies", "lockfile": tc.lockfile}
			if tc.state != "" {
				metadata["dependency_resolution_state"] = tc.state
			}
			dependencies := ExtractPackageManifestDependencies([]facts.Envelope{
				packageManifestDependencyFactWithMetadata(
					"repo-jvm", "jvm-app", "build.gradle", "g:a", tc.packageManager, tc.value, observedAt, metadata),
			})
			if len(dependencies) != 1 {
				t.Fatalf("dependencies = %d, want 1", len(dependencies))
			}
			if got := dependencies[0].ObservedVersion; got != tc.want {
				t.Fatalf("ObservedVersion = %q, want %q (state %q, value %q)", got, tc.want, tc.state, tc.value)
			}
			if got := dependencies[0].DependencyRange; got != tc.value {
				t.Fatalf("DependencyRange = %q, want the text as written %q", got, tc.value)
			}
		})
	}
}

// TestSecurityAlertManifestConsumptionCarriesTheResolutionState (#8988): the
// manifest bridge hands the parser's resolution state to the security-alert
// reconciliation, so an unresolved row cannot become the installed version
// through the text fallback there either.
func TestSecurityAlertManifestConsumptionCarriesTheResolutionState(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	alert := securityalert.ProviderSecurityAlert{
		SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
			RepositoryID: "repo-jvm",
			PackageID:    "pkg:maven/g/a",
			Ecosystem:    "maven",
			PackageName:  "g:a",
		},
	}
	envelopes := []facts.Envelope{
		packageManifestDependencyFactWithMetadata("repo-jvm", "jvm-app", "build.gradle", "g:a", "gradle", "1.0:jdk8", observedAt,
			map[string]any{"section": "dependencies", "dependency_resolution_state": "unresolved"}),
	}
	consumptions := ExtractSecurityAlertManifestConsumptions([]securityalert.ProviderSecurityAlert{alert}, envelopes)
	if len(consumptions) != 1 {
		t.Fatalf("consumptions = %d, want 1: %#v", len(consumptions), consumptions)
	}
	if got := consumptions[0].DependencyResolutionState; got != "unresolved" {
		t.Fatalf("DependencyResolutionState = %q, want unresolved", got)
	}
	if got := consumptions[0].ObservedVersion; got != "" {
		t.Fatalf("ObservedVersion = %q, want none for an unresolved row", got)
	}
}

// TestTypedPackageConsumptionPayloadCarriesTheResolutionState (#8988): the
// consumption writer persists dependency_resolution_state when the decision has
// one and omits the key otherwise, so a fact for an ecosystem that emits no
// state, and a fact written before the field existed, look the same.
func TestTypedPackageConsumptionPayloadCarriesTheResolutionState(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"unresolved", "partial", "resolved", "", "  "} {
		payload, err := typedPackageConsumptionPayload(PackageWrite{}, PackageConsumptionDecision{
			PackageID:                 "pkg:maven/g/a",
			DependencyRange:           "1.0:jdk8",
			DependencyResolutionState: state,
		})
		if err != nil {
			t.Fatalf("typedPackageConsumptionPayload() error = %v", err)
		}
		got, present := payload["dependency_resolution_state"]
		switch want := strings.TrimSpace(state); {
		case want == "" && present:
			t.Fatalf("state %q: payload carries dependency_resolution_state %v, want the key omitted", state, got)
		case want != "" && got != want:
			t.Fatalf("state %q: dependency_resolution_state = %v, want %q", state, got, want)
		}
	}
}
