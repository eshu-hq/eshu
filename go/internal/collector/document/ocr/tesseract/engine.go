// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// languageTag is the BCP 47 tag recorded for the reviewed "eng" model.
const languageTag = "en"

// Recognize runs one bounded tesseract subprocess over img and returns line
// regions in reading order. Every check that can run before exec does:
// context, pinned-file identity, byte cap, format, header dimensions, and
// pixel cap. Any failure returns an empty result and a classified *Error
// whose message never contains OCR text.
func (e *Engine) Recognize(ctx context.Context, img ocr.Image) (ocr.EngineResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	ctx, span := e.startRecognizeSpan(ctx, img)
	defer span.End()

	stats := recognizeStats{inputBytes: len(img.Body)}
	result, err := e.recognize(ctx, img, &stats)
	e.recordRecognition(ctx, span, stats, outcomeOfResult(result, err), time.Since(start), err)
	if err != nil {
		return ocr.EngineResult{}, err
	}
	return result, nil
}

// outcomeOfResult classifies a Recognize return: the error's outcome, or
// success versus no_text for a clean run.
func outcomeOfResult(result ocr.EngineResult, err error) Outcome {
	if err != nil {
		return OutcomeOf(err)
	}
	if len(result.Regions) == 0 {
		return OutcomeNoText
	}
	return OutcomeSuccess
}

// recognizeStats carries bounded per-run values for telemetry.
type recognizeStats struct {
	inputBytes  int
	format      string
	pixels      int64
	started     bool
	peakRSS     int64
	stdoutBytes int
	stderrBytes int64
	regions     int
}

func (e *Engine) recognize(ctx context.Context, img ocr.Image, stats *recognizeStats) (ocr.EngineResult, error) {
	if err := ctx.Err(); err != nil {
		return ocr.EngineResult{}, failure(OutcomeCanceled, "context ended before exec", err)
	}
	if err := e.unchangedSinceConstruction(); err != nil {
		return ocr.EngineResult{}, err
	}
	validated, err := e.validateInput(img)
	if err != nil {
		return ocr.EngineResult{}, err
	}
	stats.format = validated.format
	stats.pixels = int64(validated.width) * int64(validated.height)

	run, err := e.run(ctx, img.Body)
	stats.started = run.started
	stats.peakRSS = run.peakRSS
	stats.stderrBytes = run.stderrBytes
	if err != nil {
		return ocr.EngineResult{}, err
	}
	stats.stdoutBytes = len(run.stdout)
	regions, err := parseTSV(run.stdout, validated.width, validated.height, e.cfg.MaxRegions)
	if err != nil {
		return ocr.EngineResult{}, err
	}
	stats.regions = len(regions)
	return ocr.EngineResult{
		EngineName:    EngineName,
		EngineVersion: e.version,
		Language:      languageTag,
		Regions:       regions,
	}, nil
}
