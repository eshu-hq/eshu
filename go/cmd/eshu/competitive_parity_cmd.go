// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/compparity"
	"github.com/eshu-hq/eshu/go/internal/competitiveparity"
)

func init() {
	rootCmd.AddCommand(newCompetitiveParityCommand())
}

// newCompetitiveParityCommand builds the competitive-parity command group.
// The gate logic lives in internal/cli/compparity; this file keeps only what
// must stay in package main: cobra registration, flag and stream resolution,
// the cobra-tree walk for command paths, and the exit-code mapping.
func newCompetitiveParityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "competitive-parity",
		Short:         "Validate shipped competitive-parity surfaces",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newCompetitiveParityValidateCommand())
	return cmd
}

func newCompetitiveParityValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Run the offline competitive parity gate",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runCompetitiveParityValidate,
	}
	cmd.Flags().String("repo-root", ".", "Repository root used to read public contract docs")
	cmd.Flags().Bool("json", false, "Emit the parity artifact as JSON instead of Markdown")
	cmd.Flags().String("out", "", "Optional path to write the parity artifact")
	return cmd
}

func runCompetitiveParityValidate(cmd *cobra.Command, _ []string) error {
	repoRoot, err := cmd.Flags().GetString("repo-root")
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	outPath, err := cmd.Flags().GetString("out")
	if err != nil {
		return err
	}
	inventory, err := compparity.Inventory(repoRoot, commandPaths(rootCmd))
	if err != nil {
		return err
	}
	report := competitiveparity.Validate(inventory, competitiveparity.DefaultExpectations())
	artifact, err := compparity.Artifact(report, jsonOut)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outPath) != "" {
		if err := os.WriteFile(outPath, artifact, 0o600); err != nil {
			return fmt.Errorf("write competitive parity artifact: %w", err)
		}
	} else if _, err := cmd.OutOrStdout().Write(artifact); err != nil {
		return fmt.Errorf("write competitive parity artifact: %w", err)
	}
	if !report.Pass {
		return commandExitError{message: "competitive parity gate failed", code: 1}
	}
	return nil
}

// commandPaths walks the cobra tree and returns every runnable or grouping
// command path as a sorted, space-joined list. It needs rootCmd, so it lives
// here rather than in internal/cli/compparity.
func commandPaths(root *cobra.Command) []string {
	var paths []string
	var walk func(cmd *cobra.Command, prefix []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		children := cmd.Commands()
		sort.SliceStable(children, func(i, j int) bool {
			return commandName(children[i]) < commandName(children[j])
		})
		for _, child := range children {
			if !child.Runnable() && !child.HasSubCommands() {
				continue
			}
			next := append(append([]string{}, prefix...), commandName(child))
			paths = append(paths, strings.Join(next, " "))
			walk(child, next)
		}
	}
	walk(root, nil)
	sort.Strings(paths)
	return paths
}

func commandName(cmd *cobra.Command) string {
	return strings.Fields(cmd.Use)[0]
}
