// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenContentSearchReadAdmission pins the exact method set behind the
// versioned #9669 W4 admission contract for the Content search reads: the
// paged file/entity seams, the scoped and corpus-wide substring reads, the
// bounded unscoped walks, and the Dockerfile-install and PHP-config entity
// pages. The entries re-declare the ContentReader signatures in this
// Content-scoped shape instead of embedding the shared port, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden,
// and the widened shape needs arbiter review before satellite consumption.
//
// D-wave consumer: W4 search family, per the arbiter W4 ruling on #9669
// (APPROVED-WITH-CONDITIONS).
var frozenContentSearchReadAdmission = []string{
	"SearchDockerfileInstallEntities func(context.Context, string, []string, string, string, int, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntities func(context.Context, string, []string, string, int, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesUnscoped func(context.Context, string, int, int, querycontract.SearchCursor) (querycontract.EntitySearchPage, error)",
	"SearchEntityContent func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntityContentAnyRepo func(context.Context, string, int) ([]querycontract.EntityContent, error)",
	"SearchFileContent func(context.Context, string, string, int) ([]querycontract.FileContent, error)",
	"SearchFileContentAnyRepo func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"SearchFiles func(context.Context, string, []string, string, int, int) ([]querycontract.FileContent, error)",
	"SearchFilesUnscoped func(context.Context, string, int, int, string, string, int) (querycontract.FileSearchPage, error)",
	"SearchPHPConfigEntities func(context.Context, []string, string, *querycontract.PHPConfigCursor, int) ([]querycontract.EntityContent, error)",
}

// contentSearchReadAdmissionMethodSet reflects the live
// ContentSearchReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func contentSearchReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestContentSearchReadAdmissionContractIsVersioned pins the #9669 W4
// admission-contract version for the Content search reads. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps
// in the same diff.
func TestContentSearchReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if ContentSearchReadAdmissionContractVersion != "v0" {
		t.Errorf("ContentSearchReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", ContentSearchReadAdmissionContractVersion, "v0")
	}
	got := contentSearchReadAdmissionMethodSet(t, (*ContentSearchReadAdmission)(nil))
	if len(got) != len(frozenContentSearchReadAdmission) {
		t.Fatalf("ContentSearchReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenContentSearchReadAdmission, len(frozenContentSearchReadAdmission), ContentSearchReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenContentSearchReadAdmission[i] {
			t.Errorf("ContentSearchReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenContentSearchReadAdmission[i], ContentSearchReadAdmissionContractVersion)
		}
	}
}

// TestContentSearchReadAdmissionWireShapesAreSealed proves the W4 seal on
// the pin: exactly the ten re-declared search reads, with no point,
// list, ledger, stored-summary, or snapshot surface riding along. The
// frozen method-set contract test pins the exact names; this test pins
// the exclusions the arbiter ruling demands.
func TestContentSearchReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*ContentSearchReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 10 {
		t.Fatalf("ContentSearchReadAdmission has %d methods, want exactly 10: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Snapshot", "Ledger", "Summary", "StartupError",
			"ExactCase", "FileReference", "ByExactName", "ReferencingComponent",
			"ByLanguageAndType", "ListRepo", "GetFile", "GetEntity", "NonBlank",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("ContentSearchReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededContentSearchDrift is a deliberately widened copy of the pin: the
// ten frozen reads plus the exact-case AnyRepo twin that the census keeps
// OUT. It never ships; it proves the frozen-set comparison below can see
// a one-method widening.
type seededContentSearchDrift interface {
	ContentSearchReadAdmission
	SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]querycontract.FileContent, error)
}

// TestContentSearchReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestContentSearchReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := contentSearchReadAdmissionMethodSet(t, (*seededContentSearchDrift)(nil))
	if reflect.DeepEqual(got, frozenContentSearchReadAdmission) {
		t.Fatal("seeded widening (SearchFileContentAnyRepoExactCase) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenContentSearchReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenContentSearchReadAdmission)+1)
	}
}
