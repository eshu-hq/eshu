// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// frozenStatusReadAdmission pins the exact method set behind the
// versioned #9669 D6 admission contract for the Status family reads
// the status endpoints admit through Reader. A renamed, added,
// removed, or re-signed method fails here until that list is
// deliberately updated; bump StatusReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the
// eshu-api extraction boundary from drifting silently.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669).
var frozenStatusReadAdmission = []string{
	"ReadStatusSnapshot func(context.Context, time.Time) (status.RawSnapshot, error)",
	"ReadStatusSnapshotFiltered func(context.Context, time.Time, status.SnapshotSelection) (status.RawSnapshot, error)",
}

// statusReadAdmissionWitness proves StatusReadAdmission is satisfiable
// with the minimal shape: the two snapshot reads, no wider store
// surface.
type statusReadAdmissionWitness struct{}

func (statusReadAdmissionWitness) ReadStatusSnapshot(context.Context, time.Time) (RawSnapshot, error) {
	return RawSnapshot{}, nil
}

func (statusReadAdmissionWitness) ReadStatusSnapshotFiltered(context.Context, time.Time, SnapshotSelection) (RawSnapshot, error) {
	return RawSnapshot{}, nil
}

var _ StatusReadAdmission = statusReadAdmissionWitness{}

// statusReadAdmissionMethodSet reflects the live StatusReadAdmission
// method set into sorted "name signature" strings for comparison with
// the frozen list.
func statusReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*StatusReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestStatusReadAdmissionContractIsVersioned pins the #9669 D6
// admission-contract version for the Status family reads (the status
// endpoints admit through Reader). The version value plus the frozen
// method set below are the enforcement: a breaking change to the
// admission shape fails this test until the version bumps in the same
// diff.
func TestStatusReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if StatusReadAdmissionContractVersion != "v0" {
		t.Errorf("StatusReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", StatusReadAdmissionContractVersion, "v0")
	}
	got := statusReadAdmissionMethodSet(t)
	if len(got) != len(frozenStatusReadAdmission) {
		t.Fatalf("StatusReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenStatusReadAdmission, len(frozenStatusReadAdmission), StatusReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenStatusReadAdmission[i] {
			t.Errorf("StatusReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenStatusReadAdmission[i], StatusReadAdmissionContractVersion)
		}
	}
}

// TestStatusReadAdmissionWireShapesAreSealed proves the seal on the pin:
// exactly the two aliased snapshot reads, with no List, Append, Exec,
// or snapshot-write surface riding along. The frozen method-set
// contract test pins the exact names; this test pins the exclusion the
// arbiter ruling demands.
func TestStatusReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*StatusReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("StatusReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"List", "Append", "Exec", "Write", "Ledger"} {
			if strings.Contains(name, banned) {
				t.Errorf("StatusReadAdmission.%s contains %q: list, append, exec, and ledger legs stay out of this pin", name, banned)
			}
		}
	}
}

// seededStatusReadAdmissionDrift is a deliberately widened copy of the
// pin: the two frozen reads plus a ledger Exec no Status leg admits
// through. It never ships; it proves the frozen-set comparison below
// can see a one-method widening.
type seededStatusReadAdmissionDrift interface {
	StatusReadAdmission
	Exec(context.Context, string, ...any) error
}

// TestStatusReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test runs
// must report a mismatch on the planted widening. It passes on a clean
// tree precisely because the guard fires on drift — a guard that cannot
// see the seeded method is no guard.
func TestStatusReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*seededStatusReadAdmissionDrift)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	if len(got) != len(frozenStatusReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenStatusReadAdmission)+1)
	}
	for _, want := range frozenStatusReadAdmission {
		found := false
		for _, have := range got {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("seeded drift dropped frozen entry %q: the seed must only widen, never narrow", want)
		}
	}
}
