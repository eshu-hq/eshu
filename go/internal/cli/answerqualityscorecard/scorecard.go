// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerqualityscorecard

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
)

// ReadEvidence returns the captured answer-quality evidence bytes for `eshu
// answer-quality-scorecard`: the file at path when path is non-empty, and
// otherwise everything on stdin. A blank or all-whitespace path selects stdin,
// so a caller that resolved an unset --from flag does not have to special-case
// it. The returned bytes are not decoded or validated here; that is
// answerquality.ParseEvidence's job.
func ReadEvidence(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read answer-quality evidence from stdin: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local evidence artifact path, not an HTTP request param //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read answer-quality evidence file %q: %w", path, err)
	}
	return raw, nil
}

// RenderVerdict writes the compact, human-readable view of a scorecard verdict
// to w. It renders the header, the run and score lines, one line per scored
// criterion, and the titles and labels of any follow-up issues.
//
// This is the text-mode renderer only. The --json output path marshals
// answerquality.Verdict directly so the machine-readable shape stays exactly
// what answerquality produces, and it never calls this function.
//
// Two values reach this output straight out of the captured evidence artifact:
// the run id (printed on the run line) and any evidence-derived text that
// answerquality.Score composed into a criterion detail. Both are screened by
// the scorecard's publish-safety criterion before the verdict is built; this
// renderer performs no redaction of its own and must not be relied on for any.
func RenderVerdict(w io.Writer, verdict answerquality.Verdict) {
	header := "Answer-quality scorecard PASSED"
	if !verdict.Pass {
		header = "Answer-quality scorecard FAILED"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintf(w, "  run   : %s\n", quoteIfEmpty(verdict.RunID))
	_, _ = fmt.Fprintf(w, "  score : %d\n", verdict.Score)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 44))
	for _, criterion := range verdict.Criteria {
		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", marker(criterion.Status), criterion.Name, criterion.Detail)
	}
	if len(verdict.FollowUpIssues) > 0 {
		_, _ = fmt.Fprintln(w, "follow-up issues:")
		for _, issue := range verdict.FollowUpIssues {
			_, _ = fmt.Fprintf(w, "  - %s [%s]\n", issue.Title, strings.Join(issue.Labels, ", "))
		}
	}
}

// FailureSummary joins every failed criterion into one line for the error the
// command returns when the scorecard rejects its evidence. It returns "unknown
// failure" when the verdict failed but no criterion is marked failed, rather
// than naming a cause the criteria do not support.
func FailureSummary(verdict answerquality.Verdict) string {
	var failures []string
	for _, criterion := range verdict.Criteria {
		if criterion.Status == answerquality.CriterionFail {
			failures = append(failures, fmt.Sprintf("%s: %s", criterion.Name, criterion.Detail))
		}
	}
	if len(failures) == 0 {
		return "unknown failure"
	}
	return strings.Join(failures, "; ")
}

// marker is the fixed-width status glyph each criterion line opens with, so
// the criteria column stays aligned in a terminal. A status the scorer does
// not classify as pass or fail renders as not-measured rather than as a pass.
func marker(status answerquality.CriterionStatus) string {
	switch status {
	case answerquality.CriterionPass:
		return "[ok]"
	case answerquality.CriterionFail:
		return "[!!]"
	default:
		return "[--]"
	}
}

// quoteIfEmpty renders a placeholder for an empty run id so the rendered line
// stays readable instead of trailing off after the label.
//
// This is a copy of the helper in go/cmd/eshu/first_run.go, not a move: that
// file is package main, so nothing can import it, and the original still has
// callers there. The two are independent by construction.
func quoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}
