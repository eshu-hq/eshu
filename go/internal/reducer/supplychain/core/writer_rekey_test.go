// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// This file pins the L1 writer-core contract for re-keying on advisory group
// merge/split by re-derivation (issue #10408, criterion 3.3): the writer
// names the touched advisory-group set (old plus new group ids, sorted and
// de-duplicated, per the merged #10411 contract the graph projection
// consumes) and unions member ids into the changed and recovery CVE sets
// that feed the holder fan-out. The re-key itself happens upstream by
// re-derivation (alias grouping lands the AdvisoryGroupMembers sets); these
// tests hold the writer boundary only: what the commit reports and persists,
// never the grouping decision.

const rekeyTestScope = "vuln-intel://osv/npm/rekey-example"

// rekeyFinding builds a finding keyed by cveID with the given advisory group
// members, on top of the shared changed-test fixture.
func rekeyFinding(cveID string, members ...string) SupplyChainImpactFinding {
	finding := changedTestFinding(cveID)
	finding.AdvisoryGroupMembers = append([]string(nil), members...)
	return finding
}

// rekeyWrite builds a write for one generation of the rekey scope.
func rekeyWrite(generation string, findings ...SupplyChainImpactFinding) SupplyChainImpactWrite {
	write := changedTestWrite(findings...)
	write.ScopeID = rekeyTestScope
	write.GenerationID = generation
	return write
}

// rekeyPriorRow returns the fake stored row the prior generation left for
// finding, computed against the prior generation's write so the fact id,
// stable key, and digest match what that pass committed.
func rekeyPriorRow(prior SupplyChainImpactWrite, finding SupplyChainImpactFinding) fakeStoredFinding {
	return fakeStoredFinding{
		factID:       supplyChainImpactFactID(prior, finding),
		generationID: prior.GenerationID,
		stableKey:    supplyChainImpactStableFactKey(prior, finding),
		digest:       supplyChainImpactContentDigest(prior, finding),
		cveID:        finding.CVEID,
		fencingToken: 5,
		members:      append([]string(nil), finding.AdvisoryGroupMembers...),
	}
}

// rekeyDecodeInserted decodes every row the write under test upserted.
func rekeyDecodeInserted(t *testing.T, db *testutil.FakeExecer) []map[string]any {
	t.Helper()
	rows := testutil.DecodeBatchedVersionedFactCalls(t, db.Execs)
	out := make([]map[string]any, 0, len(rows))
	for i, row := range rows {
		var payload map[string]any
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatalf("inserted row %d payload is not JSON: %v", i, err)
		}
		out = append(out, payload)
	}
	return out
}

// rekeyPayloadCVEIDs returns the cve_id of every decoded inserted payload.
func rekeyPayloadCVEIDs(t *testing.T, payloads []map[string]any) []string {
	t.Helper()
	out := make([]string, 0, len(payloads))
	for i, payload := range payloads {
		id, _ := payload["cve_id"].(string)
		if id == "" {
			t.Fatalf("inserted row %d payload has no cve_id: %v", i, payload["cve_id"])
		}
		out = append(out, id)
	}
	return out
}

