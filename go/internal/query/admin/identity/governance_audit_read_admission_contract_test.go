// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"reflect"
	"sort"
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
// strings for comparison with the frozen list.
func governanceAuditReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*GovernanceAuditReadAdmission)(nil)).Elem()
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
	got := governanceAuditReadAdmissionMethodSet(t)
	if len(got) != len(frozenGovernanceAuditReadAdmission) {
		t.Fatalf("GovernanceAuditReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenGovernanceAuditReadAdmission, len(frozenGovernanceAuditReadAdmission), GovernanceAuditReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenGovernanceAuditReadAdmission[i] {
			t.Errorf("GovernanceAuditReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenGovernanceAuditReadAdmission[i], GovernanceAuditReadAdmissionContractVersion)
		}
	}
}
