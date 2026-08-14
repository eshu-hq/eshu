// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"errors"
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

	// Both are checked, and before the request goes out: --endpoint reaches the
	// bundle when --tool is absent and reaches the failure message either way,
	// so checking only the one that becomes query.target leaves the other live.
	if err := rejectTargetCredentials("--endpoint", endpoint); err != nil {
		return err
	}
	if err := rejectTargetCredentials("--tool", tool); err != nil {
		return err
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
		// --endpoint may already carry its own query string. Splitting it and
		// re-encoding both sources together builds one well-formed URL;
		// appending "?"+params to a path that already had a "?" produced a
		// malformed second query string. reportbundle.SplitTargetQuery is the
		// same function Capture uses to keep the credential out of the
		// recorded bundle, so the request and the record cannot drift.
		//
		// A query string it cannot parse stops the run here, before the
		// request goes out. Capture would refuse the same target anyway, and
		// issuing a query the reporter did not type only to throw the answer
		// away is worse than not issuing it.
		path, targetParams, err := reportbundle.SplitTargetQuery(endpoint)
		if err != nil {
			return query.ResponseEnvelope{}, err
		}
		values := url.Values{}
		for key, value := range targetParams {
			repeated, ok := value.([]any)
			if !ok {
				values.Set(key, fmt.Sprintf("%v", value))
				continue
			}
			for _, item := range repeated {
				values.Add(key, fmt.Sprintf("%v", item))
			}
		}
		// An explicit --params entry replaces a same-named endpoint parameter,
		// matching how Capture resolves the same collision.
		for key, value := range params {
			values.Set(key, fmt.Sprintf("%v", value))
		}
		requestPath := path
		if len(values) > 0 {
			requestPath += "?" + values.Encode()
		}
		if err := client.GetEnvelope(requestPath, &envelope); err != nil {
			return query.ResponseEnvelope{}, requestErrorWithoutURL(err, path)
		}
	case "POST":
		postPath, _, err := reportbundle.SplitTargetQuery(endpoint)
		if err != nil {
			return query.ResponseEnvelope{}, err
		}
		if err := client.PostEnvelope(endpoint, params, &envelope); err != nil {
			return query.ResponseEnvelope{}, requestErrorWithoutURL(err, postPath)
		}
	default:
		return query.ResponseEnvelope{}, fmt.Errorf("unsupported --method %q: want GET or POST", method)
	}
	return envelope, nil
}

// rejectTargetCredentials refuses a --endpoint or --tool value carrying URL
// userinfo, the `user:password@host` an authority component may hold.
//
// Every redaction rule in internal/reportbundle matches an object KEY name, and
// SplitTargetQuery exists to turn a target's query string back into keys so
// those rules can reach it. Userinfo sits before the "?", so the split never
// sees it and there is no key name to match: `--tool
// https://svc:PASSWORD@mcp.internal/tool` put the password verbatim into
// query.target of a bundle stamped `"profile": "public"`, `"rules": []` and
// `"status": "passed"` — a share-safe artifact, meant for a public issue, that
// certified it had screened itself.
//
// It refuses rather than stripping, matching how Capture handles an unparseable
// query string and how sdk/go/collector's validateSourceURI handles the same
// userinfo on a fact source_ref: a bundle that quietly drops half of what the
// reporter asked misreports the query under investigation, and the reporter is
// the only one who can supply a target without the credential in it.
//
// net/url decides what an authority is, so an "@" inside a path segment
// (`/api/v0/owners/dev@example.com/services`) is untouched. A hand-written
// character rule is exactly what has been wrong here before.
//
// Not covered: a full URL pasted INSIDE a path segment
// (`/api/v0/x/https://svc:pw@host/y`) is not an authority component, so
// net/url reports no userinfo and the value passes. An unparseable target is
// refused instead of passed, because nothing can separate a credential from a
// string that cannot be taken apart.
//
// The error names the flag and never repeats the value, per the same rule
// internal/reportbundle states in its doc.go: these messages reach terminals,
// CI logs and pasted bug reports — the places the bundle beside them is
// redacted for.
func rejectTargetCredentials(flag, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return commandExitError{
			message: flag + " cannot be parsed as a URL, so it cannot be checked for an embedded credential; pass a plain path or tool name",
			code:    2,
		}
	}
	if parsed.User != nil {
		return commandExitError{
			message: flag + " carries a credential in its URL userinfo (user:password@host); remove it and rerun, because the captured bundle is meant to be attached to a public issue",
			code:    2,
		}
	}
	return nil
}

// safeErrorPath returns path with any URL userinfo replaced by a fixed marker,
// for use in a message a reporter reads.
//
// Unlike rejectTargetCredentials this redacts instead of refusing: the caller
// is already reporting a failure, and the host and path are what a reader needs
// to fix it. A path net/url cannot parse is replaced wholesale, since a string
// that cannot be taken apart cannot have a credential separated out of it.
func safeErrorPath(path string) string {
	parsed, err := url.Parse(path)
	if err != nil {
		return "[unparseable endpoint]"
	}
	if parsed.User == nil {
		// Returned verbatim rather than through parsed.String(), which
		// re-encodes and would change messages that were already safe.
		return path
	}
	parsed.User = url.User("redacted")
	return parsed.String()
}

// requestErrorWithoutURL strips the request URL out of a transport error and
// puts the bare endpoint path in its place.
//
// The capture command has to issue the reporter's real request, credentials and
// all — reproducing the exact query is the feature. net/http reports a failed
// request as a *url.Error whose Error() quotes that whole URL, so a wrong port
// or an unreachable service printed the credential to stderr and into any CI
// log that captured the run. None of the bundle-side redaction applies: this
// error exists before Capture is ever called.
//
// The wrapped transport error is preserved with %w, so errors.Is/As still work
// and the reader still learns what actually failed (connection refused, TLS
// handshake, timeout). Those carry host:port, never the query string.
//
// The substituted path goes through safeErrorPath rather than being trusted:
// it is the reporter's own --endpoint, and stripping the query string leaves
// `https://svc:PASSWORD@host/path` fully intact. runReportCapture already
// refuses such an endpoint before any request goes out, so this is the second
// of two guards — deliberately, because "the caller checked it" is the kind of
// ordering assumption this function exists to stop depending on.
//
// Not covered: a server that echoes the request URL back inside a 4xx/5xx
// response body, which arrives as apiHTTPError.Body rather than a *url.Error.
// Reaching into that body means reading apiHTTPError, which lives in package
// main and so cannot be read from internal/cli; issue #6059's sibling branch
// (PR #6117) adds the accessor that would make it possible.
func requestErrorWithoutURL(err error, safePath string) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	return fmt.Errorf("%s %s: %w", urlErr.Op, safeErrorPath(safePath), urlErr.Err)
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
