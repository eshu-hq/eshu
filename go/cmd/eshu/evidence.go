// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/evbundle"
	"github.com/eshu-hq/eshu/go/internal/cli/evidpacket"
	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
	"github.com/spf13/cobra"
)

// evidenceBundleLiveNow is time.Now, overridable in tests for a deterministic
// Identity.CreatedAt. It lives here rather than in internal/cli/evbundle
// because the package takes the fetch time as a parameter and never reads a
// clock of its own.
var evidenceBundleLiveNow = time.Now

func init() {
	rootCmd.AddCommand(newEvidenceCommand())
}

func newEvidenceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "evidence",
		Short:         "Work with portable evidence artifacts",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newEvidenceBundleCommand())
	return cmd
}

func newEvidenceBundleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "bundle",
		Short:         "Export and validate portable evidence bundles",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newEvidenceBundleExportCommand())
	cmd.AddCommand(newEvidenceBundleValidateCommand())
	return cmd
}

func newEvidenceBundleExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "export",
		Short:         "Export a deterministic share-safe evidence bundle",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runEvidenceBundleExport,
	}
	cmd.Flags().String("scope", "repo:demo/service", "Share-safe scope handle for the bundle")
	cmd.Flags().String("out", "", "Path to write the bundle JSON; stdout when omitted")
	cmd.Flags().Bool("live", false, "Compose the bundle from the running stack's status endpoints instead of the fixture demo bundle")
	addRemoteFlags(cmd)
	return cmd
}

func newEvidenceBundleValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate a portable evidence bundle",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runEvidenceBundleValidate,
	}
	cmd.Flags().String("from", "", "Path to a bundle JSON file; stdin when omitted")
	return cmd
}

func runEvidenceBundleExport(cmd *cobra.Command, _ []string) error {
	outPath, err := cmd.Flags().GetString("out")
	if err != nil {
		return err
	}
	live, err := cmd.Flags().GetBool("live")
	if err != nil {
		return err
	}
	if live {
		// All three status routes a live bundle reads are stack-global:
		// /status/index counts every repository, and the pipeline and collector
		// routes carry no scope selector. Labelling that evidence with a
		// narrower scope would attribute other repositories' counts and
		// backlogs to the named one, so a custom --scope is refused rather
		// than silently mislabelling the artifact.
		if cmd.Flags().Changed("scope") {
			return fmt.Errorf("--scope cannot be combined with --live: the status routes a live bundle reads are stack-wide, so a repository-scoped label would misattribute other repositories' evidence")
		}
		// Empty scope makes the live export use its stack-wide identity. The
		// clock is handed over unevaluated so ExportLive reads it after the
		// status fetch returns; calling it here would stamp the fetch-START
		// time, which is the pre-extraction behavior inverted.
		raw, err := evbundle.ExportLive(apiClientFromCmd(cmd), "", evidenceBundleLiveNow)
		if err != nil {
			return err
		}
		return evbundle.WriteBundle(cmd.OutOrStdout(), raw, outPath)
	}

	scope, err := cmd.Flags().GetString("scope")
	if err != nil {
		return err
	}
	raw, err := evbundle.ExportDemo(scope)
	if err != nil {
		return err
	}
	return evbundle.WriteBundle(cmd.OutOrStdout(), raw, outPath)
}

func runEvidenceBundleValidate(cmd *cobra.Command, _ []string) error {
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	raw, err := evbundle.ReadBundleInput(cmd.InOrStdin(), from)
	if err != nil {
		return err
	}
	return evbundle.ValidateBundle(cmd.OutOrStdout(), raw)
}

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
