// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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

The stack runs under its own Compose project (default "` + defaultDemoProject + `"),
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
	cmd.Flags().String("project", defaultDemoProject,
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

// demoEnvelope is the canonical `{data, truth, error}` envelope, matching the
// first-run contract so the TTFA harness reads one shape across both commands.
type demoEnvelope struct {
	Data  demoResult             `json:"data"`
	Truth map[string]any         `json:"truth"`
	Error *firstRunEnvelopeError `json:"error"`
}

// demoEnvelopeFor renders a result (or a failure) into the shared envelope.
// The answer's truth labels are lifted to the envelope's Truth field so a
// consumer never has to reach into Data to judge provenance.
func demoEnvelopeFor(res demoResult, err error) demoEnvelope {
	env := demoEnvelope{Data: res, Truth: res.FirstAnswer.Truth}
	if env.Truth == nil {
		env.Truth = map[string]any{}
	}
	if err != nil {
		env.Error = &firstRunEnvelopeError{Message: err.Error()}
	}
	return env
}

func runDemoUp(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	jsonOut, _ := cmd.Flags().GetBool("json")
	rt, err := newResolvedDemoRuntime(project)
	if err != nil {
		return err
	}

	res, err := rt.up(cmd.Context())
	if jsonOut {
		if encErr := writeDemoJSON(cmd.OutOrStdout(), demoEnvelopeFor(res, err)); encErr != nil {
			return encErr
		}
		return err
	}
	if err != nil {
		return err
	}
	printDemoSuccess(cmd.OutOrStdout(), res)
	return nil
}

func runDemoDown(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	jsonOut, _ := cmd.Flags().GetBool("json")
	rt, err := newResolvedDemoRuntime(project)
	if err != nil {
		return err
	}

	err = rt.down(cmd.Context())
	if jsonOut {
		if encErr := writeDemoJSON(cmd.OutOrStdout(), demoEnvelopeFor(demoResult{Project: project}, err)); encErr != nil {
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

	res, err := rt.status(cmd.Context())
	if jsonOut {
		if encErr := writeDemoJSON(cmd.OutOrStdout(), demoEnvelopeFor(res, err)); encErr != nil {
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

// writeDemoJSON emits the envelope with a trailing newline so shell pipelines
// and `jq` behave.
func writeDemoJSON(w io.Writer, env demoEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// printDemoSuccess renders the human path: what was proven, then what to ask
// next. The guided questions are the point of the command.
func printDemoSuccess(w io.Writer, res demoResult) {
	_, _ = fmt.Fprintf(w, "Demo stack %q is up and indexed in %s.\n\n", res.Project,
		(time.Duration(res.TotalMillis) * time.Millisecond).Round(time.Second))
	_, _ = fmt.Fprintf(w, "First answer\n  Q: %s\n  A: %s\n", res.FirstAnswer.Question, res.FirstAnswer.Answer)
	if len(res.FirstAnswer.Truth) > 0 {
		_, _ = fmt.Fprintf(w, "  Truth: %s\n", formatDemoTruth(res.FirstAnswer.Truth))
	}
	printDemoGuidedPath(w)
	_, _ = fmt.Fprintf(w, "\nWhen you are done: eshu demo down --project %s\n", res.Project)
}

// printDemoGuidedPath prints the remaining manifest questions with the command
// that actually answers each one.
//
// Generated from the manifest, never hand-written: the questions and their
// callable surfaces are declared there, and a list maintained beside it drifts
// into naming services the corpus does not contain.
func printDemoGuidedPath(w io.Writer) {
	m, err := loadDemoManifest(demoManifestPath)
	if err != nil || len(m.Questions) < 2 {
		// Saying nothing reads as "there is nothing more to ask", so the
		// operator never learns the section was dropped. Name the manifest:
		// it is the one fact that makes this actionable.
		_, _ = fmt.Fprintf(w, "\nGuided questions unavailable: could not read %s\n", demoManifestPath)
		return
	}
	_, _ = fmt.Fprintf(w, "\nAsk these next:\n")
	n := 2
	for _, q := range m.Questions[1:] {
		runnable := q.RunnableForm()
		if runnable == "" {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %d. %s\n     %s\n", n, collapseDemoWhitespace(q.Question), runnable)
		n++
	}
}

// collapseDemoWhitespace flattens a manifest question's folded YAML text onto
// one line.
func collapseDemoWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// formatDemoTruth renders truth labels deterministically so two runs of the
// same stack print the same line.
func formatDemoTruth(truth map[string]any) string {
	keys := make([]string, 0, len(truth))
	for k := range truth {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, truth[k]))
	}
	return strings.Join(parts, " ")
}

// askDemoQuestion executes the manifest's first question against the running
// demo stack.
//
// It resolves the surface the manifest declares rather than calling a general
// query route, because no such route exists. An earlier draft invented
// GET /api/v0/query; the manifest is the acceptance oracle precisely so
// sibling issues do not do that.
func askDemoQuestion(ctx context.Context, apiBase, apiKey string) (demoAnswer, error) {
	m, err := loadDemoManifest(demoManifestPath)
	if err != nil {
		return demoAnswer{}, err
	}
	return executeDemoQuestion(ctx, apiBase, demoMCPBase(), apiKey, m.Questions[0])
}
