// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/demo"
	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
	"github.com/spf13/cobra"
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

func init() {
	rootCmd.AddCommand(newDemoBenchmarkCommand())
}

// newDemoBenchmarkCommand builds a fresh demo-benchmark command. A constructor
// rather than a package-level singleton keeps each invocation, including tests,
// free of leaked flag state.
func newDemoBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo-benchmark",
		Short: "Score an eshu demo --json envelope for time-to-first-answer",
		Long: `demo-benchmark scores the canonical envelope emitted by
"eshu demo up --json" for time-to-first-answer (TTFA).

TTFA is measured from command invocation to the first successful
graph-authoritative answer. COLD (images missing, so built or pulled) and WARM
(images already present) are scored separately and never averaged: a blended number
understates what someone installing for the first time actually waits through.

--images records what the harness observed about the image cache BEFORE the
run. It must be probed before "demo up", because afterwards the images are
always present, and it is cross-checked against --mode so a mislabelled run
fails instead of publishing the wrong number.

Typical use:

  eshu demo up --json > /tmp/demo.json
  eshu demo-benchmark --envelope /tmp/demo.json --mode warm --images present`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runDemoBenchmark,
	}
	cmd.Flags().String("envelope", "", "Path to an eshu demo --json envelope (default: read stdin)")
	cmd.Flags().String("mode", demo.ModeWarm, "Run mode: cold (images built or pulled) or warm (images present)")
	cmd.Flags().Duration("target", 0, "TTFA budget for this mode (0 = record the measurement without judging it)")
	cmd.Flags().String("images", "", "Image cache observed BEFORE the run: present, absent, or empty for not probed")
	cmd.Flags().Bool("json", false, "Emit the scorecard as JSON")
	return cmd
}

// runDemoBenchmark reads the envelope, scores it, prints the scorecard, and
// returns a non-zero exit (via error) when the verdict is FAIL, so a run that
// missed its target or lost its phase breakdown cannot be recorded as a pass.
func runDemoBenchmark(cmd *cobra.Command, _ []string) error {
	envelopePath, _ := cmd.Flags().GetString("envelope")
	mode, _ := cmd.Flags().GetString("mode")
	target, _ := cmd.Flags().GetDuration("target")
	images, _ := cmd.Flags().GetString("images")
	jsonOut, _ := cmd.Flags().GetBool("json")

	observed, err := demo.ParseImageState(images)
	if err != nil {
		return err
	}

	raw, err := firstrunbench.ReadEnvelope(cmd.InOrStdin(), envelopePath)
	if err != nil {
		return err
	}
	var env demo.Envelope
	if decErr := json.Unmarshal(raw, &env); decErr != nil {
		return fmt.Errorf("decode demo envelope: %w", decErr)
	}

	verdict := demo.EvaluateBenchmark(env, demo.BenchmarkMeasurements{
		Mode:           mode,
		Target:         target,
		ImagesObserved: observed,
	})

	if jsonOut {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), verdict); writeErr != nil {
			return writeErr
		}
	} else {
		demo.RenderBenchmarkVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("demo TTFA benchmark FAILED: %s", strings.Join(verdict.FailureReasons(), "; "))
	}
	return nil
}
