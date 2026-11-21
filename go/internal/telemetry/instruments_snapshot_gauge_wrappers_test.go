// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// snapshotGaugeObserver serves both reducer snapshot observers from fixed
// values, the way cmd/reducer serves them from the Postgres gauge snapshot.
type snapshotGaugeObserver struct {
	orphanRows  int64
	expired     int64
	expiredAge  float64
	orphanCalls int
}

func (o *snapshotGaugeObserver) ReducerLeaseOrphanRows(context.Context) (int64, error) {
	o.orphanCalls++
	return o.orphanRows, nil
}

func (o *snapshotGaugeObserver) ReducerClaimedExpiredCounts(context.Context) (int64, float64, error) {
	return o.expired, o.expiredAge, nil
}

// int64GaugeValue collects reader and returns the single unlabeled data point
// of the named Int64 gauge, failing when the metric is absent.
func int64GaugeValue(t *testing.T, reader sdkmetric.Reader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "%s data = %T, want Gauge[int64]", name, m.Data)
			require.Len(t, gauge.DataPoints, 1, "%s data points", name)
			assert.Zero(t, gauge.DataPoints[0].Attributes.Len(), "%s must carry no labels", name)
			return gauge.DataPoints[0].Value
		}
	}
	t.Fatalf("metric %s was not collected", name)
	return 0
}

// TestRegisterReducerLeaseOrphanObservableGauges proves the snapshot-served
// registration entry point the reducer calls: it refuses nil instruments,
// treats a nil observer as "not wired" without registering a gauge, and
// otherwise reports the observer's count on collection.
func TestRegisterReducerLeaseOrphanObservableGauges(t *testing.T) {
	t.Parallel()

	newMeter := func() (sdkmetric.Reader, *sdkmetric.MeterProvider) {
		reader := sdkmetric.NewManualReader()
		return reader, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	}

	t.Run("nil instruments is an error", func(t *testing.T) {
		t.Parallel()
		_, provider := newMeter()
		err := RegisterReducerLeaseOrphanObservableGauges(nil, provider.Meter("test"), &snapshotGaugeObserver{})
		require.Error(t, err)
	})

	t.Run("nil observer registers nothing", func(t *testing.T) {
		t.Parallel()
		_, provider := newMeter()
		inst := &Instruments{}
		require.NoError(t, RegisterReducerLeaseOrphanObservableGauges(inst, provider.Meter("test"), nil))
		assert.Nil(t, inst.ReducerLeaseOrphanRows)
	})

	t.Run("reports the observed count", func(t *testing.T) {
		t.Parallel()
		reader, provider := newMeter()
		inst := &Instruments{}
		obs := &snapshotGaugeObserver{orphanRows: 14}
		require.NoError(t, RegisterReducerLeaseOrphanObservableGauges(inst, provider.Meter("test"), obs))
		require.NotNil(t, inst.ReducerLeaseOrphanRows)
		assert.EqualValues(t, 14, int64GaugeValue(t, reader, "eshu_dp_reducer_lease_orphan_rows"))
		assert.Equal(t, 1, obs.orphanCalls, "one collection reads the snapshot once")
	})
}

// TestRegisterReducerClaimedExpiredObservableGauges proves the sibling
// snapshot-served entry point (#10865) reports its count and refuses the same
// unwired inputs, so the two reducer snapshot gauges stay symmetric.
func TestRegisterReducerClaimedExpiredObservableGauges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	require.Error(t, RegisterReducerClaimedExpiredObservableGauges(nil, meter, &snapshotGaugeObserver{}))

	unwired := &Instruments{}
	require.NoError(t, RegisterReducerClaimedExpiredObservableGauges(unwired, meter, nil))
	assert.Nil(t, unwired.ReducerClaimedExpired)

	inst := &Instruments{}
	obs := &snapshotGaugeObserver{expired: 7, expiredAge: 90}
	require.NoError(t, RegisterReducerClaimedExpiredObservableGauges(inst, meter, obs))
	assert.EqualValues(t, 7, int64GaugeValue(t, reader, "eshu_dp_reducer_claimed_expired"))
}

// TestAttemptBucketForCountFoldsToClosedBuckets pins the closed attempt
// buckets: non-positive and first attempts share the first bucket so a
// hand-built intent cannot mint a new label value.
func TestAttemptBucketForCountFoldsToClosedBuckets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		attempt int
		want    string
	}{
		{-3, AttemptBucketFirst},
		{0, AttemptBucketFirst},
		{1, AttemptBucketFirst},
		{2, AttemptBucketSecond},
		{3, AttemptBucketLater},
		{1000, AttemptBucketLater},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, AttemptBucketForCount(tc.attempt), "attempt %d", tc.attempt)
	}
}

// TestClosedDimensionAttributeHelpersUseContractKeys proves each attribute
// helper writes the value under its documented metric dimension key.
func TestClosedDimensionAttributeHelpersUseContractKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  attribute.KeyValue
		key  string
		val  string
	}{
		{"attempt", AttrAttempt(AttemptBucketSecond), MetricDimensionAttempt, AttemptBucketSecond},
		{"scope_class", AttrScopeClass("single"), MetricDimensionScopeClass, "single"},
		{"pattern_class", AttrPatternClass("trigram"), MetricDimensionPatternClass, "trigram"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.key, string(tc.got.Key), tc.name)
		assert.Equal(t, tc.val, tc.got.Value.AsString(), tc.name)
	}
}

// TestRepositoryCatalogDomainRoundTrip proves the catalog-domain context
// value survives the round trip and reads empty when never set.
func TestRepositoryCatalogDomainRoundTrip(t *testing.T) {
	t.Parallel()

	assert.Empty(t, RepositoryCatalogDomain(context.Background()))
	ctx := ContextWithRepositoryCatalogDomain(context.Background(), "code_import_repo_edge")
	assert.Equal(t, "code_import_repo_edge", RepositoryCatalogDomain(ctx))
}
