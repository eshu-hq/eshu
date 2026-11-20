// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// frozenMetricsReadAdmission pins the exact method set behind the
// versioned #9669 O6 admission contract for GET
// /api/v0/metrics/timeseries: the single bounded time-series range
// read the route admits through (RangeQuery). The entry re-declares
// the TimeSeriesSource signature in this Metrics-scoped shape
// instead of embedding the shared handle, so no other family can
// cite this pin — embedding or aliasing TimeSeriesSource here would
// be a blanket pin. A renamed, added, removed, or re-signed method
// fails here until that list is deliberately updated; bump
// MetricsReadAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: O6 pin 2 (per the arbiter O6 ruling minting
// order). The code-graph-metrics POST (POST
// /api/v0/code/call-graph/metrics) stays OUT: it is a GraphQuery
// reader, not a TimeSeriesSource consumer.
var frozenMetricsReadAdmission = []string{
	"RangeQuery func(context.Context, metrics.RangeQuery) ([]metrics.Point, error)",
}

// metricsReadAdmissionWitness proves MetricsReadAdmission is
// satisfiable: the core-owned time-series source satisfies it. It
// uses the package's existing fake rather than inventing a new
// satisfier.
var _ MetricsReadAdmission = fakeMetricsSource{}

// metricsReadAdmissionMethodSet reflects the live
// MetricsReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list. The iface parameter
// forces every caller — including the seeded-drift RED proof below —
// through this same guard comparison, so a passing suite proves the
// guard itself can see drift rather than proving a private copy can.
func metricsReadAdmissionMethodSet(t *testing.T, iface any) []string {
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

// TestMetricsReadAdmissionContractIsVersioned pins the #9669 O6
// admission-contract version for the metrics time-series read. The
// frozen method set below is the enforcement: an added, removed,
// renamed, or re-signed method fails this test until the frozen list
// is deliberately updated. Bump
// MetricsReadAdmissionContractVersion alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestMetricsReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if MetricsReadAdmissionContractVersion != "v0" {
		t.Errorf("MetricsReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", MetricsReadAdmissionContractVersion, "v0")
	}
	got := metricsReadAdmissionMethodSet(t, (*MetricsReadAdmission)(nil))
	if len(got) != len(frozenMetricsReadAdmission) {
		t.Fatalf("MetricsReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenMetricsReadAdmission, len(frozenMetricsReadAdmission), MetricsReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenMetricsReadAdmission[i] {
			t.Errorf("MetricsReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenMetricsReadAdmission[i], MetricsReadAdmissionContractVersion)
		}
	}
}

// TestMetricsReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the one re-declared range read, with no graph,
// search, list, snapshot, or ledger surface riding along. The frozen
// method-set contract test pins the exact name; this test pins the
// exclusion the arbiter ruling demands.
func TestMetricsReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*MetricsReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("MetricsReadAdmission has %d methods, want exactly 1: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := range rt.NumMethod() {
		name := rt.Method(i).Name
		for _, banned := range []string{
			"Run", "Search", "Get", "Match", "List", "Snapshot", "Ledger", "Summary",
		} {
			if strings.Contains(name, banned) {
				t.Errorf("MetricsReadAdmission.%s contains %q: that leg stays out of this pin", name, banned)
			}
		}
	}
}

// seededMetricsDrift is a deliberately widened copy of the pin: the
// one frozen range read plus a point-write the time-series lane never
// issues. It never ships; it proves the frozen-set comparison below
// can see a one-method widening.
type seededMetricsDrift interface {
	MetricsReadAdmission
	WritePoint(context.Context, Point) error
}

// TestMetricsReadAdmissionSeededDriftIsDetected is the RED half of the
// seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on
// a clean tree precisely because the guard fires on drift — a guard
// that cannot see the seeded method is no guard.
func TestMetricsReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := metricsReadAdmissionMethodSet(t, (*seededMetricsDrift)(nil))
	if reflect.DeepEqual(got, frozenMetricsReadAdmission) {
		t.Fatal("seeded widening (WritePoint) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenMetricsReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenMetricsReadAdmission)+1)
	}
}
