// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// extractRequest builds the media.Extract request one caller artifact drains:
// content-addressed identity, operator-supplied name, and the raw body the
// engine normalizes before exec.
func extractRequest(body []byte) docmedia.Request {
	return docmedia.Request{
		ScopeID:      "doc-source:git:repo-media",
		GenerationID: "gen-1",
		ObservedAt:   time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		SourceSystem: "git",
		SourceURI:    "docs/standup.wav",
		SourceName:   "docs/standup.wav",
		RevisionID:   "rev-1",
		Body:         body,
	}
}

// extractSpansByName indexes ended spans by name for the wrapper assertions.
func extractSpansByName(t *testing.T, h telemetryHarness) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, span := range h.recorder.Ended() {
		out[span.Name()]++
	}
	return out
}

// assertExtractNoCanary fails when transcript or stderr text reaches any
// caller-visible surface: envelopes, logs, spans, or collected metrics.
func assertExtractNoCanary(t *testing.T, result docmedia.Result, h telemetryHarness) {
	t.Helper()
	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	var metrics metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var spans strings.Builder
	for _, span := range h.recorder.Ended() {
		for _, attr := range span.Attributes() {
			spans.WriteString(string(attr.Key))
			spans.WriteString(attr.Value.String())
		}
	}
	evidence := map[string]string{
		"persisted envelopes": string(encoded),
		"logs":                h.logs.String(),
		"span attributes":     spans.String(),
		"metrics":             fmt.Sprint(metrics),
	}
	for where, text := range evidence {
		for _, canary := range []string{canaryPassword, canaryEmail, canaryStderr, "payments worker"} {
			if strings.Contains(text, canary) {
				t.Fatalf("%s leaked transcript or stderr text %q", where, canary)
			}
		}
	}
}

// TestExtractCountsCallerTelemetryWithoutTranscriptText runs the pinned
// engine through the caller Extract wrapper and proves extraction telemetry
// is counted by the caller with no transcript text on any signal.
func TestExtractCountsCallerTelemetryWithoutTranscriptText(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, "success", Limits{})
	body := encodeWAV(t, 8000, 1, 16, 8000*4)

	result, err := engine.Extract(context.Background(), extractRequest(body))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(result.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(result.Sections))
	}
	redacted := 0
	for _, section := range result.Sections {
		if section.Content != "[redacted]" || section.SourceMetadata["redacted"] != "true" {
			t.Fatalf("section not redacted: %+v", section)
		}
		redacted++
	}
	if status := result.Document.SourceMetadata["transcript_status"]; status != "completed" {
		t.Fatalf("transcript_status = %q, want completed", status)
	}

	var metrics metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	extractions := counterByAttr(metrics, "eshu_dp_media_transcript_extractions_total", telemetry.MetricDimensionStatus)
	if extractions["completed"] != 1 || len(extractions) != 1 {
		t.Fatalf("extractions_total by status = %v, want {completed:1}", extractions)
	}
	redactedTotal := counterByAttr(metrics, "eshu_dp_media_transcript_redacted_segments_total", telemetry.MetricDimensionStatus)
	if redactedTotal[""] != int64(redacted) || redacted == 0 {
		t.Fatalf("redacted_segments_total = %v, want %d unattributed", redactedTotal, redacted)
	}
	names := extractSpansByName(t, h)
	if names[telemetry.SpanMediaTranscriptExtract] != 1 {
		t.Fatalf("spans by name = %v, want one %s", names, telemetry.SpanMediaTranscriptExtract)
	}
	if names[telemetry.SpanMediaTranscriptEngineRun] != 1 {
		t.Fatalf("spans by name = %v, want one engine run span", names)
	}
	logText := h.logs.String()
	if !strings.Contains(logText, `"transcript_status":"completed"`) {
		t.Fatalf("log line missing completed status: %s", logText)
	}
	assertExtractNoCanary(t, result, h)
}

// TestExtractRecordsNoSpeechStatus proves a heard-no-speech run drains as
// no_speech through the wrapper without an error.
func TestExtractRecordsNoSpeechStatus(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, "nospeech", Limits{})
	body := encodeWAV(t, 16000, 1, 16, 16000)

	result, err := engine.Extract(context.Background(), extractRequest(body))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if status := result.Document.SourceMetadata["transcript_status"]; status != "no_speech" {
		t.Fatalf("transcript_status = %q, want no_speech", status)
	}
	var metrics metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	extractions := counterByAttr(metrics, "eshu_dp_media_transcript_extractions_total", telemetry.MetricDimensionStatus)
	if extractions["no_speech"] != 1 || len(extractions) != 1 {
		t.Fatalf("extractions_total by status = %v, want {no_speech:1}", extractions)
	}
	assertExtractNoCanary(t, result, h)
}

// TestExtractSkipsUnsupportedFormatWithoutEngineCall proves a non-WAV body
// drains as skipped through the wrapper and never reaches the engine: the
// fail-if-called fake would exit nonzero on any invocation.
func TestExtractSkipsUnsupportedFormatWithoutEngineCall(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, "fail-if-called", Limits{})

	result, err := engine.Extract(context.Background(), extractRequest([]byte("this is not audio")))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if status := result.Document.SourceMetadata["transcript_status"]; status != "skipped" {
		t.Fatalf("transcript_status = %q, want skipped", status)
	}
	var metrics metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	extractions := counterByAttr(metrics, "eshu_dp_media_transcript_extractions_total", telemetry.MetricDimensionStatus)
	if extractions["skipped"] != 1 || len(extractions) != 1 {
		t.Fatalf("extractions_total by status = %v, want {skipped:1}", extractions)
	}
	assertExtractNoCanary(t, result, h)
}

// TestExtractRecordsFailedStatusWithoutTranscriptText proves an engine
// failure drains as failed through the wrapper with no transcript text on
// the error, logs, spans, or metrics.
func TestExtractRecordsFailedStatusWithoutTranscriptText(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, "exit", Limits{})
	body := encodeWAV(t, 8000, 1, 16, 8000*4)

	_, err := engine.Extract(context.Background(), extractRequest(body))
	if err == nil {
		t.Fatal("Extract() error = nil, want engine failure")
	}
	var metrics metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	extractions := counterByAttr(metrics, "eshu_dp_media_transcript_extractions_total", telemetry.MetricDimensionStatus)
	if extractions["failed"] != 1 || len(extractions) != 1 {
		t.Fatalf("extractions_total by status = %v, want {failed:1}", extractions)
	}
	for _, canary := range []string{canaryPassword, canaryEmail, canaryStderr} {
		if strings.Contains(err.Error(), canary) {
			t.Fatalf("error leaked transcript text %q", canary)
		}
	}
	var spans strings.Builder
	for _, span := range h.recorder.Ended() {
		for _, attr := range span.Attributes() {
			spans.WriteString(attr.Value.String())
		}
	}
	for _, canary := range []string{canaryPassword, canaryEmail, canaryStderr, "payments worker"} {
		if strings.Contains(h.logs.String()+spans.String()+fmt.Sprint(metrics), canary) {
			t.Fatalf("telemetry leaked transcript or stderr text %q", canary)
		}
	}
}

// TestExtractWithoutTelemetryIsSafe proves the wrapper drains without
// caller-provided instruments, tracer, or logger.
func TestExtractWithoutTelemetryIsSafe(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "success", Limits{})
	body := encodeWAV(t, 8000, 1, 16, 8000*4)

	result, err := engine.Extract(context.Background(), extractRequest(body))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if status := result.Document.SourceMetadata["transcript_status"]; status != "completed" {
		t.Fatalf("transcript_status = %q, want completed", status)
	}
}
