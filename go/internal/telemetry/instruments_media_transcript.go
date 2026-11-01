// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// mediaTranscriptEngineBuckets spans sub-second fixture runs through the
// multi-minute ceiling a long local speech-to-text run can reach before its
// wall-clock cap kills it.
var mediaTranscriptEngineBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200}

// registerMediaTranscriptEngineInstruments records bounded out-of-process
// speech-to-text engine signals for the document/media Engine contract
// (#9729). Labels are the closed outcome or segment kind only; the engine
// identity rides on the span and log line, and no source, path, model, or
// transcript value is a label.
func registerMediaTranscriptEngineInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	inst.MediaTranscriptEngineRuns, err = meter.Int64Counter(
		"eshu_dp_media_transcript_engine_runs_total",
		metric.WithDescription("Local media transcript engine runs by closed outcome: completed, no_speech, timeout, canceled, resource_limit_exceeded, unsupported_format, malformed_media, malformed_output, or engine_failed"),
	)
	if err != nil {
		return fmt.Errorf("register MediaTranscriptEngineRuns counter: %w", err)
	}
	inst.MediaTranscriptEngineDuration, err = meter.Float64Histogram(
		"eshu_dp_media_transcript_engine_duration_seconds",
		metric.WithDescription("Wall-clock seconds for one local media transcript engine run, from input validation through output parsing, by closed outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(mediaTranscriptEngineBuckets...),
	)
	if err != nil {
		return fmt.Errorf("register MediaTranscriptEngineDuration histogram: %w", err)
	}
	inst.MediaTranscriptEngineSegments, err = meter.Int64Counter(
		"eshu_dp_media_transcript_engine_segments_total",
		metric.WithDescription("Transcript segments returned by a local media transcript engine, by closed kind: kept or non_speech_dropped"),
	)
	if err != nil {
		return fmt.Errorf("register MediaTranscriptEngineSegments counter: %w", err)
	}
	return nil
}
