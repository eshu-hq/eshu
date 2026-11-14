// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// frozenCodeRelationshipReadAdmission pins the exact method set behind the
// versioned #9669 G-b admission contract for the two relationship routes:
// POST /api/v0/code/relationships and POST
// /api/v0/code/relationships/story admit their bounded graph traversals
// through Run and RunSingle, their twelve ContentStore reads, and their
// three grant-scoped reads (the grant three are IN, per the arbiter
// SUFFICIENT ruling on eshu-hq/eshu-api#122: two fail closed and the
// third changes scoped language-story answers by fallback, so a pin
// missing them would license an accuracy regression). The entries
// re-declare the staying GraphQuery, ContentStore, and grant-port
// signatures in this relationships-scoped shape instead of embedding any
// of those ports, so no other family can cite this pin — embedding or
// aliasing a shared port here would be a blanket pin, which the rulings
// forbid. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// CodeRelationshipReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// W-wave consumer: relationships/story G-b ONLY (per the arbiter
// SUFFICIENT ruling on eshu-hq/eshu-api#122, SPLIT). The satellite
// records this pin in a follow-up; a satellite PR consuming an unminted
// pin is invalid. Arbiter review of this pin precedes satellite
// termination of the G-b legs. G-a graph, divergence, flow, and every
// other family are NOT approved here.
var frozenCodeRelationshipReadAdmission = []string{
	"GetEntityContent func(context.Context, string) (*querycontract.EntityContent, error)",
	"GetEntityContentInRepositories func(context.Context, string, []string) (*querycontract.EntityContent, error)",
	"ListRepoEntitiesByIDs func(context.Context, string, []string, int) ([]querycontract.EntityContent, error)",
	"ListRepoEntitiesByType func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"ListRepositories func(context.Context) ([]querycontract.RepositoryCatalogEntry, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"RunSingle func(context.Context, string, map[string]interface {}) (map[string]interface {}, error)",
	"SearchEntitiesByExactName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByExactNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByLanguageAndType func(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByLanguageAndTypeForAccess func(context.Context, taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByName func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByNameAnyRepo func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesByNameInRepositories func(context.Context, []string, string, string, int) ([]querycontract.EntityContent, error)",
	"SearchEntitiesReferencingComponent func(context.Context, string, string, int) ([]querycontract.EntityContent, error)",
}

// codeRelationshipReadAdmissionWitness proves
// CodeRelationshipReadAdmission is satisfiable with the minimal shape:
// the two graph traversals, the twelve ContentStore reads, and the
// three grant-scoped reads — no wider store surface.
type codeRelationshipReadAdmissionWitness struct{}

func (codeRelationshipReadAdmissionWitness) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByExactName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByExactNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByLanguageAndType(context.Context, string, string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) GetEntityContentInRepositories(context.Context, string, []string) (*querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByNameInRepositories(context.Context, []string, string, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

func (codeRelationshipReadAdmissionWitness) SearchEntitiesByLanguageAndTypeForAccess(context.Context, taxonomy.LanguageEntitySearch) ([]querycontract.EntityContent, error) {
	return nil, nil
}

var _ CodeRelationshipReadAdmission = codeRelationshipReadAdmissionWitness{}

// codeRelationshipReadAdmissionResignedWitness carries a deliberately
// re-signed SearchEntitiesByNameInRepositories (an extra trailing
// parameter). It MUST NOT satisfy CodeRelationshipReadAdmission: the
// seeded rejection test below proves a signature drift on any pinned
// method breaks the pin instead of sliding through silently.
type codeRelationshipReadAdmissionResignedWitness struct {
	codeRelationshipReadAdmissionWitness
}

func (codeRelationshipReadAdmissionResignedWitness) SearchEntitiesByNameInRepositories(context.Context, []string, string, string, int, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// codeRelationshipReadAdmissionWidenedWitness carries one extra method
// beyond the frozen seventeen. It still satisfies the pin (Go
// structural typing is one-directional by design): widening is
// rejected by the frozen method-set length check in
// TestCodeRelationshipReadAdmissionContractIsVersioned, not by
// assignability. The widening test below proves the frozen list
// would catch exactly this shape.
type codeRelationshipReadAdmissionWidenedWitness struct {
	codeRelationshipReadAdmissionWitness
}

func (codeRelationshipReadAdmissionWidenedWitness) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, nil
}

// codeRelationshipReadAdmissionMethodSet reflects the live
// CodeRelationshipReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func codeRelationshipReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeRelationshipReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeRelationshipReadAdmissionContractIsVersioned pins the #9669
// G-b admission-contract version for the two relationship routes. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list
// is deliberately updated. Bump
// CodeRelationshipReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestCodeRelationshipReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeRelationshipReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeRelationshipReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeRelationshipReadAdmissionContractVersion, "v0")
	}
	got := codeRelationshipReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeRelationshipReadAdmission) {
		t.Fatalf("CodeRelationshipReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeRelationshipReadAdmission, len(frozenCodeRelationshipReadAdmission), CodeRelationshipReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeRelationshipReadAdmission[i] {
			t.Errorf("CodeRelationshipReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeRelationshipReadAdmission[i], CodeRelationshipReadAdmissionContractVersion)
		}
	}
}

// TestCodeRelationshipReadAdmissionRejectsResignedMethod is the seeded
// RED half of the contract proof: a witness whose
// SearchEntitiesByNameInRepositories carries one extra parameter MUST
// NOT satisfy the pin. If a re-signed method ever slides through, this
// test fails and the frozen shape above can no longer be trusted to
// catch drift.
func TestCodeRelationshipReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeRelationshipReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeRelationshipReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeRelationshipReadAdmissionResignedWitness implements CodeRelationshipReadAdmission: a re-signed SearchEntitiesByNameInRepositories must break the pin")
	}
	if !reflect.TypeOf(codeRelationshipReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeRelationshipReadAdmissionWitness does not implement CodeRelationshipReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestCodeRelationshipReadAdmissionWideningIsCaughtByFrozenSet proves
// the widening half of the enforcement: a witness carrying an
// eighteenth method still satisfies the pin by assignability, so the
// frozen seventeen-entry list — not Implements — is what rejects a
// widened shape. If the widened witness ever stopped satisfying the
// pin, or ever matched the frozen length, this test fails and the
// one-directional enforcement story above can no longer be trusted.
func TestCodeRelationshipReadAdmissionWideningIsCaughtByFrozenSet(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeRelationshipReadAdmission)(nil)).Elem()
	if !reflect.TypeOf(codeRelationshipReadAdmissionWidenedWitness{}).Implements(pin) {
		t.Errorf("codeRelationshipReadAdmissionWidenedWitness does not implement CodeRelationshipReadAdmission: extra methods must not break assignability (the frozen list rejects widening, not Implements)")
	}
	widened := reflect.TypeOf(codeRelationshipReadAdmissionWidenedWitness{})
	var count int
	for i := 0; i < widened.NumMethod(); i++ {
		if _, ok := pin.MethodByName(widened.Method(i).Name); ok {
			count++
		}
	}
	if count != len(frozenCodeRelationshipReadAdmission) {
		t.Errorf("widened witness shares %d pin methods, want %d: the witness no longer spans the frozen set", count, len(frozenCodeRelationshipReadAdmission))
	}
	if widened.NumMethod() != len(frozenCodeRelationshipReadAdmission)+1 {
		t.Errorf("widened witness carries %d methods, want %d (frozen set plus one): the widening this test guards is gone", widened.NumMethod(), len(frozenCodeRelationshipReadAdmission)+1)
	}
}
