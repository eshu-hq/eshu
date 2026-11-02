// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"reflect"
	"sort"
	"testing"
)

// frozenProviderConfigReadAdmission pins the exact method set behind the
// versioned #8872 admission contract for provider-config reads backing
// the mutation enable-time guard. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// ProviderConfigReadAdmissionContractVersion alongside so dest consumers
// can pin the shape, keeping the eshu-api extraction boundary from
// drifting silently.
var frozenProviderConfigReadAdmission = []string{
	"GetProviderConfigDetail func(context.Context, string, string) (config.Detail, bool, error)",
	"ListProviderConfigDetails func(context.Context, string) ([]config.Detail, error)",
	"ListProviderConfigRevisions func(context.Context, string, string) ([]config.RevisionItem, error)",
}

// providerConfigReadAdmissionMethodSet reflects the live
// ProviderConfigReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func providerConfigReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*ProviderConfigReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestProviderConfigReadAdmissionContractIsVersioned pins the #8872
// admission-contract version for provider-config reads. The version value
// plus the frozen method set below are the enforcement: a breaking change
// to the admission shape fails this test until the frozen list is
// deliberately updated and the version bumps in the same diff.
func TestProviderConfigReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ProviderConfigReadAdmissionContractVersion != "v0" {
		t.Errorf("ProviderConfigReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ProviderConfigReadAdmissionContractVersion, "v0")
	}
	got := providerConfigReadAdmissionMethodSet(t)
	if len(got) != len(frozenProviderConfigReadAdmission) {
		t.Fatalf("ProviderConfigReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenProviderConfigReadAdmission, len(frozenProviderConfigReadAdmission), ProviderConfigReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenProviderConfigReadAdmission[i] {
			t.Errorf("ProviderConfigReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenProviderConfigReadAdmission[i], ProviderConfigReadAdmissionContractVersion)
		}
	}
}
