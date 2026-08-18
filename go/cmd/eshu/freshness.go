// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

func init() {
	freshnessCmd := &cobra.Command{
		Use:   "freshness",
		Short: "Inspect ingestion freshness and generation lifecycle",
	}
	generationsCmd := &cobra.Command{
		Use:   "generations",
		Short: "Drill into scope generation lifecycle history",
		Args:  cobra.NoArgs,
		RunE:  runFreshnessGenerations,
	}
	addFreshnessGenerationsFlags(generationsCmd)
	addRemoteFlags(generationsCmd)
	freshnessCmd.AddCommand(generationsCmd)
	freshnessCmd.AddCommand(newFreshnessChangedSinceCommand())
	freshnessCmd.AddCommand(newFreshnessServiceChangedSinceCommand())
	rootCmd.AddCommand(freshnessCmd)
}

func addFreshnessGenerationsFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the canonical generation lifecycle envelope as JSON")
	cmd.Flags().String("scope-id", "", "Exact ingestion scope id to drill into")
	cmd.Flags().String("repository", "", "Canonical repository id (repository-kind scopes)")
	cmd.Flags().String("collector-kind", "", "Collector kind filter, for example git or terraform_state")
	cmd.Flags().String("source-system", "", "Source system filter, for example github")
	cmd.Flags().String("generation-id", "", "Exact generation id to drill into a single row")
	cmd.Flags().String("status", "", "Generation status filter (pending|active|superseded|completed|failed)")
	cmd.Flags().Int("limit", 50, "Maximum generation lifecycle rows to return (max 500)")
}

func runFreshnessGenerations(cmd *cobra.Command, _ []string) error {
	opts, err := freshnessGenerationsOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	return freshnessExitError(freshness.RunGenerations(cmd.OutOrStdout(), apiClientFromCmd(cmd), opts))
}

func freshnessGenerationsOptionsFromCommand(cmd *cobra.Command) (freshness.GenerationsOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	scopeID, err := cmd.Flags().GetString("scope-id")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	repository, err := cmd.Flags().GetString("repository")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	collectorKind, err := cmd.Flags().GetString("collector-kind")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	sourceSystem, err := cmd.Flags().GetString("source-system")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	generationID, err := cmd.Flags().GetString("generation-id")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	statusFilter, err := cmd.Flags().GetString("status")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return freshness.GenerationsOptions{}, err
	}
	return freshness.GenerationsOptions{
		JSON:          jsonOutput,
		ScopeID:       scopeID,
		Repository:    repository,
		CollectorKind: collectorKind,
		SourceSystem:  sourceSystem,
		GenerationID:  generationID,
		Status:        statusFilter,
		Limit:         limit,
	}, nil
}

// freshnessExitError maps a freshness.Failure onto the CLI's exit-code
// contract. The internal/cli/freshness package classifies the failure and
// chooses the number; commandExitError is defined here, in package main, so
// the conversion has to happen here too. Any other error passes through
// untouched and exits 1.
func freshnessExitError(err error) error {
	var failure *freshness.Failure
	if errors.As(err, &failure) {
		return commandExitError{message: failure.Message, code: failure.Code}
	}
	return err
}

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
