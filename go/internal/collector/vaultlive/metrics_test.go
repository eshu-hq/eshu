// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCollectRecordsURIRedactionsByFieldClass proves the redaction counter fires
// once per stripped credential-bearing URI component, keyed by the bounded
// field_class label, at the single sanitize site. A URI carrying userinfo, a
// query, and a fragment must increment uri_userinfo, uri_query, and uri_fragment
// exactly once each.
func TestCollectRecordsURIRedactionsByFieldClass(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	target := testTarget()
	target.SourceURI = "https://vaultuser:s3cr3t@vault.example.com:8200/v1/sys/auth?token=abcd#frag"
	client := &fakeVaultClient{authMounts: []AuthMount{{Path: "kubernetes/", Accessor: "acc", Method: "kubernetes"}}}

	source := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t), Instruments: instruments}
	if _, err := source.Collect(context.Background(), target, client); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	rm := collectVaultMetrics(t, reader)
	for _, fieldClass := range []string{
		telemetry.FieldClassURIUserinfo,
		telemetry.FieldClassURIQuery,
		telemetry.FieldClassURIFragment,
	} {
		if got := vaultCounterValue(t, rm, "eshu_dp_secrets_iam_source_redactions_total", map[string]string{
			telemetry.MetricDimensionSource:     secretsIAMSourceVault,
			telemetry.MetricDimensionFieldClass: fieldClass,
		}); got != 1 {
			t.Fatalf("redactions[%s] = %d, want 1", fieldClass, got)
		}
	}
}

// TestCollectRecordsNoRedactionForCleanURI proves a clean URI strips nothing and
// emits no redaction data points, so the counter reflects real redactions only.
func TestCollectRecordsNoRedactionForCleanURI(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	target := testTarget()
	target.SourceURI = "https://vault.example.com:8200/v1/sys/auth"
	client := &fakeVaultClient{authMounts: []AuthMount{{Path: "kubernetes/", Accessor: "acc", Method: "kubernetes"}}}

	source := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t), Instruments: instruments}
	if _, err := source.Collect(context.Background(), target, client); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	rm := collectVaultMetrics(t, reader)
	if found := vaultMetricPresent(rm, "eshu_dp_secrets_iam_source_redactions_total"); found {
		t.Fatal("redactions counter emitted a data point for a clean URI, want none")
	}
}

// TestSnapshotRecordsScopeFreshness proves the freshness gauge records the
// generation age (now minus observed-at) at finalization, labeled by source and
// the bounded scope kind, never a cluster id or namespace.
func TestSnapshotRecordsScopeFreshness(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	source := &SnapshotSource{
		Config: Config{
			CollectorInstanceID: "vaultlive-1",
			RedactionKey:        testRedactionKey(t),
			Targets:             []ClusterTarget{{VaultClusterID: "vault-prod", Namespace: "admin", FencingToken: 1}},
		},
		ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		Instruments:   instruments,
		Clock:         advancingClock(time.Unix(1700000000, 0).UTC(), 5*time.Second),
	}

	if _, ok, err := source.Next(context.Background()); err != nil || !ok {
		t.Fatalf("Next() = ok %v, err %v; want ok true, nil err", ok, err)
	}

	rm := collectVaultMetrics(t, reader)
	got := vaultGaugeValue(t, rm, "eshu_dp_secrets_iam_source_scope_freshness_seconds", map[string]string{
		telemetry.MetricDimensionSource:    secretsIAMSourceVault,
		telemetry.MetricDimensionScopeKind: "vault_cluster",
	})
	// collectTarget calls now() at start, then observedAt = now(), then finalizes
	// with now() again; with a 5s step the age at finalization is one step (5s).
	if got != 5 {
		t.Fatalf("scope_freshness_seconds = %v, want 5", got)
	}
}

// advancingClock returns a clock that advances by step on each call, so tests
// can exercise a non-zero generation age deterministically.
func advancingClock(start time.Time, step time.Duration) func() time.Time {
	current := start.Add(-step)
	return func() time.Time {
		current = current.Add(step)
		return current
	}
}

func collectVaultMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

func vaultMetricPresent(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					return len(sum.DataPoints) > 0
				}
				return true
			}
		}
	}
	return false
}

func vaultCounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want map[string]string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				if vaultAttrsMatch(dp.Attributes.ToSlice(), want) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v not found", name, want)
	return 0
}

func vaultGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want map[string]string) float64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want Gauge[float64]", name, m.Data)
			}
			for _, dp := range gauge.DataPoints {
				if vaultAttrsMatch(dp.Attributes.ToSlice(), want) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v not found", name, want)
	return 0
}

func vaultAttrsMatch(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}
	return true
}
