package doctruth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestExtractorRecordsMetricsAndStructuredLog(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("documentation-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	var logs bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("documentation-test")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v, want nil", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "documentation", "extractor", &logs)
	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
	}, doctruth.Options{
		Instruments: instruments,
		Logger:      logger,
	})
	section := baseSectionInput("payment-api deploys through the payment-prod Helm release.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:deployment:payment-api",
		ClaimType:   "service_deployment",
		ClaimText:   "payment-api deploys through the payment-prod Helm release.",
		SubjectText: "payment-api",
		SubjectKind: "service",
	}}

	if _, err := extractor.Extract(context.Background(), section); err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertCounterValue(t, rm, "eshu_dp_documentation_entity_mentions_extracted_total", 1)
	assertCounterValue(t, rm, "eshu_dp_documentation_claim_candidates_extracted_total", 1)

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("json.Unmarshal(log) error = %v, want nil; log=%s", err, logs.String())
	}
	if got, want := entry["event_name"], "documentation.extraction.completed"; got != want {
		t.Fatalf("event_name = %v, want %q", got, want)
	}
	if got, want := entry["mentions_exact"], float64(1); got != want {
		t.Fatalf("mentions_exact = %v, want %v", got, want)
	}
	if got, want := entry["claim_candidates"], float64(1); got != want {
		t.Fatalf("claim_candidates = %v, want %v", got, want)
	}
	if got, want := entry["document_id"], section.DocumentID; got != want {
		t.Fatalf("document_id = %v, want %q", got, want)
	}
	if got, want := entry["revision_id"], section.RevisionID; got != want {
		t.Fatalf("revision_id = %v, want %q", got, want)
	}
	if got, want := entry["section_id"], section.SectionID; got != want {
		t.Fatalf("section_id = %v, want %q", got, want)
	}
}

func TestExtractorRecordsSuppressedClaimMetric(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("documentation-suppression-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payments-api", Aliases: []string{"payments"}},
		{Kind: "service", ID: "service:payments-worker", Aliases: []string{"payments"}},
	}, doctruth.Options{Instruments: instruments})
	section := baseSectionInput("payments uses the shared checkout database.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:payments:database",
		ClaimType:   "service_dependency",
		ClaimText:   "payments uses the shared checkout database.",
		SubjectText: "payments",
		SubjectKind: "service",
	}}

	if _, err := extractor.Extract(context.Background(), section); err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertCounterValue(t, rm, "eshu_dp_documentation_claim_candidates_suppressed_total", 1)
}

func TestExtractorRecordsUnresolvedObjectSuppressionOutcome(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("documentation-object-suppression-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
	}, doctruth.Options{Instruments: instruments})
	section := baseSectionInput("payment-api deploys through payment-prod.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:payment-api:deployment",
		ClaimType:   "service_deployment",
		ClaimText:   "payment-api deploys through payment-prod.",
		SubjectText: "payment-api",
		SubjectKind: "service",
		ObjectMentions: []doctruth.MentionHint{{
			Text: "payment-prod",
			Kind: "workload",
		}},
	}}

	if _, err := extractor.Extract(context.Background(), section); err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertCounterValueWithAttrs(t, rm, "eshu_dp_documentation_claim_candidates_suppressed_total", map[string]string{
		"source_system": "confluence",
		"outcome":       "unresolved_object",
	}, 1)
}

func assertCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, want int64) {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			var got int64
			for _, point := range sum.DataPoints {
				got += point.Value
			}
			if got != want {
				t.Fatalf("%s value = %d, want %d", metricName, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %q not found", metricName)
}

func assertCounterValueWithAttrs(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	want int64,
) {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, metricRecord := range sm.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if !hasMetricAttrs(point.Attributes.ToSlice(), wantAttrs) {
					continue
				}
				if point.Value != want {
					t.Fatalf("%s value = %d, want %d", metricName, point.Value, want)
				}
				return
			}
		}
	}
	t.Fatalf("metric %q with attrs %#v not found", metricName, wantAttrs)
}

func hasMetricAttrs(attrs []attribute.KeyValue, want map[string]string) bool {
	for key, value := range want {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == key && attr.Value.AsString() == value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
