// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// frozenCodeLanguageQueryReadAdmission pins the exact method set behind
// the versioned #9669 admission contract for the W6c language-query read:
// POST /api/v0/code/language-query admits its bounded graph traversal
// through Run, its unscoped content-entity lookup through
// SearchEntitiesByLanguageAndType, its grant-bound lookup through
// SearchEntitiesByLanguageAndTypeForAccess, and its id-anchored metadata
// enrichment through ListEntityMetadataByIDsForAccess. The entries
// re-declare the staying GraphQuery, ContentStore, and taxonomy search
// signatures in this language-scoped shape instead of embedding any
// port, so no other family can cite this pin — embedding or aliasing a
// shared port here would be a blanket pin, which the arbiter ruling
// forbids. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// CodeLanguageQueryReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// W-wave consumer: W6c language-query ONLY (per the arbiter supplemental
// ruling on eshu-hq/eshu-api#98, §2E). This pin lives in package
// language, not codequery: language.Handler mounts the route and
// codequery cannot mount it without an import cycle
// (codequery/handler.go records why).
var frozenCodeLanguageQueryReadAdmission = []string{
	"ListEntityMetadataByIDsForAccess func(context.Context, taxonomy.LanguageEntityIDLookup) ([]querycontract.EntityContent, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"SearchEntitiesByLanguageAndType func(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByLanguageAndTypeForAccess func(context.Context, taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error)",
}

// codeLanguageQueryReadAdmissionWitness proves
// CodeLanguageQueryReadAdmission is satisfiable with the minimal shape:
// the one graph traversal plus the three content reads (unscoped,
// grant-bound search, id-anchored metadata), no wider store surface.
type codeLanguageQueryReadAdmissionWitness struct{}

func (codeLanguageQueryReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeLanguageQueryReadAdmissionWitness) SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeLanguageQueryReadAdmissionWitness) SearchEntitiesByLanguageAndTypeForAccess(context.Context, taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeLanguageQueryReadAdmissionWitness) ListEntityMetadataByIDsForAccess(context.Context, taxonomy.LanguageEntityIDLookup) ([]querycontract.EntityContent, error) {
	return nil, nil
}

var _ CodeLanguageQueryReadAdmission = codeLanguageQueryReadAdmissionWitness{}

// codeLanguageQueryReadAdmissionResignedWitness carries a deliberately
// re-signed SearchEntitiesByLanguageAndTypeForAccess (an extra trailing
// parameter). It MUST NOT satisfy CodeLanguageQueryReadAdmission: the
// seeded rejection test below proves a signature drift on any pinned
// method breaks the pin instead of sliding through silently.
type codeLanguageQueryReadAdmissionResignedWitness struct{}

func (codeLanguageQueryReadAdmissionResignedWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeLanguageQueryReadAdmissionResignedWitness) SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeLanguageQueryReadAdmissionResignedWitness) SearchEntitiesByLanguageAndTypeForAccess(context.Context, taxonomy.LanguageEntitySearch, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeLanguageQueryReadAdmissionResignedWitness) ListEntityMetadataByIDsForAccess(context.Context, taxonomy.LanguageEntityIDLookup) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// codeLanguageQueryReadAdmissionMethodSet reflects the live
// CodeLanguageQueryReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func codeLanguageQueryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCodeLanguageQueryReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the W6c language-query read. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is deliberately
// updated. Bump CodeLanguageQueryReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestCodeLanguageQueryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeLanguageQueryReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeLanguageQueryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeLanguageQueryReadAdmissionContractVersion, "v0")
	}
	got := codeLanguageQueryReadAdmissionMethodSet(t, (*CodeLanguageQueryReadAdmission)(nil))
	if len(got) != len(frozenCodeLanguageQueryReadAdmission) {
		t.Fatalf("CodeLanguageQueryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeLanguageQueryReadAdmission, len(frozenCodeLanguageQueryReadAdmission), CodeLanguageQueryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeLanguageQueryReadAdmission[i] {
			t.Errorf("CodeLanguageQueryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeLanguageQueryReadAdmission[i], CodeLanguageQueryReadAdmissionContractVersion)
		}
	}
}

// TestCodeLanguageQueryReadAdmissionRejectsResignedMethod is the seeded
// RED half of the contract proof: a witness whose
// SearchEntitiesByLanguageAndTypeForAccess carries one extra parameter
// MUST NOT satisfy the pin. If a re-signed method ever slides through,
// this test fails and the frozen shape above can no longer be trusted to
// catch drift.
func TestCodeLanguageQueryReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeLanguageQueryReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeLanguageQueryReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeLanguageQueryReadAdmissionResignedWitness implements CodeLanguageQueryReadAdmission: a re-signed SearchEntitiesByLanguageAndTypeForAccess must break the pin")
	}
	if !reflect.TypeOf(codeLanguageQueryReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeLanguageQueryReadAdmissionWitness does not implement CodeLanguageQueryReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestCodeLanguageQueryReadAdmissionWireShapesAreSealed proves the seal
// on the pin: exactly the four re-declared language reads, with
// RunSingle and every name, exact-name, component, file, catalog, or
// story surface riding along refused. The frozen method-set contract
// test pins the exact names; this test pins the exclusions the arbiter
// ruling demands. The per-repo fallback loops and the page-read fallback
// behind the two optional interfaces stay handler behavior OUT: the
// satellite keeps them, this pin carries the read shape only.
func TestCodeLanguageQueryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeLanguageQueryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 4 {
		t.Fatalf("CodeLanguageQueryReadAdmission has %d methods, want exactly 4: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "SearchEntitiesByName", "ByExactName",
			"ReferencingComponent", "ListRepoFiles", "ListRepoEntitiesByType",
			"ListRepositories", "MatchRepositories", "NonBlank", "Snapshot",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeLanguageQueryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCodeLanguageQueryDrift is a deliberately widened copy of the pin:
// the four frozen language reads plus a single-row graph read, which the
// language-query lane keeps OUT. It never ships; it proves the frozen-set
// comparison below can see a one-method widening.
type seededCodeLanguageQueryDrift interface {
	CodeLanguageQueryReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestCodeLanguageQueryReadAdmissionSeededDriftIsDetected is the RED half
// of the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestCodeLanguageQueryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := codeLanguageQueryReadAdmissionMethodSet(t, (*seededCodeLanguageQueryDrift)(nil))
	if reflect.DeepEqual(got, frozenCodeLanguageQueryReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeLanguageQueryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeLanguageQueryReadAdmission)+1)
	}
}
