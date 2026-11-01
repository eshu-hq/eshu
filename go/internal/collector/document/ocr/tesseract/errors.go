// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"errors"
	"fmt"
)

// Outcome is the closed, low-cardinality classification of one recognition
// or construction attempt. It is the metric label, span attribute, and log
// failure class; it never carries OCR text, paths, or source names.
type Outcome string

// Outcome values. Every failure outcome fails closed: the engine returns no
// regions alongside the error.
const (
	// OutcomeSuccess means the subprocess exited zero and produced regions.
	OutcomeSuccess Outcome = "success"
	// OutcomeNoText means the subprocess exited zero with no text regions.
	OutcomeNoText Outcome = "no_text"
	// OutcomeInputRejected means validation refused the image before exec:
	// empty, oversize, unknown format, malformed header, or over the pixel cap.
	OutcomeInputRejected Outcome = "input_rejected"
	// OutcomeTimeout means the configured per-image deadline killed the run.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeCanceled means the caller's context ended the run.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeOutputOversize means stdout or stderr passed its byte cap and
	// the run was killed.
	OutcomeOutputOversize Outcome = "output_oversize"
	// OutcomeExecFailed means the process could not start or exited non-zero.
	OutcomeExecFailed Outcome = "exec_failed"
	// OutcomeParseFailed means stdout was not well-formed tesseract TSV.
	OutcomeParseFailed Outcome = "parse_failed"
	// OutcomeSignaled means a signal the engine did not send ended the
	// child: typically the sandbox OOM killer (SIGKILL) or an abort after an
	// allocation failed against MaxAddressSpaceBytes (SIGABRT).
	OutcomeSignaled Outcome = "signaled"
	// OutcomeRegionCapExceeded means the output held more line regions than
	// MaxRegions; the engine fails rather than truncating.
	OutcomeRegionCapExceeded Outcome = "region_cap_exceeded"
	// OutcomeDigestMismatch means the binary or model no longer matches its
	// pinned SHA-256, at construction or since construction.
	OutcomeDigestMismatch Outcome = "digest_mismatch"
	// OutcomeConfigInvalid means construction refused the configuration.
	OutcomeConfigInvalid Outcome = "config_invalid"
	// OutcomeInternal covers scratch-directory and pipe setup failures.
	OutcomeInternal Outcome = "internal"
)

// Error is a classified engine failure. Detail is built only from bounded
// values (sizes, caps, exit codes, formats) and never from subprocess
// stdout or stderr, so wrapping it can not leak OCR text.
type Error struct {
	Outcome Outcome
	Detail  string
	Err     error
}

// Error implements error.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("tesseract OCR %s: %s: %v", e.Outcome, e.Detail, e.Err)
	}
	return fmt.Sprintf("tesseract OCR %s: %s", e.Outcome, e.Detail)
}

// Unwrap returns the underlying cause, such as context.Canceled.
func (e *Error) Unwrap() error {
	return e.Err
}

// OutcomeOf returns the classified outcome of err, OutcomeSuccess for nil,
// and OutcomeInternal for an error this package did not classify.
func OutcomeOf(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	var classified *Error
	if errors.As(err, &classified) {
		return classified.Outcome
	}
	return OutcomeInternal
}

func failure(outcome Outcome, detail string, cause error) error {
	return &Error{Outcome: outcome, Detail: detail, Err: cause}
}
