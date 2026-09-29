// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// classifiedDetailedError is a cause that curates its own class and details,
// like GraphWriteTimeoutError.
type classifiedDetailedError struct {
	message string
	class   string
	details string
}

func (e classifiedDetailedError) Error() string          { return e.message }
func (e classifiedDetailedError) FailureClass() string   { return e.class }
func (e classifiedDetailedError) FailureDetails() string { return e.details }

// truncationMarker is the exact marker text the writer appends (#7407).
func truncationMarker(original, kept int) string {
	return fmt.Sprintf("...[truncated: %d bytes, kept %d]", original, kept)
}

// requireBounded asserts got is a bounded rendering of original: within limit,
// valid UTF-8, an unchanged prefix of original, and ending in the marker that
// names the original size and the kept prefix length.
func requireBounded(t *testing.T, label, got, original string, limit int) {
	t.Helper()
	if len(got) > limit {
		t.Fatalf("%s: stored %d bytes, limit %d", label, len(got), limit)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("%s: stored value is not valid UTF-8", label)
	}
	at := strings.LastIndex(got, "...[truncated: ")
	if at < 0 {
		t.Fatalf("%s: no truncation marker in %.80q...", label, got)
	}
	prefix, marker := got[:at], got[at:]
	if !strings.HasPrefix(original, prefix) {
		t.Fatalf("%s: stored prefix is not a prefix of the original", label)
	}
	if want := truncationMarker(len(original), len(prefix)); marker != want {
		t.Fatalf("%s: marker = %q, want %q", label, marker, want)
	}
}

func TestQueueFailureMetadataBoundsDetails(t *testing.T) {
	t.Parallel()

	original := strings.Repeat("d", 10000)
	cause := classifiedDetailedError{message: "boom", class: "graph_write_timeout", details: original}

	class, message, details := QueueFailureMetadata(cause, "projection_retryable")

	requireBounded(t, "details", details, original, MaxFailureDetailsBytes)
	if class != "graph_write_timeout" {
		t.Fatalf("class = %q, want the cause's own class unchanged", class)
	}
	if message != "boom" {
		t.Fatalf("message = %q, want an in-limit message unchanged", message)
	}
}

func TestQueueFailureMetadataCutsOnRuneBoundary(t *testing.T) {
	t.Parallel()

	// Every rune is two bytes, so a cut at an odd offset lands inside a rune.
	original := strings.Repeat("é", 5000)
	cause := classifiedDetailedError{message: "boom", class: "graph_write_timeout", details: original}

	_, _, details := QueueFailureMetadata(cause, "projection_retryable")

	requireBounded(t, "details", details, original, MaxFailureDetailsBytes)
	at := strings.LastIndex(details, "...[truncated: ")
	if len(details[:at])%2 != 0 {
		t.Fatalf("kept %d bytes of a two-byte-rune run; the cut split a rune", len(details[:at]))
	}
}

func TestQueueFailureMetadataBoundsMessageAndKeepsClass(t *testing.T) {
	t.Parallel()

	original := strings.Repeat("m", 5000)

	class, message, details := QueueFailureMetadata(errors.New(original), "projection_retryable")

	requireBounded(t, "message", message, original, MaxFailureMessageBytes)
	if class != "projection_retryable" {
		t.Fatalf("class = %q, want the fallback class unchanged", class)
	}
	// With no FailureDetails() the details carry the same text, bounded by the
	// details limit, which is larger than the message limit.
	requireBounded(t, "details", details, original, MaxFailureDetailsBytes)
	if len(details) <= len(message) {
		t.Fatalf("details (%d bytes) must keep more of the text than the message (%d bytes)", len(details), len(message))
	}
}

