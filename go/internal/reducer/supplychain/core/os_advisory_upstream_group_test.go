// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// Frozen fixture keys (testdata/osv-converted, eshu-hq/eshu#9999) for the
// distro records this package used to build through the in-tree converter.
// Each key names the record the helper it replaces constructed.
const (
	frozenDebianCVE      = "built-debian-cve"
	frozenDLA9999        = "built-dla-9999"
	frozenDLA9999Grouped = "built-dla-9999-group"
	frozenDSA9998        = "built-dsa-9998"
	frozenDSA9998Grouped = "built-dsa-9998-group"
)

// nvdCVEFact is the NVD record for a CVE: the sibling every distro record's
// upstream list points at. It carries no score and no package fact.
func nvdCVEFact(cveID string) facts.Envelope {
	return vulnerabilityCVEFactWithProvenance("nvd-"+cveID, cveID, "nvd", cveID, 0, "", "", "2026-10-01T00:00:00Z")
}

// upstreamEnvelopes loads the frozen distro-record fixtures for keys and
// appends an NVD sibling for each id in nvd, so each test states only what
// differs.
func upstreamEnvelopes(t *testing.T, nvd []string, keys ...string) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for _, key := range keys {
		out = append(out, convertedOSVFixture(t, key, false)...)
	}
	for _, cveID := range nvd {
		out = append(out, nvdCVEFact(cveID))
	}
	return out
}

// orderings returns the envelope orders every grouping test runs: as built,
// reversed, and three seeded shuffles. Grouping must not depend on which fact
// of a record or group arrives first.
func orderings(envelopes []facts.Envelope) map[string][]facts.Envelope {
	reversed := slices.Clone(envelopes)
	slices.Reverse(reversed)
	out := map[string][]facts.Envelope{
		"as built": slices.Clone(envelopes),
		"reversed": reversed,
	}
	for seed := int64(1); seed <= 3; seed++ {
		shuffled := slices.Clone(envelopes)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		out[fmt.Sprintf("shuffle %d", seed)] = shuffled
	}
	return out
}

// TestUpstreamGroupingMergesBulletinsIntoOneFindingPerCVE is the #9914
// acceptance test: a Debian security record that names CVE-X in its upstream
// list groups with every other record for CVE-X, so one installed package reads
// ONE finding under CVE-X whose advisory_id names the record that decided.
func TestUpstreamGroupingMergesBulletinsIntoOneFindingPerCVE(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"

	tests := []struct {
		name         string
		records      []string
		host         installedOSRow
		wantStatus   SupplyChainImpactStatus
		wantAdvisory string
		wantFix      string
	}{
		{
			name:         "bullseye below the DLA fix reads exact and names the DLA",
			records:      []string{frozenDebianCVE, frozenDLA9999Grouped},
			host:         debianCurl("11", "7.74.0-1.3+deb11u7"),
			wantStatus:   SupplyChainImpactAffectedExact,
			wantAdvisory: "DLA-9999-1",
			wantFix:      "7.74.0-1.3+deb11u10",
		},
		{
			name:         "bullseye at the DLA fix is known fixed and names the DLA",
			records:      []string{frozenDebianCVE, frozenDLA9999Grouped},
			host:         debianCurl("11", "7.74.0-1.3+deb11u10"),
			wantStatus:   SupplyChainImpactNotAffectedKnownFixed,
			wantAdvisory: "DLA-9999-1",
			wantFix:      "7.74.0-1.3+deb11u10",
		},
		{
			// The unlisted DLA copy must not name the finding or supply its
			// (blanked) fix for a host the DEBIAN-CVE record lists.
			name:         "bookworm host keeps the record that lists bookworm",
			records:      []string{frozenDebianCVE, frozenDLA9999Grouped},
			host:         debianCurl("12", "7.88.1-10+deb12u2"),
			wantStatus:   SupplyChainImpactAffectedExact,
			wantAdvisory: "DEBIAN-CVE-2023-38039",
			wantFix:      "7.88.1-10+deb12u3",
		},
		{
			// Two records list bookworm: the AdvisoryID order breaks the tie.
			name:         "two records list the host line, advisory id order decides",
			records:      []string{frozenDSA9998Grouped, frozenDebianCVE},
			host:         debianCurl("12", "7.88.1-10+deb12u2"),
			wantStatus:   SupplyChainImpactAffectedExact,
			wantAdvisory: "DEBIAN-CVE-2023-38039",
			wantFix:      "7.88.1-10+deb12u3",
		},
	}
	for _, tc := range tests {
		for order, envelopes := range orderings(append(upstreamEnvelopes(t, []string{cve}, tc.records...), tc.host.envelope())) {
			t.Run(tc.name+" ("+order+")", func(t *testing.T) {
				t.Parallel()

				findings := BuildSupplyChainImpactFindings(envelopes)
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want ONE finding for %s: %#v", len(findings), cve, findings)
				}
				got := findings[0]
				if got.CVEID != cve {
					t.Errorf("CVEID = %q, want %q", got.CVEID, cve)
				}
				assertSupplyChainImpactStatus(t, got, tc.wantStatus)
				if got.AdvisoryID != tc.wantAdvisory {
					t.Errorf("AdvisoryID = %q, want the deciding record %q", got.AdvisoryID, tc.wantAdvisory)
				}
				if got.FixedVersion != tc.wantFix {
					t.Errorf("FixedVersion = %q, want %q", got.FixedVersion, tc.wantFix)
				}
			})
		}
	}
}

