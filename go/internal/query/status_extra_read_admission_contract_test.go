// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// frozenStatusExtraReadAdmission pins the exact method set behind the
// versioned #9669 D6 admission contract for the extra-port Status
// family reads (the bounded LiveActivity and retirement-summary reads;
// the governance Summary leg stays carried relay-disclosed per the
// row-7 Path B ruling and is not pinned here). A renamed, added,
// removed, or re-signed
// method fails here until that list is deliberately updated; bump
// StatusExtraReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669, gaps 03+04 follow-up).
var frozenStatusExtraReadAdmission = []string{
	"ReadLiveActivity func(context.Context, int, bool, []string, []string) ([]status.LiveActivityRow, bool, error)",
	"ReadRetirementSummary func(context.Context) (status.RetirementSummary, error)",
}

// statusExtraReadAdmissionWitness proves StatusExtraReadAdmission is
// satisfiable with the minimal shape: the bounded LiveActivity read and
// the retirement-summary read, no wider store surface.
type statusExtraReadAdmissionWitness struct{}

func (statusExtraReadAdmissionWitness) ReadLiveActivity(context.Context, int, bool, []string, []string) ([]status.LiveActivityRow, bool, error) {
	return nil, false, nil
}

func (statusExtraReadAdmissionWitness) ReadRetirementSummary(context.Context) (status.RetirementSummary, error) {
	return status.RetirementSummary{}, nil
}

var _ StatusExtraReadAdmission = statusExtraReadAdmissionWitness{}

// statusExtraReadAdmissionMethodSet reflects the live
// StatusExtraReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func statusExtraReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestStatusExtraReadAdmissionContractIsVersioned pins the #9669 D6
// admission-contract version for the extra-port Status family reads.
// The version value plus the frozen method set below are the
// enforcement: a breaking change to the admission shape fails this
// test until the version bumps in the same diff.
func TestStatusExtraReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if StatusExtraReadAdmissionContractVersion != "v0" {
		t.Errorf("StatusExtraReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", StatusExtraReadAdmissionContractVersion, "v0")
	}
	got := statusExtraReadAdmissionMethodSet(t, (*StatusExtraReadAdmission)(nil))
	if len(got) != len(frozenStatusExtraReadAdmission) {
		t.Fatalf("StatusExtraReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenStatusExtraReadAdmission, len(frozenStatusExtraReadAdmission), StatusExtraReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenStatusExtraReadAdmission[i] {
			t.Errorf("StatusExtraReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenStatusExtraReadAdmission[i], StatusExtraReadAdmissionContractVersion)
		}
	}
}

// TestStatusExtraReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the two embedded narrow reads, with no graph, session,
// write, or snapshot surface riding along. The frozen method-set
// contract test pins the exact names; this test pins the exclusion the
// arbiter ruling demands.
func TestStatusExtraReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*StatusExtraReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 2 {
		t.Fatalf("StatusExtraReadAdmission has %d methods, want exactly 2: the seal forbids any wider surface", rt.NumMethod())
	}
	allowed := map[string]bool{"ReadLiveActivity": true, "ReadRetirementSummary": true}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		if !allowed[name] {
			t.Errorf("StatusExtraReadAdmission.%s is not one of the two pinned reads: the seal forbids any wider surface", name)
		}
		for _, banned := range []string{"Graph", "Session", "Write", "Snapshot", "Exec", "List", "Ledger", "Tenant", "Append"} {
			if strings.Contains(name, banned) {
				t.Errorf("StatusExtraReadAdmission.%s contains %q: graph, write, and snapshot legs stay out of this pin", name, banned)
			}
		}
	}
}

// seededStatusExtraDrift is a deliberately widened copy of the pin: the
// two frozen reads plus a graph Exec no Status leg admits through. It
// never ships; it proves the frozen-set comparison below can see a
// one-method widening.
type seededStatusExtraDrift interface {
	StatusExtraReadAdmission
	Exec(context.Context, string, map[string]any) error
}

// TestStatusExtraReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestStatusExtraReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := statusExtraReadAdmissionMethodSet(t, (*seededStatusExtraDrift)(nil))
	if reflect.DeepEqual(got, frozenStatusExtraReadAdmission) {
		t.Fatal("seeded widening (Exec) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenStatusExtraReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenStatusExtraReadAdmission)+1)
	}
	for _, want := range frozenStatusExtraReadAdmission {
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
