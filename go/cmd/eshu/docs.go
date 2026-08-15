// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/cli/docs"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// docsVerifyFlags is the resolved flag state for one `docs verify` run. It
// holds the CLI-only knobs alongside the options the docs package consumes:
// failOn selects which finding statuses become a non-zero exit, and jsonOutput
// picks the output format. Neither is the docs package's business.
type docsVerifyFlags struct {
	verify     docs.VerifyOptions
	failOn     []string
	jsonOutput bool
}

func init() {
	rootCmd.AddCommand(newDocsCommand())
}

func newDocsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Verify and inspect documentation truth",
	}
	cmd.AddCommand(newDocsVerifyCommand())
	return cmd
}

func newDocsVerifyCommand() *cobra.Command {
	return newDocsVerifyCommandWithDeps(defaultDocsVerifyDeps())
}

// newDocsVerifyCommandWithDeps builds `docs verify` against injectable
// dependencies so tests can supply a fake persistence store and a fixed clock.
func newDocsVerifyCommandWithDeps(deps docs.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify [path]",
		Short: "Verify documentation claims against Eshu truth sources",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDocsVerifyWithDeps(cmd, args, deps)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().Int("limit", 50, "Maximum documentation files to scan")
	cmd.Flags().Int("max-bytes", 256*1024, "Maximum bytes to read from each documentation file")
	cmd.Flags().String("fail-on", "", "Comma-separated finding statuses that should fail the command")
	cmd.Flags().String("scope", "", "Documentation verification scope identifier")
	cmd.Flags().String("repo", "", "Repository selector recorded on persisted documentation scope")
	cmd.Flags().String("image-truth", "auto", "Container image truth source: auto, local, or api")
	cmd.Flags().Bool("persist", false, "Persist generated documentation findings and evidence packets to Postgres")
	cmd.Flags().Bool("json", false, "Write documentation verification as JSON")
	addRemoteFlags(cmd)
	return cmd
}

// runDocsVerifyWithDeps resolves process state -- flags, the effective image
// truth source, and the cobra output stream -- then hands the verification
// itself to the docs package and maps its result onto the CLI's exit contract.
func runDocsVerifyWithDeps(cmd *cobra.Command, args []string, deps docs.Deps) error {
	flags, err := docsVerifyFlagsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	flags.verify.ImageTruth = effectiveDocsVerifyImageTruth(cmd, flags.verify.ImageTruth)
	deps.ContainerImageResolver = docsVerifyContainerImageResolver(cmd, flags.verify)
	result, err := docs.Verify(cmd.Context(), flags.verify, deps)
	if err != nil {
		return err
	}
	exitErr := docsVerifyFailure(flags.failOn, result.Verification)
	envelope := docs.NewEnvelope(result.Verification, exitErr)
	envelope.Data.Persistence = result.Persistence
	return writeDocsVerifyOutput(cmd, flags, result.Verification, envelope, exitErr)
}

// writeDocsVerifyOutput renders the run to the command's output stream and then
// returns the exit error, so a failing run still prints its report.
func writeDocsVerifyOutput(
	cmd *cobra.Command,
	flags docsVerifyFlags,
	result doctruth.VerificationResult,
	envelope docs.Envelope,
	exitErr error,
) error {
	if flags.jsonOutput {
		if err := docs.WriteJSON(cmd.OutOrStdout(), envelope); err != nil {
			return err
		}
		return exitErr
	}
	if err := docs.RenderText(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	return exitErr
}

// docsVerifyFlagsFromCommand reads the flag set into resolved options,
// rejecting invalid values with the CLI's usage exit code 2.
func docsVerifyFlagsFromCommand(cmd *cobra.Command, args []string) (docsVerifyFlags, error) {
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	maxBytes, err := cmd.Flags().GetInt("max-bytes")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	failOn, err := cmd.Flags().GetString("fail-on")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	persist, err := cmd.Flags().GetBool("persist")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	scopeID, err := cmd.Flags().GetString("scope")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	imageTruth, err := cmd.Flags().GetString("image-truth")
	if err != nil {
		return docsVerifyFlags{}, err
	}
	imageTruth = docs.NormalizeImageTruthMode(imageTruth)
	switch imageTruth {
	case "auto", "local", "api":
	default:
		return docsVerifyFlags{}, commandExitError{message: "--image-truth must be auto, local, or api", code: 2}
	}
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if limit <= 0 {
		return docsVerifyFlags{}, commandExitError{message: "--limit must be greater than 0", code: 2}
	}
	if maxBytes <= 0 {
		return docsVerifyFlags{}, commandExitError{message: "--max-bytes must be greater than 0", code: 2}
	}
	return docsVerifyFlags{
		verify: docs.VerifyOptions{
			Path:             path,
			Limit:            limit,
			MaxDocumentBytes: maxBytes,
			Persist:          persist,
			Scope:            scopeID,
			Repo:             repo,
			ImageTruth:       imageTruth,
		},
		failOn:     splitCSV(failOn),
		jsonOutput: jsonOutput,
	}, nil
}

// docsVerifyFailure maps the verification result onto the CLI exit contract:
// exit 1 when any finding carries a status the caller listed in --fail-on.
func docsVerifyFailure(failOn []string, result doctruth.VerificationResult) error {
	statuses := map[string]struct{}{}
	for _, status := range failOn {
		statuses[status] = struct{}{}
	}
	for _, finding := range result.Findings {
		if _, ok := statuses[finding.Status]; ok {
			return commandExitError{
				message: "documentation verification has " + finding.Status + " findings",
				code:    1,
			}
		}
	}
	return nil
}

// defaultDocsVerifyDeps wires the production dependencies: Postgres opened from
// the process environment, and the command surface walked off the live cobra
// tree.
func defaultDocsVerifyDeps() docs.Deps {
	return docs.Deps{
		OpenPersistence: openDocsVerifyPostgresPersistence,
		CommandTruth:    func() []doctruth.CommandTruth { return commandTruthFromCobra(rootCmd) },
		Now:             func() time.Time { return time.Now().UTC() },
	}
}

// openDocsVerifyPostgresPersistence opens the fact store `docs verify --persist`
// writes to. It lives here rather than in the docs package because resolving
// the DSN reads the process environment.
func openDocsVerifyPostgresPersistence(ctx context.Context) (docs.Persistence, func() error, error) {
	db, err := runtimecfg.OpenPostgres(ctx, os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	return docs.NewPostgresPersistence(db), db.Close, nil
}

// commandTruthFromCobra walks the live command tree into the command surface
// documentation claims are checked against. Hidden commands are excluded: a
// document should not be able to cite one as supported.
func commandTruthFromCobra(root *cobra.Command) []doctruth.CommandTruth {
	out := []doctruth.CommandTruth{}
	var walk func(*cobra.Command, []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}
			name := strings.Fields(child.Use)
			if len(name) == 0 {
				continue
			}
			path := append(append([]string{}, prefix...), name[0])
			out = append(out, doctruth.CommandTruth{Path: path, AllowsArgs: commandUseAllowsArgs(child.Use)})
			walk(child, path)
		}
	}
	walk(root, nil)
	return out
}

// commandUseAllowsArgs reports whether a cobra Use string declares positional
// arguments, which is what lets a documented `eshu docs verify .` be valid.
func commandUseAllowsArgs(use string) bool {
	return len(strings.Fields(use)) > 1
}

func splitCSV(value string) []string {
	parts := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
