// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakePartitionSizesCensuser returns canned size censuses in order.
type fakePartitionSizesCensuser struct {
	results []PartitionSizesResult
	err     error
	calls   int
}

func (s *fakePartitionSizesCensuser) CensusSearchIndexPartitionSizes(context.Context) (PartitionSizesResult, error) {
	s.calls++
	if s.err != nil {
		return PartitionSizesResult{}, s.err
	}
	if len(s.results) == 0 {
		return PartitionSizesResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

// TestGenerationRetentionRunnerCensusesPartitionSizesOncePerPollInterval
// pins the #10874 cadence: the size census rides the orphan sweep, so a pass
// inside the same poll interval skips it, and the first pass after the
// interval censuses again.
func TestGenerationRetentionRunnerCensusesPartitionSizesOncePerPollInterval(t *testing.T) {
	now := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	sizes := &fakePartitionSizesCensuser{}
	runner := &Runner{
		Pruner:         &fakeGenerationRetentionPruner{},
		OrphanSweeper:  &fakeOrphanSweeper{},
		PartitionSizes: sizes,
		Config:         Config{PollInterval: time.Hour},
		Now:            func() time.Time { return now },
	}
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sizes.calls)

	now = now.Add(30 * time.Minute)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sizes.calls, "a pass inside the poll interval must not re-census")

	now = now.Add(31 * time.Minute)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, sizes.calls, "the first pass after the interval censuses again")
}

// TestGenerationRetentionRunnerRecordsPartitionSizes pins the #10874
// operator signals: per-partition bytes by component, estimated rows, the
// bytes-per-row series, the census phase, and one WARN log line per suspect
// leaf carrying no scope or generation identifiers.
func TestGenerationRetentionRunnerRecordsPartitionSizes(t *testing.T) {
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	sizes := &fakePartitionSizesCensuser{results: []PartitionSizesResult{{
		Leaves: []PartitionLeafSize{
			{Partition: "p29", Leaf: "eshu_search_index_terms_p29", TotalBytes: 207544205312, HeapBytes: 30502215680, IndexBytes: 176990003200, EstimatedRows: 386974, BytesPerRow: 536256, Suspect: "page"},
			{Partition: "p03", Leaf: "eshu_search_index_terms_p03", TotalBytes: 34493464576, HeapBytes: 11549392896, IndexBytes: 22940844032, EstimatedRows: 21326268, Suspect: ""},
		},
		CensusDuration: 300 * time.Millisecond,
	}}}
	now := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	runner := &Runner{
		Pruner:         &fakeGenerationRetentionPruner{},
		OrphanSweeper:  &fakeOrphanSweeper{},
		PartitionSizes: sizes,
		Config:         Config{PollInterval: time.Hour},
		Instruments:    instruments,
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Now:            func() time.Time { return now },
	}
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(207544205312), testutil.GaugeValue(t, rm, "eshu_dp_search_index_partition_bytes",
		map[string]string{"partition": "p29", "component": "total"}))
	require.Equal(t, int64(30502215680), testutil.GaugeValue(t, rm, "eshu_dp_search_index_partition_bytes",
		map[string]string{"partition": "p29", "component": "heap"}))
	require.Equal(t, int64(176990003200), testutil.GaugeValue(t, rm, "eshu_dp_search_index_partition_bytes",
		map[string]string{"partition": "p29", "component": "index"}))
	require.Equal(t, int64(386974), testutil.GaugeValue(t, rm, "eshu_dp_search_index_partition_estimated_rows",
		map[string]string{"partition": "p29"}))
	require.InDelta(t, 536256.0, floatGaugeValue(t, rm, "eshu_dp_search_index_partition_bytes_per_row",
		map[string]string{"partition": "p29", "component": "total"}), 1e-6)
	require.Equal(t, int64(21326268), testutil.GaugeValue(t, rm, "eshu_dp_search_index_partition_estimated_rows",
		map[string]string{"partition": "p03"}))
	count, sum := retentionHistogramPoint(t, rm, "eshu_dp_generation_retention_phase_duration_seconds",
		map[string]string{"phase": "census_search_index_partition_sizes"})
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 0.3, sum, 1e-9)
	require.Contains(t, logs.String(), `"msg":"generation retention search index partition size suspect"`)
	require.Contains(t, logs.String(), `"suspect":"page"`)
	require.Contains(t, logs.String(), `"partition":"p29"`)
	require.NotContains(t, logs.String(), "scope-b")

	// A failed census must not fail the retention pass.
	sizes.err = errors.New("census down")
	now = now.Add(time.Hour)
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err, "a failed size census must not fail the retention pass")
	rm = metricdata.ResourceMetrics{}
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm, "eshu_dp_generation_retention_failures_total",
		map[string]string{"reason": "search_index_partition_size_census"}))
}

// floatGaugeValue reads one Float64 gauge point for the partition-size
// series, which the Int64 testutil helpers cannot express.
func floatGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want map[string]string) float64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != name {
				continue
			}
			gauge, ok := record.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Float64 gauge", name, record.Data)
			}
			for _, point := range gauge.DataPoints {
				if testutil.HasAttrs(point.Attributes.ToSlice(), want) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("gauge %s with attrs %#v not found", name, want)
	return 0
}
