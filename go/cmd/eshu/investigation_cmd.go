// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/investigation"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// investigationExportDepsValue is the read seam the CLI tests swap so they can
// drive every export path without a live API. Production runs keep the default.
var investigationExportDepsValue = investigation.DefaultDeps()

func init() {
	rootCmd.AddCommand(newInvestigationCommand())
}

// newInvestigationCommand groups the portable investigation evidence packet
// subcommands.
func newInvestigationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigation",
		Short: "Emit portable, source-backed investigation evidence packets",
	}
	cmd.AddCommand(newInvestigationExportCommand())
	return cmd
}

// newInvestigationExportCommand builds the `eshu investigation export` command,
// which emits an investigation_evidence_packet.v2 artifact for a supported
// investigation family.
func newInvestigationExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an investigation evidence packet (json, md, or html)",
		Long: `export emits a portable investigation_evidence_packet.v2 artifact for one
investigation. The packet separates raw source facts, reducer decisions,
graph/query truth, missing-evidence reasons, freshness, and optional semantic
observations. It is deterministic with no provider keys and bounded with explicit
truncation. An unknown family or unanswerable scope yields a valid refusal packet
rather than a fabricated answer.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runInvestigationExport,
	}
	cmd.Flags().String("family", "", "Investigation family: supply_chain_impact, deployable_unit, or drift")
	cmd.Flags().StringArray("subject", nil, "Scope key=value (repeatable), e.g. --subject advisory_id=GHSA-...")
	cmd.Flags().String("format", "json", "Artifact format: json, md, or html")
	cmd.Flags().String("out", "", "Write the artifact to this path instead of stdout")
	cmd.Flags().Int("max-source-facts", 0, "Override the source-facts cap (0 = contract default)")
	addRemoteFlags(cmd)
	return cmd
}

// runInvestigationExport reads the flags, resolves the API client and the output
// streams, and hands the rest to internal/cli/investigation. The order of the
// two parse steps is load-bearing: a run with both a bad --format and a bad
// --subject reports the format first, as it always has.
func runInvestigationExport(cmd *cobra.Command, _ []string) error {
	rawFamily, _ := cmd.Flags().GetString("family")
	rawSubjects, _ := cmd.Flags().GetStringArray("subject")
	rawFormat, _ := cmd.Flags().GetString("format")
	out, _ := cmd.Flags().GetString("out")
	maxSourceFacts, _ := cmd.Flags().GetInt("max-source-facts")

	format, err := query.ParseInvestigationPacketFormat(rawFormat)
	if err != nil {
		return err
	}
	subject, err := investigation.ParseSubjectFlags(rawSubjects)
	if err != nil {
		return err
	}

	packet, err := investigation.BuildPacket(apiClientFromCmd(cmd), investigationExportDepsValue, investigation.Request{
		Family:  investigation.ParseFamily(rawFamily),
		Subject: subject,
		Bounds:  investigation.BoundsFromMaxSourceFacts(maxSourceFacts),
	})
	if err != nil {
		return err
	}
	data, err := query.RenderInvestigationPacket(packet, format)
	if err != nil {
		return err
	}
	return investigation.WriteArtifact(cmd.OutOrStdout(), cmd.ErrOrStderr(), out, data)
}
