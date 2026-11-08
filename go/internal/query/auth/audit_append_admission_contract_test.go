// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// frozenAuditAppendAdmission pins the exact method set behind the
// versioned #9669 O2 admission contract for the audit-append port: the
// one append the 18 tier-C audit-emitting routes admit through. The
// entry re-declares the shared-port signature in this audit-scoped
// shape instead of embedding the shared port, so no other family can
// cite this pin — embedding or aliasing GovernanceAuditAppender here
// would be a blanket pin. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// AuditAppendAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the extraction boundary from
// drifting silently.
//
// D-wave consumer: O2 ORDER one shared pin for all 18 tier-C legs;
// satellite records this pin in api/version.go and contracts/ with
// per-route byte-parity/conformance plus D0 re-review.
var frozenAuditAppendAdmission = []string{
	"Append func(context.Context, []governanceaudit.Event) error",
}

// auditAppendAdmissionWitness proves AuditAppendAdmission is
// satisfiable: a minimal recorder carrying exactly the pinned method.
// It witnesses the pin only — production satisfaction stays with the
// existing GovernanceAuditAppender implementations, named by the
// compile-time mirror assertion on the pin.
type auditAppendAdmissionWitness struct {
	appended [][]governanceaudit.Event
}

func (w *auditAppendAdmissionWitness) Append(_ context.Context, events []governanceaudit.Event) error {
	w.appended = append(w.appended, events)
	return nil
}

var _ AuditAppendAdmission = (*auditAppendAdmissionWitness)(nil)

// auditAppendAdmissionMethodSet reflects the live
// AuditAppendAdmission method set into sorted "name signature" strings
// for comparison with the frozen list.
func auditAppendAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*AuditAppendAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestAuditAppendAdmissionContractIsVersioned pins the #9669 O2
// admission-contract version for the audit-append port. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump AuditAppendAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestAuditAppendAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if AuditAppendAdmissionContractVersion != "v0" {
		t.Errorf("AuditAppendAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", AuditAppendAdmissionContractVersion, "v0")
	}
	got := auditAppendAdmissionMethodSet(t)
	if len(got) != len(frozenAuditAppendAdmission) {
		t.Fatalf("AuditAppendAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenAuditAppendAdmission, len(frozenAuditAppendAdmission), AuditAppendAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenAuditAppendAdmission[i] {
			t.Errorf("AuditAppendAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenAuditAppendAdmission[i], AuditAppendAdmissionContractVersion)
		}
	}
}
