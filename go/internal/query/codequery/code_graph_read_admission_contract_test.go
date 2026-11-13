// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenCodeGraphReadAdmission pins the exact method set behind the
// versioned #9669 G-a admission contract for the G-a 4 graph reads:
// POST /api/v0/code/call-chain, /call-chain/compare,
// /call-graph/metrics, and /routes/callers admit their bounded graph
// traversal through Run and RunSingle and their entity-name
// resolution through SearchEntitiesByName. The entries re-declare
// the staying GraphQuery and ContentStore signatures in this G-a
// scoped shape instead of embedding either port, so no other family
// can cite this pin — embedding or aliasing a shared port here
// would be a blanket pin, which the arbiter ruling forbids. A
// renamed, added, removed, or re-signed method fails here until that
// list is deliberately updated; bump
// CodeGraphReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: G-a graph ONLY (per the arbiter Wave-7 ruling on
// eshu-hq/eshu#9669, SPLIT). Relationships/story rides G-b
// (eshu-hq/eshu-api#122) and is NOT approved here.
var frozenCodeGraphReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
}

// codeGraphReadAdmissionWitness proves CodeGraphReadAdmission is
// satisfiable with the minimal shape: two graph-traversal methods
// plus the one entity-name read, no wider store surface.
type codeGraphReadAdmissionWitness struct{}

func (codeGraphReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeGraphReadAdmissionWitness) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (codeGraphReadAdmissionWitness) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

var _ CodeGraphReadAdmission = codeGraphReadAdmissionWitness{}

// codeGraphReadAdmissionResignedWitness carries a deliberately
// re-signed RunSingle (an extra trailing parameter). It MUST NOT
// satisfy CodeGraphReadAdmission: the seeded rejection test below
// proves a signature drift on any pinned method breaks the pin
// instead of sliding through silently.
type codeGraphReadAdmissionResignedWitness struct{}

func (codeGraphReadAdmissionResignedWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeGraphReadAdmissionResignedWitness) RunSingle(context.Context, string, map[string]any, int) (map[string]any, error) {
	return nil, nil
}

func (codeGraphReadAdmissionResignedWitness) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// codeGraphReadAdmissionMethodSet reflects the live
// CodeGraphReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeGraphReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeGraphReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeGraphReadAdmissionContractIsVersioned pins the #9669 G-a
// admission-contract version for the G-a 4 graph reads. The frozen
// method set below is the enforcement: an added, removed, renamed,
// or re-signed method fails this test until the frozen list is
// deliberately updated. Bump CodeGraphReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestCodeGraphReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeGraphReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeGraphReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeGraphReadAdmissionContractVersion, "v0")
	}
	got := codeGraphReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeGraphReadAdmission) {
		t.Fatalf("CodeGraphReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeGraphReadAdmission, len(frozenCodeGraphReadAdmission), CodeGraphReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeGraphReadAdmission[i] {
			t.Errorf("CodeGraphReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeGraphReadAdmission[i], CodeGraphReadAdmissionContractVersion)
		}
	}
}

// TestCodeGraphReadAdmissionRejectsResignedMethod is the seeded RED
// half of the contract proof: a witness whose RunSingle carries one
// extra parameter MUST NOT satisfy the pin. If a re-signed method
// ever slides through, this test fails and the frozen shape above
// can no longer be trusted to catch drift.
func TestCodeGraphReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeGraphReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeGraphReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeGraphReadAdmissionResignedWitness implements CodeGraphReadAdmission: a re-signed RunSingle must break the pin")
	}
	if !reflect.TypeOf(codeGraphReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeGraphReadAdmissionWitness does not implement CodeGraphReadAdmission: the minimal shape must satisfy the pin")
	}
}