// TestUpstreamGroupingFindingIsOrderIndependent pins that the whole finding,
// not only its verdict, is the same for every envelope order.
func TestUpstreamGroupingFindingIsOrderIndependent(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	envelopes := append(upstreamEnvelopes(t, []string{cve},
		frozenDebianCVE, frozenDLA9999Grouped, frozenDSA9998,
	), debianCurl("11", "7.74.0-1.3+deb11u7").envelope())

	want := BuildSupplyChainImpactFindings(envelopes)
	if len(want) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(want), want)
	}
	for order, shuffled := range orderings(envelopes) {
		if got := BuildSupplyChainImpactFindings(shuffled); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: findings differ from the as-built order\n got: %#v\nwant: %#v", order, got, want)
		}
	}
}

// TestUpstreamGroupingBundleSplitsIntoOneFindingPerCVE: a USN-style bundle
// names several CVEs, so one installed package reads one finding per upstream
// CVE, and each finding carries the bundle's own fact ids (its cve and
// affected_package facts), not the ids of the other CVE's group.
func TestUpstreamGroupingBundleSplitsIntoOneFindingPerCVE(t *testing.T) {
	t.Parallel()

	envelopes := append(convertedOSVFixture(t, "built-bundle-usn", false), ubuntuCurl("22.04", "7.81.0-1ubuntu1.1").envelope())

	for order, shuffled := range orderings(envelopes) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 2 {
			t.Fatalf("%s: findings = %d, want one per upstream CVE: %#v", order, len(findings), findings)
		}
		var gotCVEs []string
		for _, finding := range findings {
			gotCVEs = append(gotCVEs, finding.CVEID)
			if finding.AdvisoryID != "USN-6766-1" {
				t.Errorf("%s: %s AdvisoryID = %q, want the bundle USN-6766-1", order, finding.CVEID, finding.AdvisoryID)
			}
			assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
			cveFacts := 0
			for _, id := range finding.EvidenceFactIDs {
				for _, envelope := range envelopes {
					if envelope.FactID == id && envelope.FactKind == "vulnerability.cve" {
						cveFacts++
					}
				}
			}
			if cveFacts != 1 {
				t.Errorf("%s: %s carries %d vulnerability.cve evidence facts, want the bundle's 1: %v", order, finding.CVEID, cveFacts, finding.EvidenceFactIDs)
			}
		}
		slices.Sort(gotCVEs)
		if want := []string{"CVE-2024-2004", "CVE-2024-2398"}; !slices.Equal(gotCVEs, want) {
			t.Errorf("%s: finding CVEIDs = %v, want %v", order, gotCVEs, want)
		}
	}
}

// TestUpstreamGroupingUbuntuAndAlpineRekey: the rule is data-driven by the
// upstream list, so Ubuntu and Alpine records re-key to their CVE exactly as
// Debian does, with the verdict unchanged.
func TestUpstreamGroupingUbuntuAndAlpineRekey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		record     string
		host       installedOSRow
		wantCVE    string
		wantAdvID  string
		wantStatus SupplyChainImpactStatus
	}{
		{
			name:   "ubuntu cve record",
			record: "built-group-ubuntu-cve",
			host:   ubuntuCurl("22.04", "7.81.0-1ubuntu1"), wantCVE: "CVE-2022-27775", wantAdvID: "UBUNTU-CVE-2022-27775",
			wantStatus: SupplyChainImpactAffectedExact,
		},
		{
			name:   "alpine cve record",
			record: "built-group-alpine-cve",
			host:   alpineCurl("3.18.4", "7.88.1-r0"), wantCVE: "CVE-2022-1271", wantAdvID: "ALPINE-CVE-2022-1271",
			wantStatus: SupplyChainImpactAffectedExact,
		},
	}
	for _, tc := range tests {
		envelopes := append(convertedOSVFixture(t, tc.record, false), tc.host.envelope())
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) != 1 {
				t.Fatalf("%s (%s): findings = %d, want 1: %#v", tc.name, order, len(findings), findings)
			}
			if findings[0].CVEID != tc.wantCVE || findings[0].AdvisoryID != tc.wantAdvID {
				t.Errorf("%s (%s): cve/advisory = %q/%q, want %q/%q", tc.name, order,
					findings[0].CVEID, findings[0].AdvisoryID, tc.wantCVE, tc.wantAdvID)
			}
			assertSupplyChainImpactStatus(t, findings[0], tc.wantStatus)
		}
	}
}

