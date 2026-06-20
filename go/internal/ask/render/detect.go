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
//  2. contains "yaml" -> FormatYAML
//  3. contains "csv" -> FormatCSV
//  4. contains "json" -> FormatJSON
//  5. otherwise -> FormatMarkdown
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

	// Check in documented precedence order.
	if strings.Contains(q, "mermaid") || strings.Contains(q, "diagram") {
		return FormatMermaid
	}
	if strings.Contains(q, "yaml") {
		return FormatYAML
	}
	if strings.Contains(q, "csv") {
		return FormatCSV
	}
	if strings.Contains(q, "json") {
		return FormatJSON
	}

	// Default to Markdown.
	return FormatMarkdown
}
