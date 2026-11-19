// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenPlaybookReadAdmission pins the exact method set behind the
// versioned #9669 W5 admission contract for the query-playbook reads
// (GET /api/v0/query-playbooks, POST /api/v0/query-playbooks/resolve):
// the catalog list, the playbook lookup, and the playbook resolve.
// The entries re-declare the staying static signatures whole in this
// playbook-scoped shape instead of aliasing the catalog surface, so
// no other family can cite this pin — a blanket pin is forbidden, and
// the widened shape needs arbiter review before satellite
// consumption. A renamed, added, removed, or re-signed method fails
// here until that list is deliberately updated; bump
// PlaybookReadAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// W-wave consumer: W5 playbooks (both oracle Playbooks rows).
var frozenPlaybookReadAdmission = []string{
	"ListPlaybookDefinitions func() []playbook.Definition",
	"LookupPlaybook func(string) (playbook.Definition, bool)",
	"ResolvePlaybook func(playbook.Definition, map[string]string) (playbook.ResolvedPlaybook, error)",
}

// playbookReadAdmissionWitness proves PlaybookReadAdmission is
// satisfiable with the minimal shape: one catalog list, one lookup,
// and one resolve, no wider catalog surface.
type playbookReadAdmissionWitness struct{}

func (playbookReadAdmissionWitness) ListPlaybookDefinitions() []Definition {
	return nil
}

func (playbookReadAdmissionWitness) LookupPlaybook(string) (Definition, bool) {
	return Definition{}, false
}

func (playbookReadAdmissionWitness) ResolvePlaybook(Definition, map[string]string) (ResolvedPlaybook, error) {
	return ResolvedPlaybook{}, nil
}

var _ PlaybookReadAdmission = playbookReadAdmissionWitness{}

// playbookReadAdmissionMethodSet reflects the live
// PlaybookReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func playbookReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestPlaybookReadAdmissionContractIsVersioned pins the #9669 W5
// admission-contract version for the query-playbook reads. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump PlaybookReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
func TestPlaybookReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if PlaybookReadAdmissionContractVersion != "v0" {
		t.Errorf("PlaybookReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", PlaybookReadAdmissionContractVersion, "v0")
	}
	got := playbookReadAdmissionMethodSet(t, (*PlaybookReadAdmission)(nil))
	if len(got) != len(frozenPlaybookReadAdmission) {
		t.Fatalf("PlaybookReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenPlaybookReadAdmission, len(frozenPlaybookReadAdmission), PlaybookReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenPlaybookReadAdmission[i] {
			t.Errorf("PlaybookReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenPlaybookReadAdmission[i], PlaybookReadAdmissionContractVersion)
		}
	}
}

// TestPlaybookReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the three re-declared catalog reads, with no versions
// marker, mutation, snapshot, or ledger surface riding along. The
// frozen method-set contract test pins the exact names; this test
// pins the exclusion the arbiter ruling demands.
func TestPlaybookReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*PlaybookReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 3 {
		t.Fatalf("PlaybookReadAdmission has %d methods, want exactly 3: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Versions", "Delete", "Write", "Snapshot", "Ledger", "Execute", "Publish",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("PlaybookReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededPlaybookDrift is a deliberately widened copy of the pin: the
// three frozen catalog reads plus a versions-marker read the
// playbook lane never issues. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededPlaybookDrift interface {
	PlaybookReadAdmission
	ListPlaybookVersions() []VersionRef
}

// TestPlaybookReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract
// test runs must report a mismatch on the planted widening. It passes
// on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestPlaybookReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := playbookReadAdmissionMethodSet(t, (*seededPlaybookDrift)(nil))
	if reflect.DeepEqual(got, frozenPlaybookReadAdmission) {
		t.Fatal("seeded widening (ListPlaybookVersions) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenPlaybookReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenPlaybookReadAdmission)+1)
	}
}
