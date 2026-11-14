// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/incident/store"
)

// frozenIncidentContextReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the incident context read (GET
// /api/v0/incidents/{incident_id}/context): the context read plus the scoped
// authorizer read. The authorizer is IN, not OUT: scoped callers hit
// ResolveDurableIncidentRepositories before the store read, and a pin over
// the context read alone would leave the fail-closed gate unpinned. The
// entries re-declare the staying model port signatures in this
// incident-scoped shape instead of embedding the shared ports, so no other
// family can cite this pin — a blanket or shared-port pin is forbidden, and
// the widened shape needs arbiter review before satellite consumption.
//
// Satellite consumer: the W8b-wide2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenIncidentContextReadAdmission = []string{
	"ReadIncidentContext func(context.Context, model.IncidentContextFilter) (model.IncidentContextSnapshot, error)",
	"ResolveDurableIncidentRepositories func(context.Context, string, string, string) ([]string, error)",
}

// incidentContextReadAdmissionMethodSet reflects the live
// IncidentContextReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func incidentContextReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestIncidentContextReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the incident context read. The version value
// plus the frozen method set below are the enforcement: a breaking change to
// the admission shape fails this test until the version bumps in the same
// diff.
func TestIncidentContextReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if IncidentContextReadAdmissionContractVersion != "v0" {
		t.Errorf("IncidentContextReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", IncidentContextReadAdmissionContractVersion, "v0")
	}
	got := incidentContextReadAdmissionMethodSet(t, (*IncidentContextReadAdmission)(nil))
	if len(got) != len(frozenIncidentContextReadAdmission) {
		t.Fatalf("IncidentContextReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenIncidentContextReadAdmission, len(frozenIncidentContextReadAdmission), IncidentContextReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenIncidentContextReadAdmission[i] {
			t.Errorf("IncidentContextReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenIncidentContextReadAdmission[i], IncidentContextReadAdmissionContractVersion)
		}
	}
}

// TestIncidentContextReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the two re-declared incident reads, with graph, search, list,
// content, or snapshot surfaces riding along refused. The frozen method-set
// contract test pins the exact names; this test pins the exclusions the
// arbiter ruling demands. The identical-envelope denial stays handler
// behavior OUT.
func TestIncidentContextReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*IncidentContextReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("IncidentContextReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("IncidentContextReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// The satisfier assertions name the production adapters the wiring builds
// today (store.NewStoreWithReadStore plus
// store.NewPostgresIncidentRepositoryAuthorizerWithReadStore in
// go/cmd/api/wiring_handlers.go). This test file may import the store — the
// pin file itself must not, per this package's ownership rule — so the
// witness pair lives here: the concrete Postgres context store and the
// concrete Postgres repository authorizer jointly serve the pinned reads, so
// the pin is satisfiable with the minimal shape, no wider store surface.
var (
	_ model.IncidentContextStore         = store.PostgresIncidentContextStore{}
	_ model.IncidentRepositoryAuthorizer = store.PostgresIncidentRepositoryAuthorizer{}
)

// incidentContextAdmissionCannedStore is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the two pinned reads over fixed
// canned values and is compile-asserted against the pin, so the proof below
// runs through the admission shape, not the wider staying ports.
type incidentContextAdmissionCannedStore struct {
	calls []string
}

var _ IncidentContextReadAdmission = (*incidentContextAdmissionCannedStore)(nil)

func (s *incidentContextAdmissionCannedStore) ReadIncidentContext(_ context.Context, _ model.IncidentContextFilter) (model.IncidentContextSnapshot, error) {
	s.calls = append(s.calls, "ReadIncidentContext")
	return model.IncidentContextSnapshot{}, nil
}

func (s *incidentContextAdmissionCannedStore) ResolveDurableIncidentRepositories(_ context.Context, _, _, _ string) ([]string, error) {
	s.calls = append(s.calls, "ResolveDurableIncidentRepositories")
	return []string{"repo-canned"}, nil
}

// TestIncidentContextReadAdmissionWireShapesServeTheRoute proves the frozen
// shape is sufficient: both pinned reads answer through the pin-typed canned
// store, including the scoped-only authorizer leg that gates scoped callers.
func TestIncidentContextReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin IncidentContextReadAdmission = &incidentContextAdmissionCannedStore{}
	if _, err := pin.ReadIncidentContext(context.Background(), model.IncidentContextFilter{}); err != nil {
		t.Fatalf("ReadIncidentContext through the pin failed: %v", err)
	}
	repos, err := pin.ResolveDurableIncidentRepositories(context.Background(), "pagerduty", "PD-1", "scope-1")
	if err != nil {
		t.Fatalf("ResolveDurableIncidentRepositories through the pin failed: %v", err)
	}
	if len(repos) != 1 || repos[0] != "repo-canned" {
		t.Errorf("ResolveDurableIncidentRepositories through the pin = %q, want [repo-canned]: the scoped gate must be servable through the pin", repos)
	}
}

// seededIncidentContextDrift is a deliberately widened copy of the pin: the
// two frozen reads plus a graph Run the incident lane never issues. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededIncidentContextDrift interface {
	IncidentContextReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestIncidentContextReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestIncidentContextReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := incidentContextReadAdmissionMethodSet(t, (*seededIncidentContextDrift)(nil))
	if reflect.DeepEqual(got, frozenIncidentContextReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenIncidentContextReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenIncidentContextReadAdmission)+1)
	}
}
