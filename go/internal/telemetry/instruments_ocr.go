// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// ocrDurationBuckets cover a sub-second screenshot through the 30 s
// per-artifact wall-time cap in design 1737 section 6, with headroom.
var ocrDurationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60}

// ocrPeakRSSBuckets span a small fixture (~30 MiB) through a 2 GiB sandbox
// memory ceiling in bytes.
var ocrPeakRSSBuckets = []float64{
	16 << 20, 32 << 20, 64 << 20, 128 << 20, 256 << 20, 512 << 20, 1 << 30, 2 << 30,
}

// registerOCRInstruments registers the #9728 out-of-process OCR family on
// inst. The tesseract engine records it; labels are the closed outcome set
// for recognitions and the closed ocr_status set for extractions. No label
// carries a source, path, scope, or OCR text value.
func registerOCRInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.OCRRecognitions, err = meter.Int64Counter(
		"eshu_dp_ocr_recognitions_total",
		metric.WithDescription("OCR subprocess recognitions by outcome (success, no_text, input_rejected, timeout, canceled, output_oversize, exec_failed, signaled, parse_failed, region_cap_exceeded, digest_mismatch, internal) (#9728)"),
	); err != nil {
		return fmt.Errorf("register OCRRecognitions counter: %w", err)
	}
	if inst.OCRRecognitionDuration, err = meter.Float64Histogram(
		"eshu_dp_ocr_recognition_duration_seconds",
		metric.WithDescription("Wall time of one OCR recognition including the sandboxed subprocess, by outcome (#9728)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(ocrDurationBuckets...),
	); err != nil {
		return fmt.Errorf("register OCRRecognitionDuration histogram: %w", err)
	}
	if inst.OCRPeakRSS, err = meter.Int64Histogram(
		"eshu_dp_ocr_engine_peak_rss_bytes",
		metric.WithDescription("Sampled peak resident set size (/proc VmHWM) of the OCR subprocess itself, for runs that started; rusage maxrss is not used because it inherits the spawning parent's RSS on Linux (#9728)"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(ocrPeakRSSBuckets...),
	); err != nil {
		return fmt.Errorf("register OCRPeakRSS histogram: %w", err)
	}
	if inst.OCRExtractions, err = meter.Int64Counter(
		"eshu_dp_ocr_extractions_total",
		metric.WithDescription("OCR image extractions by status (completed, no_text, skipped, failed) (#9728)"),
	); err != nil {
		return fmt.Errorf("register OCRExtractions counter: %w", err)
	}
	if inst.OCRRedactedRegions, err = meter.Int64Counter(
		"eshu_dp_ocr_redacted_regions_total",
		metric.WithDescription("OCR regions whose text was redacted before documentation facts were built (#9728)"),
	); err != nil {
		return fmt.Errorf("register OCRRedactedRegions counter: %w", err)
	}
	return nil
}
