// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// frozenCodeFlowReadAdmission pins the exact method set behind the
// versioned #9669 W6a admission contract for the Code-flow 4 reads:
// POST /api/v0/code/flow/cfg-summary, /pdg-summary, /reaching-def,
// and /taint-path admit their store read through this single method.
// The entry re-declares the staying CodeFlowStore signature in this
// Code-flow-scoped shape instead of embedding the port, so no other
// family can cite this pin — embedding or aliasing the port here
// would be a blanket pin, which the arbiter ruling forbids. A
// renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump
// CodeFlowReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W6a Code-flow 4 ONLY (per the arbiter W-wave
// ruling on eshu-hq/eshu#9669, APPROVED-WITH-CONDITIONS). W6b-W6f
// are NOT approved and are not pre-minted here.
var frozenCodeFlowReadAdmission = []string{
	"ListCodeFlow func(context.Context, codemodel.CodeFlowFilter) (codemodel.CodeFlowReadModel, error)",
}

// codeFlowReadAdmissionWitness proves CodeFlowReadAdmission is
// satisfiable with the minimal shape: one ListCodeFlow method, no
// wider store surface.
type codeFlowReadAdmissionWitness struct{}

func (codeFlowReadAdmissionWitness) ListCodeFlow(context.Context, codemodel.CodeFlowFilter) (codemodel.CodeFlowReadModel, error) {
	return codemodel.CodeFlowReadModel{}, nil
}

var _ CodeFlowReadAdmission = codeFlowReadAdmissionWitness{}

// codeFlowReadAdmissionMethodSet reflects the live
// CodeFlowReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeFlowReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeFlowReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeFlowReadAdmissionContractIsVersioned pins the #9669 W6a
// admission-contract version for the Code-flow 4 reads. The frozen
// method set below is the enforcement: an added, removed, renamed,
// or re-signed method fails this test until the frozen list is
// deliberately updated. Bump CodeFlowReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestCodeFlowReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeFlowReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeFlowReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeFlowReadAdmissionContractVersion, "v0")
	}
	got := codeFlowReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeFlowReadAdmission) {
		t.Fatalf("CodeFlowReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeFlowReadAdmission, len(frozenCodeFlowReadAdmission), CodeFlowReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeFlowReadAdmission[i] {
			t.Errorf("CodeFlowReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeFlowReadAdmission[i], CodeFlowReadAdmissionContractVersion)
		}
	}
}

// TestCodeFlowReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared flow read, with no search, list,
// ledger, stored-summary, or snapshot surface riding along. The frozen
// method-set contract test pins the exact name; this test pins the
// exclusion the arbiter ruling demands.
func TestCodeFlowReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeFlowReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("CodeFlowReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Snapshot",
			"Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeFlowReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCodeFlowDrift is a deliberately widened copy of the pin: the
// one frozen flow read plus a single-row graph read, which the flow
// lane keeps OUT. It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededCodeFlowDrift interface {
	CodeFlowReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestCodeFlowReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestCodeFlowReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*seededCodeFlowDrift)(nil)).Elem()
	var got []string
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	if reflect.DeepEqual(got, frozenCodeFlowReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeFlowReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeFlowReadAdmission)+1)
	}
}