func TestQueueFailureMetadataLeavesValuesAtTheLimitUntouched(t *testing.T) {
	t.Parallel()

	details := strings.Repeat("d", MaxFailureDetailsBytes)
	message := strings.Repeat("m", MaxFailureMessageBytes)
	cause := classifiedDetailedError{message: message, class: "graph_write_timeout", details: details}

	_, gotMessage, gotDetails := QueueFailureMetadata(cause, "projection_retryable")

	if gotDetails != details {
		t.Fatalf("details at exactly the limit changed: %d bytes, marker present = %t", len(gotDetails), strings.Contains(gotDetails, "[truncated"))
	}
	if gotMessage != message {
		t.Fatalf("message at exactly the limit changed: %d bytes, marker present = %t", len(gotMessage), strings.Contains(gotMessage, "[truncated"))
	}

	over := classifiedDetailedError{message: message + "x", class: "graph_write_timeout", details: details + "x"}
	_, overMessage, overDetails := QueueFailureMetadata(over, "projection_retryable")
	requireBounded(t, "details one byte over", overDetails, details+"x", MaxFailureDetailsBytes)
	requireBounded(t, "message one byte over", overMessage, message+"x", MaxFailureMessageBytes)
}

func TestQueueFailureMetadataBoundsAfterSanitizing(t *testing.T) {
	t.Parallel()

	// NUL bytes and invalid UTF-8 are stripped first, so the limit and the
	// marker's byte counts describe the sanitized text that would be stored.
	original := strings.Repeat("a\x00\xff", 5000)
	sanitized := strings.Repeat("a", 5000)

	_, _, details := QueueFailureMetadata(classifiedDetailedError{message: "boom", class: "c", details: original}, "x")

	requireBounded(t, "details", details, sanitized, MaxFailureDetailsBytes)
}

func TestDeadLetterTriageMetadataBoundsTriageDetails(t *testing.T) {
	t.Parallel()

	original := strings.Repeat("t", 10000)

	class, message, details := DeadLetterTriageMetadata(errors.New(original), "project_work_item", false)

	if class == "" {
		t.Fatal("triage class is empty")
	}
	requireBounded(t, "message", message, original, MaxFailureMessageBytes)
	if len(details) > MaxFailureDetailsBytes {
		t.Fatalf("triage details stored %d bytes, limit %d", len(details), MaxFailureDetailsBytes)
	}
	if !strings.HasPrefix(details, "stage=project_work_item triage=") {
		t.Fatalf("details must be the structured triage string, not a message echo: %.120q", details)
	}
	if !strings.Contains(details, "...[truncated: ") {
		t.Fatalf("triage details over the limit carry no marker: %.120q...", details)
	}
}

func TestDeadLetterTriageMetadataKeepsSelfDetailsWhenTheyDifferFromTheMessage(t *testing.T) {
	t.Parallel()

	// A self-detailing cause keeps its own details; the triage string replaces
	// details only when the cause supplies none. A long message must not make a
	// plain error look self-detailed just because bounding made the two differ.
	own := classifiedDetailedError{message: "boom", class: "graph_write_timeout", details: "curated context"}
	_, _, details := DeadLetterTriageMetadata(own, "project_work_item", true)
	if details != "curated context" {
		t.Fatalf("self-detailing cause lost its details: %q", details)
	}

	plain := errors.New(strings.Repeat("p", 2000))
	_, _, details = DeadLetterTriageMetadata(plain, "project_work_item", true)
	if !strings.HasPrefix(details, "stage=project_work_item triage=") {
		t.Fatalf("plain error with a message over the message limit must still get triage details: %.120q", details)
	}
}

// BenchmarkQueueFailureMetadata measures the writer's added work: an in-limit
// cause (the common case, no truncation) and a 64 KB details cause bounded to
// MaxFailureDetailsBytes. Both include sanitizing.
func BenchmarkQueueFailureMetadata(b *testing.B) {
	for _, tc := range []struct {
		name  string
		cause error
	}{
		{"in_limit_800B", classifiedDetailedError{message: "neo4j execute group timed out after 2s", class: "graph_write_timeout", details: strings.Repeat("d", 800)}},
		{"truncated_64KB", classifiedDetailedError{message: "neo4j execute group timed out after 2s", class: "graph_write_timeout", details: strings.Repeat("d", 65536)}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _, _ = QueueFailureMetadata(tc.cause, "projection_retryable")
			}
		})
	}
}
