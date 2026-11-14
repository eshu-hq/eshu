// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenCodeGraphScanReadAdmission pins the exact method set behind the
// versioned #9669 admission contract for the W6b 4 graph-scan reads: POST
// /api/v0/code/complexity, /api/v0/code/quality/inspect,
// /api/v0/code/imports/investigate, and /api/v0/code/bundles admit
// their bounded graph traversal through Run and their by-ID enrichment
// through ListRepoEntitiesByIDs. The entries re-declare the staying
// GraphQuery and ContentStore signatures in this graph-scan-scoped shape
// instead of embedding either port, so no other family can cite this
// pin — embedding or aliasing a shared port here would be a blanket pin,
// which the arbiter ruling forbids. A renamed, added, removed, or
// re-signed method fails here until that list is deliberately updated;
// bump CodeGraphScanReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// W-wave consumer: W6b graph-scan ONLY (per the arbiter supplemental
// ruling on eshu-hq/eshu-api#98, §2A).
var frozenCodeGraphScanReadAdmission = []string{
	"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
}

// codeGraphScanReadAdmissionWitness proves
// CodeGraphScanReadAdmission is satisfiable with the minimal shape: one
// graph traversal plus the one by-ID enrichment read, no wider store
// surface.
type codeGraphScanReadAdmissionWitness struct{}

func (codeGraphScanReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeGraphScanReadAdmissionWitness) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

var _ CodeGraphScanReadAdmission = codeGraphScanReadAdmissionWitness{}

// codeGraphScanReadAdmissionResignedWitness carries a deliberately
// re-signed ListRepoEntitiesByIDs (an extra trailing parameter). It MUST
// NOT satisfy CodeGraphScanReadAdmission: the seeded rejection test
// below proves a signature drift on any pinned method breaks the pin
// instead of sliding through silently.
type codeGraphScanReadAdmissionResignedWitness struct{}

func (codeGraphScanReadAdmissionResignedWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeGraphScanReadAdmissionResignedWitness) ListRepoEntitiesByIDs(context.Context, string, []string, int, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// codeGraphScanReadAdmissionMethodSet reflects the live
// CodeGraphScanReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeGraphScanReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeGraphScanReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeGraphScanReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the W6b 4 graph-scan reads. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is deliberately
// updated. Bump CodeGraphScanReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestCodeGraphScanReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeGraphScanReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeGraphScanReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeGraphScanReadAdmissionContractVersion, "v0")
	}
	got := codeGraphScanReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeGraphScanReadAdmission) {
		t.Fatalf("CodeGraphScanReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeGraphScanReadAdmission, len(frozenCodeGraphScanReadAdmission), CodeGraphScanReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeGraphScanReadAdmission[i] {
			t.Errorf("CodeGraphScanReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeGraphScanReadAdmission[i], CodeGraphScanReadAdmissionContractVersion)
		}
	}
}

// TestCodeGraphScanReadAdmissionRejectsResignedMethod is the seeded RED
// half of the contract proof: a witness whose ListRepoEntitiesByIDs
// carries one extra parameter MUST NOT satisfy the pin. If a re-signed
// method ever slides through, this test fails and the frozen shape above
// can no longer be trusted to catch drift.
func TestCodeGraphScanReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeGraphScanReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeGraphScanReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeGraphScanReadAdmissionResignedWitness implements CodeGraphScanReadAdmission: a re-signed ListRepoEntitiesByIDs must break the pin")
	}
	if !reflect.TypeOf(codeGraphScanReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeGraphScanReadAdmissionWitness does not implement CodeGraphScanReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestCodeGraphScanReadAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the two re-declared graph-scan reads, with RunSingle
// and every search, file, list-twin, catalog, grant-scoped, or snapshot
// surface riding along refused. The frozen method-set contract test pins
// the exact names; this test pins the exclusions the arbiter ruling
// demands.
func TestCodeGraphScanReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeGraphScanReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("CodeGraphScanReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "GetFile", "GetEntityContent",
			"ListRepoFiles", "ListRepoEntitiesByType", "ListRepositories",
			"MatchRepositories", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeGraphScanReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}
