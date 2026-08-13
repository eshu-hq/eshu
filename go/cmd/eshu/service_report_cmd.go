// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/servicereport"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

func init() {
	rootCmd.AddCommand(newServiceReportCommand())
}

// newServiceReportCommand builds the offline service intelligence report
// renderer. It composes an operator-ready report from a captured
// get_service_story response without running an LLM interpretation path.
func newServiceReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service-report",
		Short: "Compose a service intelligence report from a captured service-story response",
		Long: `service-report composes an operator-ready service intelligence report from a
captured get_service_story response.

It reads JSON from --from or stdin. The input is the response from the service
story route (the standard {"data": ..., "truth": ...} envelope, or a bare
dossier object). The command maps the dossier into the identity,
code_to_runtime, and deployment_config sections, then composes the report. It
runs no query, store, or LLM path: each section's truth is the captured truth,
and sections with no supporting evidence stay visible as partial or unsupported
with explicit limitations and bounded next calls.

Supply-chain and incident sections are emitted unsupported unless their evidence
is wired in; this slice sources only the service story.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runServiceReport,
	}
	cmd.Flags().String("from", "", "Path to a captured service-story response JSON file (default: read stdin)")
	cmd.Flags().String("supply-chain-from", "", "Optional path to a captured supply-chain impact inventory response JSON file")
	cmd.Flags().Bool("json", false, "Emit the composed report as JSON")
	return cmd
}

func runServiceReport(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("from")
	supplyChainPath, _ := cmd.Flags().GetString("supply-chain-from")
	jsonOut, _ := cmd.Flags().GetBool("json")

	raw, err := servicereport.ReadInput(cmd.InOrStdin(), path)
	if err != nil {
		return err
	}
	dossier, truth, err := servicereport.ParseServiceStoryResponse(raw)
	if err != nil {
		return err
	}

	input := serviceintel.FromServiceStory(dossier, truth)
	if section, err := servicereport.SupplyChainSection(supplyChainPath, input.Subject); err != nil {
		return err
	} else if section != nil {
		input.Sections = append(input.Sections, *section)
	}

	report := serviceintel.Compose(input)

	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("write service report JSON: %w", err)
		}
		return nil
	}
	servicereport.RenderReport(cmd.OutOrStdout(), report)
	return nil
}
