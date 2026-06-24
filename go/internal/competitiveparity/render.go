package competitiveparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// RenderJSON serializes a parity report as stable, indented JSON.
func RenderJSON(report Report) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return nil, fmt.Errorf("encode competitive parity report: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderMarkdown serializes a parity report as a bounded Markdown artifact.
func RenderMarkdown(report Report) string {
	var b strings.Builder
	status := "PASSED"
	if !report.Pass {
		status = "FAILED"
	}
	fmt.Fprintf(&b, "# Competitive Parity Gate\n\n")
	fmt.Fprintf(&b, "- Schema: `%s`\n", report.SchemaVersion)
	fmt.Fprintf(&b, "- Status: **%s**\n", status)
	fmt.Fprintf(&b, "- Surfaces: %d passed, %d failed\n\n", report.Summary.Passed, report.Summary.Failed)
	for _, surface := range report.Surfaces {
		marker := "ok"
		if !surface.Pass {
			marker = "fail"
		}
		fmt.Fprintf(&b, "## %s (%s)\n\n", surface.DisplayName, marker)
		fmt.Fprintf(&b, "- Peer baseline: %s\n", surface.PeerBaseline)
		fmt.Fprintf(&b, "- Presence: %s\n", passLabel(surface.PresencePass))
		fmt.Fprintf(&b, "- Quality: %s\n", passLabel(surface.QualityPass))
		fmt.Fprintf(
			&b, "- Quality score: %d/%d dimensions passed (%d/%d signals)\n",
			surface.QualityScore.Passed,
			len(surface.Quality),
			surface.QualityScore.Score,
			surface.QualityScore.Max,
		)
		if len(surface.RelatedIssues) > 0 {
			fmt.Fprintf(&b, "- Related issues:")
			for _, issue := range surface.RelatedIssues {
				fmt.Fprintf(&b, " #%d (%s)", issue.Number, issue.Reason)
			}
			fmt.Fprintf(&b, "\n")
		}
		if len(surface.ResidualIssues) > 0 {
			fmt.Fprintf(&b, "- Residual issues:")
			for _, issue := range surface.ResidualIssues {
				fmt.Fprintf(&b, " #%d (%s)", issue.Number, issue.Reason)
			}
			fmt.Fprintf(&b, "\n")
		}
		fmt.Fprintf(&b, "\n| Check | Target | Status |\n| --- | --- | --- |\n")
		for _, check := range surface.Checks {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", check.Kind, check.Target, check.Status)
		}
		if len(surface.Quality) > 0 {
			fmt.Fprintf(&b, "\n| Quality dimension | Score | Status | Missing signals |\n| --- | --- | --- | --- |\n")
			for _, quality := range surface.Quality {
				fmt.Fprintf(
					&b, "| `%s` | %d/%d | %s | %s |\n",
					quality.Dimension,
					quality.Score,
					quality.MaxScore,
					passLabel(quality.Pass),
					renderMissingSignals(quality.Missing),
				)
			}
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

func passLabel(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}

func renderMissingSignals(signals []QualitySignal) string {
	if len(signals) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(signals))
	for _, signal := range signals {
		if strings.TrimSpace(signal.SourcePath) == "" {
			parts = append(parts, fmt.Sprintf("`%s`", signal.Term))
			continue
		}
		parts = append(parts, fmt.Sprintf("`%s` in `%s`", signal.Term, signal.SourcePath))
	}
	return strings.Join(parts, "<br>")
}
