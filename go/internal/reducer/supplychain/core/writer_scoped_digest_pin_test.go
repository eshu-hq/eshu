// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
)

// TestScopedWriteKeepsSameScopeDigests pins the D7 differential premise
// (#10972): for the same finding on the same scope, a scoped write and a full
// write build identical typed payloads, so both the content digest and the
// identity digest match. The stamp filters which findings reach the
// transaction but never rewrites a kept finding: writer.go computes both
// digests from supplyChainImpactTypedPayload, and neither digest zeroes the
// scope id, so changing only the scope id changes both digests — which is why
// the cross-scope live differential redacts both.
func TestScopedWriteKeepsSameScopeDigests(t *testing.T) {
	t.Parallel()

	kept := SupplyChainImpactFinding{
		CVEID: "CVE-2026-9001", PackageID: "pkg-a",
		PURL: "pkg:npm/a@1.0.0", ObservedVersion: "1.0.0",
		Status: SupplyChainImpactAffectedExact,
	}
	dropped := SupplyChainImpactFinding{
		CVEID: "CVE-2026-9002", PackageID: "pkg-b",
		PURL: "pkg:npm/b@2.0.0", ObservedVersion: "2.0.0",
		Status: SupplyChainImpactAffectedExact,
	}
	base := SupplyChainImpactWrite{
		IntentID: "intent-1", ScopeID: "scope:s", GenerationID: "gen",
		SourceSystem: "synthetic", Cause: "differential",
		Findings:     []SupplyChainImpactFinding{kept, dropped},
		FencingToken: 7,
	}
	scoped := base
	scoped.ScopedCVEIDs = []string{"CVE-2026-9001"}

	filtered := filterSupplyChainImpactFindingsForScope(scoped)
	if !reflect.DeepEqual(filtered, []SupplyChainImpactFinding{kept}) {
		t.Fatalf("scoped filter kept %+v, want exactly the stamped finding unchanged", filtered)
	}

	fullPayload := supplyChainImpactTypedPayload(base, kept)
	scopedPayload := supplyChainImpactTypedPayload(scoped, kept)
	fullContent, scopedContent := contentDigestOf(fullPayload), contentDigestOf(scopedPayload)
	fullIdentity, scopedIdentity := identityDigestOf(fullPayload), identityDigestOf(scopedPayload)
	if fullContent == "" || fullIdentity == "" {
		t.Fatal("empty digest: the pin would pass vacuously")
	}
	if fullContent != scopedContent {
		t.Errorf("content digest differs for the same finding on the same scope: full=%s scoped=%s", fullContent, scopedContent)
	}
	if fullIdentity != scopedIdentity {
		t.Errorf("identity digest differs for the same finding on the same scope: full=%s scoped=%s", fullIdentity, scopedIdentity)
	}

	otherScope := scoped
	otherScope.ScopeID = "scope:other"
	moved := supplyChainImpactTypedPayload(otherScope, kept)
	if contentDigestOf(moved) == scopedContent {
		t.Error("changing only the scope id left the content digest unchanged: the live differential would compare pass identity as truth")
	}
	if identityDigestOf(moved) == scopedIdentity {
		t.Error("changing only the scope id left the identity digest unchanged: the live differential would compare pass identity as truth")
	}
}

// TestScopedDigestPinIsSensitiveToKeptFindingMutation is the seeded check for
// the pin above: a mutation of the scoped path that alters a kept finding
// must break the digest equality, so the pin cannot pass vacuously.
func TestScopedDigestPinIsSensitiveToKeptFindingMutation(t *testing.T) {
	t.Parallel()

	kept := SupplyChainImpactFinding{
		CVEID: "CVE-2026-9001", PackageID: "pkg-a",
		PURL: "pkg:npm/a@1.0.0", ObservedVersion: "1.0.0",
		Status: SupplyChainImpactAffectedExact,
	}
	write := SupplyChainImpactWrite{
		IntentID: "intent-1", ScopeID: "scope:s", GenerationID: "gen",
		SourceSystem: "synthetic", Cause: "differential",
		Findings:     []SupplyChainImpactFinding{kept},
		ScopedCVEIDs: []string{"CVE-2026-9001"},
		FencingToken: 7,
	}
	want := supplyChainImpactTypedPayload(write, kept)
	wantContent, wantIdentity := contentDigestOf(want), identityDigestOf(want)

	mutated := kept
	mutated.ObservedVersion = "1.0.1"
	got := supplyChainImpactTypedPayload(write, mutated)
	if contentDigestOf(got) == wantContent {
		t.Error("mutating a kept finding left the content digest unchanged: the pin would miss a filter or payload-build mutation")
	}
	if identityDigestOf(got) == wantIdentity {
		t.Error("mutating a kept finding left the identity digest unchanged: the pin would miss a filter or payload-build mutation")
	}
}
