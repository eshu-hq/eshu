// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

const aliasGroupTestPackageID = "npm://registry.npmjs.org/parse-server"

const aliasGroupTestRepoID = "repository:supply-chain-demo/example-service"

// aliasCVEFact builds a vulnerability.cve envelope with an explicit alias
// list, so grouping tests state exactly the alias edges the test needs
// instead of inheriting the self-alias default of
// vulnerabilityCVEFactWithProvenance.
func aliasCVEFact(factID, cveID, source, advisoryID string, aliases ...string) facts.Envelope {
	raw := make([]any, 0, len(aliases))
	for _, alias := range aliases {
		raw = append(raw, alias)
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":      cveID,
			"advisory_id": advisoryID,
			"source":      source,
			"aliases":     raw,
		},
	}
}

// aliasAffectedPackageFact builds a vulnerability.affected_package envelope
// for the shared npm fixture package of the alias-grouping tests.
func aliasAffectedPackageFact(factID, cveID, source, advisoryID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":       cveID,
			"advisory_id":  advisoryID,
			"source":       source,
			"package_id":   aliasGroupTestPackageID,
			"ecosystem":    "npm",
			"package_name": "parse-server",
		},
	}
}

// aliasGroupConsumption anchors the shared fixture package to a repository so
// the package findings the grouping tests assert are retained.
func aliasGroupConsumption(factID string) facts.Envelope {
	return packageConsumptionCorrelationEnvelope(factID, aliasGroupTestRepoID, aliasGroupTestPackageID, "package.json")
}

// aliasGroupFindingsByCVE builds findings for envelopes and indexes them by
// finding CVE id, failing the test on any build error or duplicate key.
func aliasGroupFindingsByCVE(t *testing.T, envelopes []facts.Envelope) map[string]SupplyChainImpactFinding {
	t.Helper()
	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactFindingsWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none", quarantined)
	}
	out := make(map[string]SupplyChainImpactFinding, len(findings))
	for _, finding := range findings {
		if prior, dup := out[finding.CVEID]; dup {
			t.Fatalf("duplicate finding CVEID %q: %#v and %#v", finding.CVEID, prior, finding)
		}
		out[finding.CVEID] = finding
	}
	return out
}

