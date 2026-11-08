// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"reflect"
	"sort"
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
// strings for comparison with the frozen list.
func metricsReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*MetricsReadAdmission)(nil)).Elem()
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
	got := metricsReadAdmissionMethodSet(t)
	if len(got) != len(frozenMetricsReadAdmission) {
		t.Fatalf("MetricsReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenMetricsReadAdmission, len(frozenMetricsReadAdmission), MetricsReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenMetricsReadAdmission[i] {
			t.Errorf("MetricsReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenMetricsReadAdmission[i], MetricsReadAdmissionContractVersion)
		}
	}
}
