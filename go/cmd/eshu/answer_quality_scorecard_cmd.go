// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
)

func init() {
	rootCmd.AddCommand(newAnswerQualityScorecardCommand())
}

// newAnswerQualityScorecardCommand builds the answer-quality dogfood scorer.
func newAnswerQualityScorecardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "answer-quality-scorecard",
		Short: "Score captured answer evidence across API, MCP, CLI, and hosted surfaces",
		Long: `answer-quality-scorecard scores a redacted answer-quality evidence
artifact against the dogfood criteria from issue #1935.

It reads JSON from --from or stdin. The evidence must be captured from real API,
MCP, CLI, or hosted runs before scoring, then redacted so it contains no private
paths, hostnames, credentials, raw addresses, or sensitive excerpts. The command
exits non-zero when usefulness, truth honesty, citation coverage, boundedness,
narration fallback preservation, parity, follow-up usefulness, family coverage,
or publish safety fails.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runAnswerQualityScorecard,
	}
	cmd.Flags().String("from", "", "Path to a redacted answer-quality scorecard evidence JSON file (default: read stdin)")
	cmd.Flags().Bool("json", false, "Emit the scorecard verdict as JSON")
	return cmd
}

func runAnswerQualityScorecard(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("from")
	jsonOut, _ := cmd.Flags().GetBool("json")
	raw, err := readAnswerQualityEvidence(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	evidence, err := answerquality.ParseEvidence(raw)
	if err != nil {
		return err
	}
	verdict := answerquality.Score(evidence)
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(verdict); err != nil {
			return fmt.Errorf("write answer-quality scorecard JSON: %w", err)
		}
	} else {
		renderAnswerQualityVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("answer-quality scorecard FAILED: %s", answerQualityFailureSummary(verdict))
	}
	return nil
}

func readAnswerQualityEvidence(stdin io.Reader, path string) ([]byte, error) {
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

func renderAnswerQualityVerdict(w io.Writer, verdict answerquality.Verdict) {
	header := "Answer-quality scorecard PASSED"
	if !verdict.Pass {
		header = "Answer-quality scorecard FAILED"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintf(w, "  run   : %s\n", quoteIfEmpty(verdict.RunID))
	_, _ = fmt.Fprintf(w, "  score : %d\n", verdict.Score)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 44))
	for _, criterion := range verdict.Criteria {
		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", answerQualityMarker(criterion.Status), criterion.Name, criterion.Detail)
	}
	if len(verdict.FollowUpIssues) > 0 {
		_, _ = fmt.Fprintln(w, "follow-up issues:")
		for _, issue := range verdict.FollowUpIssues {
			_, _ = fmt.Fprintf(w, "  - %s [%s]\n", issue.Title, strings.Join(issue.Labels, ", "))
		}
	}
}

func answerQualityMarker(status answerquality.CriterionStatus) string {
	switch status {
	case answerquality.CriterionPass:
		return "[ok]"
	case answerquality.CriterionFail:
		return "[!!]"
	default:
		return "[--]"
	}
}

func answerQualityFailureSummary(verdict answerquality.Verdict) string {
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