// rekeyPayloadMembers returns the provenance advisory_group_members of one
// decoded inserted payload, or nil when the finding carries none.
func rekeyPayloadMembers(t *testing.T, payload map[string]any) []string {
	t.Helper()
	provenance, _ := payload["provenance"].(map[string]any)
	if provenance == nil {
		return nil
	}
	raw, ok := provenance["advisory_group_members"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, member := range raw {
		id, _ := member.(string)
		if id == "" {
			t.Fatalf("advisory_group_members holds a non-string: %v", member)
		}
		out = append(out, id)
	}
	return out
}

// rekeyRetractKeepSet returns the fact-id keep set of the retraction
// statement the write under test executed: the rows the pass just wrote.
func rekeyRetractKeepSet(t *testing.T, beginner fakeImpactBeginner) []string {
	t.Helper()
	for _, call := range beginner.state.all {
		if call.Query != retractSupersededSupplyChainImpactFindingsQuery {
			continue
		}
		keep, ok := call.Args[3].([]string)
		if !ok {
			t.Fatalf("retraction keep set has type %T, want []string", call.Args[3])
		}
		return keep
	}
	t.Fatalf("write executed no retraction statement")
	return nil
}

// TestWriterRekeyLateAliasMerge pins criterion 1a: a prior generation held
// two separate groups and the new pass re-derives one finding keyed by the
// survivor with both members. The commit reports both ids and persists the
// merged member set on the survivor's row.
func TestWriterRekeyLateAliasMerge(t *testing.T) {
	t.Parallel()

	prior := rekeyWrite("gen-rekey-prior",
		rekeyFinding("CVE-2026-0101", "CVE-2026-0101"),
		rekeyFinding("CVE-2026-0102", "CVE-2026-0102"),
	)
	merged := rekeyFinding("CVE-2026-0101", "CVE-2026-0101", "CVE-2026-0102")
	current := rekeyWrite("gen-rekey-current", merged)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{
		rekeyPriorRow(prior, prior.Findings[0]),
		rekeyPriorRow(prior, prior.Findings[1]),
	}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), current)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	want := []string{"CVE-2026-0101", "CVE-2026-0102"}
	if !reflect.DeepEqual(result.ChangedCVEIDs, want) {
		t.Fatalf("ChangedCVEIDs = %v, want %v (survivor changed, dead id retracted)", result.ChangedCVEIDs, want)
	}
	if !reflect.DeepEqual(result.TouchedGroupIDs, want) {
		t.Fatalf("TouchedGroupIDs = %v, want %v (both priors plus survivor)", result.TouchedGroupIDs, want)
	}

	payloads := rekeyDecodeInserted(t, beginner.inserts)
	if len(payloads) != 1 {
		t.Fatalf("inserted rows = %d, want 1 (the survivor only)", len(payloads))
	}
	if got := rekeyPayloadCVEIDs(t, payloads); !reflect.DeepEqual(got, []string{"CVE-2026-0101"}) {
		t.Fatalf("inserted cve_ids = %v, want the survivor only", got)
	}
	if got := rekeyPayloadMembers(t, payloads[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("survivor members = %v, want %v", got, want)
	}
}

// TestWriterRekeyTwoGroupMergeDeadIDHasZeroRows pins criterion 1b: the merged
// pass commits no row for the dead group id (the survivor is the lower id)
// while still reporting it, so the retraction retires it and the fan-out
// reopens its holders.
func TestWriterRekeyTwoGroupMergeDeadIDHasZeroRows(t *testing.T) {
	t.Parallel()

	const survivor, dead = "CVE-2026-0201", "CVE-2026-0202"
	prior := rekeyWrite("gen-rekey-prior",
		rekeyFinding(survivor, survivor),
		rekeyFinding(dead, dead),
	)
	current := rekeyWrite("gen-rekey-current", rekeyFinding(survivor, survivor, dead))
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{
		rekeyPriorRow(prior, prior.Findings[0]),
		rekeyPriorRow(prior, prior.Findings[1]),
	}
	deadFactID := supplyChainImpactFactID(prior, prior.Findings[1])

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), current)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	for _, id := range rekeyPayloadCVEIDs(t, rekeyDecodeInserted(t, beginner.inserts)) {
		if id == dead {
			t.Fatalf("inserted a row for the dead id %s: a merged pass commits zero dead-id rows", dead)
		}
	}
	for _, kept := range rekeyRetractKeepSet(t, beginner) {
		if kept == deadFactID {
			t.Fatalf("retraction keep set holds dead fact %s: the dead row must retire", deadFactID)
		}
	}
	for _, id := range result.TouchedGroupIDs {
		if id == dead {
			return
		}
	}
	t.Fatalf("TouchedGroupIDs = %v, want the dead id %s present although the committer held no dead-id rows", result.TouchedGroupIDs, dead)
}

