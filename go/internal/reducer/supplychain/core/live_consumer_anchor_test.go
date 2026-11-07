// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// RED for the live-record-with-consumer limit tracked in #10034: a live
// record whose only anchor is a consuming repository takes the whole group,
// so a host row the retracted record joins reads no finding. The consumer
// says the package is depended upon, but it does not speak to the scanned
// row: against a retracted record that joins the row, the row wins and the
// finding is the retracted record's, with the retraction shown. (The
// retracted side already counts only a joined row or an SBOM path; the live
// side must not count more.)

// TestLiveConsumerDoesNotBeatARetractedRowJoin pins the limit: the withdrawn
// USN joins the host row, the live DEBIAN record joins nothing but carries a
// consuming repository. The host reads the retracted USN with its retraction,
// in every fact order.
func TestLiveConsumerDoesNotBeatARetractedRowJoin(t *testing.T) {
	t.Parallel()
	// The live record shares the CVE and the package id, so one group holds
	// both records; its affected fact names the Debian vendor and joins no
	// Ubuntu row, and its only anchor is the consuming repository.
	liveCVEFact := func() facts.Envelope {
		return vulnerabilityCVEFactWithProvenance("cve-live", recordCVE, "debian", "DSA-9990-1", 7.5, "", "HIGH", "2026-10-01T00:00Z")
	}
	liveAffected := func() facts.Envelope {
		return facts.Envelope{FactID: "affected-live", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
			"cve_id": recordCVE, "advisory_id": "DSA-9990-1", "source": "debian",
			"ecosystem": "deb", "package_id": recordPackage, "package_name": "curl",
			"purl":              "pkg:deb/debian/curl?arch=amd64&distro=debian-12",
			"affected_versions": []any{"7.88.1-10"},
		}}
	}
	consumer := func() facts.Envelope {
		return packageConsumptionFactWithRange("use-live", recordPackage, "repository:r_live", "7.88.1-10")
	}
	row := func() []facts.Envelope {
		return []facts.Envelope{
			recordWithdrawn("cve-own", "ubuntu", recordVendor),
			liveCVEFact(), liveAffected(), consumer(),
			recordHost(),
			recordVendorFact("a", recordHostLine, recordVersion, "1.0-5"),
			recordVendorFact("b", recordHostLine, recordVersion, "1.0-6"),
		}
	}
	var first []SupplyChainImpactFinding
	for _, order := range everyOrder(row()) {
		findings := BuildSupplyChainImpactFindings(order)
		got := oneRecordFinding(t, findings)
		assertRetractedOffersNoFix(t, findings)
		if first == nil {
			first = findings
		} else if repDirectSummary(findings) != repDirectSummary(first) {
			t.Fatalf("order %s reads %s, want %s", repDirectOrderLabel(order), repDirectSummary(findings), repDirectSummary(first))
		}
		if got.AdvisoryID != recordVendor || got.AdvisoryRetraction == nil {
			t.Fatalf("order %s: advisory/retraction = %q/%#v, want %q with a retraction",
				repDirectOrderLabel(order), got.AdvisoryID, got.AdvisoryRetraction, recordVendor)
		}
	}
}
