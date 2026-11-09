// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"
)

// TestTouchedGroupIDsJoinTheHolderReadSet proves the L2 fan-out contract
// (#10408): a genuine pass fans out for the union of its member-expanded
// changed set and its touched group set, so scopes holding retired members
// reopen. Before L2 the read carried only the changed set.
func TestTouchedGroupIDsJoinTheHolderReadSet(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		ChangedCVEIDs:   []string{"CVE-2026-0401"},
		TouchedGroupIDs: []string{"CVE-2026-0401", "CVE-2026-0402", "GHSA-0401-bbbb"},
	}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := [][]string{{"CVE-2026-0401", "CVE-2026-0402", "GHSA-0401-bbbb"}}
	if !reflect.DeepEqual(port.findCalls, want) {
		t.Fatalf("FindHolderScopes cve ids = %v, want the changed/touched union %v", port.findCalls, want)
	}
}

// TestRekeySetsReachHoldersByUnion pins criterion 3's reachability inputs:
// the dead group id of a merge, the non-minimum member behind a quarantined
// fallback-singleton row, and member-expanded changed ids all reach
// FindHolderScopes. A holder keyed by any of them is found by the cve_id arm.
func TestRekeySetsReachHoldersByUnion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		result SupplyChainImpactWriteResult
		want   [][]string
	}{
		{
			name: "dead group id rides the touched set although no new row names it",
			result: SupplyChainImpactWriteResult{
				ChangedCVEIDs:   []string{"CVE-2026-0401"},
				TouchedGroupIDs: []string{"CVE-2026-0401", "CVE-2026-0402"},
			},
			want: [][]string{{"CVE-2026-0401", "CVE-2026-0402"}},
		},
		{
			name: "non-minimum member behind a quarantined singleton rides the touched set",
			result: SupplyChainImpactWriteResult{
				ChangedCVEIDs:   []string{"CVE-2026-0401"},
				TouchedGroupIDs: []string{"CVE-2026-0401", "GHSA-0401-zzzz"},
			},
			want: [][]string{{"CVE-2026-0401", "GHSA-0401-zzzz"}},
		},
		{
			name: "member-expanded changed set survives the union",
			result: SupplyChainImpactWriteResult{
				ChangedCVEIDs:   []string{"CVE-2026-0401", "GHSA-0401-bbbb"},
				TouchedGroupIDs: []string{"CVE-2026-0401"},
			},
			want: [][]string{{"CVE-2026-0401", "GHSA-0401-bbbb"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			port := &stubFanOut{holders: []string{"holder-a"}}
			writer := &changeWriter{result: tc.result}
			if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if !reflect.DeepEqual(port.findCalls, tc.want) {
				t.Fatalf("FindHolderScopes cve ids = %v, want %v", port.findCalls, tc.want)
			}
		})
	}
}

// TestStampedPassIgnoresTouchedGroupIDs pins the one-hop rule for re-key
// (#10408 D3): a stamped re-derivation fans out nothing of its own rewrite
// except the suppression diff's ids. The touched set stays home like the
// changed set, so a re-key wave ends after one hop.
func TestStampedPassIgnoresTouchedGroupIDs(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		ChangedCVEIDs:     []string{"CVE-2026-0401"},
		TouchedGroupIDs:   []string{"CVE-2026-0401", "CVE-2026-0402"},
		SuppressionCVEIDs: []string{"CVE-2026-0401"},
	}}
	payload := map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope": "src"}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := [][]string{{"CVE-2026-0401"}}
	if !reflect.DeepEqual(port.findCalls, want) {
		t.Fatalf("FindHolderScopes cve ids = %v, want only the suppression set %v", port.findCalls, want)
	}
}

// TestOwedRetryUnionsTouchedWithRecovery pins the crash-recovery union
// (#10408): a retry that owes fan-out fans out for the recovery set plus the
// touched set, so a commit-then-crash re-key still reaches every holder.
func TestOwedRetryUnionsTouchedWithRecovery(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		holders: []string{"holder-a"},
		payload: map[string]any{PayloadKeyFanOutOwed: true},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		RecoveryCVEIDs:  []string{"CVE-2026-0401", "GHSA-0401-bbbb"},
		TouchedGroupIDs: []string{"CVE-2026-0401", "CVE-2026-0402"},
	}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(2, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := [][]string{{"CVE-2026-0401", "CVE-2026-0402", "GHSA-0401-bbbb"}}
	if !reflect.DeepEqual(port.findCalls, want) {
		t.Fatalf("FindHolderScopes cve ids = %v, want the recovery/touched union %v", port.findCalls, want)
	}
}
