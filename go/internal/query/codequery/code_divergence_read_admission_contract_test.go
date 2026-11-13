// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
)

// frozenCodeDivergenceReadAdmission pins the exact method set behind
// the versioned #9669 admission contract for the divergence findings
// read: POST /api/v0/code/divergence/findings admits its bounded
// content reads through DivergenceGroupStats, DivergenceMembers,
// DivergenceMembersByEntityID, DriftedFindingStats, and
// DriftedFindingRows, and its bounded graph traversal through Run.
// The entries re-declare the staying divergenceStore and GraphQuery
// signatures in this divergence-scoped shape instead of embedding
// either port, so no other family can cite this pin — embedding or
// aliasing a shared port here would be a blanket pin, which the
// arbiter ruling forbids. A renamed, added, removed, or re-signed
// method fails here until that list is deliberately updated; bump
// CodeDivergenceReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: divergence findings ONLY (per the arbiter
// supplemental ruling on eshu-hq/eshu-api#125).
var frozenCodeDivergenceReadAdmission = []string{
	"DivergenceGroupStats func(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error)",
	"DivergenceMembers func(context.Context, string, codedivergence.Kind, []string) (map[string][]codedivergence.Member, error)",
	"DivergenceMembersByEntityID func(context.Context, string, []string) (map[string]codedivergence.Member, error)",
	"DriftedFindingRows func(context.Context, string, []string) (map[string]codedivergence.DriftedRow, error)",
	"DriftedFindingStats func(context.Context, string) ([]codedivergence.GroupStat, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// codeDivergenceReadAdmissionWitness proves
// CodeDivergenceReadAdmission is satisfiable with the minimal shape:
// the five divergence content reads plus the one graph traversal,
// no wider store surface.
type codeDivergenceReadAdmissionWitness struct{}

func (codeDivergenceReadAdmissionWitness) DivergenceGroupStats(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionWitness) DivergenceMembers(context.Context, string, codedivergence.Kind, []string) (map[string][]codedivergence.Member, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionWitness) DivergenceMembersByEntityID(context.Context, string, []string) (map[string]codedivergence.Member, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionWitness) DriftedFindingStats(context.Context, string) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionWitness) DriftedFindingRows(context.Context, string, []string) (map[string]codedivergence.DriftedRow, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

var _ CodeDivergenceReadAdmission = codeDivergenceReadAdmissionWitness{}

// codeDivergenceReadAdmissionResignedWitness carries a deliberately
// re-signed DriftedFindingRows (an extra trailing parameter). It
// MUST NOT satisfy CodeDivergenceReadAdmission: the seeded rejection
// test below proves a signature drift on any pinned method breaks
// the pin instead of sliding through silently.
type codeDivergenceReadAdmissionResignedWitness struct{}

func (codeDivergenceReadAdmissionResignedWitness) DivergenceGroupStats(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionResignedWitness) DivergenceMembers(context.Context, string, codedivergence.Kind, []string) (map[string][]codedivergence.Member, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionResignedWitness) DivergenceMembersByEntityID(context.Context, string, []string) (map[string]codedivergence.Member, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionResignedWitness) DriftedFindingStats(context.Context, string) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionResignedWitness) DriftedFindingRows(context.Context, string, []string, int) (map[string]codedivergence.DriftedRow, error) {
	return nil, nil
}

func (codeDivergenceReadAdmissionResignedWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

// codeDivergenceReadAdmissionMethodSet reflects the live
// CodeDivergenceReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func codeDivergenceReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeDivergenceReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeDivergenceReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the divergence findings read. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list
// is deliberately updated. Bump
// CodeDivergenceReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so
// dest consumers can pin the shape.
func TestCodeDivergenceReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeDivergenceReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeDivergenceReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeDivergenceReadAdmissionContractVersion, "v0")
	}
	got := codeDivergenceReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeDivergenceReadAdmission) {
		t.Fatalf("CodeDivergenceReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeDivergenceReadAdmission, len(frozenCodeDivergenceReadAdmission), CodeDivergenceReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeDivergenceReadAdmission[i] {
			t.Errorf("CodeDivergenceReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeDivergenceReadAdmission[i], CodeDivergenceReadAdmissionContractVersion)
		}
	}
}

// TestCodeDivergenceReadAdmissionRejectsResignedMethod is the seeded
// RED half of the contract proof: a witness whose
// DriftedFindingRows carries one extra parameter MUST NOT satisfy
// the pin. If a re-signed method ever slides through, this test
// fails and the frozen shape above can no longer be trusted to
// catch drift.
func TestCodeDivergenceReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeDivergenceReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeDivergenceReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeDivergenceReadAdmissionResignedWitness implements CodeDivergenceReadAdmission: a re-signed DriftedFindingRows must break the pin")
	}
	if !reflect.TypeOf(codeDivergenceReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeDivergenceReadAdmissionWitness does not implement CodeDivergenceReadAdmission: the minimal shape must satisfy the pin")
	}
}
