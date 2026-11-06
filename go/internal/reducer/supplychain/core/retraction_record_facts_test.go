// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// The tests in this file pin arbiter ruling 9914-retract: a record is retracted as a
// whole, so a fact of a retracted record reads as that record does, whichever cve fact
// it paired with and whatever order the facts arrive in.

const (
	recordCVE      = "CVE-2023-9990"
	recordPackage  = "pkg:deb/ubuntu/curl"
	recordVendor   = "USN-9990-1"
	recordOSV      = "OSV-1"
	recordGHSA     = "GHSA-1"
	recordNPM      = "npm://registry.npmjs.org/lodash"
	recordHostLine = "ubuntu-22.04"
	recordVersion  = "1.0-3"
	recordScope    = "vuln-intel://ubuntu/feed"
	recordGen      = "gen-ubuntu"
)

// recordOSFact is one affected_package fact of an OS record, listing version on line.
func recordOSFact(id, source, advisory, line, version string, fix ...any) facts.Envelope {
	payload := map[string]any{
		"cve_id": recordCVE, "advisory_id": advisory, "source": source, "ecosystem": "os",
		"package_id": recordPackage, "package_name": "curl",
		"purl": "pkg:deb/ubuntu/curl?arch=amd64&distro=" + line, "affected_versions": []any{version},
	}
	if len(fix) > 0 {
		payload["fixed_versions"] = fix
	}
	return facts.Envelope{FactID: id, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// recordVendorFact is an affected_package fact of the Ubuntu record.
func recordVendorFact(id, line, version string, fix ...any) facts.Envelope {
	return recordOSFact(id, "ubuntu", recordVendor, line, version, fix...)
}

// recordNPMFact is an affected_package fact of an npm record.
func recordNPMFact(id, advisory, version, fix string) facts.Envelope {
	return vulnerabilityAffectedPackageFactWithSource(id, recordCVE, "osv", advisory, recordNPM, "npm", "lodash", version, fix)
}

// recordWithdrawn is a withdrawn cve fact.
func recordWithdrawn(id, source, advisory string) facts.Envelope {
	return vulnerabilityCVEFactWithdrawn(id, recordCVE, source, advisory, 7.5, "HIGH", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z")
}

// recordLive is a live cve fact.
func recordLive(id, source, advisory string) facts.Envelope {
	return vulnerabilityCVEFactWithProvenance(id, recordCVE, source, advisory, 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
}

// recordRejected is a cve fact whose status is Rejected.
func recordRejected(id, source, advisory string) facts.Envelope {
	envelope := recordLive(id, source, advisory)
	envelope.Payload["status"] = "Rejected"
	return envelope
}

func recordHost() facts.Envelope { return ubuntuCurl("22.04", recordVersion).envelope() }

func recordConsumer() facts.Envelope {
	return packageConsumptionFactWithRange("use", recordNPM, testImpactRepositoryID, "4.17.20")
}

// assertRetractedOffersNoFix is the ruling's invariant: a retracted finding offers no fix.
func assertRetractedOffersNoFix(t *testing.T, findings []SupplyChainImpactFinding) {
	t.Helper()
	for _, finding := range findings {
		if finding.AdvisoryRetraction != nil && (finding.FixedVersion != "" || len(finding.FixedVersionBranches) != 0) {
			t.Errorf("retracted finding %s offers fix %q and branches %#v", finding.AdvisoryID, finding.FixedVersion, finding.FixedVersionBranches)
		}
	}
}

// oneRecordFinding requires exactly one finding.
func oneRecordFinding(t *testing.T, findings []SupplyChainImpactFinding) SupplyChainImpactFinding {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	return findings[0]
}

// TestRetractedRecordWithSeveralFactsReadsAsItsOneFactControl is grid A of the ruling.
func TestRetractedRecordWithSeveralFactsReadsAsItsOneFactControl(t *testing.T) {
	t.Parallel()

	classes := []struct {
		name, source, advisory string
		anchor                 func() facts.Envelope
		one, sameLine          func() facts.Envelope
		lines                  []func() facts.Envelope
	}{
		{
			name: "vendor", source: "ubuntu", advisory: recordVendor, anchor: recordHost,
			one:      func() facts.Envelope { return recordVendorFact("a", recordHostLine, recordVersion, "1.0-5") },
			sameLine: func() facts.Envelope { return recordVendorFact("b", recordHostLine, recordVersion, "1.0-6") },
			lines: []func() facts.Envelope{
				func() facts.Envelope { return recordVendorFact("b", "ubuntu-20.04", "1.0-1", "1.0-2") },
				func() facts.Envelope { return recordVendorFact("c", "ubuntu-24.04", "1.0-7", "1.0-8") },
			},
		},
		{
			name: "osv", source: "osv", advisory: recordOSV, anchor: recordHost,
			one: func() facts.Envelope {
				return recordOSFact("a", "osv", recordOSV, recordHostLine, recordVersion, "1.0-5")
			},
			sameLine: func() facts.Envelope {
				return recordOSFact("b", "osv", recordOSV, recordHostLine, recordVersion, "1.0-6")
			},
			lines: []func() facts.Envelope{
				func() facts.Envelope { return recordOSFact("b", "osv", recordOSV, "ubuntu-20.04", "1.0-1", "1.0-2") },
				func() facts.Envelope { return recordOSFact("c", "osv", recordOSV, "ubuntu-24.04", "1.0-7", "1.0-8") },
			},
		},
		{
			name: "npm", source: "osv", advisory: recordGHSA, anchor: recordConsumer,
			one:      func() facts.Envelope { return recordNPMFact("a", recordGHSA, "4.17.20", "4.17.21") },
			sameLine: func() facts.Envelope { return recordNPMFact("b", recordGHSA, "4.17.20", "4.17.22") },
			lines: []func() facts.Envelope{
				func() facts.Envelope { return recordNPMFact("b", recordGHSA, "4.17.10", "4.17.11") },
				func() facts.Envelope { return recordNPMFact("c", recordGHSA, "4.16.0", "4.16.1") },
			},
		},
	}
	lifecycles := map[string]func(id, source, advisory string) facts.Envelope{"withdrawn": recordWithdrawn, "rejected": recordRejected}

	for _, class := range classes {
		for lifecycleName, lifecycle := range lifecycles {
			for cveFacts := 1; cveFacts <= 2; cveFacts++ {
				cves := []facts.Envelope{lifecycle("cve-1", class.source, class.advisory)}
				if cveFacts == 2 {
					cves = append(cves, lifecycle("cve-2", class.source, class.advisory))
				}
				for shape, extra := range map[string][]func() facts.Envelope{
					"two on one line": {class.sameLine},
					"two lines":       class.lines[:1],
					"three lines":     class.lines,
				} {
					listed := []facts.Envelope{class.one()}
					ids := []string{"a"}
					for _, build := range extra {
						envelope := build()
						listed = append(listed, envelope)
						ids = append(ids, envelope.FactID)
					}
					name := class.name + "/" + lifecycleName + "/" + shape + "/" + string(rune('0'+cveFacts)) + " cve"
					t.Run("fleet/"+name, func(t *testing.T) {
						t.Parallel()
						control := oneRecordFinding(t, BuildSupplyChainImpactFindings(slices.Concat(cves, []facts.Envelope{class.anchor(), class.one()})))
						if control.AdvisoryRetraction == nil {
							t.Fatalf("control shows no retraction: %#v", control)
						}
						wantSources := control.AdvisorySources
						if class.name == "osv" && cveFacts == 2 {
							// One package fact leaves the second cve fact unpaired, and only a
							// paired cve fact takes the vendor label from its package's purl, so
							// this control lists ubuntu and osv for one record. Every row with two
							// or more facts pairs both cve facts and lists ubuntu once, so these cells
							// compare with the one-cve control. That labeling is not this rule's.
							wantSources = oneRecordFinding(t, BuildSupplyChainImpactFindings(
								slices.Concat(cves[:1], []facts.Envelope{class.anchor(), class.one()}),
							)).AdvisorySources
						}
						var first []SupplyChainImpactFinding
						for _, order := range everyOrder(slices.Concat(cves, []facts.Envelope{class.anchor()}, listed)) {
							findings := BuildSupplyChainImpactFindings(order)
							got := oneRecordFinding(t, findings)
							assertRetractedOffersNoFix(t, findings)
							if first == nil {
								first = findings
							} else if !reflect.DeepEqual(findings, first) {
								t.Fatalf("finding depends on fact order:\n got %#v\nwant %#v", findings, first)
							}
							if got.AdvisoryRetraction == nil {
								t.Fatalf("no retraction: fix %q, sources %#v", got.FixedVersion, got.AdvisorySources)
							}
							if got.Status != control.Status || got.MatchReason != control.MatchReason || got.AdvisoryID != control.AdvisoryID {
								t.Errorf("status/reason/advisory = %s/%s/%s, control %s/%s/%s", got.Status, got.MatchReason, got.AdvisoryID, control.Status, control.MatchReason, control.AdvisoryID)
							}
							if !reflect.DeepEqual(got.AdvisorySources, wantSources) {
								t.Errorf("advisory sources:\n got %#v\nwant %#v", got.AdvisorySources, wantSources)
							}
							for _, id := range ids {
								if !slices.Contains(got.EvidenceFactIDs, id) {
									t.Errorf("evidence %v is missing fact %s", got.EvidenceFactIDs, id)
								}
							}
							if t.Failed() {
								return
							}
						}
					})
					if class.name != "vendor" {
						continue
					}
					t.Run("authorized/"+name, func(t *testing.T) {
						t.Parallel()
						control := oneRecordFinding(t, recordAuthorized(t, slices.Concat(cves, []facts.Envelope{class.one()})))
						// Two facts on the host's line with different fixes are an advisory
						// conflict, and the conflict withholds the fix. See ruling 3.2.
						wantConflict := shape == "two on one line"
						var first []SupplyChainImpactFinding
						for _, order := range everyOrder(slices.Concat(cves, listed)) {
							findings := recordAuthorized(t, order)
							got := oneRecordFinding(t, findings)
							assertRetractedOffersNoFix(t, findings)
							if first == nil {
								first = findings
							} else if !reflect.DeepEqual(findings, first) {
								t.Fatalf("finding depends on fact order:\n got %#v\nwant %#v", findings, first)
							}
							if got.AdvisoryRetraction == nil || got.AdvisoryID != control.AdvisoryID {
								t.Fatalf("advisory/retraction = %q/%#v, want %q with a retraction", got.AdvisoryID, got.AdvisoryRetraction, control.AdvisoryID)
							}
							if isConflict := got.MatchReason == supplyChainVersionReasonAdvisoryFixedConflict; isConflict != wantConflict {
								t.Fatalf("conflict = %v (reason %q), want %v", isConflict, got.MatchReason, wantConflict)
							}
							if !wantConflict && (got.Status != control.Status || got.MatchReason != control.MatchReason) {
								t.Errorf("status/reason = %s/%s, control %s/%s", got.Status, got.MatchReason, control.Status, control.MatchReason)
							}
						}
					})
				}
			}
		}
	}
}

// recordAuthorized builds the authorized findings of the Ubuntu host from advisories.
func recordAuthorized(t *testing.T, advisories []facts.Envelope) []SupplyChainImpactFinding {
	t.Helper()
	placed := make([]facts.Envelope, 0, len(advisories))
	for _, envelope := range advisories {
		envelope.ScopeID, envelope.GenerationID = recordScope, recordGen
		envelope.SourceRef.SourceRecordID = envelope.FactID
		placed = append(placed, envelope)
	}
	authority := []AdvisoryAuthorityDependency{{
		Kind: "public_registration", ID: "registration-ubuntu", RevisionID: "revision-ubuntu",
		AdvisoryScopeID: recordScope, AdvisoryGenerationID: recordGen, ConfigFingerprint: "config-ubuntu",
		ProviderID: "ubuntu", TrustedSourceClass: "ubuntu", ContentProfile: "supply_chain_impact_v1",
	}}
	findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{
		authorizedOSVTarget(0, placed, authority, ubuntuCurl("22.04", recordVersion)),
	})
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	return findings
}

// TestRetractedRecordWithSeveralFactsBesideOtherRecords is set B of the ruling: the
// same defect with another record in the group. Each row must read as its twin, the
// same group with one fact fewer on the retracted record.
func TestRetractedRecordWithSeveralFactsBesideOtherRecords(t *testing.T) {
	t.Parallel()

	withdrawnGHSA := func() facts.Envelope { return recordWithdrawn("ghsa-cve", "osv", recordGHSA) }
	withdrawnUSN := func() facts.Envelope { return recordWithdrawn("cve-own", "ubuntu", recordVendor) }
	withdrawnGHSA2 := func() facts.Envelope { return recordWithdrawn("w2", "osv", "GHSA-2") }
	withdrawnUSN2 := func() facts.Envelope { return recordWithdrawn("w2", "ubuntu", "USN-9991-1") }
	ghsa := func(id, fix string) facts.Envelope { return recordNPMFact(id, recordGHSA, "4.17.20", fix) }
	ghsa2 := func(id, fix string) facts.Envelope { return recordNPMFact(id, "GHSA-2", "4.17.20", fix) }
	usn := func(id string, fix ...any) facts.Envelope {
		return recordVendorFact(id, recordHostLine, recordVersion, fix...)
	}
	usn2 := func(id string, fix ...any) facts.Envelope {
		return recordOSFact(id, "ubuntu", "USN-9991-1", recordHostLine, recordVersion, fix...)
	}
	liveNVD := func() facts.Envelope { return recordLive("nvd", "nvd", recordCVE) }
	liveExt := func() facts.Envelope { return recordLive("cve-other", "nvd", "EXT-1") }
	livePkg := func() facts.Envelope {
		return facts.Envelope{FactID: "affected-other", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
			"cve_id": recordCVE, "advisory_id": "EXT-1", "source": "nvd", "ecosystem": "os",
			"package_id": recordPackage, "package_name": "curl", "fixed_versions": []any{"1.0-2"},
		}}
	}
	quarantined := func() facts.Envelope { return quarantinedCVEFact("q", recordCVE, "osv", recordGHSA) }

	for _, tc := range []struct {
		name, wantAdvisory string
		wantRetraction     bool
		row, twin          []facts.Envelope
	}{
		{
			name: "X4 the second record has two facts", wantAdvisory: recordGHSA, wantRetraction: true,
			row:  []facts.Envelope{withdrawnGHSA(), withdrawnGHSA2(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa2("p2", "4.17.23"), ghsa2("p2b", "4.17.24")},
			twin: []facts.Envelope{withdrawnGHSA(), withdrawnGHSA2(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa2("p2", "4.17.23")},
		},
		{
			name: "X5 the first record has two facts", wantAdvisory: recordGHSA, wantRetraction: true,
			row:  []facts.Envelope{withdrawnGHSA(), withdrawnGHSA2(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p1b", "4.17.22"), ghsa2("p2", "4.17.23")},
			twin: []facts.Envelope{withdrawnGHSA(), withdrawnGHSA2(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa2("p2", "4.17.23")},
		},
		{
			name: "X6 two OS records, one with two facts", wantAdvisory: recordVendor, wantRetraction: true,
			row:  []facts.Envelope{withdrawnUSN(), withdrawnUSN2(), recordHost(), usn("a", "1.0-5"), usn2("l", "1.0-7"), usn2("l2", "1.0-8")},
			twin: []facts.Envelope{withdrawnUSN(), withdrawnUSN2(), recordHost(), usn("a", "1.0-5"), usn2("l", "1.0-7")},
		},
		{
			name: "N1 npm beside a cve-only live record", wantAdvisory: recordCVE,
			row:  []facts.Envelope{withdrawnGHSA(), liveNVD(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p2", "4.17.22")},
			twin: []facts.Envelope{withdrawnGHSA(), liveNVD(), recordConsumer(), ghsa("p1", "4.17.21")},
		},
		{
			name: "N2 OS beside a cve-only live record", wantAdvisory: recordVendor, wantRetraction: true,
			row:  []facts.Envelope{withdrawnUSN(), liveNVD(), recordHost(), usn("a", "1.0-5"), usn("b", "1.0-6")},
			twin: []facts.Envelope{withdrawnUSN(), liveNVD(), recordHost(), usn("a", "1.0-5")},
		},
		{
			name: "S2 two withdrawn cve facts, three package facts", wantAdvisory: recordGHSA, wantRetraction: true,
			row:  []facts.Envelope{withdrawnGHSA(), recordWithdrawn("ghsa-cve2", "osv", recordGHSA), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p2", "4.17.21"), ghsa("p3", "4.17.22")},
			twin: []facts.Envelope{withdrawnGHSA(), recordWithdrawn("ghsa-cve2", "osv", recordGHSA), recordConsumer(), ghsa("p1", "4.17.21")},
		},
		{
			name: "R5-2 beside a live record that cannot join", wantAdvisory: recordVendor, wantRetraction: true,
			row:  []facts.Envelope{withdrawnUSN(), liveExt(), livePkg(), recordHost(), usn("a", "1.0-5"), usn("b", "1.0-6")},
			twin: []facts.Envelope{withdrawnUSN(), liveExt(), livePkg(), recordHost(), usn("a", "1.0-5")},
		},
		{
			name: "R5-2n the same with no fix listed", wantAdvisory: recordVendor, wantRetraction: true,
			row:  []facts.Envelope{withdrawnUSN(), liveExt(), livePkg(), recordHost(), usn("a"), usn("b")},
			twin: []facts.Envelope{withdrawnUSN(), liveExt(), livePkg(), recordHost(), usn("a")},
		},
		{
			name: "R5-0 beside a cve-only live record from another advisory", wantAdvisory: recordVendor, wantRetraction: true,
			row:  []facts.Envelope{withdrawnUSN(), liveExt(), recordHost(), usn("a", "1.0-5"), usn("b", "1.0-6")},
			twin: []facts.Envelope{withdrawnUSN(), liveExt(), recordHost(), usn("a", "1.0-5")},
		},
		{
			name: "Q1 a quarantined sibling cve fact still blocks the retraction", wantAdvisory: recordGHSA,
			row:  []facts.Envelope{withdrawnGHSA(), quarantined(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p2", "4.17.22")},
			twin: []facts.Envelope{withdrawnGHSA(), quarantined(), recordConsumer(), ghsa("p1", "4.17.21")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			twin := oneRecordFinding(t, BuildSupplyChainImpactFindings(slices.Clone(tc.twin)))
			var first []SupplyChainImpactFinding
			for _, order := range everyOrder(tc.row) {
				findings := BuildSupplyChainImpactFindings(order)
				got := oneRecordFinding(t, findings)
				assertRetractedOffersNoFix(t, findings)
				if first == nil {
					first = findings
				} else if !reflect.DeepEqual(findings, first) {
					t.Fatalf("finding depends on fact order:\n got %#v\nwant %#v", findings, first)
				}
				if got.AdvisoryID != tc.wantAdvisory || (got.AdvisoryRetraction != nil) != tc.wantRetraction {
					t.Fatalf("advisory/retraction = %q/%v, want %q/%v", got.AdvisoryID, got.AdvisoryRetraction != nil, tc.wantAdvisory, tc.wantRetraction)
				}
				if got.FixedVersion != "" || len(got.FixedVersionBranches) != 0 {
					t.Fatalf("offers fix %q and branches %#v", got.FixedVersion, got.FixedVersionBranches)
				}
				if got.Status != twin.Status || got.MatchReason != twin.MatchReason || got.AdvisoryID != twin.AdvisoryID || (got.AdvisoryRetraction != nil) != (twin.AdvisoryRetraction != nil) {
					t.Fatalf("differs from its twin: %s/%s/%s/%v against %s/%s/%s/%v", got.Status, got.MatchReason, got.AdvisoryID, got.AdvisoryRetraction != nil, twin.Status, twin.MatchReason, twin.AdvisoryID, twin.AdvisoryRetraction != nil)
				}
			}
		})
	}
}
