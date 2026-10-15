// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

// TestSnapshotExportDenialCounterHasOnlyClosedReason pins the #7957 counter:
// one closed reason label, with any unexpected value collapsed to unknown.
func TestSnapshotExportDenialCounterHasOnlyClosedReason(t *testing.T) {
	manual := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual))
	observer, err := NewObserver(provider.Meter("test"), noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	denials, ok := observer.(snapshotExportDenialObserver)
	if !ok {
		t.Fatal("otel observer does not count snapshot-export denials")
	}
	denials.ObserveSnapshotExportDenial(context.Background(), snapshotExportReasonNoDeclaration)
	denials.ObserveSnapshotExportDenial(context.Background(), snapshotExportReasonNoDeclaration)
	denials.ObserveSnapshotExportDenial(context.Background(), snapshotExportReasonUndeclared)
	denials.ObserveSnapshotExportDenial(context.Background(), "postgres://user:secret@reader")
	points := snapshotExportMetricPoints(t, collectSnapshotExportMetrics(t, manual), MetricReaderSnapshotExportDenials, "reason")
	if len(points) != 3 || points["no_declaration"] != 2 || points["undeclared_build"] != 1 || points["unknown"] != 1 {
		t.Fatalf("denial points = %v", points)
	}
}

// TestSnapshotExportQualifiedGaugeFollowsAtomicVerdicts pins the #7957 gauge:
// unlabeled for a single pool, one member_ordinal series per live fleet
// member, read from the in-memory verdicts with no SQL at scrape.
func TestSnapshotExportQualifiedGaugeFollowsAtomicVerdicts(t *testing.T) {
	t.Run("single pool", func(t *testing.T) {
		stats := &exportSpyStats{}
		access := &Access{
			writer: sql.OpenDB(exportSpyConnector{stats: stats}), reader: sql.OpenDB(exportSpyConnector{stats: stats}),
			snapshotExport: newSnapshotExportQualification(nil),
		}
		t.Cleanup(func() { _ = access.writer.Close(); _ = access.reader.Close() })
		manual := sdkmetric.NewManualReader()
		registration, err := RegisterPoolMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual)).Meter("test"), access)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registration.Unregister() })
		for _, tc := range []struct {
			version    string
			versionNum int
			want       int64
		}{{"", 0, 0}, {"18.6", 180006, 0}, {"18.7", 180007, 1}, {"18.6", 180006, 0}} {
			if tc.versionNum != 0 {
				access.snapshotExport.observe(tc.version, tc.versionNum, false)
			}
			points := snapshotExportMetricPoints(t, collectSnapshotExportMetrics(t, manual), MetricReaderSnapshotExportQualified, "")
			if len(points) != 1 || points[""] != tc.want {
				t.Fatalf("after %q gauge = %v, want one unlabeled %d", tc.version, points, tc.want)
			}
		}
		if stats.dials.Load() != 0 {
			t.Fatalf("scrape dialed %d connections", stats.dials.Load())
		}
	})
	t.Run("fleet", func(t *testing.T) {
		access, _ := newExportSpyFleetAccess(t, patchedBuild, nil,
			exportSpyMember{stats: &exportSpyStats{}, version: "18.6 (eshu-pg r2)", versionNum: 180006},
			exportSpyMember{stats: &exportSpyStats{}, version: "18.6", versionNum: 180006},
		)
		access.writer = sql.OpenDB(exportSpyConnector{stats: &exportSpyStats{}})
		t.Cleanup(func() { _ = access.writer.Close() })
		manual := sdkmetric.NewManualReader()
		registration, err := RegisterPoolMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(manual)).Meter("test"), access)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registration.Unregister() })
		points := snapshotExportMetricPoints(t, collectSnapshotExportMetrics(t, manual), MetricReaderSnapshotExportQualified, "member_ordinal")
		if len(points) != 2 || points["0"] != 1 || points["1"] != 0 {
			t.Fatalf("fleet gauge = %v, want member 0 qualified and member 1 not", points)
		}
		access.readerMembers[0].snapshotExport.observe("18.5", 180005, true)
		points = snapshotExportMetricPoints(t, collectSnapshotExportMetrics(t, manual), MetricReaderSnapshotExportQualified, "member_ordinal")
		if points["0"] != 0 {
			t.Fatalf("fleet gauge after flip = %v", points)
		}
	})
}

func collectSnapshotExportMetrics(t *testing.T, manual *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := manual.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

// snapshotExportMetricPoints returns name's points keyed by the value of the
// one allowed label (or "" for an unlabeled series) and fails on any other label.
func snapshotExportMetricPoints(t *testing.T, rm metricdata.ResourceMetrics, name, label string) map[string]int64 {
	t.Helper()
	points := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			var values []metricdata.DataPoint[int64]
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				values = data.DataPoints
			case metricdata.Gauge[int64]:
				values = data.DataPoints
			default:
				t.Fatalf("%s data = %T", name, m.Data)
			}
			for _, point := range values {
				key := ""
				for _, attr := range point.Attributes.ToSlice() {
					if label == "" || string(attr.Key) != label {
						t.Fatalf("%s unexpected label %s", name, attr.Key)
					}
					key = attr.Value.AsString()
				}
				points[key] = point.Value
			}
		}
	}
	return points
}
