// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenCodeContentInvestigationReadAdmission pins the exact method set
// behind the versioned #9669 admission contract for the W6c content
// investigation reads: POST /api/v0/code/structure/inventory admits its
// bounded content-index reads through InspectStructuralInventory and
// CountStructuralInventoryByFile, and POST
// /api/v0/code/topics/investigate admits its bounded investigation
// through InvestigateCodeTopic. The entries re-declare the staying
// structuralInventoryContentStore and CodeTopicContentInvestigator
// signatures in this investigation-scoped shape instead of embedding
// either port, so no other family can cite this pin — embedding or
// aliasing a shared port here would be a blanket pin, which the arbiter
// ruling forbids. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// CodeContentInvestigationReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently.
//
// W-wave consumer: W6c content-investigation ONLY (per the arbiter
// supplemental ruling on eshu-hq/eshu-api#98, §2B). The hardcoded-secret
// leg stays out on purpose: it rides its own isolated pin (§2C).
var frozenCodeContentInvestigationReadAdmission = []string{
	"CountStructuralInventoryByFile func(context.Context, codequery.StructuralInventoryRequest) ([]codequery.StructuralInventoryFileCount, error)",
	"InspectStructuralInventory func(context.Context, codequery.StructuralInventoryRequest) ([]querycontract.EntityContent, error)",
	"InvestigateCodeTopic func(context.Context, codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error)",
}

// codeContentInvestigationReadAdmissionWitness proves
// CodeContentInvestigationReadAdmission is satisfiable with the minimal
// shape: the two structural-inventory reads plus the one topic
// investigation, no wider store surface.
type codeContentInvestigationReadAdmissionWitness struct{}

func (codeContentInvestigationReadAdmissionWitness) InspectStructuralInventory(context.Context, StructuralInventoryRequest) ([]EntityContent, error) {
	return nil, nil
}

func (codeContentInvestigationReadAdmissionWitness) CountStructuralInventoryByFile(context.Context, StructuralInventoryRequest) ([]StructuralInventoryFileCount, error) {
	return nil, nil
}

func (codeContentInvestigationReadAdmissionWitness) InvestigateCodeTopic(context.Context, CodeTopicInvestigationRequest) ([]CodeTopicEvidenceRow, error) {
	return nil, nil
}

var _ CodeContentInvestigationReadAdmission = codeContentInvestigationReadAdmissionWitness{}

// codeContentInvestigationReadAdmissionResignedWitness carries a
// deliberately re-signed InvestigateCodeTopic (an extra trailing
// parameter). It MUST NOT satisfy
// CodeContentInvestigationReadAdmission: the seeded rejection test below
// proves a signature drift on any pinned method breaks the pin instead
// of sliding through silently.
type codeContentInvestigationReadAdmissionResignedWitness struct{}

func (codeContentInvestigationReadAdmissionResignedWitness) InspectStructuralInventory(context.Context, StructuralInventoryRequest) ([]EntityContent, error) {
	return nil, nil
}

func (codeContentInvestigationReadAdmissionResignedWitness) CountStructuralInventoryByFile(context.Context, StructuralInventoryRequest) ([]StructuralInventoryFileCount, error) {
	return nil, nil
}

func (codeContentInvestigationReadAdmissionResignedWitness) InvestigateCodeTopic(context.Context, CodeTopicInvestigationRequest, int) ([]CodeTopicEvidenceRow, error) {
	return nil, nil
}

// codeContentInvestigationReadAdmissionMethodSet reflects the live
// CodeContentInvestigationReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func codeContentInvestigationReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCodeContentInvestigationReadAdmissionContractIsVersioned pins the
// #9669 admission-contract version for the W6c content investigation
// reads. The frozen method set below is the enforcement: an added,
// removed, renamed, or re-signed method fails this test until the frozen
// list is deliberately updated. Bump
// CodeContentInvestigationReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestCodeContentInvestigationReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeContentInvestigationReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeContentInvestigationReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeContentInvestigationReadAdmissionContractVersion, "v0")
	}
	got := codeContentInvestigationReadAdmissionMethodSet(t, (*CodeContentInvestigationReadAdmission)(nil))
	if len(got) != len(frozenCodeContentInvestigationReadAdmission) {
		t.Fatalf("CodeContentInvestigationReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeContentInvestigationReadAdmission, len(frozenCodeContentInvestigationReadAdmission), CodeContentInvestigationReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeContentInvestigationReadAdmission[i] {
			t.Errorf("CodeContentInvestigationReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeContentInvestigationReadAdmission[i], CodeContentInvestigationReadAdmissionContractVersion)
		}
	}
}

// TestCodeContentInvestigationReadAdmissionRejectsResignedMethod is the
// seeded RED half of the contract proof: a witness whose
// InvestigateCodeTopic carries one extra parameter MUST NOT satisfy the
// pin. If a re-signed method ever slides through, this test fails and
// the frozen shape above can no longer be trusted to catch drift.
func TestCodeContentInvestigationReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeContentInvestigationReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeContentInvestigationReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeContentInvestigationReadAdmissionResignedWitness implements CodeContentInvestigationReadAdmission: a re-signed InvestigateCodeTopic must break the pin")
	}
	if !reflect.TypeOf(codeContentInvestigationReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeContentInvestigationReadAdmissionWitness does not implement CodeContentInvestigationReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestCodeContentInvestigationReadAdmissionWireShapesAreSealed proves the
// seal on the pin: exactly the three re-declared investigation reads,
// with graph traversal, search, list, file, secret, snapshot, or ledger
// surfaces riding along refused. The frozen method-set contract test
// pins the exact names; this test pins the exclusions the arbiter ruling
// demands.
func TestCodeContentInvestigationReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeContentInvestigationReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("CodeContentInvestigationReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "List", "GetFile", "WithSource",
			"Secret", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeContentInvestigationReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCodeContentInvestigationDrift is a deliberately widened copy of
// the pin: the three frozen content-investigation reads plus a bounded
// graph traversal, which the investigation lane keeps OUT (it admits
// only the structural-inventory and topic projections). It never ships;
// it proves the frozen-set comparison below can see a one-method
// widening.
type seededCodeContentInvestigationDrift interface {
	CodeContentInvestigationReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestCodeContentInvestigationReadAdmissionSeededDriftIsDetected is the
// RED half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening. It
// passes on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestCodeContentInvestigationReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := codeContentInvestigationReadAdmissionMethodSet(t, (*seededCodeContentInvestigationDrift)(nil))
	if reflect.DeepEqual(got, frozenCodeContentInvestigationReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeContentInvestigationReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeContentInvestigationReadAdmission)+1)
	}
}
