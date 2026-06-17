package auditreport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderJSON renders the report as deterministic, indented JSON.
func RenderJSON(report Report) ([]byte, error) {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal audit report: %w", err)
	}
	return append(payload, '\n'), nil
}

// RenderMarkdown renders the report as deterministic Markdown: a recommendation
// summary followed by a per-finding table. Entries are already sorted by
// Generate.
func RenderMarkdown(report Report) string {
	var b strings.Builder
	b.WriteString("# Competitive Audit Report\n\n")
	b.WriteString(renderSummary(report))
	b.WriteString("\n## Findings\n\n")
	b.WriteString("| Competitor | Feature | Gap class | Recommendation | Detail | Competitor files | Eshu evidence |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range report.Entries {
		fmt.Fprintf(&b, "| %s | %s | %s | `%s` | %s | %s | %s |\n",
			cell(entry.Competitor), cell(entry.Feature), cell(entry.GapClass),
			entry.Recommendation, cell(entry.RecommendationDetail),
			fileCell(entry.CompetitorFiles), fileCell(entry.EvidenceFiles))
	}
	return b.String()
}

// fileCell renders a list of inspected files into a Markdown table cell so a
// reviewer can verify the source-backed evidence before acting.
func fileCell(files []string) string {
	if len(files) == 0 {
		return "—"
	}
	escaped := make([]string, len(files))
	for i, f := range files {
		escaped[i] = strings.ReplaceAll(f, "|", "\\|")
	}
	return "`" + strings.Join(escaped, "`, `") + "`"
}

func renderSummary(report Report) string {
	counts := map[Recommendation]int{}
	for _, entry := range report.Entries {
		counts[entry.Recommendation]++
	}
	// Severity order is intentional and already deterministic; do not re-sort.
	order := []Recommendation{RecCreateNew, RecLinkExisting, RecUpdateExisting, RecNoIssue, RecReview}
	var parts []string
	for _, rec := range order {
		if counts[rec] > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", rec, counts[rec]))
		}
	}
	return fmt.Sprintf("%d findings — %s\n", len(report.Entries), strings.Join(parts, ", "))
}

// cell escapes pipe characters so a value cannot break the Markdown table.
func cell(value string) string {
	if value == "" {
		return "—"
	}
	return strings.ReplaceAll(value, "|", "\\|")
}
