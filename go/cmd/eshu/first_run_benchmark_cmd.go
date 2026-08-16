// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
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
	env, err := firstrunbench.ParseEnvelope(raw)
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

// firstRunEnvelope is the canonical `{data, truth, error}` envelope emitted by
// `eshu first-run --json`, typed against firstrun.Result so the
// first-run-evidence family can lift envelope.Data into a full result
// (including truth restoration and the diagnosis block). The benchmark itself
// scores the mirror in internal/cli/firstrunbench; the wire-parity test in
// first_run_benchmark_cmd_test.go pins the two shapes together.
type firstRunEnvelope struct {
	// Data is the machine-readable first-run result.
	Data firstrun.Result `json:"data"`
	// Truth carries the freshness/completeness/backend labels for the answer.
	Truth map[string]any `json:"truth"`
	// Error is non-nil when the run failed.
	Error *firstRunEnvelopeError `json:"error"`
}

// firstRunEnvelopeError is the error object inside the JSON envelope. The
// alias keeps the demo family's envelope and this one decoding the same shape.
type firstRunEnvelopeError = firstrunbench.EnvelopeError

// parseFirstRunEnvelope decodes the canonical `eshu first-run --json` output
// into the package-main envelope. It returns a descriptive error when the
// payload is not the expected envelope so callers fail loudly rather than
// silently consuming malformed input.
func parseFirstRunEnvelope(raw []byte) (firstRunEnvelope, error) {
	var env firstRunEnvelope
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&env); err != nil {
		return firstRunEnvelope{}, fmt.Errorf("decode first-run envelope: %w", err)
	}
	return env, nil
}

// The demo-benchmark family scores its own envelope with the shared criterion
// vocabulary that moved to internal/cli/firstrunbench. These aliases are the
// demo family's seam onto that package so its files stay unchanged until its
// own extraction imports firstrunbench directly.
type (
	benchmarkCriterionName = firstrunbench.CriterionName
	benchmarkCriterion     = firstrunbench.Criterion
)

const (
	benchmarkCriterionPass        = firstrunbench.CriterionPass
	benchmarkCriterionFail        = firstrunbench.CriterionFail
	benchmarkCriterionNotMeasured = firstrunbench.CriterionNotMeasured
	criterionFirstAnswer          = firstrunbench.CriterionFirstAnswer
	criterionTruthMetadata        = firstrunbench.CriterionTruthMetadata
	criterionRepoIndexed          = firstrunbench.CriterionRepoIndexed
	criterionTimeToAnswer         = firstrunbench.CriterionTimeToAnswer
)

// readBenchmarkEnvelope reads the envelope bytes from a file path or, when the
// path is empty, from the provided reader (stdin). Kept for the demo-benchmark
// family; the first-run benchmark calls firstrunbench.ReadEnvelope directly.
func readBenchmarkEnvelope(stdin io.Reader, path string) ([]byte, error) {
	return firstrunbench.ReadEnvelope(stdin, path)
}

// benchmarkMarker maps a criterion status to a stable ASCII marker. Kept for
// the demo-benchmark scorecard renderer.
func benchmarkMarker(status firstrunbench.CriterionStatus) string {
	return firstrunbench.Marker(status)
}
