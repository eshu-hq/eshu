// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/change"
)

// changeImpactFetch and changePlanFetch are indirections the command tests
// replace to run the two RunE paths without an API server.
var (
	changeImpactFetch = fetchChangeImpact
	changePlanFetch   = fetchChangePlan
)

func init() {
	rootCmd.AddCommand(newChangeCommand())
}

func newChangeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Inspect pre-change impact over Eshu evidence",
	}
	cmd.AddCommand(newChangeImpactCommand())
	cmd.AddCommand(newChangePlanCommand())
	return cmd
}

func newChangeImpactCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impact",
		Short: "Map a local diff or changed file list to bounded impact evidence",
		Args:  cobra.NoArgs,
		RunE:  runChangeImpact,
	}
	addChangeImpactFlags(cmd)
	return cmd
}

func newChangePlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Build a read-only developer change plan over bounded impact evidence",
		Args:  cobra.NoArgs,
		RunE:  runChangePlan,
	}
	addChangeImpactFlags(cmd)
	cmd.Flags().String("intent", "", "Optional developer intent used to rank and explain plan actions")
	return cmd
}

func addChangeImpactFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the canonical pre-change impact envelope as JSON")
	cmd.Flags().String("repo-id", "", "Repository selector for changed-path lookup")
	cmd.Flags().String("base", "", "Git base ref for local diff derivation")
	cmd.Flags().String("head", "", "Git head ref for local diff derivation")
	cmd.Flags().StringArray("file", nil, "Repo-relative changed file path; repeat for multiple files")
	cmd.Flags().String("repo-path", ".", "Local repository path used to derive --base/--head diffs")
	cmd.Flags().String("target", "", "Optional canonical entity id or exact entity name")
	cmd.Flags().String("target-type", "", "Optional target kind")
	cmd.Flags().String("service-name", "", "Optional service or workload name")
	cmd.Flags().String("workload-id", "", "Optional canonical workload id")
	cmd.Flags().String("resource-id", "", "Optional canonical cloud resource id")
	cmd.Flags().String("module-id", "", "Optional Terraform module id")
	cmd.Flags().String("topic", "", "Optional code topic to scope impact")
	cmd.Flags().String("env", "", "Optional environment filter")
	cmd.Flags().Int("max-depth", 4, "Maximum graph traversal depth (max 8)")
	cmd.Flags().Int("limit", 25, "Maximum rows per response section (max 100)")
	cmd.Flags().Int("offset", 0, "Result offset for content-backed code investigation")
	addRemoteFlags(cmd)
}

func runChangeImpact(cmd *cobra.Command, _ []string) error {
	opts, err := changeImpactOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	if err := resolveChangeDiff(&opts); err != nil {
		return err
	}
	if err := change.Validate(opts); err != nil {
		return changeExitError(err)
	}

	out := cmd.OutOrStdout()
	envelope, err := changeImpactFetch(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = change.EnvelopeFromTransportError(err)
		return change.FinishImpact(out, opts, envelope, changeExitError(change.EnvelopeFailure(envelope.Error)))
	}
	if envelope.Error != nil {
		return change.FinishImpact(out, opts, envelope, changeExitError(change.EnvelopeFailure(envelope.Error)))
	}
	return change.FinishImpact(out, opts, envelope, changeExitError(change.ClassifyImpact(envelope)))
}

func runChangePlan(cmd *cobra.Command, _ []string) error {
	opts, err := changeImpactOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	if opts.DeveloperIntent, err = trimmedFlag(cmd, "intent"); err != nil {
		return err
	}
	if err := resolveChangeDiff(&opts); err != nil {
		return err
	}
	if err := change.Validate(opts); err != nil {
		return changeExitError(err)
	}

	out := cmd.OutOrStdout()
	envelope, err := changePlanFetch(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = change.EnvelopeFromTransportError(err)
		return change.FinishPlan(out, opts, envelope, changeExitError(change.EnvelopeFailure(envelope.Error)))
	}
	if envelope.Error != nil {
		return change.FinishPlan(out, opts, envelope, changeExitError(change.EnvelopeFailure(envelope.Error)))
	}
	return change.FinishPlan(out, opts, envelope, changeExitError(change.ClassifyPlan(envelope)))
}

