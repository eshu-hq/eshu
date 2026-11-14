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

// frozenCloudInventoryReadAdmission pins the exact method set behind the
// versioned #9669 W8b admission contract for the cloud inventory read (GET
// /api/v0/cloud/inventory): the canonical-identity read plus the conditional
// pre-rollout probe second read. The probe is IN, not OUT: listInventory
// calls CloudInventoryPreRolloutEvidenceExists whenever an account-alias
// filter on page 0 matched zero rows, and that call changes warning_flags
// bytes. The entries re-declare the staying cloudInventoryReadModelStore
// signatures whole in this cloud-inventory-scoped shape instead of embedding
// the shared port, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter review
// before satellite consumption. Unexported filter and read-model types keep
// this pin in package query.
//
// Satellite consumer: the W8b-wide2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid.
var frozenCloudInventoryReadAdmission = []string{
	"CloudInventoryIdentities func(context.Context, query.cloudInventoryFilter) (query.cloudInventoryListReadModel, error)",
	"CloudInventoryPreRolloutEvidenceExists func(context.Context, query.cloudInventoryFilter) (bool, error)",
}

// cloudInventoryReadAdmissionMethodSet reflects the live
// CloudInventoryReadAdmission method set into sorted "name signature" strings
// for comparison with the frozen list.
func cloudInventoryReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestCloudInventoryReadAdmissionContractIsVersioned pins the #9669 W8b
// admission-contract version for the cloud inventory read. The version value
// plus the frozen method set below are the enforcement: a breaking change to
// the admission shape fails this test until the version bumps in the same
// diff.
func TestCloudInventoryReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if CloudInventoryReadAdmissionContractVersion != "v0" {
		t.Errorf("CloudInventoryReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", CloudInventoryReadAdmissionContractVersion, "v0")
	}
	got := cloudInventoryReadAdmissionMethodSet(t, (*CloudInventoryReadAdmission)(nil))
	if len(got) != len(frozenCloudInventoryReadAdmission) {
		t.Fatalf("CloudInventoryReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenCloudInventoryReadAdmission, len(frozenCloudInventoryReadAdmission), CloudInventoryReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenCloudInventoryReadAdmission[i] {
			t.Errorf("CloudInventoryReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenCloudInventoryReadAdmission[i], CloudInventoryReadAdmissionContractVersion)
		}
	}
}

// TestCloudInventoryReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the two re-declared cloud inventory reads, with graph, search,
// list, content, or snapshot surfaces riding along refused. The frozen
// method-set contract test pins the exact names; this test pins the exclusions
// the arbiter ruling demands. This pin is its own over the canonical-identity
// read model: citing MultiCloud drift, TerraformDrift, or IaC pins here is
// void.
func TestCloudInventoryReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*CloudInventoryReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("CloudInventoryReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("CloudInventoryReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// cloudInventoryAdmissionCannedStore is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the two pinned reads over fixed
// canned values and is compile-asserted against the pin, so the proof below
// runs through the admission shape, not the wider staying port.
type cloudInventoryAdmissionCannedStore struct {
	preRollout bool
	calls      []string
}

var _ CloudInventoryReadAdmission = (*cloudInventoryAdmissionCannedStore)(nil)

func (s *cloudInventoryAdmissionCannedStore) CloudInventoryIdentities(_ context.Context, _ cloudInventoryFilter) (cloudInventoryListReadModel, error) {
	s.calls = append(s.calls, "CloudInventoryIdentities")
	return cloudInventoryListReadModel{}, nil
}

func (s *cloudInventoryAdmissionCannedStore) CloudInventoryPreRolloutEvidenceExists(_ context.Context, _ cloudInventoryFilter) (bool, error) {
	s.calls = append(s.calls, "CloudInventoryPreRolloutEvidenceExists")
	return s.preRollout, nil
}

// TestCloudInventoryReadAdmissionWireShapesServeTheRoute proves the frozen
// shape is sufficient: both pinned reads answer through the pin-typed canned
// store, including the conditional second read the rollout-signal leg takes
// when the identity page is empty.
func TestCloudInventoryReadAdmissionWireShapesServeTheRoute(t *testing.T) {
	t.Parallel()

	var pin CloudInventoryReadAdmission = &cloudInventoryAdmissionCannedStore{preRollout: true}
	if _, err := pin.CloudInventoryIdentities(context.Background(), cloudInventoryFilter{}); err != nil {
		t.Fatalf("CloudInventoryIdentities through the pin failed: %v", err)
	}
	exists, err := pin.CloudInventoryPreRolloutEvidenceExists(context.Background(), cloudInventoryFilter{})
	if err != nil {
		t.Fatalf("CloudInventoryPreRolloutEvidenceExists through the pin failed: %v", err)
	}
	if !exists {
		t.Errorf("CloudInventoryPreRolloutEvidenceExists through the pin = false, want true: the conditional leg must be servable through the pin")
	}
}

// seededCloudInventoryDrift is a deliberately widened copy of the pin: the
// two frozen reads plus a graph Run the inventory lane never issues. It never
// ships; it proves the frozen-set comparison below can see a one-method
// widening.
type seededCloudInventoryDrift interface {
	CloudInventoryReadAdmission
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// TestCloudInventoryReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs must
// report a mismatch on the planted widening. It passes on a clean tree
// precisely because the guard fires on drift — a guard that cannot see the
// seeded method is no guard.
func TestCloudInventoryReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := cloudInventoryReadAdmissionMethodSet(t, (*seededCloudInventoryDrift)(nil))
	if reflect.DeepEqual(got, frozenCloudInventoryReadAdmission) {
		t.Fatal("seeded widening (Run) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenCloudInventoryReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenCloudInventoryReadAdmission)+1)
	}
}
