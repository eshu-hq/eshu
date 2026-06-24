// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstateruntime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestCompositeCaptureRecorderLogsSkipReason(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	recorder := compositeCaptureLoggingRecorder{
		logger: slog.New(slog.NewJSONHandler(&logBuffer, nil)),
	}

	recorder.Record(context.Background(), terraformstate.CompositeCaptureSkip{
		ResourceType: "aws_iam_user",
		AttributeKey: "secret_block",
		Path:         "resources.*.attributes.secret_block",
		Reason:       terraformstate.CompositeCaptureSkipReasonSensitiveSource,
	})

	logLine := logBuffer.String()
	if !strings.Contains(logLine, `"reason":"known_sensitive_key"`) {
		t.Fatalf("log line = %s, want known_sensitive_key reason", logLine)
	}
	if strings.Contains(logLine, "provider schema does not cover") {
		t.Fatalf("log line = %s, must not claim provider schema is missing for sensitive-source skip", logLine)
	}
}

func TestCompositeCaptureRecorderBoundsRepeatedSkipLogsByShape(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	recorder := compositeCaptureLoggingRecorder{
		logger: slog.New(slog.NewJSONHandler(&logBuffer, nil)),
	}
	for i := 0; i < 5; i++ {
		recorder.Record(context.Background(), terraformstate.CompositeCaptureSkip{
			ResourceType: "aws_s3_bucket",
			AttributeKey: "server_side_encryption_configuration",
			Path:         "resources.*.attributes.server_side_encryption_configuration",
			Reason:       terraformstate.CompositeCaptureSkipReasonSchemaUnknown,
		})
	}

	logLines := strings.Split(strings.TrimSpace(logBuffer.String()), "\n")
	if got, want := len(logLines), 1; got != want {
		t.Fatalf("log line count = %d, want %d bounded by repeated shape:\n%s", got, want, logBuffer.String())
	}
	logLine := logLines[0]
	for _, want := range []string{
		`"resource_type":"aws_s3_bucket"`,
		`"attribute_key":"server_side_encryption_configuration"`,
		`"reason":"schema_unknown"`,
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log line = %s, want field %s", logLine, want)
		}
	}
}

func TestCompositeCaptureRecorderCountsEverySkipWithReason(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("tfstate-composite-recorder-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	recorder := compositeCaptureLoggingRecorder{
		counter: instruments.DriftSchemaUnknownComposite,
	}
	for i := 0; i < 5; i++ {
		recorder.Record(context.Background(), terraformstate.CompositeCaptureSkip{
			ResourceType: "aws_s3_bucket",
			AttributeKey: "server_side_encryption_configuration",
			Path:         "resources.*.attributes.server_side_encryption_configuration",
			Reason:       terraformstate.CompositeCaptureSkipReasonSchemaUnknown,
		})
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertRecorderCounter(t, rm, "eshu_dp_drift_schema_unknown_composite_total", 5)
	assertRecorderCounterLabel(t, rm, "eshu_dp_drift_schema_unknown_composite_total", "resource_type", "aws_s3_bucket")
	assertRecorderCounterLabel(t, rm, "eshu_dp_drift_schema_unknown_composite_total", "reason", "schema_unknown")
}

func assertRecorderCounter(t *testing.T, rm metricdata.ResourceMetrics, metricName string, want int64) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			var got int64
			for _, point := range sum.DataPoints {
				got += point.Value
			}
			if got != want {
				t.Fatalf("%s = %d, want %d", metricName, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found", metricName)
}

func assertRecorderCounterLabel(t *testing.T, rm metricdata.ResourceMetrics, metricName, key, value string) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == key && attr.Value.AsString() == value {
						return
					}
				}
			}
			t.Fatalf("%s missing label %q=%q", metricName, key, value)
		}
	}
	t.Fatalf("metric %s not found", metricName)
}
