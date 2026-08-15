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
