// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenContentPointReadAdmission pins the exact method set behind the
// versioned #9669 D13 Wave-2 admission contract for the Content point
// reads (the file-content read, the file line-range read, and the
// entity-content read). The entries re-declare the ContentStore
// signatures in this Content-scoped shape instead of embedding the
// shared port, so no other family can cite this pin — embedding or
// aliasing querycontract.ContentStore here would be a blanket pin,
// which the arbiter ruling forbids. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump ContentPointReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: D13 Content-first Wave-2, per the arbiter Wave-2
// ruling on #9669.
//
// Evidence (not enforcement): the shared handler double behind the
// route tests is testutil/content.FakePortContentStore, blob
// 5c836cce6245ac9c470a837c986fcde96e40946d
// (go/internal/query/testutil/content/port_store.go, last touched by
// 030c434afda78a7c0d5ec6dbb97a877237b4837c).
var frozenContentPointReadAdmission = []string{
	"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"GetFileLines func(context.Context, string, string, int, int) (*querycontract.FileContent, error)",
}

// contentPointReadAdmissionMethodSet reflects the live
// ContentPointReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func contentPointReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*ContentPointReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestContentPointReadAdmissionContractIsVersioned pins the #9669 D13
// Wave-2 admission-contract version for the Content point reads. The
// version value plus the frozen method set below are the enforcement:
// a breaking change to the admission shape fails this test until the
// version bumps in the same diff.
func TestContentPointReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ContentPointReadAdmissionContractVersion != "v0" {
		t.Errorf("ContentPointReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ContentPointReadAdmissionContractVersion, "v0")
	}
	got := contentPointReadAdmissionMethodSet(t)
	if len(got) != len(frozenContentPointReadAdmission) {
		t.Fatalf("ContentPointReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenContentPointReadAdmission, len(frozenContentPointReadAdmission), ContentPointReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenContentPointReadAdmission[i] {
			t.Errorf("ContentPointReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenContentPointReadAdmission[i], ContentPointReadAdmissionContractVersion)
		}
	}
}

// TestContentPointReadAdmissionWireShapesAreSealed proves the Wave-2
// seal on the pin: exactly the three re-declared point reads, with no
// ledger, stored-summary, or snapshot surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestContentPointReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ContentPointReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("ContentPointReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Snapshot", "Ledger", "Summary", "StartupError"} {
			if strings.Contains(name, banned) {
				t.Errorf("ContentPointReadAdmission.%s contains %q: ledger, stored-summary, and snapshot legs stay out of this pin", name, banned)
			}
		}
	}
}