// TestUpstreamGroupingLeavesRecordsWithoutACVEKeyUnchanged covers the
// negative and ambiguous inputs: nothing re-keys, so the findings match those
// of a record that carried no upstream at all.
func TestUpstreamGroupingLeavesRecordsWithoutACVEKeyUnchanged(t *testing.T) {
	t.Parallel()

	const id = "DEBIAN-CVE-2023-38039"
	host := debianCurl("12", "7.88.1-10+deb12u2")
	runKeys := map[string]string{
		"only non-CVE upstream ids":             "built-run-variant-0",
		"blank and whitespace entries":          "built-run-variant-1",
		"a CVE prefix with nothing after it":    "built-run-variant-2",
		"upstream equal to the record's own id": "built-run-variant-3",
	}
	run := func(key string) []SupplyChainImpactFinding {
		return BuildSupplyChainImpactFindings(append(convertedOSVFixture(t, key, false), host.envelope()))
	}
	baseline := run("built-debian-cve-noupstream")
	if len(baseline) != 1 || baseline[0].CVEID != id {
		t.Fatalf("baseline without upstream = %#v, want one finding keyed %q", baseline, id)
	}
	for name, key := range runKeys {
		if got := run(key); !reflect.DeepEqual(got, baseline) {
			t.Errorf("%s: findings changed\n got: %#v\nwant: %#v", name, got, baseline)
		}
	}
}

// TestUpstreamGroupingKeysAreCaseAndWhitespaceInsensitive: an upstream entry
// groups under its canonical CVE id however it is spelled. Each spelling is the
// record's only key: beside the canonical id a respelled entry that was rejected
// outright would go unnoticed. The list is written to the stored payload, not the
// OSV record, because the collector trims entries on the way in and would hide the
// reducer's own rule; a fact from another producer has no such guarantee.
func TestUpstreamGroupingKeysAreCaseAndWhitespaceInsensitive(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	for name, upstream := range map[string][]any{
		"lowercase only":              {"cve-2023-38039"},
		"mixed case prefix only":      {"Cve-2023-38039"},
		"surrounding whitespace only": {" CVE-2023-38039\t"},
		"lowercase and whitespace":    {" cve-2023-38039 "},
		"respelled beside canonical":  {" cve-2023-38039 ", cve},
	} {
		envelopes := convertedOSVFixture(t, "built-debian-cve-noupstream", false)
		for _, envelope := range envelopes {
			if envelope.FactKind == chain.VulnerabilityCVEFactKind {
				envelope.Payload["upstream"] = upstream
			}
		}
		findings := BuildSupplyChainImpactFindings(append(envelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope()))
		if len(findings) != 1 || findings[0].CVEID != cve {
			t.Errorf("%s: findings = %#v, want ONE finding under %s", name, findings, cve)
		}
	}
}

// TestUpstreamGroupingWithoutASiblingRecordStillRekeys: the CVE the record
// names has no record of its own (no NVD fact, no other distro record). The
// distro record still answers under the CVE id, so a CVE query finds it.
func TestUpstreamGroupingWithoutASiblingRecordStillRekeys(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(append(
		convertedOSVFixture(t, frozenDSA9998, false), debianCurl("12", "7.88.1-10+deb12u2").envelope()))
	if len(findings) != 1 || findings[0].CVEID != "CVE-2023-38039" || findings[0].AdvisoryID != "DSA-9998-1" {
		t.Fatalf("findings = %#v, want CVE-2023-38039 deciding record DSA-9998-1", findings)
	}
}

// TestUpstreamGroupingMutualUpstreamTerminatesAndIsStable: two CVE records that
// list each other (a cycle) are not closed transitively; each record is a
// member of both groups, once.
func TestUpstreamGroupingMutualUpstreamTerminatesAndIsStable(t *testing.T) {
	t.Parallel()

	envelopes := append(upstreamEnvelopes(t, nil, "built-cycle-a", "built-cycle-b"), debianCurl("12", "7.88.1-10+deb12u2").envelope())
	want := BuildSupplyChainImpactFindings(envelopes)
	var gotCVEs []string
	for _, finding := range want {
		gotCVEs = append(gotCVEs, finding.CVEID)
	}
	if !slices.Equal(gotCVEs, []string{"CVE-2024-0001", "CVE-2024-0002"}) {
		t.Fatalf("CVEIDs = %v, want exactly the two groups, no duplicates", gotCVEs)
	}
	for order, shuffled := range orderings(envelopes) {
		if got := BuildSupplyChainImpactFindings(shuffled); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: findings differ from the as-built order", order)
		}
	}
}