// TestWriterRekeyAliasRemovalSplit pins criterion 1c: the alias that joined
// two CVEs is removed, so the new pass re-derives one winner per fragment.
// Both fragments commit, and the touched set names the old group plus every
// fragment.
func TestWriterRekeyAliasRemovalSplit(t *testing.T) {
	t.Parallel()

	prior := rekeyWrite("gen-rekey-prior",
		rekeyFinding("CVE-2026-0301", "CVE-2026-0301", "CVE-2026-0302"),
	)
	current := rekeyWrite("gen-rekey-current",
		rekeyFinding("CVE-2026-0301", "CVE-2026-0301"),
		rekeyFinding("CVE-2026-0302", "CVE-2026-0302"),
	)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{rekeyPriorRow(prior, prior.Findings[0])}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), current)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	want := []string{"CVE-2026-0301", "CVE-2026-0302"}
	if !reflect.DeepEqual(result.ChangedCVEIDs, want) {
		t.Fatalf("ChangedCVEIDs = %v, want %v (one winner per fragment)", result.ChangedCVEIDs, want)
	}
	if !reflect.DeepEqual(result.TouchedGroupIDs, want) {
		t.Fatalf("TouchedGroupIDs = %v, want %v (old group plus every fragment)", result.TouchedGroupIDs, want)
	}
	if got := rekeyPayloadCVEIDs(t, rekeyDecodeInserted(t, beginner.inserts)); !reflect.DeepEqual(got, want) {
		t.Fatalf("inserted cve_ids = %v, want %v (one row per fragment)", got, want)
	}
}

// TestWriterRekeyWithdrawalSplitRetractedFragment pins criterion 1d: one
// fragment's advisory is withdrawn, so its re-derived finding carries the
// retraction block and reads the advisory_retracted suppression state while
// keeping its identity.
func TestWriterRekeyWithdrawalSplitRetractedFragment(t *testing.T) {
	t.Parallel()

	prior := rekeyWrite("gen-rekey-prior",
		rekeyFinding("CVE-2026-0401", "CVE-2026-0401", "CVE-2026-0402"),
	)
	live := rekeyFinding("CVE-2026-0401", "CVE-2026-0401")
	withdrawn := rekeyFinding("CVE-2026-0402", "CVE-2026-0402")
	withdrawn.Suppression = SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateAdvisoryRetracted}
	withdrawn.AdvisoryRetraction = &AdvisoryRetraction{
		ReasonCode: AdvisoryRetractionWithdrawn,
		Reason:     "advisory withdrawn by the issuing authority",
		Sources: []AdvisoryRetractionSource{{
			Source:     "osv",
			AdvisoryID: "GHSA-0402-test",
			Code:       AdvisoryRetractionWithdrawn,
		}},
	}
	current := rekeyWrite("gen-rekey-current", live, withdrawn)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{rekeyPriorRow(prior, prior.Findings[0])}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), current)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	wantChanged := []string{"CVE-2026-0401", "CVE-2026-0402"}
	if !reflect.DeepEqual(result.ChangedCVEIDs, wantChanged) {
		t.Fatalf("ChangedCVEIDs = %v, want %v (live fragment re-keyed, withdrawn fragment retracted)", result.ChangedCVEIDs, wantChanged)
	}
	found := false
	for _, id := range result.TouchedGroupIDs {
		if id == "CVE-2026-0402" {
			found = true
		}
	}
	if !found {
		t.Fatalf("TouchedGroupIDs = %v, want the withdrawn fragment present", result.TouchedGroupIDs)
	}

	payloads := rekeyDecodeInserted(t, beginner.inserts)
	if len(payloads) != 2 {
		t.Fatalf("inserted rows = %d, want 2 (live plus withdrawn fragments)", len(payloads))
	}
	var withdrawnPayload map[string]any
	for _, payload := range payloads {
		if id, _ := payload["cve_id"].(string); id == "CVE-2026-0402" {
			withdrawnPayload = payload
		}
	}
	if withdrawnPayload == nil {
		t.Fatalf("no inserted row for the withdrawn fragment CVE-2026-0402")
	}
	if state, _ := withdrawnPayload["suppression_state"].(string); state != string(SupplyChainSuppressionStateAdvisoryRetracted) {
		t.Fatalf("withdrawn suppression_state = %q, want %q", state, SupplyChainSuppressionStateAdvisoryRetracted)
	}
	provenance, _ := withdrawnPayload["provenance"].(map[string]any)
	retraction, _ := provenance["advisory_retraction"].(map[string]any)
	if code, _ := retraction["reason_code"].(string); code != AdvisoryRetractionWithdrawn {
		t.Fatalf("withdrawn reason_code = %q, want %q", code, AdvisoryRetractionWithdrawn)
	}
}

