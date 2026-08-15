// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

// newFreshnessChangedSinceCommand builds the `eshu freshness changed-since`
// subcommand. It diffs a prior generation against the current active generation
// of a repository scope and renders the bounded per-category delta summary.
func newFreshnessChangedSinceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changed-since",
		Short: "Summarize what changed since a prior generation or instant",
		Args:  cobra.NoArgs,
		RunE:  runFreshnessChangedSince,
	}
	cmd.Flags().Bool("json", false, "Write the canonical changed-since envelope as JSON")
	cmd.Flags().String("scope-id", "", "Exact ingestion scope id (required unless --repository is set)")
	cmd.Flags().String("repository", "", "Canonical repository id (required unless --scope-id is set)")
	cmd.Flags().String("since-generation-id", "", "Prior generation id to diff from")
	cmd.Flags().String("since-observed-at", "", "RFC3339 instant; diff from the generation observed at or before this time")
	cmd.Flags().Int("sample-limit", 25, "Maximum sample handles per classification per category (max 200)")
	addRemoteFlags(cmd)
	return cmd
}

func runFreshnessChangedSince(cmd *cobra.Command, _ []string) error {
	opts, err := freshnessChangedSinceOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	return freshnessExitError(freshness.RunChangedSince(cmd.OutOrStdout(), apiClientFromCmd(cmd), opts))
}

func freshnessChangedSinceOptionsFromCommand(cmd *cobra.Command) (freshness.ChangedSinceOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return freshness.ChangedSinceOptions{}, err
	}
	scopeID, err := cmd.Flags().GetString("scope-id")
	if err != nil {
		return freshness.ChangedSinceOptions{}, err
	}
	repository, err := cmd.Flags().GetString("repository")
	if err != nil {
		return freshness.ChangedSinceOptions{}, err
	}
	sinceGenerationID, err := cmd.Flags().GetString("since-generation-id")
	if err != nil {
		return freshness.ChangedSinceOptions{}, err
	}
	sinceObservedAt, err := cmd.Flags().GetString("since-observed-at")
	if err != nil {
		return freshness.ChangedSinceOptions{}, err
	}
	sampleLimit, err := cmd.Flags().GetInt("sample-limit")
	if err != nil {
		return freshness.ChangedSinceOptions{}, err
	}
	return freshness.ChangedSinceOptions{
		JSON:              jsonOutput,
		ScopeID:           scopeID,
		Repository:        repository,
		SinceGenerationID: sinceGenerationID,
		SinceObservedAt:   sinceObservedAt,
		SampleLimit:       sampleLimit,
	}, nil
}
