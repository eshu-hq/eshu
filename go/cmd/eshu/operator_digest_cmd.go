// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/opdigest"
)

func init() {
	rootCmd.AddCommand(newOperatorDigestCommand())
}

// newOperatorDigestCommand builds the deterministic operator digest renderer.
func newOperatorDigestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Render a deterministic operator digest for an explicit scope",
		Long: `report renders the operator_digest.v1 model for an explicit scope.

This first CLI implementation is an offline presentation path. It validates
share-safe input, emits deterministic unsupported sections, and points operators
to bounded follow-up routes without reading graph state, writing graph state,
claiming reducer work, or calling providers.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runOperatorDigest,
	}
	cmd.Flags().String("scope", "", "Share-safe scope such as repo:owner/name, service:name, workload:name, environment:name, or project:name")
	cmd.Flags().String("profile", opdigest.DefaultProfile, "Runtime profile used to derive the digest")
	cmd.Flags().Int("question-limit", opdigest.DefaultQuestionMax, "Maximum suggested questions to emit (0-25)")
	cmd.Flags().Bool("json", false, "Emit the digest as JSON")
	cmd.Flags().String("artifact-out", "", "Write a shareable operator_digest_artifact.v1 JSON file")
	// `report` is a single shared parent: running it renders the operator
	// digest, while the wrong-answer report bundle verbs live under it as
	// `report capture` / `report validate` (see report_cmd.go). Registering a
	// second root-level `report` would shadow this one in cobra's lookup.
	addReportBundleSubcommands(cmd)
	return cmd
}

func runOperatorDigest(cmd *cobra.Command, _ []string) error {
	rawScope, _ := cmd.Flags().GetString("scope")
	rawProfile, _ := cmd.Flags().GetString("profile")
	questionLimit, _ := cmd.Flags().GetInt("question-limit")
	jsonOut, _ := cmd.Flags().GetBool("json")
	artifactOut, _ := cmd.Flags().GetString("artifact-out")

	options, err := opdigest.OptionsFromFlags(rawScope, rawProfile, questionLimit)
	if err != nil {
		return err
	}
	digest := opdigest.BuildDigest(options)
	if strings.TrimSpace(artifactOut) != "" {
		if err := opdigest.WriteArtifact(artifactOut, digest); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote operator digest artifact to %s\n", artifactOut)
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(digest); err != nil {
			return fmt.Errorf("write operator digest JSON: %w", err)
		}
		return nil
	}
	opdigest.RenderText(cmd.OutOrStdout(), digest)
	return nil
}