// TestWriterRekeyMemberOnlyRefresh pins criterion 1e: the group keeps its key
// while its membership refreshes. The finding commits under the same stable
// key with the refreshed members, and the unchanged id is still reported.
func TestWriterRekeyMemberOnlyRefresh(t *testing.T) {
	t.Parallel()

	prior := rekeyWrite("gen-rekey-prior",
		rekeyFinding("CVE-2026-0501", "CVE-2026-0501", "CVE-2026-0502"),
	)
	refreshed := rekeyFinding("CVE-2026-0501", "CVE-2026-0501", "CVE-2026-0503")
	current := rekeyWrite("gen-rekey-current", refreshed)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{rekeyPriorRow(prior, prior.Findings[0])}

	if supplyChainImpactStableFactKey(current, refreshed) != supplyChainImpactStableFactKey(prior, prior.Findings[0]) {
		t.Fatalf("stable key moved on a member-only refresh: the finding must keep its key")
	}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), current)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	found := false
	for _, id := range result.ChangedCVEIDs {
		if id == "CVE-2026-0501" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ChangedCVEIDs = %v, want the unchanged id CVE-2026-0501 (refreshed members are a change)", result.ChangedCVEIDs)
	}
	found = false
	for _, id := range result.TouchedGroupIDs {
		if id == "CVE-2026-0501" {
			found = true
		}
	}
	if !found {
		t.Fatalf("TouchedGroupIDs = %v, want the unchanged id CVE-2026-0501", result.TouchedGroupIDs)
	}

	payloads := rekeyDecodeInserted(t, beginner.inserts)
	if len(payloads) != 1 {
		t.Fatalf("inserted rows = %d, want 1 (same key, refreshed members)", len(payloads))
	}
	wantMembers := []string{"CVE-2026-0501", "CVE-2026-0503"}
	if got := rekeyPayloadMembers(t, payloads[0]); !reflect.DeepEqual(got, wantMembers) {
		t.Fatalf("refreshed members = %v, want %v", got, wantMembers)
	}
}

