// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
	"github.com/eshu-hq/eshu/go/internal/cli/answerqualityscorecard"
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
	raw, err := answerqualityscorecard.ReadEvidence(cmd.InOrStdin(), path)
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
		answerqualityscorecard.RenderVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("answer-quality scorecard FAILED: %s", answerqualityscorecard.FailureSummary(verdict))
	}
	return nil
}
