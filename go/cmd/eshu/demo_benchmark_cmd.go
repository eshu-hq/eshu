// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/demo"
	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
)

func init() {
	rootCmd.AddCommand(newDemoBenchmarkCommand())
}

// newDemoBenchmarkCommand builds a fresh demo-benchmark command. A constructor
// rather than a package-level singleton keeps each invocation, including tests,
// free of leaked flag state.
func newDemoBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo-benchmark",
		Short: "Score an eshu demo --json envelope for time-to-first-answer",
		Long: `demo-benchmark scores the canonical envelope emitted by
"eshu demo up --json" for time-to-first-answer (TTFA).

TTFA is measured from command invocation to the first successful
graph-authoritative answer. COLD (images missing, so built or pulled) and WARM
(images already present) are scored separately and never averaged: a blended number
understates what someone installing for the first time actually waits through.

--images records what the harness observed about the image cache BEFORE the
run. It must be probed before "demo up", because afterwards the images are
always present, and it is cross-checked against --mode so a mislabelled run
fails instead of publishing the wrong number.

Typical use:

  eshu demo up --json > /tmp/demo.json
  eshu demo-benchmark --envelope /tmp/demo.json --mode warm --images present`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runDemoBenchmark,
	}
	cmd.Flags().String("envelope", "", "Path to an eshu demo --json envelope (default: read stdin)")
	cmd.Flags().String("mode", demo.ModeWarm, "Run mode: cold (images built or pulled) or warm (images present)")
	cmd.Flags().Duration("target", 0, "TTFA budget for this mode (0 = record the measurement without judging it)")
	cmd.Flags().String("images", "", "Image cache observed BEFORE the run: present, absent, or empty for not probed")
	cmd.Flags().Bool("json", false, "Emit the scorecard as JSON")
	return cmd
}

// runDemoBenchmark reads the envelope, scores it, prints the scorecard, and
// returns a non-zero exit (via error) when the verdict is FAIL, so a run that
// missed its target or lost its phase breakdown cannot be recorded as a pass.
func runDemoBenchmark(cmd *cobra.Command, _ []string) error {
	envelopePath, _ := cmd.Flags().GetString("envelope")
	mode, _ := cmd.Flags().GetString("mode")
	target, _ := cmd.Flags().GetDuration("target")
	images, _ := cmd.Flags().GetString("images")
	jsonOut, _ := cmd.Flags().GetBool("json")

	observed, err := demo.ParseImageState(images)
	if err != nil {
		return err
	}

	raw, err := firstrunbench.ReadEnvelope(cmd.InOrStdin(), envelopePath)
	if err != nil {
		return err
	}
	var env demo.Envelope
	if decErr := json.Unmarshal(raw, &env); decErr != nil {
		return fmt.Errorf("decode demo envelope: %w", decErr)
	}

	verdict := demo.EvaluateBenchmark(env, demo.BenchmarkMeasurements{
		Mode:           mode,
		Target:         target,
		ImagesObserved: observed,
	})

	if jsonOut {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), verdict); writeErr != nil {
			return writeErr
		}
	} else {
		demo.RenderBenchmarkVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("demo TTFA benchmark FAILED: %s", strings.Join(verdict.FailureReasons(), "; "))
	}
	return nil
}
