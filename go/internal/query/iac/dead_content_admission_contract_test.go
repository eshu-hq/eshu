// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// frozenIaCDeadContentAdmission pins the exact method set behind the
// versioned #9669 G4 supplemental admission contract for the
// dead-route content fallback behind POST /api/v0/iac/dead: the two
// catalog reads the route exercises (ListRepositories for the
// repo-name enrichment, MatchRepositories for the access-filtered
// selector resolution) plus the two file reads of the blank-file
// reload (ListRepoFiles, GetFileContent), re-declared here in this
// IaC-scoped shape instead of embedding the shared ContentStore
// port, so no other family can cite this pin. A renamed, added,
// removed, or re-signed method fails here until the list is
// deliberately updated; bump
// IaCDeadContentAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
var frozenIaCDeadContentAdmission = []string{
	"GetFileContent func(context.Context, string, string) (*querycontract.FileContent, error)",
	"ListRepoFiles func(context.Context, string, int) ([]querycontract.FileContent, error)",
	"ListRepositories func(context.Context) ([]querycontract.RepositoryCatalogEntry, error)",
	"MatchRepositories func(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error)",
}

// iaCDeadContentAdmissionWitness proves IaCDeadContentAdmission is
// satisfiable with the minimal shape: the two catalog reads plus the
// two file reads, no wider store surface.
type iaCDeadContentAdmissionWitness struct{}

func (iaCDeadContentAdmissionWitness) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	return nil, nil
}

func (iaCDeadContentAdmissionWitness) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, nil
}

func (iaCDeadContentAdmissionWitness) ListRepositories(context.Context) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (iaCDeadContentAdmissionWitness) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, nil
}

var _ IaCDeadContentAdmission = iaCDeadContentAdmissionWitness{}

// iaCDeadContentAdmissionMethodSet reflects the live
// IaCDeadContentAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func iaCDeadContentAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestIaCDeadContentAdmissionContractIsVersioned pins the #9669 G4
// supplemental admission-contract version for the dead-route content
// fallback. The frozen method set is the enforcement: an added,
// removed, renamed, or re-signed method fails this test until the
// frozen list is deliberately updated.
func TestIaCDeadContentAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if IaCDeadContentAdmissionContractVersion != "v0" {
		t.Errorf("IaCDeadContentAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", IaCDeadContentAdmissionContractVersion, "v0")
	}
	got := iaCDeadContentAdmissionMethodSet(t, (*IaCDeadContentAdmission)(nil))
	if len(got) != len(frozenIaCDeadContentAdmission) {
		t.Fatalf("IaCDeadContentAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenIaCDeadContentAdmission, len(frozenIaCDeadContentAdmission), IaCDeadContentAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenIaCDeadContentAdmission[i] {
			t.Errorf("IaCDeadContentAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenIaCDeadContentAdmission[i], IaCDeadContentAdmissionContractVersion)
		}
	}
}

// TestIaCDeadContentAdmissionWireShapesAreSealed proves the seal on
// the pin: exactly the four re-declared content reads, with no graph,
// search, mutation, or snapshot surface riding along. The frozen
// method-set contract test pins the exact names; this test pins the
// exclusion the arbiter ruling demands.
func TestIaCDeadContentAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*IaCDeadContentAdmission)(nil)).Elem()
	if rt.NumMethod() != 4 {
		t.Fatalf("IaCDeadContentAdmission has %d methods, want exactly 4: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Delete", "Create", "Write", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("IaCDeadContentAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededIaCDeadContentDrift is a deliberately widened copy of the pin:
// the four frozen content reads plus a single-row graph read, which
// the dead fallback never issues (RunSingle stays OUT by
// construction). It never ships; it proves the frozen-set comparison
// below can see a one-method widening.
type seededIaCDeadContentDrift interface {
	IaCDeadContentAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestIaCDeadContentAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestIaCDeadContentAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := iaCDeadContentAdmissionMethodSet(t, (*seededIaCDeadContentDrift)(nil))
	if reflect.DeepEqual(got, frozenIaCDeadContentAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenIaCDeadContentAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenIaCDeadContentAdmission)+1)
	}
}
