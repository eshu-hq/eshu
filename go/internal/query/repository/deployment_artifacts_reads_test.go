// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// errShadowBoom is the planted stored-read failure for the read-error path.
var errShadowBoom = errors.New("connection refused")

// testMeter builds instruments over a manual reader for the emission tests.
func testMeter(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("reads-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments, reader
}

// collectPoints gathers int64 sum points of the named metrics: metric name
// to sorted "key=value,..." attribute rendering to value.
func collectPoints(t *testing.T, reader *sdkmetric.ManualReader, names ...string) map[string]map[string]int64 {
	t.Helper()
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	points := map[string]map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if !want[m.Name] {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want an int64 sum", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				pairs := make([]string, 0, point.Attributes.Len())
				iter := point.Attributes.Iter()
				for iter.Next() {
					kv := iter.Attribute()
					pairs = append(pairs, fmt.Sprintf("%s=%s", string(kv.Key), kv.Value.AsString()))
				}
				sort.Strings(pairs)
				key := strings.Join(pairs, ",")
				if points[m.Name] == nil {
					points[m.Name] = map[string]int64{}
				}
				points[m.Name][key] += point.Value
			}
		}
	}
	return points
}

// TestRecordDeploymentArtifactReadsCountsThePartition pins the reads_total
// emission: stored servings count source=stored reason=trusted, and every
// live serving counts source=live with its reason.
func TestRecordDeploymentArtifactReadsCountsThePartition(t *testing.T) {
	t.Parallel()

	instruments, reader := testMeter(t)
	stats := artifacts.ConfigArtifactReadStats{
		StoredRepositoryCount: 2,
		LiveRepositoryCount:   2,
		LiveReasons:           map[string]int{"stale_version": 1, "missing": 1},
	}
	RecordDeploymentArtifactReads(context.Background(), instruments, stats)
	got := collectPoints(t, reader, "eshu_dp_config_artifact_reads_total")["eshu_dp_config_artifact_reads_total"]
	want := map[string]int64{
		`reason=trusted,source=stored`:     2,
		`reason=stale_version,source=live`: 1,
		`reason=missing,source=live`:       1,
	}
	if len(got) != len(want) {
		t.Fatalf("reads points = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("reads points = %v, want %v", got, want)
		}
	}
}

// TestRecordDeploymentArtifactReadsSkipsZeroSeries pins the sparse emission:
// a load with no stored and no live servings records no reads series, and
// nil instruments record nothing.
func TestRecordDeploymentArtifactReadsSkipsZeroSeries(t *testing.T) {
	t.Parallel()

	instruments, reader := testMeter(t)
	RecordDeploymentArtifactReads(context.Background(), instruments, artifacts.ConfigArtifactReadStats{})
	if points := collectPoints(t, reader, "eshu_dp_config_artifact_reads_total"); len(points) != 0 {
		t.Fatalf("reads points = %v, want none", points)
	}
	RecordDeploymentArtifactReads(context.Background(), nil, artifacts.ConfigArtifactReadStats{})
}

// TestReportConfigArtifactShadowEmitsPointsAndMismatchLog pins the shadow
// wiring emission: one point per nonzero outcome, and a WARN log naming the
// mismatched repositories.
func TestReportConfigArtifactShadowEmitsPointsAndMismatchLog(t *testing.T) {
	t.Parallel()

	instruments, reader := testMeter(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ReportConfigArtifactShadow(context.Background(), instruments, logger, artifacts.ShadowReport{
		Repositories: []artifacts.ShadowRepositoryResult{
			{RepoID: "repo-a", Outcome: "match", LiveRows: 3, StoredRows: 3},
			{RepoID: "repo-b", Outcome: "mismatch", LiveRows: 3, StoredRows: 2},
			{RepoID: "repo-c", Outcome: "stored_missing"},
		},
	})
	got := collectPoints(t, reader, "eshu_dp_config_artifact_shadow_compare_total")["eshu_dp_config_artifact_shadow_compare_total"]
	for _, key := range []string{"outcome=match", "outcome=mismatch", "outcome=stored_missing"} {
		if got[key] != 1 {
			t.Fatalf("shadow points = %v, want one point per outcome", got)
		}
	}
	line := logs.String()
	for _, want := range []string{"config_artifacts.shadow.mismatch", "repo-b"} {
		if !strings.Contains(line, want) {
			t.Fatalf("shadow log = %q, want it to name %q", line, want)
		}
	}
	if strings.Contains(line, "repo-a") {
		t.Fatalf("shadow log = %q, names a matching repository", line)
	}
}

// TestReportConfigArtifactShadowReadErrorLogs pins the failed-compare path:
// a stored-read failure logs the read error and records no outcome points.
func TestReportConfigArtifactShadowReadErrorLogs(t *testing.T) {
	t.Parallel()

	instruments, reader := testMeter(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ReportConfigArtifactShadow(context.Background(), instruments, logger, artifacts.ShadowReport{
		ReadErr: errShadowBoom,
	})
	if points := collectPoints(t, reader, "eshu_dp_config_artifact_shadow_compare_total"); len(points) != 0 {
		t.Fatalf("shadow points = %v, want none on a read error", points)
	}
	if line := logs.String(); !strings.Contains(line, "config_artifacts.shadow.read_error") {
		t.Fatalf("shadow log = %q, want the read_error event", line)
	}
}

// TestReportConfigArtifactShadowCleanReportStaysQuiet pins the steady state:
// an all-match report records its points and logs no warning, and nil
// instruments and logger are safe.
func TestReportConfigArtifactShadowCleanReportStaysQuiet(t *testing.T) {
	t.Parallel()

	instruments, reader := testMeter(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ReportConfigArtifactShadow(context.Background(), instruments, logger, artifacts.ShadowReport{
		Repositories: []artifacts.ShadowRepositoryResult{
			{RepoID: "repo-a", Outcome: "match", LiveRows: 3, StoredRows: 3},
		},
	})
	points := collectPoints(t, reader, "eshu_dp_config_artifact_shadow_compare_total")
	if got := points["eshu_dp_config_artifact_shadow_compare_total"]["outcome=match"]; got != 1 {
		t.Fatalf("shadow points = %v, want the match point", points)
	}
	if line := logs.String(); strings.Contains(line, "WARN") {
		t.Fatalf("shadow log = %q, want no warning on a clean report", line)
	}
	ReportConfigArtifactShadow(context.Background(), nil, nil, artifacts.ShadowReport{})
}
