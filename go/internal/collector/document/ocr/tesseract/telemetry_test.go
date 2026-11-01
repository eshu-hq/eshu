// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

type telemetryHarness struct {
	reader *sdkmetric.ManualReader
	spans  *tracetest.SpanRecorder
	logs   *bytes.Buffer
}

func newTelemetryEngine(t *testing.T, mutate func(*Config)) (*Engine, telemetryHarness) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("ocr-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	spans := tracetest.NewSpanRecorder()
	logs := &bytes.Buffer{}
	engine, _ := newTestEngine(t, func(cfg *Config) {
		cfg.Instruments = inst
		cfg.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("ocr-test")
		cfg.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		if mutate != nil {
			mutate(cfg)
		}
	})
	return engine, telemetryHarness{reader: reader, spans: spans, logs: logs}
}

func (h telemetryHarness) counter(t *testing.T, name string, attrs map[string]string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want Sum[int64]", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if matchesAttrs(point.Attributes.ToSlice(), attrs) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func (h telemetryHarness) histogramCount(t *testing.T, name string) uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var count uint64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					count += point.Count
				}
			case metricdata.Histogram[int64]:
				for _, point := range data.DataPoints {
					count += point.Count
				}
			}
		}
	}
	return count
}

func matchesAttrs(got []attribute.KeyValue, want map[string]string) bool {
	for key, value := range want {
		found := false
		for _, kv := range got {
			if string(kv.Key) == key && kv.Value.String() == value {
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

func TestExtractRedactsCanariesBeforeFactsAndRecordsTelemetry(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, nil)

	result, err := engine.Extract(context.Background(), ocr.Request{
		ScopeID:      "scope-ocr",
		GenerationID: "gen-1",
		ObservedAt:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		SourceName:   "docs/runbook.png",
		DocumentID:   "doc:runbook",
		Body:         append(fakePNG(t, 200, 100, "ok"), gateOnRSSSample(t, engine)...),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got := result.Document.SourceMetadata["ocr_status"]; got != "completed" {
		t.Fatalf("ocr_status = %q, want completed", got)
	}
	if len(result.Sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(result.Sections))
	}
	if got := result.Sections[0].Content; got != "Deploy the Checkout" {
		t.Fatalf("clean section content = %q", got)
	}
	redacted := 0
	for _, section := range result.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			redacted++
			if section.Content != "[redacted]" {
				t.Fatalf("redacted section content = %q", section.Content)
			}
		}
		if section.SourceMetadata["ocr_engine"] != EngineName {
			t.Fatalf("ocr_engine = %q", section.SourceMetadata["ocr_engine"])
		}
		if !strings.Contains(section.SourceMetadata["ocr_engine_version"], "binary=sha256:") {
			t.Fatalf("ocr_engine_version lacks pinned digest: %q", section.SourceMetadata["ocr_engine_version"])
		}
	}
	if redacted != 2 {
		t.Fatalf("redacted sections = %d, want 2 (password line and email line)", redacted)
	}
	persisted, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	assertNoCanary(t, "persisted envelopes", string(persisted))
	assertNoCanary(t, "logs", h.logs.String())
	for _, span := range h.spans.Ended() {
		for _, attr := range span.Attributes() {
			assertNoCanary(t, "span attribute "+string(attr.Key), attr.Value.String())
		}
	}

	if got := h.counter(t, "eshu_dp_ocr_recognitions_total", nil); got != 1 {
		t.Fatalf("eshu_dp_ocr_recognitions_total = %d, want 1", got)
	}
	if got := h.counter(t, "eshu_dp_ocr_extractions_total", nil); got != 1 {
		t.Fatalf("eshu_dp_ocr_extractions_total = %d, want 1", got)
	}
	if got := h.counter(t, "eshu_dp_ocr_redacted_regions_total", nil); got != 2 {
		t.Fatalf("eshu_dp_ocr_redacted_regions_total = %d, want 2", got)
	}
	if got := h.histogramCount(t, "eshu_dp_ocr_recognition_duration_seconds"); got != 1 {
		t.Fatalf("recognition duration samples = %d, want 1", got)
	}
	if got := h.histogramCount(t, "eshu_dp_ocr_engine_peak_rss_bytes"); got != 1 {
		t.Fatalf("peak RSS samples = %d, want 1", got)
	}
	names := map[string]bool{}
	for _, span := range h.spans.Ended() {
		names[span.Name()] = true
	}
	for _, want := range []string{contract.SpanOCRExtract, contract.SpanOCRRecognize} {
		if !names[want] {
			t.Fatalf("span %q not recorded; got %v", want, names)
		}
	}
	if !strings.Contains(h.logs.String(), `"outcome":"success"`) {
		t.Fatalf("recognition log missing outcome: %s", h.logs.String())
	}
}

func TestExtractRecordsFailureOutcomeWithoutLeakingText(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, nil)

	_, err := engine.Extract(context.Background(), ocr.Request{
		ScopeID: "scope-ocr", GenerationID: "gen-1", SourceName: "docs/broken.png",
		Body: fakePNG(t, 20, 10, "exit3"),
	})
	if err == nil {
		t.Fatal("Extract succeeded on a failing engine")
	}
	assertNoCanary(t, "error", err.Error())
	assertNoCanary(t, "logs", h.logs.String())
	if got := h.counter(t, "eshu_dp_ocr_recognitions_total", map[string]string{"outcome": "exec_failed"}); got != 1 {
		t.Fatalf("exec_failed recognitions = %d, want 1", got)
	}
	if got := h.counter(t, "eshu_dp_ocr_extractions_total", map[string]string{"status": "failed"}); got != 1 {
		t.Fatalf("failed extractions = %d, want 1", got)
	}
	if !strings.Contains(h.logs.String(), `"failure_class":"exec_failed"`) {
		t.Fatalf("failure log missing class: %s", h.logs.String())
	}
}

func TestExtractSkipsUnsupportedPreflightWithoutExec(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, nil)

	result, err := engine.Extract(context.Background(), ocr.Request{
		ScopeID: "scope-ocr", GenerationID: "gen-1", SourceName: "docs/notes.txt",
		Body: []byte("plain text FAKEOCR:ok "),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Document.SourceMetadata["ocr_status"] != "skipped" {
		t.Fatalf("ocr_status = %q, want skipped", result.Document.SourceMetadata["ocr_status"])
	}
	if engine.execCount.Load() != 0 {
		t.Fatal("skipped preflight reached exec")
	}
	if got := h.counter(t, "eshu_dp_ocr_extractions_total", map[string]string{"status": "skipped"}); got != 1 {
		t.Fatalf("skipped extractions = %d, want 1", got)
	}
}

func TestExtractWithNilTelemetryIsSafe(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Extract(context.Background(), ocr.Request{
		ScopeID: "scope-ocr", GenerationID: "gen-1", SourceName: "docs/runbook.png",
		Body: fakePNG(t, 200, 100, "ok"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Sections) != 3 || result.Sections[0].SourceMetadata["ocr_engine"] != EngineName {
		t.Fatalf("unexpected sections: %+v", result.Sections)
	}
}

// gateOnRSSSample holds the next fake child alive until the engine's sampler
// has read a non-zero VmHWM from it. It returns the token to append to the
// image bytes. Call it before the engine serves a request.
func gateOnRSSSample(t *testing.T, engine *Engine) []byte {
	t.Helper()
	gate := filepath.Join(t.TempDir(), "sampled")
	var once sync.Once
	engine.onRSSSample = func(value int64) {
		if value <= 0 {
			return
		}
		once.Do(func() {
			if err := os.WriteFile(gate, nil, 0o600); err != nil {
				t.Errorf("open gate: %v", err)
			}
		})
	}
	return []byte(fakeGateToken + gate + " ")
}

func assertNoCanary(t *testing.T, where, text string) {
	t.Helper()
	for _, canary := range []string{canaryPassword, canaryEmail, "Hunter2", "alice.canary"} {
		if strings.Contains(text, canary) {
			t.Fatalf("%s leaks OCR canary %q", where, canary)
		}
	}
}
