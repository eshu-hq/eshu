// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

import (
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

type classifiedFailure interface {
	FailureClass() string
}

type detailedFailure interface {
	FailureDetails() string
}

// sanitizeFailureText strips invalid UTF-8 and embedded NUL bytes from
// failure text before it is written to a durable failure_class/message/
// details column, so a byte sequence Postgres would reject (or that would
// corrupt a terminal/log renderer) never reaches storage.
func sanitizeFailureText(text string) string {
	if text == "" {
		return text
	}
	sanitized := strings.ToValidUTF8(text, "")
	return strings.ReplaceAll(sanitized, "\x00", "")
}

// QueueFailureMetadata reconciles a failure cause into the durable
// failure_class, message, and details values for a work item that is being
// retried (not dead-lettered): a self-classifying error's own class wins
// over fallbackClass, and its own details win over the sanitized error
// message.
func QueueFailureMetadata(cause error, fallbackClass string) (string, string, string) {
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

// DeadLetterTriageMetadata returns the durable failure_class, message, and
// details for a work item that is about to be dead-lettered. It reconciles three
// sources with explicit precedence so the operator-facing triage surface never
// loses curated context:
//
//  1. An error that self-classifies (implements classifiedFailure /
//     detailedFailure, e.g. GraphWriteTimeoutError) keeps its own class and
//     details — these are author-curated and the most precise.
//  2. Otherwise the failure_class is the operator-facing triage class from
//     failure.TriageFailure (retry_exhausted / input_invalid / projection_bug
//     / …), and the details are the structured triage string.
//
// retryable is the canonical IsRetryable() authority for the cause; the dead
// letter path always passes attemptsExhausted=true because by construction the
// item is no longer being retried.
func DeadLetterTriageMetadata(cause error, stage string, retryable bool) (string, string, string) {
	triage := failure.TriageFailure(cause, stage, retryable, true)
	failureClass, message, details := QueueFailureMetadata(cause, triage.FailureClass)

	// QueueFailureMetadata returns the sanitized message as details only when
	// the error does not self-provide FailureDetails(). In that case prefer
	// the structured triage details so the dead-letter row carries the
	// triage classification rather than a bare message echo.
	if details == message {
		details = sanitizeFailureText(triage.Details)
	}

	return failureClass, message, details
}
