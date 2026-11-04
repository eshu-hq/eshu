// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fleetOSRow binds one vendor installed row to its own image scope and the
// sibling scanner analysis that resolves that scope to an image digest, the
// shape the fleet-wide index carries in production.
func fleetOSRow(row installedOSRow, scope, generation, digest string) []facts.Envelope {
	osPackage := row.envelope()
	osPackage.FactID = "os-" + scope
	osPackage.ScopeID = scope
	osPackage.GenerationID = generation
	analysis := scannerWorkerAnalysisFact(scope, generation, digest, "registry.example/app@"+digest)
	return []facts.Envelope{osPackage, analysis}
}

// TestFleetOSAdvisoryClassifiesOneFindingPerDigest pins #9858. Two images
// install the same package at different versions — xenial at its fix,
// trusty below every fix — so the fleet-wide pass must classify one finding
// per subject digest whatever order the installed rows arrive in. Before the
// fix firstOSPackageImpactPath kept the first matching row, so [fixed,
// vulnerable] produced one known-fixed finding and [vulnerable, fixed] one
// possibly-affected finding, leaving the other image without a verdict.
// Since the #9806 PR-2b-1 activation the trusty image reads affected_exact:
// its host line is listed, so the precise verdict is allowed.
func TestFleetOSAdvisoryClassifiesOneFindingPerDigest(t *testing.T) {
	t.Parallel()

	advisory := convertedOSVFixture(t, "ubuntu-cve-2016-5419.json", false)
	const (
		scopeFixed = "image://fleet/xenial-fixed"
		genFixed   = "fleet-gen-fixed"
		scopeVuln  = "image://fleet/trusty-vuln"
		genVuln    = "fleet-gen-vuln"
	)
	digestFixed := fmt.Sprintf("sha256:%064x", 0xf1)
	digestVuln := fmt.Sprintf("sha256:%064x", 0x9e)
	fixed := fleetOSRow(ubuntuCurl("16.04", "7.47.0-1ubuntu2.1"), scopeFixed, genFixed, digestFixed)
	vuln := fleetOSRow(ubuntuCurl("14.04", "7.35.0-1ubuntu2.7"), scopeVuln, genVuln, digestVuln)

	orders := []struct {
		name      string
		envelopes []facts.Envelope
	}{
		{"fixed row first", append(append(slices.Clone(advisory), fixed...), vuln...)},
		{"vulnerable row first", append(append(slices.Clone(advisory), vuln...), fixed...)},
	}
	summaries := make([][]string, 0, len(orders))
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			envelopes := order.envelopes
			findings := BuildSupplyChainImpactFindings(envelopes)
			if len(findings) != 2 {
				t.Fatalf("findings = %d, want 2 (one per image): %#v", len(findings), findings)
			}
			byDigest := make(map[string]SupplyChainImpactFinding, len(findings))
			for _, finding := range findings {
				byDigest[finding.SubjectDigest] = finding
			}
			if len(byDigest) != 2 {
				t.Fatalf("subject digests collide: %#v", findings)
			}
			gotFixed, ok := byDigest[digestFixed]
			if !ok {
				t.Fatalf("no finding for fixed image digest %s: %#v", digestFixed, findings)
			}
			if gotFixed.Status != SupplyChainImpactNotAffectedKnownFixed {
				t.Fatalf("fixed image status = %q, want not_affected_known_fixed", gotFixed.Status)
			}
			if gotFixed.ObservedVersion != "7.47.0-1ubuntu2.1" {
				t.Fatalf("fixed image ObservedVersion = %q, want the xenial installed version", gotFixed.ObservedVersion)
			}
			gotVuln, ok := byDigest[digestVuln]
			if !ok {
				t.Fatalf("no finding for vulnerable image digest %s: %#v", digestVuln, findings)
			}
			// Live tags (#9806 PR-2b-1 activation): the trusty host line
			// Ubuntu:14.04:LTS is listed, its range contains the observed
			// version with no fix at or below it, so only the host line
			// decides and the image reads affected_exact. Exact with a
			// host-tag match is the ruled-precise outcome, not a bug.
			if gotVuln.Status != SupplyChainImpactAffectedExact {
				t.Fatalf("vulnerable image status = %q, want affected_exact", gotVuln.Status)
			}
			if gotVuln.ObservedVersion != "7.35.0-1ubuntu2.7" {
				t.Fatalf("vulnerable image ObservedVersion = %q, want the trusty installed version", gotVuln.ObservedVersion)
			}
			// The fixed image returns through the known-fixed early return,
			// which keeps the version decision's own reachability; only the
			// vulnerable image settles through the os_package branch.
			if gotVuln.RuntimeReachability != "image_os_package" {
				t.Fatalf("vulnerable image reachability = %q, want image_os_package", gotVuln.RuntimeReachability)
			}
			// Coalesce and writer proof: the two findings must carry
			// distinct stable and writer identities, or they collapse to
			// one row downstream.
			if supplyChainImpactStableKey(findings[0]) == supplyChainImpactStableKey(findings[1]) {
				t.Fatal("stable keys collide: coalesce would merge the two image findings")
			}
			if supplyChainImpactFindingID(findings[0]) == supplyChainImpactFindingID(findings[1]) {
				t.Fatal("finding IDs collide: the writer would keep one row for two images")
			}
			summary := []string{
				string(gotFixed.Status) + "@" + gotFixed.SubjectDigest,
				string(gotVuln.Status) + "@" + gotVuln.SubjectDigest,
			}
			slices.Sort(summary)
			summaries = append(summaries, summary)
		})
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want one per row order", len(summaries))
	}
	if !slices.Equal(summaries[0], summaries[1]) {
		t.Fatalf("order-dependent findings: %q vs %q", summaries[0], summaries[1])
	}
}