// TestTouchedGroupIDsRekeyContract pins the criterion-2 touched-set unit
// contract: merge emits both priors plus the survivor, split emits the old
// group plus every fragment, a member-only refresh emits the unchanged id, a
// retire-only pass emits the old id, no change emits empty, output is sorted
// and de-duplicated, and a dead id is present even when the committer held
// no dead-id rows.
func TestTouchedGroupIDsRekeyContract(t *testing.T) {
	t.Parallel()

	row := func(factID, stableKey, digest, cveID string, members ...string) changedRow {
		return changedRow{factID: factID, stableKey: stableKey, digest: digest, cveID: cveID, members: members}
	}
	stored := func(factID, generation, stableKey, digest, cveID string, members ...string) storedFinding {
		return storedFinding{factID: factID, generationID: generation, stableKey: stableKey, digest: digest, cveID: cveID, fencingToken: 5, members: members}
	}
	tests := []struct {
		name    string
		rows    []changedRow
		stored  []storedFinding
		gen     string
		retract bool
		want    []string
	}{
		{
			name: "merge emits both priors plus survivor",
			rows: []changedRow{row("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0602")},
			stored: []storedFinding{
				stored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601"),
				stored("old-b", "gen-old", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: []string{"CVE-2026-0601", "CVE-2026-0602"},
		},
		{
			name: "split emits old group plus every fragment",
			rows: []changedRow{
				row("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601"),
				row("new-b", "k-b", "d-new", "CVE-2026-0602", "CVE-2026-0602"),
			},
			stored: []storedFinding{
				stored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: []string{"CVE-2026-0601", "CVE-2026-0602"},
		},
		{
			name: "member-only refresh emits the unchanged id",
			rows: []changedRow{row("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0603")},
			stored: []storedFinding{
				stored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: []string{"CVE-2026-0601"},
		},
		{
			name: "retire-only emits the old id",
			rows: nil,
			stored: []storedFinding{
				stored("gone", "gen", "k-gone", "d-old", "CVE-2026-0604", "CVE-2026-0604"),
			},
			gen: "gen", retract: true,
			want: []string{"CVE-2026-0604"},
		},
		{
			name: "no change emits empty",
			rows: []changedRow{row("same-a", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601")},
			stored: []storedFinding{
				stored("same-a", "gen", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601"),
			},
			gen: "gen", retract: true,
			want: nil,
		},
		{
			name: "output is sorted and de-duplicated",
			rows: []changedRow{row("new-b", "k-b", "d-new", "CVE-2026-0602", "CVE-2026-0602", "CVE-2026-0601", "CVE-2026-0602")},
			stored: []storedFinding{
				stored("old-b", "gen-old", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: []string{"CVE-2026-0602"},
		},
		{
			name: "dead id present although the committer held no dead-id rows",
			rows: []changedRow{row("same-a", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601")},
			stored: []storedFinding{
				stored("same-a", "gen", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601"),
				stored("dead-b", "gen", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen", retract: true,
			want: []string{"CVE-2026-0602"},
		},
		{
			name: "partial evidence stays upsert-only",
			rows: []changedRow{row("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601")},
			stored: []storedFinding{
				stored("dead-b", "gen", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen", retract: false,
			want: []string{"CVE-2026-0601"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := touchedGroupIDs(tc.rows, tc.stored, tc.gen, 7, tc.retract); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("touchedGroupIDs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestChangedCVEIDsUnionsAdvisoryGroupMembers pins the criterion-2 changed
// member union: a new row contributes its member ids beyond its own key
// (including a non-minimum member), a retracted stored row contributes its
// members, and a member-less row falls back to its singleton id even when
// that id is not the minimum of the set.
func TestChangedCVEIDsUnionsAdvisoryGroupMembers(t *testing.T) {
	t.Parallel()

	t.Run("new row members beyond its key", func(t *testing.T) {
		t.Parallel()
		rows := []changedRow{{
			factID: "new-a", stableKey: "k-a", digest: "d-new",
			cveID:   "CVE-2026-0702",
			members: []string{"CVE-2026-0702", "CVE-2026-0705"},
		}}
		want := []string{"CVE-2026-0702", "CVE-2026-0705"}
		if got := changedCVEIDs(rows, nil, "gen-new", 7, true); !reflect.DeepEqual(got, want) {
			t.Fatalf("changedCVEIDs() = %v, want %v (non-minimum member named)", got, want)
		}
	})

	t.Run("refreshed members on same key", func(t *testing.T) {
		t.Parallel()
		rows := []changedRow{{
			factID: "new-a", stableKey: "k-a", digest: "d-new", cveID: "CVE-2026-0601",
			members: []string{"CVE-2026-0601", "CVE-2026-0603"},
		}}
		stored := []storedFinding{{
			factID: "old-a", generationID: "gen-old", stableKey: "k-a", digest: "d-old",
			cveID: "CVE-2026-0601", fencingToken: 5,
			members: []string{"CVE-2026-0601", "CVE-2026-0602"},
		}}
		want := []string{"CVE-2026-0601", "CVE-2026-0602", "CVE-2026-0603"}
		if got := changedCVEIDs(rows, stored, "gen-new", 7, true); !reflect.DeepEqual(got, want) {
			t.Fatalf("changedCVEIDs() = %v, want %v (old plus new members on refresh)", got, want)
		}
	})

	t.Run("retracted stored members and fallback singleton", func(t *testing.T) {
		t.Parallel()
		rows := []changedRow{{
			factID: "new-b", stableKey: "k-b", digest: "d-new", cveID: "CVE-2026-0709",
		}}
		stored := []storedFinding{{
			factID: "old-a", generationID: "gen-old", stableKey: "k-a", digest: "d-old",
			cveID: "CVE-2026-0701", fencingToken: 5,
			members: []string{"CVE-2026-0701", "CVE-2026-0702"},
		}}
		want := []string{"CVE-2026-0701", "CVE-2026-0702", "CVE-2026-0709"}
		if got := changedCVEIDs(rows, stored, "gen-new", 7, true); !reflect.DeepEqual(got, want) {
			t.Fatalf("changedCVEIDs() = %v, want %v (stored members plus fallback singleton)", got, want)
		}
	})
}

// TestExpandSuppressionCVEIDsThroughMembers pins the suppression member
// union: a suppression naming a grouped CVE widens to every member of each
// group that names it, unknown ids pass through, and blanks drop.
func TestExpandSuppressionCVEIDsThroughMembers(t *testing.T) {
	t.Parallel()

	rows := []changedRow{{
		factID: "new-a", stableKey: "k-a", digest: "d-new",
		cveID:   "CVE-2026-0801",
		members: []string{"CVE-2026-0801", "CVE-2026-0802"},
	}}
	stored := []storedFinding{{
		factID: "old-c", generationID: "gen-old", stableKey: "k-c", digest: "d-old",
		cveID: "CVE-2026-0803", fencingToken: 5,
		members: []string{"CVE-2026-0803", "CVE-2026-0804"},
	}}
	tests := []struct {
		name string
		ids  []string
		want []string
	}{
		{"new row group widens", []string{"CVE-2026-0801"}, []string{"CVE-2026-0801", "CVE-2026-0802"}},
		{"stored group widens", []string{"CVE-2026-0804"}, []string{"CVE-2026-0803", "CVE-2026-0804"}},
		{"unknown id passes through", []string{"CVE-2026-0899"}, []string{"CVE-2026-0899"}},
		{"blanks drop", []string{"", "CVE-2026-0802"}, []string{"CVE-2026-0801", "CVE-2026-0802"}},
		{"empty stays empty", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := expandSuppressionCVEIDsThroughMembers(tc.ids, rows, stored); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expandSuppressionCVEIDsThroughMembers(%v) = %v, want %v", tc.ids, got, tc.want)
			}
		})
	}
}

// TestParseAdvisoryGroupMembers pins the stored member-set decode: present
// arrays decode, absent and empty read as nil, and malformed JSON fails the
// pass instead of silently narrowing the fan-out.
func TestParseAdvisoryGroupMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{"array decodes", `["CVE-2026-0901","CVE-2026-0902"]`, []string{"CVE-2026-0901", "CVE-2026-0902"}, false},
		{"empty array reads nil", `[]`, nil, false},
		{"blank reads nil", ``, nil, false},
		{"null reads nil", `null`, nil, false},
		{"blanks drop", `["CVE-2026-0901",""," "]`, []string{"CVE-2026-0901"}, false},
		{"malformed fails", `not-json`, nil, true},
		{"object fails", `{"cve_id":"CVE-2026-0901"}`, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseAdvisoryGroupMembers(tc.raw)
			if tc.wantErr != (err != nil) {
				t.Fatalf("parseAdvisoryGroupMembers(%q) error = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseAdvisoryGroupMembers(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestRecoveryCVEIDsUnionMembers pins the criterion-2 recovery member union:
// a retry that lists the scope's stored CVE ids also names the stored member
// sets, so the rerun's own rewrite cannot hide a group the dead attempt left
// behind.
func TestRecoveryCVEIDsUnionMembers(t *testing.T) {
	t.Parallel()

	write := rekeyWrite("gen-rekey-current", rekeyFinding("CVE-2026-1001", "CVE-2026-1001"))
	write.Recovery = true
	writer, beginner := changedTestWriter()
	beginner.state.cveRows = []string{"CVE-2026-1001"}
	beginner.state.cveMembers = map[string][]string{
		"CVE-2026-1001": {"CVE-2026-1001", "CVE-2026-1002"},
	}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	want := []string{"CVE-2026-1001", "CVE-2026-1002"}
	if !reflect.DeepEqual(result.RecoveryCVEIDs, want) {
		t.Fatalf("RecoveryCVEIDs = %v, want %v (stored members unioned)", result.RecoveryCVEIDs, want)
	}
}
