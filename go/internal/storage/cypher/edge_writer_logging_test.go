// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestObserveRationaleRetractProbeUsesBoundedRepoSignal proves the #5998
// review F9 fix: the span event and structured log observeRationaleRetractProbe
// emits carry repo_count and one sample_repo_id, never the full repoIDs slice.
// Batch size scales with BatchLimit (default 100), so an unbounded repoIDs
// attribute would put roughly a hundred identifiers on every span event and
// log line per retract -- the same bounded-signal shape
// reportUnroutableRows (edge_writer_unroutable.go) already uses for its
// input_rows/dropped_rows counts plus one sample_intent_id.
func TestObserveRationaleRetractProbeUsesBoundedRepoSignal(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	w := &EdgeWriter{Logger: logger}

	// A repoIDs slice bigger than one, so a sample (not the whole slice) is
	// the only thing that could appear.
	repoIDs := []string{"repo-a", "repo-b", "repo-c"}

	ctx, span := tracer.Start(context.Background(), "test-span")
	w.observeRationaleRetractProbe(ctx, rationaleRetractProbeOutcomeSkipped, repoIDs, rationaleDeltaProbeScope, 0.021, nil)
	span.End()

	// --- span event ---
	spans := spanRecorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("len(spans) = %d, want %d", got, want)
	}
	events := spans[0].Events()
	if got, want := len(events), 1; got != want {
		t.Fatalf("len(events) = %d, want %d", got, want)
	}
	if got, want := events[0].Name, rationaleRetractProbeSpanEvent; got != want {
		t.Fatalf("event name = %q, want %q", got, want)
	}
	var sawRepoCount, sawSampleRepoID, sawScope, sawOutcome bool
	for _, attr := range events[0].Attributes {
		switch string(attr.Key) {
		case "repo_ids":
			t.Fatalf("span event carries unbounded repo_ids attribute: %v", attr.Value)
		case "repo_count":
			sawRepoCount = true
			if got, want := attr.Value.AsInt64(), int64(len(repoIDs)); got != want {
				t.Fatalf("repo_count = %d, want %d", got, want)
			}
		case "sample_repo_id":
			sawSampleRepoID = true
			if got, want := attr.Value.AsString(), repoIDs[0]; got != want {
				t.Fatalf("sample_repo_id = %q, want %q", got, want)
			}
		// scope and outcome are the two bounded metric labels. Asserting them
		// here is what keeps an unrelated string from reaching a label: an
		// earlier draft of this test passed an evidence source as the scope
		// argument and nothing noticed, because nothing read scope back.
		case "scope":
			sawScope = true
			if got, want := attr.Value.AsString(), string(rationaleDeltaProbeScope); got != want {
				t.Fatalf("scope = %q, want %q", got, want)
			}
		case "outcome":
			sawOutcome = true
			if got, want := attr.Value.AsString(), string(rationaleRetractProbeOutcomeSkipped); got != want {
				t.Fatalf("outcome = %q, want %q", got, want)
			}
		}
	}
	if !sawRepoCount {
		t.Fatal("span event missing repo_count attribute")
	}
	if !sawSampleRepoID {
		t.Fatal("span event missing sample_repo_id attribute")
	}
	if !sawScope {
		t.Fatal("span event missing scope attribute")
	}
	if !sawOutcome {
		t.Fatal("span event missing outcome attribute")
	}

	// --- structured log ---
	var logLine map[string]any
	if err := json.Unmarshal(logBuf.Bytes(), &logLine); err != nil {
		t.Fatalf("unmarshal log line: %v, body: %s", err, logBuf.String())
	}
	if _, ok := logLine["repo_ids"]; ok {
		t.Fatalf("log line carries unbounded repo_ids key: %#v", logLine)
	}
	if got, want := logLine["repo_count"], float64(len(repoIDs)); got != want {
		t.Fatalf("log repo_count = %#v, want %v", got, want)
	}
	if got, want := logLine["sample_repo_id"], repoIDs[0]; got != want {
		t.Fatalf("log sample_repo_id = %#v, want %q", got, want)
	}
	if got, want := logLine["scope"], string(rationaleDeltaProbeScope); got != want {
		t.Fatalf("log scope = %#v, want %q", got, want)
	}
	if got, want := logLine["outcome"], string(rationaleRetractProbeOutcomeSkipped); got != want {
		t.Fatalf("log outcome = %#v, want %q", got, want)
	}
}
