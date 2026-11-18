// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenCodeHardcodedSecretReadAdmission pins the exact method set behind
// the versioned #9669 admission contract for the W6c secret read: POST
// /api/v0/code/security/secrets/investigate admits its bounded
// investigation through InvestigateHardcodedSecrets, preferring the
// storage-path-reporting InvestigateHardcodedSecretsWithSource where the
// store satisfies it. The entries re-declare the staying
// HardcodedSecretInvestigator and HardcodedSecretSourceInvestigator
// signatures in this secret-scoped shape instead of embedding either
// port, so no other family can cite this pin — embedding or aliasing a
// shared port here would be a blanket pin, which the arbiter ruling
// forbids. A renamed, added, removed, or re-signed method fails here
// until that list is deliberately updated; bump
// CodeHardcodedSecretReadAdmissionContractVersion alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction boundary
// from drifting silently.
//
// W-wave consumer: W6c secret ONLY (per the arbiter supplemental ruling
// on eshu-hq/eshu-api#98, §2C). The pin stays isolated from the
// content-investigation pin on purpose: redacted secret-line text takes
// the least-privilege seal, never a bundle.
var frozenCodeHardcodedSecretReadAdmission = []string{
	"InvestigateHardcodedSecrets func(context.Context, codequery.HardcodedSecretInvestigationRequest) ([]codequery.HardcodedSecretFindingRow, error)",
	"InvestigateHardcodedSecretsWithSource func(context.Context, codequery.HardcodedSecretInvestigationRequest) ([]codequery.HardcodedSecretFindingRow, codequery.HardcodedSecretReadSource, error)",
}

// codeHardcodedSecretReadAdmissionWitness proves
// CodeHardcodedSecretReadAdmission is satisfiable with the minimal
// shape: the plain investigation plus the source-reporting twin, no
// wider store surface.
type codeHardcodedSecretReadAdmissionWitness struct{}

func (codeHardcodedSecretReadAdmissionWitness) InvestigateHardcodedSecrets(context.Context, HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, error) {
	return nil, nil
}

func (codeHardcodedSecretReadAdmissionWitness) InvestigateHardcodedSecretsWithSource(context.Context, HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, HardcodedSecretReadSource, error) {
	return nil, "", nil
}

var _ CodeHardcodedSecretReadAdmission = codeHardcodedSecretReadAdmissionWitness{}

// codeHardcodedSecretReadAdmissionResignedWitness carries a deliberately
// re-signed InvestigateHardcodedSecretsWithSource (an extra trailing
// parameter). It MUST NOT satisfy CodeHardcodedSecretReadAdmission: the
// seeded rejection test below proves a signature drift on any pinned
// method breaks the pin instead of sliding through silently.
type codeHardcodedSecretReadAdmissionResignedWitness struct{}

func (codeHardcodedSecretReadAdmissionResignedWitness) InvestigateHardcodedSecrets(context.Context, HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, error) {
	return nil, nil
}

func (codeHardcodedSecretReadAdmissionResignedWitness) InvestigateHardcodedSecretsWithSource(context.Context, HardcodedSecretInvestigationRequest, int) ([]HardcodedSecretFindingRow, HardcodedSecretReadSource, error) {
	return nil, "", nil
}

// codeHardcodedSecretReadAdmissionMethodSet reflects the live
// CodeHardcodedSecretReadAdmission method set into sorted "name
// signature" strings for comparison with the frozen list.
func codeHardcodedSecretReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*CodeHardcodedSecretReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestCodeHardcodedSecretReadAdmissionContractIsVersioned pins the #9669
// admission-contract version for the W6c secret read. The frozen method
// set below is the enforcement: an added, removed, renamed, or re-signed
// method fails this test until the frozen list is deliberately updated.
// Bump CodeHardcodedSecretReadAdmissionContractVersion alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
func TestCodeHardcodedSecretReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CodeHardcodedSecretReadAdmissionContractVersion != "v0" {
		t.Errorf("CodeHardcodedSecretReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CodeHardcodedSecretReadAdmissionContractVersion, "v0")
	}
	got := codeHardcodedSecretReadAdmissionMethodSet(t)
	if len(got) != len(frozenCodeHardcodedSecretReadAdmission) {
		t.Fatalf("CodeHardcodedSecretReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCodeHardcodedSecretReadAdmission, len(frozenCodeHardcodedSecretReadAdmission), CodeHardcodedSecretReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCodeHardcodedSecretReadAdmission[i] {
			t.Errorf("CodeHardcodedSecretReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCodeHardcodedSecretReadAdmission[i], CodeHardcodedSecretReadAdmissionContractVersion)
		}
	}
}

// TestCodeHardcodedSecretReadAdmissionRejectsResignedMethod is the
// seeded RED half of the contract proof: a witness whose
// InvestigateHardcodedSecretsWithSource carries one extra parameter
// MUST NOT satisfy the pin. If a re-signed method ever slides through,
// this test fails and the frozen shape above can no longer be trusted to
// catch drift.
func TestCodeHardcodedSecretReadAdmissionRejectsResignedMethod(t *testing.T) {
	t.Parallel()

	pin := reflect.TypeOf((*CodeHardcodedSecretReadAdmission)(nil)).Elem()
	if reflect.TypeOf(codeHardcodedSecretReadAdmissionResignedWitness{}).Implements(pin) {
		t.Errorf("codeHardcodedSecretReadAdmissionResignedWitness implements CodeHardcodedSecretReadAdmission: a re-signed InvestigateHardcodedSecretsWithSource must break the pin")
	}
	if !reflect.TypeOf(codeHardcodedSecretReadAdmissionWitness{}).Implements(pin) {
		t.Errorf("codeHardcodedSecretReadAdmissionWitness does not implement CodeHardcodedSecretReadAdmission: the minimal shape must satisfy the pin")
	}
}

// TestCodeHardcodedSecretReadAdmissionWireShapesAreSealed proves the
// seal on the pin: exactly the two re-declared secret reads, with graph
// traversal, search, list, file, topic, inventory, snapshot, or ledger
// surfaces riding along refused. The frozen method-set contract test
// pins the exact names; this test pins the exclusions the arbiter ruling
// demands.
func TestCodeHardcodedSecretReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CodeHardcodedSecretReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("CodeHardcodedSecretReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "List", "GetFile", "GetEntity",
			"Topic", "Inventory", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CodeHardcodedSecretReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededCodeHardcodedSecretDrift is a deliberately widened copy of the
// pin: the two frozen secret-investigation reads plus a bounded graph
// traversal, which the secrets lane reaches only through its own
// investigator, never as a pinned read. It never ships; it proves the
// frozen-set comparison below can see a one-method widening.
type seededCodeHardcodedSecretDrift interface {
	CodeHardcodedSecretReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestCodeHardcodedSecretReadAdmissionSeededDriftIsDetected is the RED
// half of the seeded-drift pair: the same reflect comparison the
// contract test runs must report a mismatch on the planted widening. It
// passes on a clean tree precisely because the guard fires on drift — a
// guard that cannot see the seeded method is no guard.
func TestCodeHardcodedSecretReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*seededCodeHardcodedSecretDrift)(nil)).Elem()
	var got []string
	for i := range rt.NumMethod() {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	if reflect.DeepEqual(got, frozenCodeHardcodedSecretReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCodeHardcodedSecretReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCodeHardcodedSecretReadAdmission)+1)
	}
}