// sortedFindingKeys returns the sorted CVE ids of a finding map for failure
// messages.
func sortedFindingKeys(byCVE map[string]SupplyChainImpactFinding) []string {
	keys := make([]string, 0, len(byCVE))
	for key := range byCVE {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// TestAliasGroupingMergesGHSAAndCVELateAlias is the #10407 item (a) failing
// regression: a GHSA record that later gains a CVE alias yields one CVE-keyed
// finding, where today it yields two findings keyed differently.
func TestAliasGroupingMergesGHSAAndCVELateAlias(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		aliasCVEFact("ghsa-cve", "GHSA-2026-0001", "osv", "GHSA-2026-0001", "GHSA-2026-0001"),
		aliasAffectedPackageFact("ghsa-pkg", "GHSA-2026-0001", "osv", "GHSA-2026-0001"),
		aliasCVEFact("nvd-cve", "CVE-2026-0001", "nvd", "CVE-2026-0001", "CVE-2026-0001", "GHSA-2026-0001"),
		aliasAffectedPackageFact("nvd-pkg", "CVE-2026-0001", "nvd", "CVE-2026-0001"),
		aliasGroupConsumption("cons-1"),
	}

	for name, order := range orderings(envelopes) {
		byCVE := aliasGroupFindingsByCVE(t, order)
		if len(byCVE) != 1 {
			t.Fatalf("%s: findings = %d (%v), want exactly 1 CVE-keyed finding", name, len(byCVE), sortedFindingKeys(byCVE))
		}
		got := byCVE["CVE-2026-0001"]
		if got.CVEID != "CVE-2026-0001" {
			t.Fatalf("%s: finding CVEID = %q, want CVE-2026-0001", name, got.CVEID)
		}
		if got.AdvisoryID != "GHSA-2026-0001" {
			t.Fatalf("%s: finding AdvisoryID = %q, want the deciding record GHSA-2026-0001", name, got.AdvisoryID)
		}
		if want := []string{"CVE-2026-0001", "GHSA-2026-0001"}; !slices.Equal(got.AdvisoryGroupMembers, want) {
			t.Fatalf("%s: AdvisoryGroupMembers = %#v, want %#v", name, got.AdvisoryGroupMembers, want)
		}
	}
}

// TestAliasGroupingBridgeCaseIsOrderIndependent is the #10407 item (b)
// regression: GHSA-x -> CVE-2 and GHSA-y -> CVE-1,CVE-2 name one CVE-1 group
// regardless of fact or pass order.
func TestAliasGroupingBridgeCaseIsOrderIndependent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		aliasCVEFact("x-cve", "GHSA-x", "osv", "GHSA-x", "GHSA-x", "CVE-2"),
		aliasAffectedPackageFact("x-pkg", "GHSA-x", "osv", "GHSA-x"),
		aliasCVEFact("y-cve", "GHSA-y", "osv", "GHSA-y", "GHSA-y", "CVE-1", "CVE-2"),
		aliasAffectedPackageFact("y-pkg", "GHSA-y", "osv", "GHSA-y"),
		aliasCVEFact("cve1-cve", "CVE-1", "nvd", "CVE-1", "CVE-1"),
		aliasAffectedPackageFact("cve1-pkg", "CVE-1", "nvd", "CVE-1"),
		aliasCVEFact("cve2-cve", "CVE-2", "nvd", "CVE-2", "CVE-2"),
		aliasAffectedPackageFact("cve2-pkg", "CVE-2", "nvd", "CVE-2"),
		aliasGroupConsumption("cons-bridge"),
	}

	for name, order := range orderings(envelopes) {
		byCVE := aliasGroupFindingsByCVE(t, order)
		if len(byCVE) != 1 {
			t.Fatalf("%s: findings = %d (%v), want exactly 1 CVE-1 finding", name, len(byCVE), sortedFindingKeys(byCVE))
		}
		got := byCVE["CVE-1"]
		if got.CVEID != "CVE-1" {
			t.Fatalf("%s: finding CVEID = %q, want CVE-1", name, got.CVEID)
		}
		if want := []string{"CVE-1", "CVE-2", "GHSA-x", "GHSA-y"}; !slices.Equal(got.AdvisoryGroupMembers, want) {
			t.Fatalf("%s: AdvisoryGroupMembers = %#v, want %#v", name, got.AdvisoryGroupMembers, want)
		}
	}
}

// TestAliasGroupingKeepsDSATwentyUpstreamFindings is the #10407 item (c)
// negative case: a DSA covering 20 upstream CVEs still yields 20 findings
// per package. Upstream edges never merge groups.
func TestAliasGroupingKeepsDSATwentyUpstreamFindings(t *testing.T) {
	t.Parallel()

	const dsaAdvisory = "DSA-2026-0100"
	var upstreams []any
	var envelopes []facts.Envelope
	upstreamPayload := map[string]any{
		"cve_id":      "DEBIAN-CVE-2026-0100",
		"advisory_id": dsaAdvisory,
		"source":      "debian",
		"aliases":     []any{"DEBIAN-CVE-2026-0100", dsaAdvisory},
	}
	for i := 1; i <= 20; i++ {
		cve := fmt.Sprintf("CVE-2026-%04d", i)
		upstreams = append(upstreams, cve)
		envelopes = append(envelopes,
			aliasCVEFact(fmt.Sprintf("nvd-cve-%d", i), cve, "nvd", cve, cve),
			aliasAffectedPackageFact(fmt.Sprintf("nvd-pkg-%d", i), cve, "nvd", cve),
		)
	}
	upstreamPayload["upstream"] = upstreams
	envelopes = append(envelopes,
		facts.Envelope{FactID: "dsa-cve", FactKind: chain.VulnerabilityCVEFactKind, Payload: upstreamPayload},
		aliasAffectedPackageFact("dsa-pkg", "DEBIAN-CVE-2026-0100", "debian", dsaAdvisory),
		aliasGroupConsumption("cons-dsa"),
	)

	for name, order := range orderings(envelopes) {
		byCVE := aliasGroupFindingsByCVE(t, order)
		if len(byCVE) != 20 {
			t.Fatalf("%s: findings = %d (%v), want 20 (one per upstream CVE)", name, len(byCVE), sortedFindingKeys(byCVE))
		}
		for i := 1; i <= 20; i++ {
			cve := fmt.Sprintf("CVE-2026-%04d", i)
			got, ok := byCVE[cve]
			if !ok {
				t.Fatalf("%s: missing finding for %s", name, cve)
			}
			if len(got.AdvisoryGroupMembers) != 0 {
				t.Fatalf("%s: %s AdvisoryGroupMembers = %#v, want none (upstream never merges)", name, cve, got.AdvisoryGroupMembers)
			}
		}
	}
}

