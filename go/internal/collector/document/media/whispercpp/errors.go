// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"errors"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// ErrorClass is the closed failure vocabulary for one engine run. It is safe
// as a metric label, span attribute, and log value.
type ErrorClass string

const (
	// ClassUnsupportedFormat marks media the engine does not accept (any
	// format other than WAV in this version).
	ClassUnsupportedFormat ErrorClass = "unsupported_format"
	// ClassMalformedMedia marks a WAV body that fails bounded header or
	// sample-layout validation. The engine is never executed.
	ClassMalformedMedia ErrorClass = "malformed_media"
	// ClassResourceLimit marks a breached input, duration, output, segment,
	// or segment-text cap.
	ClassResourceLimit ErrorClass = "resource_limit_exceeded"
	// ClassTimeout marks a run stopped by the engine wall-clock cap or the
	// caller deadline.
	ClassTimeout ErrorClass = "timeout"
	// ClassCanceled marks a run stopped by caller cancellation.
	ClassCanceled ErrorClass = "canceled"
	// ClassEngineFailed marks a start failure, a non-zero exit, or a signal
	// (including a memory-cap kill). Stderr content is never surfaced.
	ClassEngineFailed ErrorClass = "engine_failed"
	// ClassMalformedOutput marks engine stdout that is not the expected JSON
	// shape or carries invalid segment offsets.
	ClassMalformedOutput ErrorClass = "malformed_output"
	// ClassNoSpeech marks a clean run with no speech segments. The error
	// wraps media.ErrNoSpeech so media.Extract records transcript_no_speech.
	ClassNoSpeech ErrorClass = "no_speech"
)

// outcomeCompleted is the closed outcome for a run that returned segments.
const outcomeCompleted = "completed"

// ErrDigestMismatch reports that a pinned binary or model file does not
// match its configured sha256 digest. New refuses to build an Engine then.
var ErrDigestMismatch = errors.New("whisper.cpp pinned file digest mismatch")

// Error is a classified engine failure. Its message carries only the class
// and a fixed, text-free detail; it never includes transcript text, stderr,
// file paths, or source names.
type Error struct {
	Class  ErrorClass
	Detail string
	err    error
}

// Error renders the class and fixed detail.
func (e *Error) Error() string {
	return "whisper.cpp transcript engine: " + string(e.Class) + ": " + e.Detail
}

// Unwrap exposes the wrapped sentinel, such as media.ErrNoSpeech or a
// context error.
func (e *Error) Unwrap() error { return e.err }

// ClassOf returns the ErrorClass of err, or an empty class when err is not a
// classified engine error.
func ClassOf(err error) ErrorClass {
	var engineErr *Error
	if errors.As(err, &engineErr) {
		return engineErr.Class
	}
	return ""
}

func classified(class ErrorClass, detail string) *Error {
	return &Error{Class: class, Detail: detail}
}

func wrapped(class ErrorClass, detail string, err error) *Error {
	return &Error{Class: class, Detail: detail, err: err}
}

func noSpeech() *Error {
	return wrapped(ClassNoSpeech, "no speech segments in engine output", docmedia.ErrNoSpeech)
}

func outcomeOf(err error) string {
	if err == nil {
		return outcomeCompleted
	}
	if class := ClassOf(err); class != "" {
		return string(class)
	}
	return string(ClassEngineFailed)
}
