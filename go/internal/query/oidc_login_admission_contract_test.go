// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenOIDCLoginAdmission pins the exact method set behind the
// versioned #8872 admission contract for OIDC login. A renamed, added,
// removed, or re-signed method fails here until that list is deliberately
// updated; bump OIDCLoginAdmissionContractVersion alongside so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently. The optional OIDCProviderLister is deliberately
// not part of v0 (see the OIDCLoginAdmission doc comment).
var frozenOIDCLoginAdmission = []string{
	"CompleteOIDCLogin func(context.Context, query.OIDCLoginCompleteRequest) (query.OIDCLoginCompleteResponse, error)",
	"StartOIDCLogin func(context.Context, query.OIDCLoginStartRequest) (query.OIDCLoginStartResponse, error)",
}

// oidcLoginAdmissionMethodSet reflects the live OIDCLoginAdmission method
// set into sorted "name signature" strings for comparison with the frozen
// list.
func oidcLoginAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*OIDCLoginAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestOIDCLoginAdmissionContractIsVersioned pins the #8872
// admission-contract version for OIDC login. The version value plus the
// frozen method set below are the enforcement: a breaking change to the
// admission shape fails this test until the frozen list is deliberately
// updated and the version bumps in the same diff.
func TestOIDCLoginAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if OIDCLoginAdmissionContractVersion != "v0" {
		t.Errorf("OIDCLoginAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", OIDCLoginAdmissionContractVersion, "v0")
	}
	got := oidcLoginAdmissionMethodSet(t)
	if len(got) != len(frozenOIDCLoginAdmission) {
		t.Fatalf("OIDCLoginAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenOIDCLoginAdmission, len(frozenOIDCLoginAdmission), OIDCLoginAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenOIDCLoginAdmission[i] {
			t.Errorf("OIDCLoginAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenOIDCLoginAdmission[i], OIDCLoginAdmissionContractVersion)
		}
	}
}