// TestAliasGroupingQuarantinesOversizeComponent is the #10407 item (d)
// regression, group-cap half: a 33-member component falls back to per-record
// keys with a counted marker instead of a partial-component group id.
func TestAliasGroupingQuarantinesOversizeComponent(t *testing.T) {
	t.Parallel()

	const target = "CVE-2026-0900"
	var envelopes []facts.Envelope
	envelopes = append(envelopes,
		aliasCVEFact("target-cve", target, "nvd", target, target),
		aliasAffectedPackageFact("target-pkg", target, "nvd", target),
	)
	for i := 1; i <= 32; i++ {
		id := fmt.Sprintf("GHSA-2026-q%02d", i)
		envelopes = append(envelopes,
			aliasCVEFact(fmt.Sprintf("q-cve-%d", i), id, "osv", id, id, target),
			aliasAffectedPackageFact(fmt.Sprintf("q-pkg-%d", i), id, "osv", id),
		)
	}
	envelopes = append(envelopes, aliasGroupConsumption("cons-quarantine"))

	byCVE := aliasGroupFindingsByCVE(t, envelopes)
	if len(byCVE) != 33 {
		t.Fatalf("findings = %d (%v), want 33 per-record fallbacks", len(byCVE), sortedFindingKeys(byCVE))
	}
	for key, finding := range byCVE {
		if len(finding.AdvisoryGroupMembers) != 0 {
			t.Fatalf("%s: AdvisoryGroupMembers = %#v, want none on a quarantined component", key, finding.AdvisoryGroupMembers)
		}
		if !slices.Contains(finding.MissingEvidence, supplyChainMissingAdvisoryGroupSizeCap) {
			t.Fatalf("%s: MissingEvidence = %#v, want the group-size marker %q", key, finding.MissingEvidence, supplyChainMissingAdvisoryGroupSizeCap)
		}
	}
}

