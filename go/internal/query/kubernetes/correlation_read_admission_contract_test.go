// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenKubernetesCorrelationReadAdmission pins the exact method set behind
// the versioned #9669 W8b admission contract for the kubernetes correlation
// read (GET /api/v0/kubernetes/correlations): the single bounded correlation
// read the route admits through ListKubernetesCorrelations. The entry
// re-declares the staying WorkloadCorrelationStore signature in this
// kubernetes-scoped shape instead of embedding the shared port, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption.
//
// Satellite consumer: the W8b-corr leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenKubernetesCorrelationReadAdmission = []string{
	"ListKubernetesCorrelations func(context.Context, kubernetes.CorrelationFilter) ([]kubernetes.CorrelationRow, error)",
}

// kubernetesCorrelationReadAdmissionWitness proves
// KubernetesCorrelationReadAdmission is satisfiable with the minimal
// shape: the single bounded correlation list, no wider store surface.
type kubernetesCorrelationReadAdmissionWitness struct{}

func (kubernetesCorrelationReadAdmissionWitness) ListKubernetesCorrelations(context.Context, CorrelationFilter) ([]CorrelationRow, error) {
	return nil, nil
}

var _ KubernetesCorrelationReadAdmission = kubernetesCorrelationReadAdmissionWitness{}

// kubernetesCorrelationReadAdmissionMethodSet reflects the live
// KubernetesCorrelationReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func kubernetesCorrelationReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
	var got []string
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestKubernetesCorrelationReadAdmissionContractIsVersioned pins the #9669
// W8b admission-contract version for the kubernetes correlation read. The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the version
// bumps in the same diff.
func TestKubernetesCorrelationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if KubernetesCorrelationReadAdmissionContractVersion != "v0" {
		t.Errorf("KubernetesCorrelationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", KubernetesCorrelationReadAdmissionContractVersion, "v0")
	}
	got := kubernetesCorrelationReadAdmissionMethodSet(t, (*KubernetesCorrelationReadAdmission)(nil))
	if len(got) != len(frozenKubernetesCorrelationReadAdmission) {
		t.Fatalf("KubernetesCorrelationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenKubernetesCorrelationReadAdmission, len(frozenKubernetesCorrelationReadAdmission), KubernetesCorrelationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenKubernetesCorrelationReadAdmission[i] {
			t.Errorf("KubernetesCorrelationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenKubernetesCorrelationReadAdmission[i], KubernetesCorrelationReadAdmissionContractVersion)
		}
	}
}

// TestKubernetesCorrelationReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the one re-declared kubernetes correlation read, with
// every graph, search, list, content, or snapshot surface riding along
// refused. The frozen method-set contract test pins the exact name; this
// test pins the exclusions the arbiter ruling demands.
func TestKubernetesCorrelationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*KubernetesCorrelationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("KubernetesCorrelationReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "ListKubernetesCorrelations" {
		t.Fatalf("KubernetesCorrelationReadAdmission sole method = %q, want %q", name, "ListKubernetesCorrelations")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("KubernetesCorrelationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededKubernetesCorrelationDrift is a deliberately widened copy of the pin:
// the one frozen read plus a graph Run the kubernetes lane never issues. It
// never ships; it proves the frozen-set comparison below can see a
// one-method widening.
type seededKubernetesCorrelationDrift interface {
	KubernetesCorrelationReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestKubernetesCorrelationReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestKubernetesCorrelationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := kubernetesCorrelationReadAdmissionMethodSet(t, (*seededKubernetesCorrelationDrift)(nil))
	if reflect.DeepEqual(got, frozenKubernetesCorrelationReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenKubernetesCorrelationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenKubernetesCorrelationReadAdmission)+1)
	}
}