// resolveChangeDiff fills in the changed-file set from a local git diff when
// the operator gave refs instead of explicit --file paths. Explicit paths win:
// an operator who listed files is not asking git what changed.
func resolveChangeDiff(opts *change.Options) error {
	if len(opts.Changes) > 0 || (opts.BaseRef == "" && opts.HeadRef == "") {
		return nil
	}
	changes, err := change.GitDiffNameStatus(opts.RepoPath, opts.BaseRef, opts.HeadRef)
	if err != nil {
		return err
	}
	opts.Changes = changes
	opts.ChangedPaths = change.ChangedPaths(changes)
	return nil
}

// changeExitError turns a change.Failure into the CLI's commandExitError. Any
// other error passes through untouched, and a nil error stays nil so callers
// can hand it the result of a classification that found nothing wrong.
func changeExitError(err error) error {
	if err == nil {
		return nil
	}
	var failure change.Failure
	if !errors.As(err, &failure) {
		return err
	}
	return commandExitError{message: failure.Message, code: changeExitCode(failure)}
}

// changeExitCode is the change family's half of the CLI exit-code contract.
//
// Only KindEnvelope routes through traceExitCode, because only there does the
// code come from the API. The other three answer directly, and that is
// load-bearing for two of them: traceExitCode answers 1 for "building" and 1
// for "truncated", while this family has always exited 4 on a still-building
// index and 5 on a truncated answer. Routing them through the shared table
// would quietly change both.
func changeExitCode(failure change.Failure) int {
	switch failure.Kind {
	case change.KindInvalidArgument:
		return 2
	case change.KindFreshness:
		return 4
	case change.KindIncomplete:
		return 5
	case change.KindEnvelope:
		return traceExitCode(failure.Code)
	default:
		return 1
	}
}

func changeImpactOptionsFromCommand(cmd *cobra.Command) (change.Options, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return change.Options{}, err
	}
	files, err := cmd.Flags().GetStringArray("file")
	if err != nil {
		return change.Options{}, err
	}
	opts := change.Options{JSON: jsonOutput, ChangedPaths: change.CleanValues(files), Changes: change.ModifiedFiles(files)}
	if opts.RepoID, err = trimmedFlag(cmd, "repo-id"); err != nil {
		return change.Options{}, err
	}
	if opts.BaseRef, err = trimmedFlag(cmd, "base"); err != nil {
		return change.Options{}, err
	}
	if opts.HeadRef, err = trimmedFlag(cmd, "head"); err != nil {
		return change.Options{}, err
	}
	if opts.RepoPath, err = trimmedFlag(cmd, "repo-path"); err != nil {
		return change.Options{}, err
	}
	if opts.Target, err = trimmedFlag(cmd, "target"); err != nil {
		return change.Options{}, err
	}
	if opts.TargetType, err = trimmedFlag(cmd, "target-type"); err != nil {
		return change.Options{}, err
	}
	if opts.ServiceName, err = trimmedFlag(cmd, "service-name"); err != nil {
		return change.Options{}, err
	}
	if opts.WorkloadID, err = trimmedFlag(cmd, "workload-id"); err != nil {
		return change.Options{}, err
	}
	if opts.ResourceID, err = trimmedFlag(cmd, "resource-id"); err != nil {
		return change.Options{}, err
	}
	if opts.ModuleID, err = trimmedFlag(cmd, "module-id"); err != nil {
		return change.Options{}, err
	}
	if opts.Topic, err = trimmedFlag(cmd, "topic"); err != nil {
		return change.Options{}, err
	}
	if opts.Environment, err = trimmedFlag(cmd, "env"); err != nil {
		return change.Options{}, err
	}
	if opts.MaxDepth, err = cmd.Flags().GetInt("max-depth"); err != nil {
		return change.Options{}, err
	}
	if opts.Limit, err = cmd.Flags().GetInt("limit"); err != nil {
		return change.Options{}, err
	}
	if opts.Offset, err = cmd.Flags().GetInt("offset"); err != nil {
		return change.Options{}, err
	}
	return opts, nil
}

func trimmedFlag(cmd *cobra.Command, name string) (string, error) {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func fetchChangeImpact(client *APIClient, opts change.Options) (change.Envelope, error) {
	var envelope change.Envelope
	if err := client.PostEnvelope(change.ImpactRoute, change.ImpactRequestBody(opts), &envelope); err != nil {
		return change.Envelope{}, err
	}
	return envelope, nil
}

func fetchChangePlan(client *APIClient, opts change.Options) (change.Envelope, error) {
	var envelope change.Envelope
	if err := client.PostEnvelope(change.PlanRoute, change.PlanRequestBody(opts), &envelope); err != nil {
		return change.Envelope{}, err
	}
	return envelope, nil
}
