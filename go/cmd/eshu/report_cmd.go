// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/report"
)

// addReportBundleSubcommands attaches the wrong-answer report bundle
// subcommands (`capture`, `validate`) to the existing top-level `report`
// command built by newOperatorDigestCommand (operator_digest_cmd.go). There
// is exactly one root-level `report` command: registering a second here would
// silently shadow the operator-digest report in cobra's name lookup and make
// one of the two features unreachable. Instead both features share the one
// report parent — `eshu report` renders the operator digest, `eshu report
// capture`/`eshu report validate` handle report bundles.
//
// The parameter is named parent rather than report so it does not shadow the
// internal/cli/report package this file imports.
func addReportBundleSubcommands(parent *cobra.Command) {
	parent.AddCommand(newReportCaptureCommand())
	parent.AddCommand(newReportValidateCommand())
}

func newReportCaptureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "capture",
		Short:         "Capture a share-safe wrong_answer_report.v1 bundle from a query",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runReportCapture,
	}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

func addReportCaptureFlags(cmd *cobra.Command) {
	cmd.Flags().String("endpoint", "", "API path to query (required)")
	cmd.Flags().String("method", "GET", "HTTP method to issue: GET or POST")
	cmd.Flags().String("params", "", "JSON object of query/body parameters as issued")
	cmd.Flags().String("note", "", "What you expected instead of the captured answer")
	cmd.Flags().String("out", "", "Path to write the report bundle JSON; stdout when omitted")
	cmd.Flags().Bool("include-payloads", false, "PRIVATE TRIAGE ONLY: attach raw fact payloads and citation excerpts (never attach to a public issue)")
	cmd.Flags().String("tool", "", "MCP tool name this query originated from, recorded as the surface; --endpoint still resolves the answer (Slice 1 records MCP capture, it does not invoke MCP itself)")
}

func newReportValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate a wrong_answer_report.v1 bundle",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runReportValidate,
	}
	addReportValidateFlags(cmd)
	return cmd
}

func addReportValidateFlags(cmd *cobra.Command) {
	cmd.Flags().String("from", "", "Path to a report bundle JSON file; stdin when omitted")
	cmd.Flags().Bool("require-public", false, "Fail if the bundle's redaction profile is not public (share-safe)")
}

func runReportCapture(cmd *cobra.Command, _ []string) error {
	endpoint, err := cmd.Flags().GetString("endpoint")
	if err != nil {
		return err
	}
	if strings.TrimSpace(endpoint) == "" {
		return commandExitError{message: "--endpoint is required", code: 2}
	}
	method, err := cmd.Flags().GetString("method")
	if err != nil {
		return err
	}
	paramsRaw, err := cmd.Flags().GetString("params")
	if err != nil {
		return err
	}
	note, err := cmd.Flags().GetString("note")
	if err != nil {
		return err
	}
	outPath, err := cmd.Flags().GetString("out")
	if err != nil {
		return err
	}
	includePayloads, err := cmd.Flags().GetBool("include-payloads")
	if err != nil {
		return err
	}
	tool, err := cmd.Flags().GetString("tool")
	if err != nil {
		return err
	}

	result, err := report.CaptureBundle(apiClientFromCmd(cmd), report.CaptureOptions{
		Endpoint:        endpoint,
		Tool:            tool,
		Method:          method,
		ParamsJSON:      paramsRaw,
		Note:            note,
		IncludePayloads: includePayloads,
	})
	if err != nil {
		// A target carrying a credential is a usage mistake the reporter can
		// fix, so it takes the usage exit code rather than the generic one.
		var credentialErr *report.TargetCredentialError
		if errors.As(err, &credentialErr) {
			return commandExitError{message: credentialErr.Error(), code: 2}
		}
		return err
	}

	if includePayloads {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), report.IncludePayloadsWarning)
	}

	if strings.TrimSpace(outPath) != "" {
		return report.WriteBundle(outPath, result.JSON)
	}
	if _, err := cmd.OutOrStdout().Write(result.JSON); err != nil {
		return fmt.Errorf("write report bundle: %w", err)
	}
	return nil
}

func runReportValidate(cmd *cobra.Command, _ []string) error {
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	requirePublic, err := cmd.Flags().GetBool("require-public")
	if err != nil {
		return err
	}
	raw, err := report.ReadBundleInput(cmd.InOrStdin(), from)
	if err != nil {
		return err
	}
	return report.ValidateBundle(cmd.OutOrStdout(), raw, requirePublic)
}
