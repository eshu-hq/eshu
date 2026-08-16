// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/cli/playbooks"
	"github.com/spf13/cobra"
)

// queryPlaybookInputs holds the repeatable --input flag values for
// "playbooks resolve". Parsing them into a map is internal/cli/playbooks's
// job; only the cobra flag binding lives here.
var queryPlaybookInputs []string

func init() {
	playbooksCmd := &cobra.Command{
		Use:   "playbooks",
		Short: "List and resolve deterministic query playbooks",
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List query playbooks",
		Args:  cobra.NoArgs,
		RunE:  runQueryPlaybookList,
	}
	resolveCmd := &cobra.Command{
		Use:   "resolve <playbook-id>",
		Short: "Resolve a query playbook into bounded calls",
		Args:  cobra.ExactArgs(1),
		RunE:  runQueryPlaybookResolve,
	}
	resolveCmd.Flags().StringArrayVar(&queryPlaybookInputs, "input", nil, "Playbook input as key=value; repeat for multiple inputs")
	addRemoteFlags(listCmd)
	addRemoteFlags(resolveCmd)
	playbooksCmd.AddCommand(listCmd, resolveCmd)
	rootCmd.AddCommand(playbooksCmd)
}

// runQueryPlaybookList resolves the command's output stream and API client —
// the two things only package main can reach — and hands the rest to
// internal/cli/playbooks.
func runQueryPlaybookList(cmd *cobra.Command, _ []string) error {
	return playbooks.RunList(cmd.OutOrStdout(), apiClientFromCmd(cmd))
}

// runQueryPlaybookResolve parses the --input flag values, then resolves the
// stream and client and delegates to internal/cli/playbooks. Parsing happens
// before the client is built, exactly as the command always behaved: a bad
// --input fails without touching the network.
func runQueryPlaybookResolve(cmd *cobra.Command, args []string) error {
	inputs, err := playbooks.ParseInputs(queryPlaybookInputs)
	if err != nil {
		return err
	}
	return playbooks.RunResolve(cmd.OutOrStdout(), apiClientFromCmd(cmd), playbooks.ResolveOptions{
		PlaybookID: args[0],
		Inputs:     inputs,
	})
}
