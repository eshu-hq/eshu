// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"testing"
)

// frozenSAMLLoginAdmission pins the exact method set behind the
// versioned #8872 admission contract for SAML login. A renamed, added,
// removed, or re-signed method fails here until that list is deliberately
// updated; bump SAMLLoginAdmissionContractVersion alongside so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently. The optional SAMLProviderIDLister is
// deliberately outside v0 (dest clients implementing only these 5
// methods get no runtime provider discovery).
var frozenSAMLLoginAdmission = []string{
	"ConsumeSAMLRequest func(context.Context, string, string, string, time.Time) (string, bool, error)",
	"CreateSAMLRequest func(context.Context, string, query.SAMLRequestCreateRecord) error",
	"GetSAMLProvider func(context.Context, string) (query.SAMLProviderConfig, bool, error)",
	"ReserveSAMLReplay func(context.Context, string, string, time.Time) (bool, error)",
	"ResolveSAMLPrincipal func(context.Context, string, samlauth.Principal, time.Time) (auth.AuthContext, bool, error)",
}

// samlLoginAdmissionMethodSet reflects the live SAMLLoginAdmission method
// set into sorted "name signature" strings for comparison with the frozen
// list.
func samlLoginAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SAMLLoginAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSAMLLoginAdmissionContractIsVersioned pins the #8872
// admission-contract version for SAML login. The version value plus the
// frozen method set below are the enforcement: a breaking change to the
// admission shape fails this test until the frozen list is deliberately
// updated and the version bumps in the same diff.
func TestSAMLLoginAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SAMLLoginAdmissionContractVersion != "v0" {
		t.Errorf("SAMLLoginAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SAMLLoginAdmissionContractVersion, "v0")
	}
	got := samlLoginAdmissionMethodSet(t)
	if len(got) != len(frozenSAMLLoginAdmission) {
		t.Fatalf("SAMLLoginAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSAMLLoginAdmission, len(frozenSAMLLoginAdmission), SAMLLoginAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSAMLLoginAdmission[i] {
			t.Errorf("SAMLLoginAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSAMLLoginAdmission[i], SAMLLoginAdmissionContractVersion)
		}
	}
}
