// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// tagHistoryScopedRowsByDisposition sums
// eshu_dp_query_container_image_tag_history_scoped_rows_total datapoints by
// their disposition attribute.
func tagHistoryScopedRowsByDisposition(t *testing.T, rm metricdata.ResourceMetrics) map[string]int64 {
	t.Helper()
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_query_container_image_tag_history_scoped_rows_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("scoped rows metric data = %T, want metricdata.Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				disposition, _ := dp.Attributes.Value(attribute.Key("disposition"))
				got[disposition.AsString()] += dp.Value
			}
		}
	}
	return got
}

// TestTagHistoryScopedFilterRecordsDispositionCounts proves an operator can
// see scoped filtering happen: the #6564 matrix keeps t1 and t4, withholds t2
// (BUILT_FROM another tenant) and t3 (no BUILT_FROM edge), and blanks t1's
// previous_digest. Not parallel: it installs a process-global meter provider.
func TestTagHistoryScopedFilterRecordsDispositionCounts(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	got := tagHistoryScopedRowsByDisposition(t, collectTagHistoryMetrics(t, reader))
	want := map[string]int64{
		tagHistoryDispositionKept:                  2,
		tagHistoryDispositionWithheldUngranted:     1,
		tagHistoryDispositionWithheldUnattributed:  1,
		tagHistoryDispositionPreviousDigestBlanked: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped rows by disposition = %v, want %v", got, want)
	}
}

// TestTagHistoryUnscopedCallerRecordsNoScopedRows proves the counter stays
// silent for an unscoped caller, so it measures scoped filtering only.
func TestTagHistoryUnscopedCallerRecordsNoScopedRows(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), nil, tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := tagHistoryScopedRowsByDisposition(t, collectTagHistoryMetrics(t, reader)); len(got) != 0 {
		t.Fatalf("scoped rows by disposition = %v, want none for an unscoped caller", got)
	}
}
