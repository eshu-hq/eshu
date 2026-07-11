// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
)

// includePayloadsWarning is printed to stderr, loudly, whenever
// --include-payloads is set — in addition to the same warning the bundle
// itself carries in Bundle.Payloads.Warning — so a terminal user sees it even
// if they only read stdout for the bundle JSON and skim past it.
const includePayloadsWarning = `
!!! PRIVATE TRIAGE ONLY !!!
This bundle includes raw fact payloads and citation excerpts because
--include-payloads was set. Do NOT attach this bundle to a public GitHub
issue or share it outside your own local triage workflow. Run without
--include-payloads for a share-safe bundle, or run
"eshu report validate --require-public" to confirm before sharing.
`

// addReportBundleSubcommands attaches the wrong-answer report bundle
// subcommands (`capture`, `validate`) to the existing top-level `report`
// command built by newOperatorDigestCommand (operator_digest_cmd.go). There
// is exactly one root-level `report` command: registering a second here would
// silently shadow the operator-digest report in cobra's name lookup and make
// one of the two features unreachable. Instead both features share the one
// report parent — `eshu report` renders the operator digest, `eshu report
// capture`/`eshu report validate` handle report bundles.
func addReportBundleSubcommands(report *cobra.Command) {
	report.AddCommand(newReportCaptureCommand())
	report.AddCommand(newReportValidateCommand())
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
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return commandExitError{message: "--endpoint is required", code: 2}
	}
	method, err := cmd.Flags().GetString("method")
	if err != nil {
		return err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	paramsRaw, err := cmd.Flags().GetString("params")
	if err != nil {
		return err
	}
	params := map[string]any{}
	if strings.TrimSpace(paramsRaw) != "" {
		if err := json.Unmarshal([]byte(paramsRaw), &params); err != nil {
			return fmt.Errorf("--params must be a JSON object: %w", err)
		}
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

	surface := "api"
	target := endpoint
	if strings.TrimSpace(tool) != "" {
		surface = "mcp"
		target = strings.TrimSpace(tool)
	}

	envelope, err := fetchReportEnvelope(apiClientFromCmd(cmd), method, endpoint, params)
	if err != nil {
		return fmt.Errorf("fetch query envelope: %w", err)
	}

	bundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface:         surface,
		Target:          target,
		Method:          method,
		Params:          params,
		Profile:         string(envelopeProfile(envelope)),
		ReporterNote:    note,
		Envelope:        envelope,
		Truncated:       observedTruncation(envelope),
		IncludePayloads: includePayloads,
	})
	if err != nil {
		return fmt.Errorf("capture report bundle: %w", err)
	}

	if includePayloads {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), includePayloadsWarning)
	}

	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report bundle: %w", err)
	}
	raw = append(raw, '\n')

	if strings.TrimSpace(outPath) != "" {
		if err := os.WriteFile(outPath, raw, 0o600); err != nil {
			return fmt.Errorf("write report bundle: %w", err)
		}
		return nil
	}
	if _, err := cmd.OutOrStdout().Write(raw); err != nil {
		return fmt.Errorf("write report bundle: %w", err)
	}
	return nil
}

// fetchReportEnvelope issues the query the reporter ran and decodes it into
// the canonical query.ResponseEnvelope shape, reusing APIClient exactly as
// every other envelope-backed verb does (client.go:80-93).
func fetchReportEnvelope(client *APIClient, method, endpoint string, params map[string]any) (query.ResponseEnvelope, error) {
	var envelope query.ResponseEnvelope
	switch method {
	case "GET":
		path := endpoint
		if len(params) > 0 {
			values := url.Values{}
			for key, value := range params {
				values.Set(key, fmt.Sprintf("%v", value))
			}
			path += "?" + values.Encode()
		}
		if err := client.GetEnvelope(path, &envelope); err != nil {
			return query.ResponseEnvelope{}, err
		}
	case "POST":
		if err := client.PostEnvelope(endpoint, params, &envelope); err != nil {
			return query.ResponseEnvelope{}, err
		}
	default:
		return query.ResponseEnvelope{}, fmt.Errorf("unsupported --method %q: want GET or POST", method)
	}
	return envelope, nil
}

// observedTruncation looks for a top-level "truncated" boolean in the
// captured response data. Truncation is a read-model field, not part of the
// envelope contract (query/answer_packet.go:88-89), so this is a best-effort
// read of the SAME shape a maintainer would see, not a new contract.
func observedTruncation(envelope query.ResponseEnvelope) bool {
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return false
	}
	truncated, ok := data["truncated"].(bool)
	return ok && truncated
}

// envelopeProfile returns the query profile the envelope's truth reports, or
// empty when no truth envelope is present (for example an error response).
func envelopeProfile(envelope query.ResponseEnvelope) query.QueryProfile {
	if envelope.Truth == nil {
		return ""
	}
	return envelope.Truth.Profile
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
	raw, err := readReportBundleInput(cmd.InOrStdin(), from)
	if err != nil {
		return err
	}
	var bundle reportbundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return fmt.Errorf("decode report bundle: %w", err)
	}
	if err := reportbundle.Validate(bundle, reportbundle.ValidateOptions{RequirePublic: requirePublic}); err != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "report bundle validation: failed")
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "report bundle validation: passed")
	return nil
}

func readReportBundleInput(in io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local validation path, not an HTTP request param //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read report bundle: %w", err)
		}
		return raw, nil
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("read report bundle stdin: %w", err)
	}
	return raw, nil
}
