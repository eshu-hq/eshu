// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/evidpacket"
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
	raw, err := evidpacket.ReadBenchmark(cmd.InOrStdin(), path)
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
		_, _ = fmt.Fprint(cmd.OutOrStdout(), evidpacket.RenderVerdict(verdict))
	}
	if !verdict.Pass {
		return fmt.Errorf("evidence-packet dogfood FAILED: %s", evidpacket.FailureSummary(verdict))
	}
	return nil
}
