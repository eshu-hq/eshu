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

// frozenSSMHostInventoryReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the SSM host inventory read
// (GET /api/v0/cloud/ssm-host-inventory): the admitted-facts read plus the
// bounded graph read — 2 seed reads, SSMHostActiveFacts, 2 node reads, with
// graph reads skipped when Graph is nil or the page is empty. RunSingle
// stays OUT by construction: the lane never issues a single-row graph read.
// The entries re-declare the staying signatures in this SSM-scoped shape
// instead of embedding the shared ports, so no other family can cite this
// pin — a blanket or shared-port pin is forbidden, and the widened shape
// needs arbiter review before satellite consumption. ssmhost.Bounds
// validation, Now, and the zero-bounds 503 are handler behavior OUT.
//
// Satellite consumer: the W8b-wide2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenSSMHostInventoryReadAdmission = []string{
	"Run func(context.Context, string, map[string]interface {}) ([]map[string]interface {}, error)",
	"SSMHostActiveFacts func(context.Context, query.ssmHostInventoryFilter) ([]query.ssmAdmittedFact, error)",
}

// ssmHostInventoryReadAdmissionMethodSet reflects the live
// SSMHostInventoryReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func ssmHostInventoryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestSSMHostInventoryReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the SSM host inventory read. The version
// value plus the frozen method set below are the enforcement: a breaking
// change to the admission shape fails this test until the version bumps in
// the same diff.
func TestSSMHostInventoryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SSMHostInventoryReadAdmissionContractVersion != "v0" {
		t.Errorf("SSMHostInventoryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SSMHostInventoryReadAdmissionContractVersion, "v0")
	}
	got := ssmHostInventoryReadAdmissionMethodSet(t, (*SSMHostInventoryReadAdmission)(nil))
	if len(got) != len(frozenSSMHostInventoryReadAdmission) {
		t.Fatalf("SSMHostInventoryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSSMHostInventoryReadAdmission, len(frozenSSMHostInventoryReadAdmission), SSMHostInventoryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSSMHostInventoryReadAdmission[i] {
			t.Errorf("SSMHostInventoryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSSMHostInventoryReadAdmission[i], SSMHostInventoryReadAdmissionContractVersion)
		}
	}
}

// TestSSMHostInventoryReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the two re-declared SSM reads, with RunSingle and every
// search, list, content, or snapshot surface riding along refused. The frozen
// method-set contract test pins the exact names; this test pins the exclusions
// the arbiter ruling demands.
func TestSSMHostInventoryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SSMHostInventoryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("SSMHostInventoryReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"RunSingle", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("SSMHostInventoryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// ssmHostInventoryAdmissionCannedStore is the pin-satisfying double behind
// the wire-shape proof: it implements exactly the two pinned reads over fixed
// canned values and is compile-asserted against the pin, so the proof below
// runs through the admission shape, not the wider staying ports.
type ssmHostInventoryAdmissionCannedStore struct {
	calls []string
}

var _ SSMHostInventoryReadAdmission = (*ssmHostInventoryAdmissionCannedStore)(nil)

func (s *ssmHostInventoryAdmissionCannedStore) SSMHostActiveFacts(_ context.Context, _ ssmHostInventoryFilter) ([]ssmAdmittedFact, error) {
	s.calls = append(s.calls, "SSMHostActiveFacts")
	return []ssmAdmittedFact{{}}, nil
}

func (s *ssmHostInventoryAdmissionCannedStore) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	s.calls = append(s.calls, "Run")
	return []map[string]any{{"ssm": "node-row"}}, nil
}

// TestSSMHostInventoryReadAdmissionWireShapesServeTheRoute proves the frozen
// shape is sufficient: both pinned reads answer through the pin-typed canned
// store, including the graph leg the handler takes when Graph is set and the
// page is non-empty.
func TestSSMHostInventoryReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin SSMHostInventoryReadAdmission = &ssmHostInventoryAdmissionCannedStore{}
	facts, err := pin.SSMHostActiveFacts(context.Background(), ssmHostInventoryFilter{})
	if err != nil {
		t.Fatalf("SSMHostActiveFacts through the pin failed: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("SSMHostActiveFacts through the pin returned %d facts, want 1", len(facts))
	}
	rows, err := pin.Run(context.Background(), "canned", map[string]any{})
	if err != nil {
		t.Fatalf("Run through the pin failed: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("Run through the pin returned %d rows, want 1", len(rows))
	}
}

// seededSSMHostInventoryDrift is a deliberately widened copy of the pin: the
// two frozen reads plus a RunSingle the SSM lane never issues. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededSSMHostInventoryDrift interface {
	SSMHostInventoryReadAdmission
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// TestSSMHostInventoryReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestSSMHostInventoryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := ssmHostInventoryReadAdmissionMethodSet(t, (*seededSSMHostInventoryDrift)(nil))
	if reflect.DeepEqual(got, frozenSSMHostInventoryReadAdmission) {
		t.Fatal("seeded widening (RunSingle) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSSMHostInventoryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSSMHostInventoryReadAdmission)+1)
	}
}
