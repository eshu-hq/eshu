// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"maps"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// RED for #10034 item 3: the join needs an outcome for an identity that can
// meet no installed row. An os:// package id with no purl names no purl key,
// and rows key by purl, so the identity can never meet a row. That is not
// "the package is not on any scanned host" (no_installed_row): the row could
// exist and the identity still could not meet it. The group yields no finding
// either way, but the outcome tells the operator which one it was.

// TestUnmeetableIdentityCountsItsOwnOutcome is the RED proof: an os://
// identity with no purl against purl-keyed rows counts no_joinable_identity,
// not no_installed_row.
func TestUnmeetableIdentityCountsItsOwnOutcome(t *testing.T) {
	t.Parallel()
	findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		repJoinNoPURLFact("os://ubuntu/curl"),
		ubuntuCurl("22.04", repJoinVersion).envelope(),
	})
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %#v", len(findings), findings)
	}
	want := map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeNoJoinableIdentity}: 1}
	if !maps.Equal(stats.osRepresentativeOutcomes, want) {
		t.Fatalf("outcomes = %v, want %v", stats.osRepresentativeOutcomes, want)
	}
}

// TestKeylessIdentityCountsItsOwnOutcome pins the degenerate end: a fact with
// neither package id nor purl names no lookup key at all, so it counts the
// same outcome.
func TestKeylessIdentityCountsItsOwnOutcome(t *testing.T) {
	t.Parallel()
	keyless := facts.Envelope{FactID: "affected-keyless", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "USN-9990-1", "source": "ubuntu",
		"ecosystem": "os", "package_name": "curl",
	}}
	findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		keyless,
		ubuntuCurl("22.04", repJoinVersion).envelope(),
	})
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %#v", len(findings), findings)
	}
	want := map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeNoJoinableIdentity}: 1}
	if !maps.Equal(stats.osRepresentativeOutcomes, want) {
		t.Fatalf("outcomes = %v, want %v", stats.osRepresentativeOutcomes, want)
	}
}
