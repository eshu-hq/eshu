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

// frozenCodeSearchReadAdmission pins the exact method set behind the
// versioned #9669 W4 admission contract for the Code search reads: the
// graph run plus the eight content reads the code/search and
// symbols/search routes reach (the global entity-name index, the
// symbol-aware read, the candidate/name/content reads in their scoped,
// grant-loop, and corpus-wide shapes, and the by-ID hydration behind
// graph enrichment). RunSingle is OUT by construction: the code search
// family never issues a single-row graph read. The entries re-declare
// the GraphQuery/ContentReader signatures in this Code-scoped shape
// instead of embedding the shared ports, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden, and the widened
// shape needs arbiter review before satellite consumption.
//
// D-wave consumer: W4 search family, per the arbiter W4 ruling on #9669
// (APPROVED-WITH-CONDITIONS).
var frozenCodeSearchReadAdmission = []string{
	"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"SearchCodeCandidates func(context.Context, string, string, string, int, bool) ([]querycontract.EntityContent, []querycontract.EntityContent, error)",
	"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntityContent func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntityContentAnyRepo func(context.Context, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntityNames func(context.Context, entity.EntityNameSearch) ([]querycontract.EntityContent, error)",
	"SearchSymbols func(context.Context, codequery.SymbolSearchRequest) ([]querycontract.EntityContent, error)",
}

// codeSearchReadAdmissionMethodSet reflects the live
// CodeSearchReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeSearchReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCodeSearchReadAdmissionContractIsVersioned pins the #9669 W4
// admission-contract version for the Code search reads. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps
// in the same diff.
func TestCodeSearchReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeSearchReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeSearchReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeSearchReadAdmissionContractVersion, "v0")
	}
	got := codeSearchReadAdmissionMethodSet(t, (*CodeSearchReadAdmission)(nil))
	if len(got) != len(frozenCodeSearchReadAdmission) {
		t.Fatalf("CodeSearchReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeSearchReadAdmission, len(frozenCodeSearchReadAdmission), CodeSearchReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeSearchReadAdmission[i] {
			t.Errorf("CodeSearchReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeSearchReadAdmission[i], CodeSearchReadAdmissionContractVersion)
		}
	}
}

// TestCodeSearchReadAdmissionWireShapesAreSealed proves the W4 seal on
// the pin: exactly the nine re-declared code search reads, with
// RunSingle and every point, list (beyond by-ID hydration), ledger,
// stored-summary, or snapshot surface riding along refused. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusions the arbiter ruling demands.
func TestCodeSearchReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeSearchReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 9 {
		t.Fatalf("CodeSearchReadAdmission has %d methods, want exactly 9: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Snapshot", "Ledger", "Summary", "StartupError",
			"ExactCase", "FileReference", "ByExactName", "ReferencingComponent",
			"ByLanguageAndType", "ListRepoFiles", "ListRepoEntitiesByType",
			"ListRepositories", "GetFile", "GetEntity", "NonBlank",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeSearchReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCodeSearchDrift is a deliberately widened copy of the pin: the
// nine frozen reads plus RunSingle, which the census keeps OUT. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededCodeSearchDrift interface {
	CodeSearchReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestCodeSearchReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestCodeSearchReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := codeSearchReadAdmissionMethodSet(t, (*seededCodeSearchDrift)(nil))
	if reflect.DeepEqual(got, frozenCodeSearchReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeSearchReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeSearchReadAdmission)+1)
	}
}
