// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenInvestigationWorkflowReadAdmission pins the exact method set
// behind the versioned #9669 W5 admission contract for the
// investigation-workflow reads (GET /api/v0/investigation-workflows,
// POST /api/v0/investigation-workflows/resolve): the catalog list, the
// workflow lookup, and the workflow resolve. The entries re-declare
// the staying static signatures whole in this workflow-scoped shape
// instead of aliasing the catalog surface, so no other family can
// cite this pin — a blanket pin is forbidden, and the widened shape
// needs arbiter review before satellite consumption. A renamed,
// added, removed, or re-signed method fails here until that list is
// deliberately updated; bump
// InvestigationWorkflowReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// W-wave consumer: W5 investigation workflows (both oracle
// InvestigationWorkflows rows).
var frozenInvestigationWorkflowReadAdmission = []string{
	"ListInvestigationWorkflows func() []query.InvestigationWorkflow",
	"LookupInvestigationWorkflow func(string) (query.InvestigationWorkflow, bool)",
	"ResolveInvestigationWorkflow func(query.InvestigationWorkflow, query.InvestigationWorkflowResolveInput) (query.ResolvedInvestigationWorkflow, error)",
}

// investigationWorkflowReadAdmissionWitness proves
// InvestigationWorkflowReadAdmission is satisfiable with the minimal
// shape: one catalog list, one lookup, and one resolve, no wider
// catalog surface.
type investigationWorkflowReadAdmissionWitness struct{}

func (investigationWorkflowReadAdmissionWitness) ListInvestigationWorkflows() []InvestigationWorkflow {
	return nil
}

func (investigationWorkflowReadAdmissionWitness) LookupInvestigationWorkflow(string) (InvestigationWorkflow, bool) {
	return InvestigationWorkflow{}, false
}

func (investigationWorkflowReadAdmissionWitness) ResolveInvestigationWorkflow(InvestigationWorkflow, InvestigationWorkflowResolveInput) (ResolvedInvestigationWorkflow, error) {
	return ResolvedInvestigationWorkflow{}, nil
}

var _ InvestigationWorkflowReadAdmission = investigationWorkflowReadAdmissionWitness{}

// investigationWorkflowReadAdmissionMethodSet reflects the live
// InvestigationWorkflowReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list. The iface
// parameter forces every caller — including the seeded-drift RED proof
// below — through this same guard comparison, so a passing suite
// proves the guard itself can see drift rather than proving a private
// copy can.
func investigationWorkflowReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestInvestigationWorkflowReadAdmissionContractIsVersioned pins the
// #9669 W5 admission-contract version for the investigation-workflow
// reads. The frozen method set below is the enforcement: an added,
// removed, renamed, or re-signed method fails this test until the
// frozen list is deliberately updated. Bump
// InvestigationWorkflowReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestInvestigationWorkflowReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if InvestigationWorkflowReadAdmissionContractVersion != "v0" {
		t.Errorf("InvestigationWorkflowReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", InvestigationWorkflowReadAdmissionContractVersion, "v0")
	}
	got := investigationWorkflowReadAdmissionMethodSet(t, (*InvestigationWorkflowReadAdmission)(nil))
	if len(got) != len(frozenInvestigationWorkflowReadAdmission) {
		t.Fatalf("InvestigationWorkflowReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenInvestigationWorkflowReadAdmission, len(frozenInvestigationWorkflowReadAdmission), InvestigationWorkflowReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenInvestigationWorkflowReadAdmission[i] {
			t.Errorf("InvestigationWorkflowReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenInvestigationWorkflowReadAdmission[i], InvestigationWorkflowReadAdmissionContractVersion)
		}
	}
}

// TestInvestigationWorkflowReadAdmissionWireShapesAreSealed proves the
// seal on the pin: exactly the three re-declared catalog reads, with
// no versions marker, mutation, snapshot, or ledger surface riding
// along. The frozen method-set contract test pins the exact names;
// this test pins the exclusion the arbiter ruling demands.
func TestInvestigationWorkflowReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*InvestigationWorkflowReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("InvestigationWorkflowReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Versions", "Delete", "Write", "Snapshot", "Ledger", "Execute", "Deploy",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("InvestigationWorkflowReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededInvestigationWorkflowDrift is a deliberately widened copy of
// the pin: the three frozen catalog reads plus a versions-marker read
// the workflow lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededInvestigationWorkflowDrift interface {
	InvestigationWorkflowReadAdmission
	ListInvestigationWorkflowVersions() []InvestigationWorkflowVersionRef
}

// TestInvestigationWorkflowReadAdmissionSeededDriftIsDetected is the
// RED half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening.
// It passes on a clean tree precisely because the guard fires on
// drift — a guard that cannot see the seeded method is no guard.
func TestInvestigationWorkflowReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := investigationWorkflowReadAdmissionMethodSet(t, (*seededInvestigationWorkflowDrift)(nil))
	if reflect.DeepEqual(got, frozenInvestigationWorkflowReadAdmission) {
		t.Fatal("seeded widening (ListInvestigationWorkflowVersions) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenInvestigationWorkflowReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenInvestigationWorkflowReadAdmission)+1)
	}
}
