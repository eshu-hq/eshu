// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
)

func init() {
	rootCmd.AddCommand(newFirstRunBenchmarkCommand())
}

// newFirstRunBenchmarkCommand builds a fresh first-run-benchmark command. A
// constructor (rather than a package-level singleton) keeps each invocation,
// including tests, free of leaked flag state.
func newFirstRunBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "first-run-benchmark",
		Short: "Score a first-run --json envelope against the onboarding benchmark",
		Long: `first-run-benchmark scores the canonical envelope emitted by
"eshu first-run --json" against the first-five-minutes onboarding success
criteria from issue #1772.

It reads the envelope from a file (--envelope) or stdin, evaluates whether a
new user reached one useful answer with bounded evidence, and prints a
scorecard. The benchmark FAILS (non-zero exit) when the "first answer" comes
from health-only status rather than a completed indexing and bounded query
proof: a missing query answer, missing truth metadata, missing source handle,
incomplete indexing, or an error envelope all reject the run.

Typical use:

  eshu first-run --json > /tmp/first-run.json
  eshu first-run-benchmark --envelope /tmp/first-run.json --path local_binary`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runFirstRunBenchmark,
	}
	cmd.Flags().String("envelope", "", "Path to a first-run --json envelope (default: read stdin)")
	cmd.Flags().String("path", "local_binary", "Onboarding path label: local_binary, local_compose, or hosted")
	cmd.Flags().Int("manual-steps", firstrunbench.NotMeasuredManualSteps, "Declared manual copy/paste step count for this path (negative = not declared)")
	cmd.Flags().Bool("json", false, "Emit the scorecard as JSON")
	return cmd
}

// runFirstRunBenchmark reads the envelope, scores it, prints the scorecard, and
// returns a non-zero exit (via error) when the benchmark verdict is FAIL so the
// health-only-rejection invariant is enforced at the process boundary.
func runFirstRunBenchmark(cmd *cobra.Command, _ []string) error {
	envelopePath, _ := cmd.Flags().GetString("envelope")
	pathLabel, _ := cmd.Flags().GetString("path")
	manualSteps, _ := cmd.Flags().GetInt("manual-steps")
	jsonOut, _ := cmd.Flags().GetBool("json")

	raw, err := firstrunbench.ReadEnvelope(cmd.InOrStdin(), envelopePath)
	if err != nil {
		return err
	}
	env, err := firstrun.ParseEnvelope(raw)
	if err != nil {
		return err
	}

	verdict := firstrunbench.Evaluate(env, firstrunbench.Measurements{
		Path:        pathLabel,
		ManualSteps: manualSteps,
	})

	if jsonOut {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), verdict); writeErr != nil {
			return writeErr
		}
	} else {
		firstrunbench.RenderVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("first-answer benchmark FAILED: %s", strings.Join(verdict.FailureReasons(), "; "))
	}
	return nil
}
