package render

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxArtifactBytes is the maximum content size accepted by any validator.
// Content exceeding this limit is rejected without further parsing.
const maxArtifactBytes = 1 << 20 // 1 MiB

// oversize returns a single-element issue slice if content exceeds
// maxArtifactBytes, or nil if the content is within the limit.
// Apply this at the start of every validator before any parsing.
func oversize(content string) []string {
	if len(content) > maxArtifactBytes {
		return []string{"artifact exceeds size limit"}
	}
	return nil
}

// validateJSON validates that content is non-empty, within the size cap, and
// parses as valid JSON. It returns a slice of human-readable issues; nil or
// empty means the content is valid.
func validateJSON(content string) []string {
	if issues := oversize(content); issues != nil {
		return issues
	}
	if strings.TrimSpace(content) == "" {
		return []string{"empty content"}
	}
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return []string{fmt.Sprintf("invalid JSON: %s", err.Error())}
	}
	return nil
}

// validateYAML validates that content is non-empty, within the size cap, and
// parses as valid YAML using gopkg.in/yaml.v3. It returns a slice of
// human-readable issues; nil or empty means the content is valid.
func validateYAML(content string) []string {
	if issues := oversize(content); issues != nil {
		return issues
	}
	if strings.TrimSpace(content) == "" {
		return []string{"empty content"}
	}
	var v any
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return []string{fmt.Sprintf("invalid YAML: %s", err.Error())}
	}
	return nil
}

// validateCSV validates that content is non-empty, within the size cap, and
// parses as valid CSV with consistent column counts. Setting FieldsPerRecord=0
// causes csv.Reader to infer the expected field count from the first record and
// return an error on any subsequent record with a different count. It returns a
// slice of human-readable issues; nil or empty means the content is valid.
func validateCSV(content string) []string {
	if issues := oversize(content); issues != nil {
		return issues
	}
	if strings.TrimSpace(content) == "" {
		return []string{"empty content"}
	}
	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = 0 // infer from first record; mismatch is an error
	if _, err := r.ReadAll(); err != nil {
		return []string{fmt.Sprintf("invalid CSV: %s", err.Error())}
	}
	return nil
}

// validateMarkdown validates that content is non-empty and within the size cap.
// Markdown has no structural grammar to enforce, so this is a passthrough
// validator: any non-empty content within the size limit is accepted.
func validateMarkdown(content string) []string {
	if issues := oversize(content); issues != nil {
		return issues
	}
	if strings.TrimSpace(content) == "" {
		return []string{"empty content"}
	}
	return nil
}

// mermaidKeywords lists the recognized Mermaid diagram type keywords.
// validateMermaid performs a bounded syntactic lint against this set; it is
// NOT a full Mermaid parse and does not catch all invalid diagrams.
var mermaidKeywords = []string{
	"graph",
	"flowchart",
	"sequenceDiagram",
	"classDiagram",
	"stateDiagram-v2",
	"stateDiagram",
	"erDiagram",
	"gantt",
	"pie",
	"journey",
	"mindmap",
	"timeline",
}

// validateMermaid performs a bounded syntactic lint on Mermaid diagram content.
// It is NOT a full Mermaid parse — a diagram that passes this validator may
// still be rejected by a Mermaid renderer. The lint checks:
//
//  1. Content is non-empty and within the size cap.
//  2. The first token on the first non-empty line is a recognized diagram keyword.
//
// Bracket balance is intentionally NOT checked: valid Mermaid relationship
// tokens (e.g. ER cardinality like "||--o{") contain unmatched delimiters by
// design, so a balance check produces false positives on correct diagrams.
//
// It returns a slice of human-readable issues; nil or empty means the content
// passed the lint.
func validateMermaid(content string) []string {
	if issues := oversize(content); issues != nil {
		return issues
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return []string{"empty content"}
	}

	var issues []string

	// Check that the first non-empty line starts with a recognized keyword.
	firstLine := firstNonEmptyLine(trimmed)
	kw := firstToken(firstLine)
	if !isMermaidKeyword(kw) {
		issues = append(issues, "unrecognized mermaid diagram type")
	}

	return issues
}

// firstNonEmptyLine returns the first line in s that is non-empty after
// trimming whitespace, or an empty string if no such line exists.
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// firstToken returns the first whitespace-delimited token from line.
func firstToken(line string) string {
	line = strings.TrimSpace(line)
	if idx := strings.IndexAny(line, " \t"); idx >= 0 {
		return line[:idx]
	}
	return line
}

// isMermaidKeyword reports whether token matches a recognized Mermaid diagram
// type keyword (case-sensitive, as Mermaid keywords are case-sensitive).
func isMermaidKeyword(token string) bool {
	for _, kw := range mermaidKeywords {
		if token == kw {
			return true
		}
	}
	return false
}
