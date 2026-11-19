// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenImageReadAdmission pins the exact method set behind the versioned
// #9669 W8b admission contract for the image inventory read (GET
// /api/v0/images): the single bounded graph read the route admits through
// Run (sole call images.go:153, imageListCypher). RunSingle stays OUT by
// construction: the lane never issues a single-row graph read. The entry
// re-declares the staying GraphQuery signature in this image-scoped shape
// instead of embedding the shared port, so no other family can cite this pin
// — a blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption. Do not merge this pin with
// TagHistoryReadAdmission: different handler, capability, and grant model.
//
// Satellite consumer: the W8b-graph1 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenImageReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// imageReadAdmissionWitness proves ImageReadAdmission is satisfiable
// with the minimal shape: the single bounded graph read, no wider
// store surface.
type imageReadAdmissionWitness struct{}

func (imageReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

var _ ImageReadAdmission = imageReadAdmissionWitness{}

// imageReadAdmissionMethodSet reflects the live ImageReadAdmission method set
// into sorted "name signature" strings for comparison with the frozen list.
func imageReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestImageReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the image inventory read. The version value
// plus the frozen method set below are the enforcement: a breaking change to
// the admission shape fails this test until the version bumps in the same
// diff.
func TestImageReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ImageReadAdmissionContractVersion != "v0" {
		t.Errorf("ImageReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ImageReadAdmissionContractVersion, "v0")
	}
	got := imageReadAdmissionMethodSet(t, (*ImageReadAdmission)(nil))
	if len(got) != len(frozenImageReadAdmission) {
		t.Fatalf("ImageReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenImageReadAdmission, len(frozenImageReadAdmission), ImageReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenImageReadAdmission[i] {
			t.Errorf("ImageReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenImageReadAdmission[i], ImageReadAdmissionContractVersion)
		}
	}
}

// TestImageReadAdmissionWireShapesAreSealed proves the seal on the pin:
// exactly the one re-declared graph Run, with RunSingle and every search,
// list, content, or snapshot surface riding along refused. The frozen
// method-set contract test pins the exact name; this test pins the exclusions
// the arbiter ruling demands.
func TestImageReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ImageReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("ImageReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "Run" {
		t.Fatalf("ImageReadAdmission sole method = %q, want %q", name, "Run")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ImageReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededImageDrift is a deliberately widened copy of the pin: the one frozen
// read plus a RunSingle the image lane never issues. It never ships; it
// proves the frozen-set comparison below can see a one-method widening.
type seededImageDrift interface {
	ImageReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestImageReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs must
// report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestImageReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := imageReadAdmissionMethodSet(t, (*seededImageDrift)(nil))
	if reflect.DeepEqual(got, frozenImageReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenImageReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenImageReadAdmission)+1)
	}
}
