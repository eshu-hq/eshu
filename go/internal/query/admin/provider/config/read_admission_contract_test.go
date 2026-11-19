// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"context"
	"reflect"
	"sort"
	"strings"
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
func providerConfigReadAdmissionMethodSet(t *testing.T, iface any) []string {
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
	got := providerConfigReadAdmissionMethodSet(t, (*ProviderConfigReadAdmission)(nil))
	if len(got) != len(frozenProviderConfigReadAdmission) {
		t.Fatalf("ProviderConfigReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenProviderConfigReadAdmission, len(frozenProviderConfigReadAdmission), ProviderConfigReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenProviderConfigReadAdmission[i] {
			t.Errorf("ProviderConfigReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenProviderConfigReadAdmission[i], ProviderConfigReadAdmissionContractVersion)
		}
	}
}

// providerConfigReadAdmissionWitness proves ProviderConfigReadAdmission
// is satisfiable with the minimal shape: the three admin reads (list,
// detail, revisions) over redacted non-secret configuration, no wider
// store surface.
type providerConfigReadAdmissionWitness struct{}

func (providerConfigReadAdmissionWitness) GetProviderConfigDetail(context.Context, string, string) (Detail, bool, error) {
	return Detail{}, false, nil
}

func (providerConfigReadAdmissionWitness) ListProviderConfigDetails(context.Context, string) ([]Detail, error) {
	return nil, nil
}

func (providerConfigReadAdmissionWitness) ListProviderConfigRevisions(context.Context, string, string) ([]RevisionItem, error) {
	return nil, nil
}

var _ ProviderConfigReadAdmission = providerConfigReadAdmissionWitness{}

// TestProviderConfigReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the three redacted reads, with no mutation
// (create/update/revert/enable/disable), connection-test, secret, or
// snapshot surface riding along. The frozen method-set contract test
// pins the exact names; this test pins the exclusion the arbiter
// ruling demands.
func TestProviderConfigReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ProviderConfigReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("ProviderConfigReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Create", "Update", "Revert", "Enable", "Disable", "Test", "Secret", "Delete", "Append", "Write", "Snapshot", "Ledger", "Tenant", "Exec", "Mutat", "Suppress"} {
			if strings.Contains(name, banned) {
				t.Errorf("ProviderConfigReadAdmission.%s contains %q: mutation, connection-test, and secret legs stay out of this pin", name, banned)
			}
		}
	}
}

// seededProviderConfigDrift is a deliberately widened copy of the pin:
// the three frozen reads plus the Create mutation the read legs never
// issue. It never ships; it proves the frozen-set comparison below can
// see a one-method widening.
type seededProviderConfigDrift interface {
	ProviderConfigReadAdmission
	CreateProviderConfig(context.Context, CreateRequest) (WriteResult, error)
}

// TestProviderConfigReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestProviderConfigReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := providerConfigReadAdmissionMethodSet(t, (*seededProviderConfigDrift)(nil))
	if reflect.DeepEqual(got, frozenProviderConfigReadAdmission) {
		t.Fatal("seeded widening (CreateProviderConfig) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenProviderConfigReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenProviderConfigReadAdmission)+1)
	}
	for _, want := range frozenProviderConfigReadAdmission {
		found := false
		for _, have := range got {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("seeded drift dropped frozen entry %q: the seed must only widen, never narrow", want)
		}
	}
}