// TestAliasGroupingQuarantinesOversizeRecord is the #10407 item (d)
// regression, record-cap half: a record with 6 aliases keeps its singleton
// with a counted marker and contributes no edges.
func TestAliasGroupingQuarantinesOversizeRecord(t *testing.T) {
	t.Parallel()

	aliases := []string{"GHSA-2026-r1", "CVE-2026-1001", "CVE-2026-1002", "CVE-2026-1003", "CVE-2026-1004", "CVE-2026-1005"}
	envelopes := []facts.Envelope{
		aliasCVEFact("big-cve", "GHSA-2026-r1", "osv", "GHSA-2026-r1", aliases...),
		aliasAffectedPackageFact("big-pkg", "GHSA-2026-r1", "osv", "GHSA-2026-r1"),
		aliasCVEFact("other-cve", "CVE-2026-1001", "nvd", "CVE-2026-1001", "CVE-2026-1001"),
		aliasAffectedPackageFact("other-pkg", "CVE-2026-1001", "nvd", "CVE-2026-1001"),
		aliasGroupConsumption("cons-record-cap"),
	}

	byCVE := aliasGroupFindingsByCVE(t, envelopes)
	if len(byCVE) != 2 {
		t.Fatalf("findings = %d (%v), want 2 per-record fallbacks", len(byCVE), sortedFindingKeys(byCVE))
	}
	big, ok := byCVE["GHSA-2026-r1"]
	if !ok {
		t.Fatalf("missing per-record fallback for GHSA-2026-r1 in %v", sortedFindingKeys(byCVE))
	}
	if !slices.Contains(big.MissingEvidence, supplyChainMissingAdvisoryGroupRecordCap) {
		t.Fatalf("MissingEvidence = %#v, want the record-cap marker %q", big.MissingEvidence, supplyChainMissingAdvisoryGroupRecordCap)
	}
	if len(big.AdvisoryGroupMembers) != 0 {
		t.Fatalf("AdvisoryGroupMembers = %#v, want none on a quarantined record", big.AdvisoryGroupMembers)
	}
	other := byCVE["CVE-2026-1001"]
	if slices.Contains(other.MissingEvidence, supplyChainMissingAdvisoryGroupRecordCap) ||
		slices.Contains(other.MissingEvidence, supplyChainMissingAdvisoryGroupSizeCap) {
		t.Fatalf("CVE-2026-1001 MissingEvidence = %#v, want no group marker on the unquarantined peer", other.MissingEvidence)
	}
}

// TestAliasGroupingWithdrawnBridgeContributesNoEdge is the #10407 item (e)
// negative case: a withdrawn record contributes no alias edges. The live
// record aliasing the withdrawn record's id still joins it (same advisory,
// one finding), but the withdrawn record's own alias target never enters the
// component: the CVE-2026-0888 record stays a separate finding.
func TestAliasGroupingWithdrawnBridgeContributesNoEdge(t *testing.T) {
	t.Parallel()

	withdrawn := aliasCVEFact("w-cve", "CVE-2026-0777", "nvd", "CVE-2026-0777", "CVE-2026-0777", "CVE-2026-0888")
	withdrawn.Payload["withdrawn_at"] = "2026-06-01T00:00:00Z"
	envelopes := []facts.Envelope{
		withdrawn,
		aliasAffectedPackageFact("w-pkg", "CVE-2026-0777", "nvd", "CVE-2026-0777"),
		aliasCVEFact("live-cve", "GHSA-2026-w1", "osv", "GHSA-2026-w1", "GHSA-2026-w1", "CVE-2026-0777"),
		aliasAffectedPackageFact("live-pkg", "GHSA-2026-w1", "osv", "GHSA-2026-w1"),
		aliasCVEFact("e-cve", "CVE-2026-0888", "nvd", "CVE-2026-0888", "CVE-2026-0888"),
		aliasAffectedPackageFact("e-pkg", "CVE-2026-0888", "nvd", "CVE-2026-0888"),
		aliasGroupConsumption("cons-withdrawn"),
	}

	for name, order := range orderings(envelopes) {
		byCVE := aliasGroupFindingsByCVE(t, order)
		if len(byCVE) != 2 {
			t.Fatalf("%s: findings = %d (%v), want 2 (withdrawn-joined group plus untouched CVE-2026-0888)", name, len(byCVE), sortedFindingKeys(byCVE))
		}
		grouped, ok := byCVE["CVE-2026-0777"]
		if !ok {
			t.Fatalf("%s: missing CVE-2026-0777 group in %v", name, sortedFindingKeys(byCVE))
		}
		if want := []string{"CVE-2026-0777", "GHSA-2026-w1"}; !slices.Equal(grouped.AdvisoryGroupMembers, want) {
			t.Fatalf("%s: members = %#v, want %#v", name, grouped.AdvisoryGroupMembers, want)
		}
		for key, finding := range byCVE {
			for _, member := range finding.AdvisoryGroupMembers {
				if member == "CVE-2026-0888" {
					t.Fatalf("%s: %s members %#v reach CVE-2026-0888 through the withdrawn record's edge", name, key, finding.AdvisoryGroupMembers)
				}
			}
		}
		if _, ok := byCVE["CVE-2026-0888"]; !ok {
			t.Fatalf("%s: missing untouched CVE-2026-0888 finding in %v", name, sortedFindingKeys(byCVE))
		}
	}
}

