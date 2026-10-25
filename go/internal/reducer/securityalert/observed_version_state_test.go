// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSecurityAlertObservedVersionHonorsAnUnresolvedState (#8988): a manifest
// consumption whose parser marked the row unresolved never falls back to its
// text as the installed version. The exact-version backstop accepts some text a
// Gradle or sbt parser left unresolved (a "1.0!", an sbt identifier). A colon
// text such as a map-form "1.0:jdk8" is refused by the backstop whatever the
// state, so it cannot show the gate and is not a case here. A resolved or partial row, and a row with no state, keep the
// fallback.
func TestSecurityAlertObservedVersionHonorsAnUnresolvedState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		state     string
		ecosystem string
		rng       string
		lockfile  bool
		want      string
	}{
		{"single bang unresolved", "unresolved", "maven", "1.0!", false, ""},
		{"identifier unresolved", "unresolved", "maven", "someIdent", false, ""},
		{"exact resolved", "resolved", "maven", "1.0", false, "1.0"},
		{"exact partial", "partial", "maven", "1.0", false, "1.0"},
		{"no state", "", "maven", "1.0", false, "1.0"},
		{"npm lockfile no state", "", "npm", "1.0.0+build.1", true, "1.0.0+build.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := securityAlertObservedVersion(
				ProviderSecurityAlert{SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{Ecosystem: tc.ecosystem}},
				SecurityAlertConsumption{
					DependencyRange:           tc.rng,
					DependencyResolutionState: tc.state,
					Lockfile:                  tc.lockfile,
				},
			)
			if got != tc.want {
				t.Fatalf("securityAlertObservedVersion() = %q, want %q (state %q, range %q)", got, tc.want, tc.state, tc.rng)
			}
		})
	}
}

// TestExtractSecurityAlertConsumptionsCarriesThePersistedResolutionState
// (#8988): a persisted reducer_package_consumption_correlation fact carries the
// parser's resolution state, and the observed version read through
// ExtractSecurityAlertConsumptions honors it. A fact written before the field
// existed has no state and keeps the text fallback.
func TestExtractSecurityAlertConsumptionsCarriesThePersistedResolutionState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		rng   string
		state string
		want  string
	}{
		{"single bang unresolved", "1.0!", "unresolved", ""},
		{"sbt identifier unresolved", "someIdent", "unresolved", ""},
		{"exact partial", "2.3.4-RC1", "partial", "2.3.4-RC1"},
		{"exact resolved", "2.3.4-RC1", "resolved", "2.3.4-RC1"},
		{"fact written before the field", "2.3.4-RC1", "", "2.3.4-RC1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelope := packageConsumptionCorrelationEnvelope("consume-1", "repo-1", "pkg:maven/org.example/lib", "build.gradle")
			envelope.Payload["dependency_range"] = tc.rng
			if tc.state != "" {
				envelope.Payload["dependency_resolution_state"] = tc.state
			}
			consumptions := ExtractSecurityAlertConsumptions([]facts.Envelope{envelope})
			if len(consumptions) != 1 {
				t.Fatalf("consumptions = %d, want 1", len(consumptions))
			}
			if got := consumptions[0].DependencyResolutionState; got != tc.state {
				t.Fatalf("DependencyResolutionState = %q, want %q", got, tc.state)
			}
			got := securityAlertObservedVersion(
				ProviderSecurityAlert{SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{Ecosystem: "maven"}},
				consumptions[0],
			)
			if got != tc.want {
				t.Fatalf("securityAlertObservedVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
