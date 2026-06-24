// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

type classifiedFailure interface {
	FailureClass() string
}

type detailedFailure interface {
	FailureDetails() string
}

func queueFailureMetadata(cause error, fallbackClass string) (string, string, string) {
	message := sanitizeFailureText(cause.Error())
	details := message
	failureClass := fallbackClass

	var classified classifiedFailure
	if errors.As(cause, &classified) {
		if class := sanitizeFailureText(classified.FailureClass()); class != "" {
			failureClass = class
		}
	}

	var detailed detailedFailure
	if errors.As(cause, &detailed) {
		if detail := sanitizeFailureText(detailed.FailureDetails()); detail != "" {
			details = detail
		}
	}

	return failureClass, message, details
}

// deadLetterTriageMetadata returns the durable failure_class, message, and
// details for a work item that is about to be dead-lettered. It reconciles three
// sources with explicit precedence so the operator-facing triage surface never
// loses curated context:
//
//  1. An error that self-classifies (implements classifiedFailure /
//     detailedFailure, e.g. GraphWriteTimeoutError) keeps its own class and
//     details — these are author-curated and the most precise.
//  2. Otherwise the failure_class is the operator-facing triage class from
//     projector.TriageFailure (retry_exhausted / input_invalid / projection_bug
//     / …), and the details are the structured triage string.
//
// retryable is the canonical IsRetryable() authority for the cause; the dead
// letter path always passes attemptsExhausted=true because by construction the
// item is no longer being retried.
func deadLetterTriageMetadata(cause error, stage string, retryable bool) (string, string, string) {
	triage := projector.TriageFailure(cause, stage, retryable, true)
	failureClass, message, details := queueFailureMetadata(cause, triage.FailureClass)

	// queueFailureMetadata returns the sanitized message as details only when the
	// error does not self-provide FailureDetails(). In that case prefer the
	// structured triage details so the dead-letter row carries the triage
	// classification rather than a bare message echo.
	if details == message {
		details = sanitizeFailureText(triage.Details)
	}

	return failureClass, message, details
}