// TestAliasGroupingWriterKeysMoveTogether proves the writer/winners/queries
// contract (#10407): a grouped finding's persisted cve_id is the group id,
// its finding_id is stable over member order, and its provenance carries the
// sorted member set for items 3.3/3.4. The SQL copies (CanonicalFindingKeySQL
// and the winners canonical_key) read the same persisted payload keys, so no
// SQL change ships with this PR and the existing parity tests still guard
// them.
func TestAliasGroupingWriterKeysMoveTogether(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		aliasCVEFact("ghsa-cve", "GHSA-2026-0001", "osv", "GHSA-2026-0001", "GHSA-2026-0001"),
		aliasAffectedPackageFact("ghsa-pkg", "GHSA-2026-0001", "osv", "GHSA-2026-0001"),
		aliasCVEFact("nvd-cve", "CVE-2026-0001", "nvd", "CVE-2026-0001", "CVE-2026-0001", "GHSA-2026-0001"),
		aliasAffectedPackageFact("nvd-pkg", "CVE-2026-0001", "nvd", "CVE-2026-0001"),
		aliasGroupConsumption("cons-1"),
	}
	byCVE := aliasGroupFindingsByCVE(t, envelopes)
	finding := byCVE["CVE-2026-0001"]

	identity := supplyChainImpactFindingID(finding)
	if identity == "" {
		t.Fatal("finding identity is empty for a grouped finding")
	}
	for _, order := range orderings(envelopes) {
		again := aliasGroupFindingsByCVE(t, order)["CVE-2026-0001"]
		if againIdentity := supplyChainImpactFindingID(again); againIdentity != identity {
			t.Fatalf("finding identity = %q, want stable %q across fact order", againIdentity, identity)
		}
	}

	write := SupplyChainImpactWrite{
		IntentID:     "test-intent",
		ScopeID:      "repository:supply-chain-demo/example-service",
		GenerationID: "test-generation",
		SourceSystem: "test",
		Cause:        "test",
	}
	payload := supplyChainImpactTypedPayload(write, finding)
	if payload.CVEID != "CVE-2026-0001" {
		t.Fatalf("payload CVEID = %q, want the group id", payload.CVEID)
	}
	if want := supplyChainImpactFindingID(finding); payload.FindingID != want || payload.FindingID == "" {
		t.Fatalf("payload FindingID = %q, want the finding identity %q", payload.FindingID, want)
	}
	members, ok := payload.Provenance["advisory_group_members"]
	if !ok {
		t.Fatalf("payload Provenance keys = %v, want advisory_group_members", keysOf(payload.Provenance))
	}
	got, ok := members.([]string)
	if !ok || !slices.Equal(got, []string{"CVE-2026-0001", "GHSA-2026-0001"}) {
		t.Fatalf("advisory_group_members = %#v, want the sorted member set", members)
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// TestAliasGroupingLowestIDUsesByteOrder pins the ruling's group-id rule where
// byte order and natural order disagree: CVE-2021-9999 sorts after
// CVE-2021-10000 in Go byte order, so a component holding both is keyed
// CVE-2021-10000.
func TestAliasGroupingLowestIDUsesByteOrder(t *testing.T) {
	t.Parallel()

	if got := lowestAdvisoryGroupID([]string{"CVE-2021-9999", "CVE-2021-10000"}); got != "CVE-2021-10000" {
		t.Fatalf("lowestAdvisoryGroupID = %q, want CVE-2021-10000 (Go byte order)", got)
	}
	if got := lowestAdvisoryGroupID([]string{"GHSA-zzzz", "CVE-2021-9999", "GHSA-aaaa"}); got != "CVE-2021-9999" {
		t.Fatalf("lowestAdvisoryGroupID = %q, want the lowest CVE CVE-2021-9999", got)
	}
	if got := lowestAdvisoryGroupID([]string{"GHSA-zzzz", "GHSA-aaaa"}); got != "GHSA-aaaa" {
		t.Fatalf("lowestAdvisoryGroupID = %q, want the lowest advisory id GHSA-aaaa", got)
	}
}

// TestAliasWithdrawnSingletonWithOutsideAliasStaysUngrouped is the #10453
// finding 2 failing regression: a withdrawn singleton's own aliases
// contributed no edges (groupOSAdvisoriesByAlias unions around withdrawn
// records), so an outside alias must not classify it grouped. The record
// keeps its per-record key with no members and no grouped counter increment.
func TestAliasWithdrawnSingletonWithOutsideAliasStaysUngrouped(t *testing.T) {
	t.Parallel()

	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{
				FactID:      "withdrawn-cve",
				CVEID:       "CVE-2026-0777",
				AdvisoryID:  "GHSA-2026-w9",
				Aliases:     []string{"GHSA-2026-w9", "CVE-2026-9999"},
				WithdrawnAt: "2026-06-01T00:00:00Z",
			},
		},
	}

	groupOSAdvisoriesByAlias(index, supplyChainAliasGroupConfig{})

	if len(index.cves) != 1 || index.cves[0].CVEID != "CVE-2026-0777" {
		t.Fatalf("index.cves = %#v, want the withdrawn singleton keyed by its per-record CVE-2026-0777", index.cves)
	}
	if len(index.aliasGroups) != 0 {
		t.Fatalf("aliasGroups = %#v, want no grouped entry (and no members) for a withdrawn singleton", index.aliasGroups)
	}
	if got := index.aliasStats.cells[supplyChainAliasGroupKey{result: aliasGroupResultGrouped}]; got != 0 {
		t.Fatalf("grouped counter = %d, want 0 for a withdrawn singleton", got)
	}
	if got := index.aliasStats.cells[supplyChainAliasGroupKey{result: aliasGroupResultUngrouped}]; got != 1 {
		t.Fatalf("ungrouped counter = %d, want 1 for a withdrawn singleton", got)
	}
}

