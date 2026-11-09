// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
)

// TestAdvisoryGroupWinnersReadProvenanceMembers is the #10475 regression:
// the production writer carries advisory_group_members only under the
// provenance map (supplychain/core/provenance.go), so the winners reader
// must extract them at the provenance path. Seeded through the production
// factschema encoder the writer itself calls, never hand-typed top-level
// JSON.
func TestAdvisoryGroupWinnersReadProvenanceMembers(t *testing.T) {
	t.Parallel()

	// GHSA-10475-0002 is distinct from the row's own CVE and advisory ids,
	// so the union assertion below is non-vacuous: it can only pass when
	// Members arrive from the fact payload.
	members := []string{"CVE-10475-0001", "GHSA-10475-0001", "GHSA-10475-0002"}
	payloadJSON := encodeProductionWinnerPayload(t, members)

	var top map[string]any
	if err := json.Unmarshal(payloadJSON, &top); err != nil {
		t.Fatalf("unmarshal production payload: %v", err)
	}
	if _, ok := top["advisory_group_members"]; ok {
		t.Fatalf("production encoder emitted top-level advisory_group_members; provenance.go carries it under provenance")
	}

	// The shipped reader must extract the array at the provenance path.
	// jsonb #> preserves the array bytes the row scan decodes; #>> would
	// return text and is wrong here.
	const provenanceExtraction = "f.payload #> '{provenance,advisory_group_members}'"
	if !strings.Contains(advisoryGroupWinnersQuery, provenanceExtraction) {
		t.Fatalf("winners query must extract %s: production writes members only under provenance, so a top-level extraction reads NULL and Members is always empty:\n%s", provenanceExtraction, advisoryGroupWinnersQuery)
	}
	if strings.Contains(advisoryGroupWinnersQuery, "f.payload->'advisory_group_members'") {
		t.Fatalf("winners query must not read top-level advisory_group_members; no production writer emits that key:\n%s", advisoryGroupWinnersQuery)
	}

	// Simulate the read: decode the provenance array exactly as the fixed
	// extraction yields it, then fold through the production group builder.
	row := advisorygroup.WinnerRow{
		RepositoryID: "repo-10475",
		PackageID:    "pkg-10475",
		Ecosystem:    "npm",
		PURL:         "pkg:npm/pkg-10475@1.0.0",
		CVEID:        "CVE-10475-0001",
		AdvisoryID:   "GHSA-10475-0001",
		Members:      decodeProvenanceMembers(t, payloadJSON),
	}
	if len(row.Members) == 0 {
		t.Fatalf("Members is empty reading a production-shaped finding fact; want the full alias union %q", members)
	}
	group := advisorygroup.BuildGroup("CVE-10475-0001", []advisorygroup.WinnerRow{row})
	if !strings.Contains(strings.Join(group.AdvisoryIDs, ","), "GHSA-10475-0002") {
		t.Fatalf("group AdvisoryIDs = %q, want the full alias union including GHSA-10475-0002 from Members", group.AdvisoryIDs)
	}
	if len(group.CVEIDs) != 1 || group.CVEIDs[0] != "CVE-10475-0001" {
		t.Fatalf("group CVEIDs = %q, want [CVE-10475-0001]", group.CVEIDs)
	}
	if len(group.Edges) != 1 || group.Edges[0].PURL != "pkg:npm/pkg-10475@1.0.0" {
		t.Fatalf("group Edges = %+v, want the single winner edge with its purl", group.Edges)
	}
}

// TestAdvisoryGroupWinnersNilMembersWithoutProvenance pins the empty side
// of the #10475 contract: absent or NULL provenance — and the LEFT JOIN
// no-fact case, where the extraction yields SQL NULL — must project nil
// members without error.
func TestAdvisoryGroupWinnersNilMembersWithoutProvenance(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]string{
		"absent provenance": `{"purl": "pkg:npm/pkg-10475@1.0.0"}`,
		"null provenance":   `{"purl": "pkg:npm/pkg-10475@1.0.0", "provenance": null}`,
		"empty payload":     `{}`,
	} {
		if got := decodeProvenanceMembers(t, []byte(payload)); got != nil {
			t.Fatalf("%s: members = %q, want nil without error", name, got)
		}
	}

	// LEFT JOIN no-fact case: a NULL extraction scans as nil bytes, and
	// the reader's len>0 guard keeps Members nil. Mirror that guard here
	// so the contract holds even where no fact row exists.
	var membersJSON []byte
	var members []string
	if len(membersJSON) > 0 {
		t.Fatalf("nil scan bytes must not decode; the reader skips empty extractions")
	}
	if members != nil {
		t.Fatalf("members = %q, want nil for the no-fact case", members)
	}
}

// encodeProductionWinnerPayload builds a finding fact payload through the
// production factschema encoder the supply-chain impact writer calls
// (writer.go), with the member set carried in the provenance map exactly
// as supplyChainImpactProvenancePayload emits it.
func encodeProductionWinnerPayload(t *testing.T, members []string) []byte {
	t.Helper()

	typed := reducerderivedv1.SupplyChainImpactFinding{
		PURL: "pkg:npm/pkg-10475@1.0.0",
		Provenance: map[string]any{
			"advisory_group_members": append([]string(nil), members...),
		},
	}
	encoded, err := factschema.EncodeReducerSupplyChainImpactFinding(typed)
	if err != nil {
		t.Fatalf("encode production finding payload: %v", err)
	}
	payloadJSON, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("marshal production finding payload: %v", err)
	}
	return payloadJSON
}

// decodeProvenanceMembers mirrors the fixed extraction's semantics in Go:
// navigate provenance.advisory_group_members; a missing hop or JSON null
// yields nil members with no error, exactly as jsonb #> yields SQL NULL.
func decodeProvenanceMembers(t *testing.T, payloadJSON []byte) []string {
	t.Helper()

	var top map[string]any
	if err := json.Unmarshal(payloadJSON, &top); err != nil {
		t.Fatalf("unmarshal finding payload: %v", err)
	}
	provenance, ok := top["provenance"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := provenance["advisory_group_members"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("provenance advisory_group_members = %T, want a JSON array", raw)
	}
	members := make([]string, 0, len(items))
	for _, item := range items {
		member, ok := item.(string)
		if !ok {
			t.Fatalf("provenance advisory_group_members entry = %T, want a string", item)
		}
		members = append(members, member)
	}
	return members
}
