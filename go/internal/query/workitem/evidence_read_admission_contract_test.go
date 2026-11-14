// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenWorkItemEvidenceReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the work-item evidence read
// (GET /api/v0/work-items/evidence): the single bounded evidence read the
// route admits through ListWorkItemEvidence. The entry re-declares the
// staying EvidenceStore signature in this work-item-scoped shape instead of
// embedding the shared port, so no other family can cite this pin — a
// blanket or shared-port pin is forbidden, and the widened shape needs
// arbiter review before satellite consumption.
//
// Satellite consumer: the W8b-corr leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenWorkItemEvidenceReadAdmission = []string{
	"ListWorkItemEvidence func(context.Context, workitem.EvidenceFilter) (workitem.EvidencePage, error)",
}

// workItemEvidenceReadAdmissionMethodSet reflects the live
// WorkItemEvidenceReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func workItemEvidenceReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestWorkItemEvidenceReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the work-item evidence read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestWorkItemEvidenceReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if WorkItemEvidenceReadAdmissionContractVersion != "v0" {
		t.Errorf("WorkItemEvidenceReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", WorkItemEvidenceReadAdmissionContractVersion, "v0")
	}
	got := workItemEvidenceReadAdmissionMethodSet(t, (*WorkItemEvidenceReadAdmission)(nil))
	if len(got) != len(frozenWorkItemEvidenceReadAdmission) {
		t.Fatalf("WorkItemEvidenceReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenWorkItemEvidenceReadAdmission, len(frozenWorkItemEvidenceReadAdmission), WorkItemEvidenceReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenWorkItemEvidenceReadAdmission[i] {
			t.Errorf("WorkItemEvidenceReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenWorkItemEvidenceReadAdmission[i], WorkItemEvidenceReadAdmissionContractVersion)
		}
	}
}

// TestWorkItemEvidenceReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared work-item evidence read, with every
// graph, search, list, content, or snapshot surface riding along refused.
// The frozen method-set contract test pins the exact name; this test pins
// the exclusions the arbiter ruling demands.
func TestWorkItemEvidenceReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*WorkItemEvidenceReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("WorkItemEvidenceReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	if name := rt.Method(0).Name; name != "ListWorkItemEvidence" {
		t.Fatalf("WorkItemEvidenceReadAdmission sole method = %q, want %q", name, "ListWorkItemEvidence")
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("WorkItemEvidenceReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededWorkItemEvidenceDrift is a deliberately widened copy of the pin: the
// one frozen read plus a graph Run the work-item lane never issues. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededWorkItemEvidenceDrift interface {
	WorkItemEvidenceReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestWorkItemEvidenceReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestWorkItemEvidenceReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := workItemEvidenceReadAdmissionMethodSet(t, (*seededWorkItemEvidenceDrift)(nil))
	if reflect.DeepEqual(got, frozenWorkItemEvidenceReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenWorkItemEvidenceReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenWorkItemEvidenceReadAdmission)+1)
	}
}
