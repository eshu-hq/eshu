// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/demo"
)

func init() {
	rootCmd.AddCommand(newDemoCommand())
}

// newDemoCommand builds the `eshu demo` tree. A constructor keeps flag state
// per-invocation, matching first-run-benchmark.
func newDemoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run a credential-free demo stack and reach a first correlated answer",
		Long: `demo brings up a self-contained Eshu stack seeded with a synthetic
organization, waits until it can actually answer, asks the first question from
the demo manifest, and prints a guided five-question path.

The corpus is the same one the golden-corpus gate proves, replayed as the acme
org, so every demo answer is backed by a fixture CI already runs. No provider
credentials are involved at any point.

  eshu demo up            bring the stack up and reach a first answer
  eshu demo status        report whether the demo stack is up and indexed
  eshu demo down          remove the stack, its volumes, and its networks

The stack runs under its own Compose project (default "` + demo.DefaultProject + `"),
so it never adopts or tears down a stack you started for real work.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}
	cmd.AddCommand(newDemoUpCommand(), newDemoDownCommand(), newDemoStatusCommand())
	return cmd
}

// demoProjectFlag registers the shared --project override.
func demoProjectFlag(cmd *cobra.Command) {
	cmd.Flags().String("project", demo.DefaultProject,
		"Compose project name for the demo stack (use a distinct name to run more than one)")
	cmd.Flags().Bool("json", false, "Emit the {data, truth, error} envelope as JSON")
}

func newDemoUpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "up",
		Short:         "Bring up the demo stack and reach a first correlated answer",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runDemoUp,
	}
	demoProjectFlag(cmd)
	return cmd
}

func newDemoDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "down",
		Short:         "Remove the demo stack, its volumes, and its networks",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runDemoDown,
	}
	demoProjectFlag(cmd)
	return cmd
}

func newDemoStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "status",
		Short:         "Report whether the demo stack is running and indexed",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runDemoStatus,
	}
	demoProjectFlag(cmd)
	return cmd
}

// resolveDemoOptions resolves the process state the demo runtime needs -- the
// working directory, ESHU_DEMO_COMPOSE_FILE, and the ESHU_DEMO_* port and
// bind-address overrides -- into the plain values internal/cli/demo consumes.
// The overlay is located relative to the working directory, so an installed
// binary works outside the repo root.
//
// It is split out from newResolvedDemoRuntime so a test can read the resolved
// values back. internal/cli/demo takes its environment lookup as a parameter,
// so no test in that package can prove this function passes os.Getenv rather
// than a lookup that always returns "" -- and Runtime keeps the resolved bases
// unexported, so asserting on demo.Options is the only seam that catches it.
func resolveDemoOptions(project string) (demo.Options, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return demo.Options{}, err
	}
	file, err := demo.ResolveComposeFile(cwd, os.Getenv)
	if err != nil {
		return demo.Options{}, err
	}
	return demo.Options{
		Project:     project,
		ComposeFile: file,
		APIBase:     demo.APIBase(os.Getenv),
		MCPBase:     demo.MCPBase(os.Getenv),
	}, nil
}

// newResolvedDemoRuntime builds the runtime from the resolved process state.
func newResolvedDemoRuntime(project string) (*demo.Runtime, error) {
	opts, err := resolveDemoOptions(project)
	if err != nil {
		return nil, err
	}
	return demo.NewRuntime(opts), nil
}

func runDemoUp(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	jsonOut, _ := cmd.Flags().GetBool("json")
	rt, err := newResolvedDemoRuntime(project)
	if err != nil {
		return err
	}

	res, err := rt.Up(cmd.Context())
	if jsonOut {
		if encErr := demo.WriteJSON(cmd.OutOrStdout(), demo.EnvelopeFor(res, err)); encErr != nil {
			return encErr
		}
		return err
	}
	if err != nil {
		return err
	}
	demo.PrintSuccess(cmd.OutOrStdout(), res)
	return nil
}

func runDemoDown(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	jsonOut, _ := cmd.Flags().GetBool("json")
	rt, err := newResolvedDemoRuntime(project)
	if err != nil {
		return err
	}

	err = rt.Down(cmd.Context())
	if jsonOut {
		if encErr := demo.WriteJSON(cmd.OutOrStdout(), demo.EnvelopeFor(demo.Result{Project: project}, err)); encErr != nil {
			return encErr
		}
		return err
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Demo stack %q removed, including its volumes and networks.\n", project)
	return nil
}

func runDemoStatus(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	jsonOut, _ := cmd.Flags().GetBool("json")
	rt, err := newResolvedDemoRuntime(project)
	if err != nil {
		return err
	}

	res, err := rt.Status(cmd.Context())
	if jsonOut {
		if encErr := demo.WriteJSON(cmd.OutOrStdout(), demo.EnvelopeFor(res, err)); encErr != nil {
			return encErr
		}
		return err
	}
	if err != nil {
		return err
	}
	state := "not running"
	if res.Ready {
		state = "running and indexed"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Demo stack %q: %s\n", project, state)
	return nil
}
