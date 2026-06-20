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
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}
