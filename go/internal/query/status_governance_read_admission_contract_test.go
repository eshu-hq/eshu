// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenStatusGovernanceReadAdmission pins the exact method set behind the
// versioned #9669 admission contract for the Status governance audit-summary
// leg: exactly the Summary read the governance-status route admits through.
// A renamed, added, removed, or re-signed method fails here until that list
// is deliberately updated; bump StatusGovernanceReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently.
//
// D-wave consumer: Status mints follow-up, per the arbiter
// APPROVED-WITH-CONDITIONS ruling on #9669. The satellite records this pin
// in a follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// audit-summary leg.
//
// This pin is narrow by construction: it re-declares exactly the Summary
// signature in this Status-scoped shape instead of embedding the
// GovernanceAuditSummaryReader port or aliasing the audit store, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption. It is
// a different port from the D1 GovernanceAuditAdmission pin, which freezes
// the single-method Append surface on GovernanceAuditAppender: neither pin
// widens to cover the other.
var frozenStatusGovernanceReadAdmission = []string{
	"Summary func(context.Context) (governanceaudit.Summary, error)",
}

// statusGovernanceReadAdmissionMethodSet reflects the live
// StatusGovernanceReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func statusGovernanceReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*StatusGovernanceReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestStatusGovernanceReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the Status governance audit-summary leg.
// The version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the frozen
// list is deliberately updated and the version bumps in the same diff.
func TestStatusGovernanceReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if StatusGovernanceReadAdmissionContractVersion != "v0" {
		t.Errorf("StatusGovernanceReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", StatusGovernanceReadAdmissionContractVersion, "v0")
	}
	got := statusGovernanceReadAdmissionMethodSet(t)
	if len(got) != len(frozenStatusGovernanceReadAdmission) {
		t.Fatalf("StatusGovernanceReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenStatusGovernanceReadAdmission, len(frozenStatusGovernanceReadAdmission), StatusGovernanceReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenStatusGovernanceReadAdmission[i] {
			t.Errorf("StatusGovernanceReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenStatusGovernanceReadAdmission[i], StatusGovernanceReadAdmissionContractVersion)
		}
	}
}

// TestStatusGovernanceReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the one re-declared Summary read, with no List, Append,
// tenant-scoped, or snapshot surface riding along. The frozen method-set
// contract test pins the exact name; this test pins the exclusion the
// arbiter ruling demands.
func TestStatusGovernanceReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*StatusGovernanceReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("StatusGovernanceReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"List", "Append", "Tenant", "Snapshot", "Ledger"} {
			if strings.Contains(name, banned) {
				t.Errorf("StatusGovernanceReadAdmission.%s contains %q: list, append, tenant, and snapshot legs stay out of this pin", name, banned)
			}
		}
	}
}
