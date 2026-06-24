// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// renderEvidenceJSON serializes the evidence report as indented JSON. The model
// is already redacted, so the bytes are safe to write to a shared artifact.
func renderEvidenceJSON(report firstRunEvidenceReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// renderEvidenceMarkdown renders the report as a compact Markdown artifact. It
// reads only the already-redacted report fields, so no endpoint, target, or
// secret can leak through this surface.
func renderEvidenceMarkdown(report firstRunEvidenceReport) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# First-run evidence\n\n")
	fmt.Fprintf(&b, "Outcome: **%s**\n\n", report.Outcome)

	fmt.Fprintf(&b, "## Runtime\n\n")
	fmt.Fprintf(&b, "- Runtime shape: `%s`\n", report.RuntimeShape)
	fmt.Fprintf(&b, "- Service endpoint: `%s`\n", evidenceMarkdownValue(report.ServiceEndpoint))
	if report.MCPEndpoint != "" {
		fmt.Fprintf(&b, "- MCP endpoint: `%s`\n", report.MCPEndpoint)
	}

	fmt.Fprintf(&b, "\n## Indexing\n\n")
	fmt.Fprintf(&b, "- Indexing state: `%s`\n", report.IndexingState)
	fmt.Fprintf(&b, "- Readiness: `%s`\n", evidenceMarkdownValue(report.Readiness))
	if report.SelectedTarget != "" {
		fmt.Fprintf(&b, "- Selected target: `%s`\n", report.SelectedTarget)
	}
	if len(report.IndexedRepositories) > 0 {
		fmt.Fprintf(&b, "- Indexed repositories: %s\n", evidenceMarkdownCodeList(report.IndexedRepositories))
	}

	fmt.Fprintf(&b, "\n## First query\n\n")
	fmt.Fprintf(&b, "- Query answered: `%t`\n", report.QueryAnswered)
	if report.QuerySummary != "" {
		fmt.Fprintf(&b, "- Query summary: %s\n", report.QuerySummary)
	}
	if freshness, completeness, ok := evidenceTruthLabels(report.Truth); ok {
		fmt.Fprintf(&b, "- Truth: freshness `%s`, completeness `%s`\n", freshness, completeness)
	}

	if report.Diagnosis != nil {
		fmt.Fprintf(&b, "\n## Diagnosis\n\n")
		fmt.Fprintf(&b, "- Class: `%s`\n", report.Diagnosis.Class)
		fmt.Fprintf(&b, "- Summary: %s\n", report.Diagnosis.Summary)
		if cause := report.Diagnosis.rootCause(); cause != "" {
			fmt.Fprintf(&b, "- Cause: %s\n", cause)
		}
	}

	evidenceMarkdownList(&b, "Missing evidence", report.MissingEvidence)
	evidenceMarkdownList(&b, "Next commands", report.NextCommands)
	evidenceMarkdownList(&b, "Docs", report.DocsLinks)
	return b.String(), nil
}

// renderEvidenceTerminal writes a concise, operator-facing terminal summary of
// the report. Like the artifact renderers it reads only redacted fields.
func renderEvidenceTerminal(w io.Writer, report firstRunEvidenceReport) {
	_, _ = fmt.Fprintln(w, "First-run evidence")
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(w, "  outcome        : %s\n", report.Outcome)
	_, _ = fmt.Fprintf(w, "  runtime shape  : %s\n", report.RuntimeShape)
	_, _ = fmt.Fprintf(w, "  service url    : %s\n", evidenceTerminalValue(report.ServiceEndpoint))
	if report.MCPEndpoint != "" {
		_, _ = fmt.Fprintf(w, "  mcp endpoint   : %s\n", report.MCPEndpoint)
	}
	_, _ = fmt.Fprintf(w, "  indexing state : %s\n", report.IndexingState)
	_, _ = fmt.Fprintf(w, "  readiness      : %s\n", evidenceTerminalValue(report.Readiness))
	if report.SelectedTarget != "" {
		_, _ = fmt.Fprintf(w, "  selected target: %s\n", report.SelectedTarget)
	}
	_, _ = fmt.Fprintf(w, "  query answered : %t\n", report.QueryAnswered)
	if report.QuerySummary != "" {
		_, _ = fmt.Fprintf(w, "  query summary  : %s\n", report.QuerySummary)
	}
	if freshness, completeness, ok := evidenceTruthLabels(report.Truth); ok {
		_, _ = fmt.Fprintf(w, "  truth          : freshness %s, completeness %s\n", freshness, completeness)
	}
	if report.Diagnosis != nil {
		_, _ = fmt.Fprintf(w, "  diagnosis      : [%s] %s\n", report.Diagnosis.Class, report.Diagnosis.Summary)
	}
	evidenceTerminalList(w, "missing evidence", report.MissingEvidence)
	evidenceTerminalList(w, "next commands", report.NextCommands)
	evidenceTerminalList(w, "docs", report.DocsLinks)
}

// evidenceTruthLabels extracts the freshness and completeness labels from the
// truth metadata, reporting ok=false when either label is absent.
func evidenceTruthLabels(truth map[string]any) (string, string, bool) {
	if truth == nil {
		return "", "", false
	}
	freshness, fOK := truth["freshness"].(string)
	completeness, cOK := truth["completeness"].(string)
	if !fOK || !cOK {
		return "", "", false
	}
	return freshness, completeness, true
}

// evidenceMarkdownValue renders a possibly-empty value for an inline code span,
// substituting a stable placeholder so the span never renders empty backticks.
func evidenceMarkdownValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

// evidenceTerminalValue renders a possibly-empty value for the terminal summary.
func evidenceTerminalValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

// evidenceMarkdownCodeList renders a slice as a comma-separated list of inline
// code spans.
func evidenceMarkdownCodeList(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, "`"+v+"`")
	}
	return strings.Join(parts, ", ")
}

// evidenceMarkdownList writes a titled bullet section when the slice is
// non-empty, and writes nothing otherwise.
func evidenceMarkdownList(b *strings.Builder, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, v := range values {
		fmt.Fprintf(b, "- %s\n", v)
	}
}

// evidenceTerminalList writes a titled, indented bullet section to the terminal
// when the slice is non-empty.
func evidenceTerminalList(w io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "%s:\n", title)
	for _, v := range values {
		_, _ = fmt.Fprintf(w, "  - %s\n", v)
	}
}
