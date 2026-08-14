// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

// newFreshnessServiceChangedSinceCommand builds the
// `eshu freshness service-changed-since` subcommand. It diffs a prior service
// materialization generation against the current active generation of a service
// and renders the bounded per-family delta summary (#1943).
func newFreshnessServiceChangedSinceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service-changed-since",
		Short: "Summarize what changed for a service since a prior service generation",
		Args:  cobra.NoArgs,
		RunE:  runFreshnessServiceChangedSince,
	}
	cmd.Flags().Bool("json", false, "Write the canonical service changed-since envelope as JSON")
	cmd.Flags().String("service-id", "", "Exact service id whose evidence lineage to diff (required)")
	cmd.Flags().String("since-generation-id", "", "Prior service materialization generation id to diff from (required)")
	cmd.Flags().Int("sample-limit", 25, "Maximum sample handles per classification per family (max 200)")
	addRemoteFlags(cmd)
	return cmd
}

func runFreshnessServiceChangedSince(cmd *cobra.Command, _ []string) error {
	opts, err := freshnessServiceChangedSinceOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	return freshnessExitError(freshness.RunServiceChangedSince(cmd.OutOrStdout(), apiClientFromCmd(cmd), opts))
}

func freshnessServiceChangedSinceOptionsFromCommand(cmd *cobra.Command) (freshness.ServiceChangedSinceOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return freshness.ServiceChangedSinceOptions{}, err
	}
	serviceID, err := cmd.Flags().GetString("service-id")
	if err != nil {
		return freshness.ServiceChangedSinceOptions{}, err
	}
	sinceGenerationID, err := cmd.Flags().GetString("since-generation-id")
	if err != nil {
		return freshness.ServiceChangedSinceOptions{}, err
	}
	sampleLimit, err := cmd.Flags().GetInt("sample-limit")
	if err != nil {
		return freshness.ServiceChangedSinceOptions{}, err
	}
	return freshness.ServiceChangedSinceOptions{
		JSON:              jsonOutput,
		ServiceID:         serviceID,
		SinceGenerationID: sinceGenerationID,
		SampleLimit:       sampleLimit,
	}, nil
}
