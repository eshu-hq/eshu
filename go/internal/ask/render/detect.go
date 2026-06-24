// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package render

import (
	"strings"
)

// DetectFormat detects the output format based on an explicit request and question content.
//
// If the requested format is a known non-auto format, it is returned immediately (explicit
// request wins). Otherwise, the question is analyzed for format cues using case-insensitive
// matching. The first matching cue in precedence order determines the format:
//
//  1. contains "mermaid" or "diagram" -> FormatMermaid
//  2. shows YAML export intent -> FormatYAML
//  3. shows CSV export intent -> FormatCSV
//  4. shows JSON export intent -> FormatJSON
//  5. otherwise -> FormatMarkdown
//
// For yaml/csv/json, a bare mention of the format word is not enough. The question
// must show EXPORT INTENT — see hasExportIntent for the conservative rule.
// Default is Markdown on any doubt. Mermaid is an exception: asking for a
// diagram is itself an export request, so "mermaid" or "diagram" always wins.
//
// If requested is empty or an unknown format string, the question is analyzed.
func DetectFormat(question, requested string) Format {
	// Try to parse the requested format.
	if f, ok := KnownFormat(requested); ok && f != FormatAuto {
		// Explicit non-auto format wins.
		return f
	}

	// Infer from question. Normalize case once.
	q := strings.ToLower(question)

	// Mermaid: asking for a diagram is itself export intent.
	if strings.Contains(q, "mermaid") || strings.Contains(q, "diagram") {
		return FormatMermaid
	}
	if hasExportIntent(q, "yaml") {
		return FormatYAML
	}
	if hasExportIntent(q, "csv") {
		return FormatCSV
	}
	if hasExportIntent(q, "json") {
		return FormatJSON
	}

	// Default to Markdown.
	return FormatMarkdown
}

// exportVerbs is the set of verbs whose presence anywhere in the question
// signals an intent to produce output in a specific format. The verb does
// not need to be adjacent to the format word; both just need to appear
// somewhere in the lowercased question.
var exportVerbs = []string{
	"export",
	"output",
	"render",
	"dump",
	"download",
	"convert",
	"serialize",
	"give me",
	"produce",
	"generate",
}

// hasExportIntent reports whether the lowercased question q shows a genuine
// intent to export output in format fmt. The rule is conservative and defaults
// to false (no export intent) on any doubt, to avoid switching away from
// Markdown when the format word merely appears in context (e.g. "yaml manifest",
// "json config field", "csv file location").
//
// Export intent is inferred when ANY of the following hold:
//   - q contains "as <fmt>", "as a <fmt>", or "as an <fmt>"
//   - q contains "in <fmt>", "into <fmt>", or "to <fmt>"
//   - q contains "<fmt> format" (e.g. "in csv format")
//   - q contains both an export verb and the format word (anywhere in the string)
//
// Bare format mentions without any of the above patterns are NOT intent:
// "yaml manifests", "json field", "csv file" all return false.
func hasExportIntent(q, fmt string) bool {
	if !strings.Contains(q, fmt) {
		return false
	}

	// Construction checks: format word in a clear output phrase.
	constructions := []string{
		"as " + fmt,
		"as a " + fmt,
		"as an " + fmt,
		"in " + fmt,
		"into " + fmt,
		"to " + fmt,
		fmt + " format",
	}
	for _, c := range constructions {
		if strings.Contains(q, c) {
			return true
		}
	}

	// Export verb + format word anywhere in the question.
	for _, verb := range exportVerbs {
		if strings.Contains(q, verb) {
			return true
		}
	}

	return false
}
