// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestAFactOfALiveOrForeignRecordKeepsTheFindingLive is set C of the ruling, the guards
// on the rule: a source with no cve fact of its own stays live, and so does a record
// one of whose cve facts is live. It passes before and after the rule exists.
func TestAFactOfALiveOrForeignRecordKeepsTheFindingLive(t *testing.T) {
	t.Parallel()

	withdrawnGHSA := func() facts.Envelope { return recordWithdrawn("ghsa-cve", "osv", recordGHSA) }
	withdrawnUSN := func() facts.Envelope { return recordWithdrawn("cve-own", "ubuntu", recordVendor) }
	ghsa := func(id, fix string) facts.Envelope { return recordNPMFact(id, recordGHSA, "4.17.20", fix) }
	ghsa2 := func(id, fix string) facts.Envelope { return recordNPMFact(id, "GHSA-2", "4.17.20", fix) }
	usn := func(id, fix string) facts.Envelope { return recordVendorFact(id, recordHostLine, recordVersion, fix) }
	liveSibling := func() facts.Envelope { return recordLive("ghsa-cve2", "osv", recordGHSA) }

	for _, tc := range []struct {
		name, wantAdvisory, wantFix string
		wantBranches                int
		row                         []facts.Envelope
	}{
		{name: "L1 nvd rejected, a ghsa fact with no cve fact", wantAdvisory: recordCVE, wantFix: "4.17.21", wantBranches: 1, row: []facts.Envelope{recordRejected("nvd", "nvd", recordCVE), recordConsumer(), ghsa("p1", "4.17.21")}},
		{name: "L2 nvd rejected, an osv fact with the same advisory id and no cve fact", wantAdvisory: recordCVE, wantFix: "4.17.21", wantBranches: 1, row: []facts.Envelope{recordRejected("nvd", "nvd", recordCVE), recordConsumer(), recordNPMFact("p1", recordCVE, "4.17.20", "4.17.21")}},
		{name: "L3 withdrawn ghsa beside a gitlab fact with no cve fact", wantAdvisory: recordGHSA, wantFix: "4.17.22", wantBranches: 1, row: []facts.Envelope{withdrawnGHSA(), recordConsumer(), ghsa("p1", "4.17.21"), vulnerabilityAffectedPackageFactWithSource("p2", recordCVE, "gitlab", "GLSA-live-1", recordNPM, "npm", "lodash", "4.17.20", "4.17.22")}},
		{name: "L4 withdrawn ghsa beside another ghsa with no cve fact", wantAdvisory: recordGHSA, wantFix: "4.17.22", wantBranches: 1, row: []facts.Envelope{withdrawnGHSA(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa2("p2", "4.17.22")}},
		{name: "X1 one of two records retracted", wantAdvisory: recordGHSA, wantFix: "4.17.23", wantBranches: 1, row: []facts.Envelope{withdrawnGHSA(), recordLive("l-cve", "osv", "GHSA-2"), recordConsumer(), ghsa("p1", "4.17.21"), ghsa2("p2", "4.17.23")}},
		{name: "X2 the retracted one has two facts", wantAdvisory: recordGHSA, wantFix: "4.17.23", wantBranches: 1, row: []facts.Envelope{withdrawnGHSA(), recordLive("l-cve", "osv", "GHSA-2"), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p1b", "4.17.22"), ghsa2("p2", "4.17.23")}},
		{name: "X3 the same on an OS record", wantAdvisory: "USN-9991-1", wantFix: "1.0-7", wantBranches: 1, row: []facts.Envelope{withdrawnUSN(), recordLive("l-cve", "ubuntu", "USN-9991-1"), recordHost(), usn("a", "1.0-5"), usn("b", "1.0-6"), recordOSFact("l", "ubuntu", "USN-9991-1", recordHostLine, recordVersion, "1.0-7")}},
		// One cve fact of the record is live, so the record is. Which fact pairs with the
		// withdrawn one still decides the fix on these rows: ruling 3.8 leaves that open,
		// so they pin only that no retraction shows.
		{name: "P1 one record, cve withdrawn and live, one fact", wantBranches: -1, row: []facts.Envelope{withdrawnGHSA(), liveSibling(), recordConsumer(), ghsa("p1", "4.17.21")}},
		{name: "P2 two facts", wantBranches: -1, row: []facts.Envelope{withdrawnGHSA(), liveSibling(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p2", "4.17.22")}},
		// P3: the two copies tie on recency, so the FactID tiebreak elects
		// the live copy (#10042) and the packages carry no scope to
		// attribute them to a copy. All three stay live observations, so
		// all three fixes branch; the set is the same in every load order.
		{name: "P3 three facts read three fix branches", wantBranches: 3, row: []facts.Envelope{withdrawnGHSA(), liveSibling(), recordConsumer(), ghsa("p1", "4.17.21"), ghsa("p2", "4.17.22"), ghsa("p3", "4.17.23")}},
		{name: "P4 an OS record", wantBranches: -1, row: []facts.Envelope{withdrawnUSN(), recordLive("cve-own2", "ubuntu", recordVendor), recordHost(), usn("a", "1.0-5"), usn("b", "1.0-6")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range everyOrder(tc.row) {
				got := oneRecordFinding(t, BuildSupplyChainImpactFindings(order))
				if got.AdvisoryRetraction != nil {
					t.Fatalf("a live source's finding shows a retraction: %#v", got.AdvisoryRetraction)
				}
				if tc.wantAdvisory != "" && (got.AdvisoryID != tc.wantAdvisory || got.FixedVersion != tc.wantFix) {
					t.Fatalf("advisory/fix = %q/%q, want %q/%q", got.AdvisoryID, got.FixedVersion, tc.wantAdvisory, tc.wantFix)
				}
				if tc.wantBranches >= 0 && len(got.FixedVersionBranches) != tc.wantBranches {
					t.Fatalf("branches = %#v, want %d", got.FixedVersionBranches, tc.wantBranches)
				}
			}
		})
	}
}

// TestAuthorizedPassOffersNoFixOnARetractedFinding is set D of the ruling. A conflict
// may still be marked, because it withholds a fix, but the pass must not select a branch
// for a retracted finding. The live twins keep their fixes.
func TestAuthorizedPassOffersNoFixOnARetractedFinding(t *testing.T) {
	t.Parallel()

	ranged := func(id, ecosystem, introduced, fixed string) facts.Envelope {
		envelope := recordVendorFact(id, recordHostLine, recordVersion, fixed)
		envelope.Payload["ecosystem"] = ecosystem
		delete(envelope.Payload, "affected_versions")
		envelope.Payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "kind": "ECOSYSTEM", "events": []any{map[string]any{"introduced": introduced}, map[string]any{"fixed": fixed}}}}
		return envelope
	}
	type want struct {
		status     SupplyChainImpactStatus
		reason     string
		fix        string
		branches   []string
		retraction bool
	}
	withdrawn := recordWithdrawn("cve-own", "ubuntu", recordVendor)
	live := recordLive("cve-own", "ubuntu", recordVendor)
	conflict := supplyChainVersionReasonAdvisoryFixedConflict
	const exact, inRange = "dpkg_exact_affected_version", "dpkg_affected_range"
	type row struct {
		name       string
		advisories []facts.Envelope
		want       want
	}
	rows := []row{
		{"T8 two facts, different fixes", []facts.Envelope{withdrawn, recordVendorFact("a", recordHostLine, recordVersion, "1.0-4"), recordVendorFact("b", recordHostLine, recordVersion, "1.0-5")}, want{SupplyChainImpactPossiblyAffected, conflict, "", nil, true}},
		{"T8s two facts, one fix", []facts.Envelope{withdrawn, recordVendorFact("a", recordHostLine, recordVersion, "1.0-5"), recordVendorFact("b", recordHostLine, recordVersion, "1.0-5")}, want{SupplyChainImpactAffectedExact, exact, "", nil, true}},
		{"D1 os", []facts.Envelope{withdrawn, ranged("a", "os", "1.0-1", "1.0-5"), ranged("b", "os", "1.0-5", "1.0-9")}, want{SupplyChainImpactPossiblyAffected, conflict, "", nil, true}},
		{"D1L os", []facts.Envelope{live, ranged("a", "os", "1.0-1", "1.0-5"), ranged("b", "os", "1.0-5", "1.0-9")}, want{SupplyChainImpactPossiblyAffected, conflict, "", []string{"1.0-5", "1.0-9"}, false}},
	}
	for _, ecosystem := range []string{"deb", "ubuntu"} {
		rows = append(rows,
			row{"D1 " + ecosystem, []facts.Envelope{withdrawn, ranged("a", ecosystem, "1.0-1", "1.0-5"), ranged("b", ecosystem, "1.0-5", "1.0-9")}, want{SupplyChainImpactAffectedExact, inRange, "", nil, true}},
			row{"D1L " + ecosystem, []facts.Envelope{live, ranged("a", ecosystem, "1.0-1", "1.0-5"), ranged("b", ecosystem, "1.0-5", "1.0-9")}, want{SupplyChainImpactAffectedExact, inRange, "1.0-5", []string{"1.0-5", "1.0-9"}, false}},
		)
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range everyOrder(tc.advisories) {
				findings := recordAuthorized(t, order)
				got := oneRecordFinding(t, findings)
				assertRetractedOffersNoFix(t, findings)
				var branches []string
				for _, branch := range got.FixedVersionBranches {
					branches = append(branches, branch.Version)
				}
				slices.Sort(branches)
				if got.Status != tc.want.status || got.MatchReason != tc.want.reason || got.FixedVersion != tc.want.fix ||
					!slices.Equal(branches, tc.want.branches) || (got.AdvisoryRetraction != nil) != tc.want.retraction {
					t.Fatalf("status/reason/fix/branches/retraction = %s/%s/%q/%v/%v, want %+v", got.Status, got.MatchReason, got.FixedVersion, branches, got.AdvisoryRetraction != nil, tc.want)
				}
			}
		})
	}
}

// TestUnpairedFactObservationTakesItsRecordsLifecycle is set E of the ruling, the unit
// rows on the rule itself.
func TestUnpairedFactObservationTakesItsRecordsLifecycle(t *testing.T) {
	t.Parallel()

	const advisory = "GHSA-1"
	fact := func(id, advisoryID string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{FactID: id, CVEID: recordCVE, AdvisoryID: advisoryID, Source: "osv"}
	}
	observationOf := func(t *testing.T, observations []AdvisoryProvenanceObservation, factID string) AdvisoryProvenanceObservation {
		t.Helper()
		for _, observation := range observations {
			if observation.AffectedFactID == factID {
				return observation
			}
		}
		t.Fatalf("no observation for fact %s in %#v", factID, observations)
		return AdvisoryProvenanceObservation{}
	}

	t.Run("a fact with a blank advisory id stays live", func(t *testing.T) {
		t.Parallel()
		cves := []supplychainmodel.ImpactCVE{{FactID: "c", CVEID: recordCVE, Source: "osv", WithdrawnAt: "2026-11-01T00:00:00Z"}}
		pkgs := []supplychainmodel.AffectedPackage{fact("p1", ""), fact("p2", "")}
		unpaired := observationOf(t, buildAdvisoryProvenanceObservations(cves, pkgs), "p2")
		if unpaired.WithdrawnAt != "" || unpaired.Status != "" {
			t.Fatalf("withdrawn/status = %q/%q, want a live observation", unpaired.WithdrawnAt, unpaired.Status)
		}
	})

	t.Run("the lifecycle comes from the newest copy, in either input order", func(t *testing.T) {
		t.Parallel()
		early := supplychainmodel.ImpactCVE{
			FactID: "c-a", CVEID: recordCVE, Source: "osv", AdvisoryID: advisory, Status: "Withdrawn",
			WithdrawnAt: "2026-11-01T00:00:00Z", SourceUpdatedAt: "2026-10-01T00:00:00Z", ReplacedBy: []string{"CVE-2023-1"},
		}
		late := supplychainmodel.ImpactCVE{
			FactID: "c-b", CVEID: recordCVE, Source: "osv", AdvisoryID: advisory, Status: "Rejected",
			WithdrawnAt: "2026-12-01T00:00:00Z", SourceUpdatedAt: "2026-11-15T00:00:00Z", ReplacedBy: []string{"CVE-2023-2"},
		}
		for _, cves := range [][]supplychainmodel.ImpactCVE{{early, late}, {late, early}} {
			pkgs := []supplychainmodel.AffectedPackage{fact("p1", advisory), fact("p2", advisory), fact("p3", advisory)}
			cvesBefore := slices.Clone(cves)
			pkgsBefore := slices.Clone(pkgs)
			unpaired := observationOf(t, buildAdvisoryProvenanceObservations(cves, pkgs), "p3")
			// The newest copy (late) is the record (#10042): the unpaired
			// fact of a superseded copy reads as its own copy did, and a
			// fact with no copy of its own reads as the newest copy does.
			if unpaired.WithdrawnAt != late.WithdrawnAt || unpaired.Status != late.Status ||
				unpaired.SourceUpdatedAt != late.SourceUpdatedAt || !slices.Equal(unpaired.ReplacedBy, late.ReplacedBy) {
				t.Errorf("first cve %s: observation = %#v, want the lifecycle of %s", cves[0].FactID, unpaired, late.FactID)
			}
			if unpaired.CVEFactID != "" || unpaired.SeverityScore != 0 {
				t.Errorf("cve fact/severity = %q/%v, want them empty so identity and severity do not move", unpaired.CVEFactID, unpaired.SeverityScore)
			}
			if !reflect.DeepEqual(cves, cvesBefore) || !reflect.DeepEqual(pkgs, pkgsBefore) {
				t.Errorf("the rule changed its input slices")
			}
		}
	})
}

// TestRetractedRecordPayloadShowsItsWithdrawal is set F of the ruling, the query
// surface: what a reader of the finding sees.
func TestRetractedRecordPayloadShowsItsWithdrawal(t *testing.T) {
	t.Parallel()

	for name, envelopes := range map[string][]facts.Envelope{
		"T4 two facts on one line": {recordWithdrawn("cve-own", "ubuntu", recordVendor), recordHost(), recordVendorFact("a", recordHostLine, recordVersion, "1.0-5"), recordVendorFact("b", recordHostLine, recordVersion, "1.0-6")},
		"T2 the other line pairs":  {recordWithdrawn("cve-own", "ubuntu", recordVendor), recordHost(), recordVendorFact("b", "ubuntu-20.04", "1.0-1", "1.0-2"), recordVendorFact("a", recordHostLine, recordVersion, "1.0-5")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			finding := oneRecordFinding(t, BuildSupplyChainImpactFindings(envelopes))
			payload := supplyChainImpactTypedPayload(SupplyChainImpactWrite{IntentID: "i", ScopeID: "s", GenerationID: "g"}, finding)
			if _, ok := payload.Provenance["advisory_retraction"]; !ok {
				t.Errorf("advisory_retraction is absent from provenance")
			}
			if branches, ok := payload.Provenance["fixed_version_branches"]; ok {
				t.Errorf("fixed_version_branches = %#v, want absent", branches)
			}
			rows, _ := payload.Provenance["advisory_sources"].([]map[string]any)
			if len(rows) == 0 || rows[0]["withdrawn_at"] != "2026-11-01T00:00:00Z" {
				t.Errorf("advisory_sources = %#v, want the first row to show the withdrawal", rows)
			}
		})
	}
}
