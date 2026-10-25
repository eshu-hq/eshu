// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// persistedConsumptionStateCases are persisted consumption facts as the writer
// stores them: the Gradle rule rejects a single '!', and an sbt version identifier keeps its name, all with
// state unresolved. A partial sbt row with an exact literal and a row from an
// ecosystem that emits no state keep their version.
var persistedConsumptionStateCases = []struct {
	name  string
	rng   string
	state string
	want  string
}{
	{"single bang unresolved", "1.0!", "unresolved", ""},
	{"sbt identifier unresolved", "someIdent", "unresolved", ""},
	{"exact partial", "2.3.4-RC1", "partial", "2.3.4-RC1"},
	{"exact resolved", "2.3.4-RC1", "resolved", "2.3.4-RC1"},
	{"exact without a state", "2.3.4-RC1", "", "2.3.4-RC1"},
}

// TestSupplyChainImpactIgnoresAnUnresolvedPersistedConsumption (#8988): the
// impact finding built from a persisted consumption fact never takes the text
// of a row its parser left unresolved as the observed version, on the
// vulnerability path (supplyChainConsumptionFromEnvelope) and on the
// provider-alert path (securityalert.SecurityAlertConsumption).
func TestSupplyChainImpactIgnoresAnUnresolvedPersistedConsumption(t *testing.T) {
	t.Parallel()

	for _, tc := range persistedConsumptionStateCases {
		t.Run("vulnerability path/"+tc.name, func(t *testing.T) {
			t.Parallel()
			consumption := packageConsumptionFactWithRange("consume-1", "pkg:maven/org.example/lib", testImpactRepositoryID, tc.rng)
			setPersistedResolutionState(consumption, tc.state)
			findings := BuildSupplyChainImpactFindings([]facts.Envelope{
				vulnerabilityCVEFact("cve-lib", "CVE-2026-88880", 7.5),
				vulnerabilityAffectedPackageRangeFact("affected-lib", "CVE-2026-88880", "pkg:maven/org.example/lib", "maven", "org.example:lib", "9.9.9"),
				consumption,
			})
			got, ok := supplyChainImpactFindingsByCVE(findings)["CVE-2026-88880"]
			if !ok {
				t.Fatalf("no finding for CVE-2026-88880 in %#v", findings)
			}
			if got.ObservedVersion != tc.want {
				t.Fatalf("ObservedVersion = %q, want %q (state %q, range %q)", got.ObservedVersion, tc.want, tc.state, tc.rng)
			}
		})
		t.Run("provider alert path/"+tc.name, func(t *testing.T) {
			t.Parallel()
			repoID := "repo://github/example/app"
			packageID := "maven://repo.maven.apache.org/maven2/org.example:lib"
			alert := securityAlertEnvelope("alert-1", repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(77),
				"provider_state":        "open",
				"package_id":            packageID,
				"ecosystem":             "maven",
				"package_name":          "org.example:lib",
				"manifest_path":         "build.gradle",
				"ghsa_ids":              []string{"GHSA-provider-0077"},
				"cve_ids":               []string{"CVE-2026-88881"},
				"vulnerable_range":      "< 9.9.9",
				"patched_version":       "9.9.9",
			})
			consumption := packageConsumptionCorrelationEnvelope("consume-2", repoID, packageID, "build.gradle")
			consumption.Payload["dependency_range"] = tc.rng
			setPersistedResolutionState(consumption, tc.state)
			findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, consumption})
			if len(findings) != 1 {
				t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
			}
			if got := findings[0].ObservedVersion; got != tc.want {
				t.Fatalf("ObservedVersion = %q, want %q (state %q, range %q)", got, tc.want, tc.state, tc.rng)
			}
		})
	}
}

// setPersistedResolutionState writes the persisted dependency_resolution_state
// key the way the consumption writer does: absent when the state is blank.
func setPersistedResolutionState(envelope facts.Envelope, state string) {
	if state != "" {
		envelope.Payload["dependency_resolution_state"] = state
	}
}
