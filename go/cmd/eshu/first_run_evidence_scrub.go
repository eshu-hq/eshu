// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"regexp"
	"strings"
)

// evidenceEmbeddedURLPattern matches an absolute URL embedded in free-form text.
// It stops at whitespace and at the quoting characters the evidence renderers
// use, so a URL at the end of a sentence or inside a Markdown code span is
// matched without swallowing the surrounding prose.
var evidenceEmbeddedURLPattern = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.\\-]*://[^\\s<>\"'`]+")

// evidenceURLTrailingPunctuation are characters that commonly follow a URL in a
// sentence but are not part of it. They are trimmed before redaction and
// restored afterwards so "reachable at http://host:1/x." keeps its full stop.
const evidenceURLTrailingPunctuation = ".,;:!?)]}"

// evidenceMinReplaceableLen is the shortest non-URL raw value that stage one
// will substitute inside free-form text. Below this a path is both too short to
// be worth redacting and long enough to collide with unrelated substrings.
const evidenceMinReplaceableLen = 8

// scrubEvidenceText makes a composed, free-form string safe for an operator
// artifact.
//
// A name-keyed redactor is correct only until a value is composed from a raw
// one: the endpoint fields are redacted while a summary, hint, cause, or
// next-command built by interpolating the same endpoint is not. This helper
// closes that gap in two stages.
//
// Stage one replaces each known raw value with the same redacted form the
// corresponding report field carries, so a composed string and the field it was
// built from never disagree. Stage two strips the userinfo from any absolute URL
// still present, which catches endpoints this call site does not know about —
// for example one wrapped into a transport error, or one restored from a saved
// envelope where the original inputs are no longer available.
//
// Stage two is what makes the guard survive a new renderer or a new composed
// string: text reaching the report is cleaned on the way in, not at each
// rendering surface.
func scrubEvidenceText(text string, rawValues []string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	scrubbed := text
	for _, raw := range rawValues {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.Contains(scrubbed, raw) {
			continue
		}
		redacted := redactEvidenceValue(raw)
		if redacted == raw {
			continue
		}
		// A short path substring would rewrite unrelated text (replacing "/a"
		// would corrupt "/api/v0"), so only substitute values distinctive enough
		// to identify. URLs are always distinctive; short paths carry nothing
		// worth redacting, and stage two still covers any URL inside them.
		if !strings.Contains(raw, "://") && len(raw) < evidenceMinReplaceableLen {
			continue
		}
		scrubbed = strings.ReplaceAll(scrubbed, raw, redacted)
	}
	return evidenceEmbeddedURLPattern.ReplaceAllStringFunc(scrubbed, redactEmbeddedURL)
}

// redactEvidenceValue picks the right redaction for a known raw value: URL-like
// values keep their scheme, host, and path with any userinfo removed, while
// filesystem targets collapse to their final element.
func redactEvidenceValue(raw string) string {
	if strings.Contains(raw, "://") {
		return redactEndpoint(raw)
	}
	return redactPath(raw)
}

// redactEmbeddedURL redacts a single URL matched inside free-form text,
// preserving any trailing sentence punctuation the match absorbed.
func redactEmbeddedURL(match string) string {
	trimmed := strings.TrimRight(match, evidenceURLTrailingPunctuation)
	suffix := match[len(trimmed):]
	if trimmed == "" {
		return match
	}
	return redactEndpoint(trimmed) + suffix
}

// scrubEvidenceTexts applies scrubEvidenceText to every element of a slice,
// returning a new slice so the caller's data is never mutated in place.
func scrubEvidenceTexts(values []string, rawValues []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, scrubEvidenceText(v, rawValues))
	}
	return out
}

// scrubEvidenceDiagnostic returns a redacted copy of the diagnostic for the
// report. It copies rather than mutating because the diagnostic is shared with
// the caller's firstRunResult, which stays raw so the run's own error reporting
// keeps its full detail.
//
// The preserved underlying error is replaced by a scrubbed error value: the
// report renders it as the "cause" line, so leaving the original chain in place
// would put the raw endpoint straight into the artifact.
func scrubEvidenceDiagnostic(d *onboardingDiagnostic, rawValues []string) *onboardingDiagnostic {
	if d == nil {
		return nil
	}
	scrubbed := *d
	scrubbed.Summary = scrubEvidenceText(d.Summary, rawValues)
	scrubbed.RecoverySteps = scrubEvidenceTexts(d.RecoverySteps, rawValues)
	scrubbed.DocsLink = scrubEvidenceText(d.DocsLink, rawValues)
	if cause := d.rootCause(); cause != "" {
		scrubbed.Underlying = errors.New(scrubEvidenceText(cause, rawValues))
	}
	return &scrubbed
}

// scrubEvidenceTruth returns a copy of the truth metadata with every string
// value scrubbed. The truth vocabulary is a bounded label set today, so this is
// a guard against a future label that carries an endpoint rather than a fix for
// an observed leak.
func scrubEvidenceTruth(truth map[string]any, rawValues []string) map[string]any {
	if truth == nil {
		return nil
	}
	out := make(map[string]any, len(truth))
	for k, v := range truth {
		if s, ok := v.(string); ok {
			out[k] = scrubEvidenceText(s, rawValues)
			continue
		}
		out[k] = v
	}
	return out
}
