// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// Stored-length limits for the failure text written at Fail time (#7407).
const (
	// MaxFailureDetailsBytes bounds failure_details.
	MaxFailureDetailsBytes = 4096
	// MaxFailureMessageBytes bounds failure_message.
	MaxFailureMessageBytes = 1024
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

// boundFailureText caps text at limit bytes for a durable failure column.
//
// Text within the limit is returned unchanged. Longer text keeps a prefix cut on
// a rune boundary and ends in "...[truncated: <original> bytes, kept <n>]",
// which names the size of the text it replaces and the length of the prefix
// kept. The marker counts toward the limit, so the stored value never exceeds
// it, for any limit at least as long as the marker (about 50 bytes; the two
// limits in this package are far above that). The cut is measured on text that has already been sanitized, so the byte
// counts describe what would have been stored, and the fold that copies the
// stored details later (#7320) copies exactly this string.
func boundFailureText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	// The kept length has at most as many digits as the limit, so reserving the
	// marker at that width leaves room for the real, possibly shorter, marker.
	widest := failureTruncationMarker(len(text), limit)
	keep := limit - len(widest)
	if keep < 0 {
		keep = 0
	}
	for keep > 0 && !utf8.RuneStart(text[keep]) {
		keep--
	}
	return text[:keep] + failureTruncationMarker(len(text), keep)
}

// failureTruncationMarker renders the marker boundFailureText appends.
func failureTruncationMarker(original, kept int) string {
	return "...[truncated: " + strconv.Itoa(original) + " bytes, kept " + strconv.Itoa(kept) + "]"
}

// QueueFailureMetadata reconciles a failure cause into the durable
// failure_class, message, and details values for a work item that is being
// retried (not dead-lettered): a self-classifying error's own class wins
// over fallbackClass, and its own details win over the sanitized error
// message. The message is bounded to MaxFailureMessageBytes and the details to
// MaxFailureDetailsBytes (#7407); the class is not bounded because it only
// ever holds constants.
func QueueFailureMetadata(cause error, fallbackClass string) (string, string, string) {
	failureClass, message, details, _ := reconcileFailureMetadata(cause, fallbackClass)
	return failureClass, message, details
}

// reconcileFailureMetadata returns the bounded class, message and details, and
// whether the cause supplied details of its own. The comparison behind that
// flag is made on the sanitized, unbounded text: bounding gives the message and
// the details different limits, so comparing the bounded values would report a
// plain error with a long message as self-detailed.
func reconcileFailureMetadata(cause error, fallbackClass string) (class, message, details string, ownDetails bool) {
	fullMessage := sanitizeFailureText(cause.Error())
	fullDetails := fullMessage
	class = fallbackClass

	var classified classifiedFailure
	if errors.As(cause, &classified) {
		if c := sanitizeFailureText(classified.FailureClass()); c != "" {
			class = c
		}
	}

	var detailed detailedFailure
	if errors.As(cause, &detailed) {
		if detail := sanitizeFailureText(detailed.FailureDetails()); detail != "" {
			fullDetails = detail
		}
	}

	return class,
		boundFailureText(fullMessage, MaxFailureMessageBytes),
		boundFailureText(fullDetails, MaxFailureDetailsBytes),
		fullDetails != fullMessage
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
	failureClass, message, details, ownDetails := reconcileFailureMetadata(cause, triage.FailureClass)

	// The sanitized message is the details only when the error does not
	// self-provide FailureDetails(). In that case prefer the structured triage
	// details so the dead-letter row carries the triage classification rather
	// than a bare message echo. The triage string embeds the whole message, so
	// it is bounded like any other details value.
	if !ownDetails {
		details = boundFailureText(sanitizeFailureText(triage.Details), MaxFailureDetailsBytes)
	}

	return failureClass, message, details
}
