// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenGovernanceAuditReadAdmission pins the exact method set behind
// the versioned #9669 D1 admission contract for the governance-audit
// reads backing the tenant-admin audit links. A renamed, added,
// removed, or re-signed method fails here until that list is
// deliberately updated; bump GovernanceAuditReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: D3 (admin/identity/recovery reads) and D6 (Status +
// SupplyChain reads), per the arbiter D-wave ruling on #9669.
var frozenGovernanceAuditReadAdmission = []string{
	"ListAuditEvents func(context.Context, identity.AuditQuery) ([]governanceaudit.Event, error)",
	"SummarizeAuditEvents func(context.Context) (governanceaudit.Summary, error)",
	"SummarizeAuditEventsForTenant func(context.Context, string) (governanceaudit.Summary, error)",
}

// governanceAuditReadAdmissionMethodSet reflects the live
// GovernanceAuditReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func governanceAuditReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestGovernanceAuditReadAdmissionContractIsVersioned pins the #9669 D1
// admission-contract version for the governance-audit reads (bounded
// event list plus shared-operator and tenant-scoped aggregate
// summaries). The version value plus the frozen method set below are
// the enforcement: a breaking change to the admission shape fails this
// test until the version bumps in the same diff.
func TestGovernanceAuditReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if GovernanceAuditReadAdmissionContractVersion != "v0" {
		t.Errorf("GovernanceAuditReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", GovernanceAuditReadAdmissionContractVersion, "v0")
	}
	got := governanceAuditReadAdmissionMethodSet(t, (*GovernanceAuditReadAdmission)(nil))
	if len(got) != len(frozenGovernanceAuditReadAdmission) {
		t.Fatalf("GovernanceAuditReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenGovernanceAuditReadAdmission, len(frozenGovernanceAuditReadAdmission), GovernanceAuditReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenGovernanceAuditReadAdmission[i] {
			t.Errorf("GovernanceAuditReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenGovernanceAuditReadAdmission[i], GovernanceAuditReadAdmissionContractVersion)
		}
	}
}

// TestGovernanceAuditReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the three re-declared audit reads, with no
// mutation, purge, or other-family surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestGovernanceAuditReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*GovernanceAuditReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("GovernanceAuditReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Create", "Update", "Delete", "Purge", "Write", "Upsert", "Snapshot", "Ledger",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("GovernanceAuditReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededGovernanceAuditDrift is a deliberately widened copy of the
// pin: the three frozen audit reads plus an audit-event purge, which
// the audit-read lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededGovernanceAuditDrift interface {
	GovernanceAuditReadAdmission
	PurgeAuditEvents(context.Context) error
}

// TestGovernanceAuditReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening.
// It passes on a clean tree precisely because the guard fires on
// drift — a guard that cannot see the seeded method is no guard.
func TestGovernanceAuditReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := governanceAuditReadAdmissionMethodSet(t, (*seededGovernanceAuditDrift)(nil))
	if reflect.DeepEqual(got, frozenGovernanceAuditReadAdmission) {
		t.Fatal("seeded widening (PurgeAuditEvents) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenGovernanceAuditReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenGovernanceAuditReadAdmission)+1)
	}
}
