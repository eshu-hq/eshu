// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/entitymap"
)

// entityMapFetch is the seam the map command's tests replace so they can drive
// the classification and rendering path without an API server.
var entityMapFetch = entitymap.Fetch

func init() {
	cmd := &cobra.Command{
		Use:   "map --from <thing>",
		Short: "Map a bounded code-to-cloud entity neighborhood",
		Args:  cobra.NoArgs,
		RunE:  runMapFrom,
	}
	addEntityMapFlags(cmd)
	addRemoteFlags(cmd)
	rootCmd.AddCommand(cmd)
}

func addEntityMapFlags(cmd *cobra.Command) {
	cmd.Flags().String("from", "", "Entity handle to map, such as terraform/aws_lb.main or workload:checkout")
	cmd.Flags().String("type", "", "Entity type hint such as service, repository, terraform_resource, k8s_resource, or file")
	cmd.Flags().String("repo", "", "Repository selector used to narrow resolution")
	cmd.Flags().String("env", "", "Environment selector used to narrow runtime/resource relationships")
	cmd.Flags().String("relationship", "", "Relationship type filter, such as DEPENDS_ON or PROVISIONS_DEPENDENCY_FOR")
	cmd.Flags().Int("depth", 1, "Maximum relationship depth to traverse")
	cmd.Flags().Int("limit", 25, "Maximum mapped relationships to return")
	cmd.Flags().Bool("json", false, "Write the canonical entity map envelope as JSON")
}

func runMapFrom(cmd *cobra.Command, _ []string) error {
	opts, err := entityMapOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	if opts.From == "" {
		return commandExitError{message: "--from is required", code: 2}
	}

	envelope, failure := entitymap.Resolve(entityMapFetch(apiClientFromCmd(cmd), opts))
	if writeErr := entitymap.Write(cmd.OutOrStdout(), opts.JSON, envelope, failure); writeErr != nil {
		return writeErr
	}
	if failure == nil {
		return nil
	}
	return commandExitError{message: failure.Message, code: entityMapExitCode(failure)}
}

// entityMapExitCode maps a classified entity-map failure to the CLI's exit
// code. It lives here, not in internal/cli/entitymap, so that every exit code
// the binary can return stays in one package: an envelope error reuses the
// shared traceExitCode table, and the three map-specific outcomes carry the
// codes the command has always returned.
func entityMapExitCode(failure *entitymap.Failure) int {
	switch failure.Kind {
	case entitymap.FailureEnvelope:
		return traceExitCode(failure.Code)
	case entitymap.FailureFreshness:
		return 4
	case entitymap.FailureAmbiguous:
		return 3
	case entitymap.FailureNoMatch:
		return 2
	default:
		return 1
	}
}

func entityMapOptionsFromCommand(cmd *cobra.Command) (entitymap.Options, error) {
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return entitymap.Options{}, err
	}
	fromType, err := cmd.Flags().GetString("type")
	if err != nil {
		return entitymap.Options{}, err
	}
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return entitymap.Options{}, err
	}
	environment, err := cmd.Flags().GetString("env")
	if err != nil {
		return entitymap.Options{}, err
	}
	relationship, err := cmd.Flags().GetString("relationship")
	if err != nil {
		return entitymap.Options{}, err
	}
	depth, err := cmd.Flags().GetInt("depth")
	if err != nil {
		return entitymap.Options{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return entitymap.Options{}, err
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return entitymap.Options{}, err
	}
	return entitymap.Options{
		From:         strings.TrimSpace(from),
		FromType:     strings.TrimSpace(fromType),
		Repo:         strings.TrimSpace(repo),
		Environment:  strings.TrimSpace(environment),
		Relationship: strings.ToUpper(strings.TrimSpace(relationship)),
		Depth:        depth,
		Limit:        limit,
		JSON:         jsonOutput,
	}, nil
}
