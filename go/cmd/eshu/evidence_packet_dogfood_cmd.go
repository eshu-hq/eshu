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

	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
)

func init() {
	rootCmd.AddCommand(newEvidencePacketDogfoodCommand())
}

// newEvidencePacketDogfoodCommand builds the evidence-packet dogfood scorer.
func newEvidencePacketDogfoodCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence-packet-dogfood",
		Short: "Score the investigation evidence packet dogfood benchmark",
		Long: `evidence-packet-dogfood scores a captured benchmark artifact from issue
#3143: whether Eshu's portable v2 evidence packets produce a faster and more
trustworthy first useful answer than raw repository search or an existing Eshu
tool drilldown.

It reads JSON from --from or stdin. The benchmark defines tasks for supply-chain
impact, deployable drift, and service context, each measuring the raw-files,
eshu-tools, and evidence-packet approaches on answer time, correctness,
missing-evidence clarity, and token budget. The command exits non-zero when the
packet approach fails any dimension: missing family coverage, a wrong answer, a
slower answer than the best baseline, a larger token budget than the best
baseline, or failing to name missing evidence (including a gap every baseline
missed).`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runEvidencePacketDogfood,
	}
	cmd.Flags().String("from", "", "Path to a dogfood benchmark JSON file (default: read stdin)")
	cmd.Flags().Bool("json", false, "Emit the verdict as JSON")
	return cmd
}

func runEvidencePacketDogfood(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("from")
	jsonOut, _ := cmd.Flags().GetBool("json")
	raw, err := readDogfoodBenchmark(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	benchmark, err := packetdogfood.ParseBenchmark(raw)
	if err != nil {
		return err
	}
	verdict := packetdogfood.Score(benchmark)
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(verdict); err != nil {
			return fmt.Errorf("write dogfood verdict JSON: %w", err)
		}
	} else {
		renderDogfoodVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("evidence-packet dogfood FAILED: %s", dogfoodFailureSummary(verdict))
	}
	return nil
}

func readDogfoodBenchmark(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read dogfood benchmark from stdin: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local benchmark artifact path, not an HTTP request param //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read dogfood benchmark file %q: %w", path, err)
	}
	return raw, nil
}

func renderDogfoodVerdict(w io.Writer, verdict packetdogfood.Verdict) {
	header := "Evidence-packet dogfood PASSED"
	if !verdict.Pass {
		header = "Evidence-packet dogfood FAILED"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintf(w, "  run     : %s (%s)\n", quoteIfEmpty(verdict.RunID), verdict.RunKind)
	_, _ = fmt.Fprintf(w, "  tasks   : %d\n", verdict.TaskCount)
	_, _ = fmt.Fprintf(w, "  families: %s\n", strings.Join(verdict.Families, ", "))
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 44))
	for _, criterion := range verdict.Criteria {
		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", dogfoodMarker(criterion.Status), criterion.Name, criterion.Detail)
	}
}

func dogfoodMarker(status packetdogfood.CriterionStatus) string {
	switch status {
	case packetdogfood.CriterionPass:
		return "[ok]"
	case packetdogfood.CriterionFail:
		return "[!!]"
	default:
		return "[--]"
	}
}

func dogfoodFailureSummary(verdict packetdogfood.Verdict) string {
	var failures []string
	for _, criterion := range verdict.Criteria {
		if criterion.Status == packetdogfood.CriterionFail {
			failures = append(failures, fmt.Sprintf("%s: %s", criterion.Name, criterion.Detail))
		}
	}
	if len(failures) == 0 {
		return "unknown failure"
	}
	return strings.Join(failures, "; ")
}