// TestAliasAllowByAliasExemptsRecordCap is the #10453 finding 3 failing
// regression: the allow list exempts a record from the record cap when any
// member id is listed, so a listed alias must lift the quarantine like a
// listed advisory or CVE id does.
func TestAliasAllowByAliasExemptsRecordCap(t *testing.T) {
	t.Parallel()

	aliases := []string{"GHSA-2026-r1", "CVE-2026-1001", "CVE-2026-1002", "CVE-2026-1003", "CVE-2026-1004", "CVE-2026-1005"}
	index := &supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{
				FactID:     "big-cve",
				CVEID:      "GHSA-2026-r1",
				AdvisoryID: "GHSA-2026-r1",
				Aliases:    aliases,
			},
		},
	}
	config := supplyChainAliasGroupConfigFromLists([]string{"CVE-2026-1003"}, nil)

	groupOSAdvisoriesByAlias(index, config)

	if got := index.aliasStats.cells[supplyChainAliasGroupKey{result: aliasGroupResultRecordCap}]; got != 0 {
		t.Fatalf("record-cap counter = %d, want 0 for an allow-by-alias record", got)
	}
	for key, group := range index.aliasGroups {
		if group.missingEvidence == supplyChainMissingAdvisoryGroupRecordCap {
			t.Fatalf("aliasGroups[%q] carries the record-cap marker, want none for an allow-by-alias record", key)
		}
	}
	if got := index.aliasStats.cells[supplyChainAliasGroupKey{result: aliasGroupResultGrouped}]; got != 1 {
		t.Fatalf("grouped counter = %d, want 1: the exempt record still groups over its alias edges", got)
	}
}
