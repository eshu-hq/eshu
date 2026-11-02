// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"reflect"
	"sort"
	"testing"
)

// frozenProviderConfigMutationAdmission pins the exact method set behind
// the versioned #8872 admission contract for provider-config mutations.
// A renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump
// ProviderConfigMutationAdmissionContractVersion alongside so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
var frozenProviderConfigMutationAdmission = []string{
	"CreateProviderConfig func(context.Context, config.CreateRequest) (config.WriteResult, error)",
	"DisableProviderConfig func(context.Context, string, string) (config.WriteResult, error)",
	"EnableProviderConfig func(context.Context, string, string, string) (config.WriteResult, error)",
	"RevertProviderConfig func(context.Context, config.RevertRequest) (config.WriteResult, error)",
	"UpdateProviderConfig func(context.Context, config.UpdateRequest) (config.WriteResult, error)",
}

// providerConfigMutationAdmissionMethodSet reflects the live
// ProviderConfigMutationAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func providerConfigMutationAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*ProviderConfigMutationAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestProviderConfigMutationAdmissionContractIsVersioned pins the #8872
// admission-contract version for provider-config mutations. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the frozen list is
// deliberately updated and the version bumps in the same diff.
func TestProviderConfigMutationAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ProviderConfigMutationAdmissionContractVersion != "v0" {
		t.Errorf("ProviderConfigMutationAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ProviderConfigMutationAdmissionContractVersion, "v0")
	}
	got := providerConfigMutationAdmissionMethodSet(t)
	if len(got) != len(frozenProviderConfigMutationAdmission) {
		t.Fatalf("ProviderConfigMutationAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenProviderConfigMutationAdmission, len(frozenProviderConfigMutationAdmission), ProviderConfigMutationAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenProviderConfigMutationAdmission[i] {
			t.Errorf("ProviderConfigMutationAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenProviderConfigMutationAdmission[i], ProviderConfigMutationAdmissionContractVersion)
		}
	}
}
