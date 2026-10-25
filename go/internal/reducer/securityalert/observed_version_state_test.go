// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import "testing"

// TestSecurityAlertObservedVersionHonorsAnUnresolvedState (#8988): a manifest
// consumption whose parser marked the row unresolved never falls back to its
// text as the installed version. The exact-version backstop accepts some text a
// Gradle or sbt parser left unresolved (a map-form "1.0:jdk8", a "1.0!", an sbt
// identifier). A resolved or partial row, and a row with no state, keep the
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
		{"colon unresolved", "unresolved", "maven", "1.0:jdk8", false, ""},
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
