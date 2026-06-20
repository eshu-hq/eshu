package render

import (
	"strings"
)

// Format represents a supported output format.
type Format string

// Known format constants.
const (
	FormatAuto     Format = "auto"
	FormatMarkdown Format = "markdown"
	FormatMermaid  Format = "mermaid"
	FormatJSON     Format = "json"
	FormatYAML     Format = "yaml"
	FormatCSV      Format = "csv"
)

// KnownFormat parses a request string to a Format.
// It performs case-insensitive matching and trims whitespace.
// It returns the matching Format and true, or ("", false) for unknown formats.
func KnownFormat(s string) (Format, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "auto":
		return FormatAuto, true
	case "markdown":
		return FormatMarkdown, true
	case "mermaid":
		return FormatMermaid, true
	case "json":
		return FormatJSON, true
	case "yaml":
		return FormatYAML, true
	case "csv":
		return FormatCSV, true
	default:
		return "", false
	}
}

// Artifact represents a formatted output with validation state.
type Artifact struct {
	// Format is the output format type.
	Format Format
	// Content is the rendered output.
	Content string
	// Issues collects validation errors that occurred during rendering.
	Issues []string
}

// Valid reports whether the artifact has no validation issues.
func (a Artifact) Valid() bool {
	return len(a.Issues) == 0
}
