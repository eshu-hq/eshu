// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/query"
)

type docsVerifyOptions struct {
	Path             string
	Limit            int
	MaxDocumentBytes int
	FailOn           []string
	JSON             bool
	Persist          bool
	Scope            string
	Repo             string
	ImageTruth       string
}

type docsVerifyEnvelope struct {
	Data  docsVerifyData   `json:"data"`
	Truth map[string]any   `json:"truth"`
	Error *docsVerifyError `json:"error"`
}

type docsVerifyData struct {
	Findings        []doctruth.VerificationFinding        `json:"findings"`
	EvidencePackets []doctruth.VerificationEvidencePacket `json:"evidence_packets"`
	Summary         doctruth.VerificationSummary          `json:"summary"`
	Truncated       bool                                  `json:"truncated"`
	Persistence     docsVerifyPersistenceSummary          `json:"persistence,omitempty"`
}

type docsInventory struct {
	Documents []doctruth.DocumentInput
	Truncated bool
}

type docsVerifyError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
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

func newDocsVerifyCommandWithDeps(deps docsVerifyDeps) *cobra.Command {
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

func runDocsVerifyWithDeps(cmd *cobra.Command, args []string, deps docsVerifyDeps) error {
	opts, err := docsVerifyOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	opts.ImageTruth = effectiveDocsVerifyImageTruth(cmd, opts.ImageTruth)
	inventory, err := inventoryDocs(opts)
	if err != nil {
		return err
	}
	persistence, closePersistence, persistSummary, err := prepareDocsVerifyPersistence(cmd.Context(), opts, inventory, deps)
	if err != nil {
		return err
	}
	if closePersistence != nil {
		defer func() { _ = closePersistence() }()
	}
	if persistSummary.Skipped {
		result, err := docsVerifyResultFromPersisted(cmd.Context(), persistence, persistSummary)
		if err != nil {
			return err
		}
		applyDocsVerifyInventorySummary(&result, inventory)
		exitErr := docsVerifyFailure(opts, result)
		envelope := docsVerifyEnvelopeForResult(result, exitErr)
		envelope.Data.Persistence = persistSummary
		return writeDocsVerifyOutput(cmd, opts, result, envelope, exitErr)
	}
	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands:               docsVerifyCommandTruth(deps),
		HTTPEndpoints:          docsVerifyHTTPEndpointTruth(),
		EnvironmentVariables:   docsVerifyEnvironmentTruth(opts.Path),
		LocalPathResolver:      docsVerifyLocalPathResolver(opts.Path),
		ContainerImageResolver: docsVerifyContainerImageResolver(cmd, opts),
		TerraformResolver:      docsVerifyTerraformAddressResolver(opts.Path),
		MaxDocuments:           opts.Limit,
		MaxDocumentBytes:       opts.MaxDocumentBytes,
		ScopeID:                persistSummary.ScopeID,
		GenerationID:           persistSummary.GenerationID,
	})
	result, err := verifier.Verify(cmd.Context(), inventory.Documents)
	if err != nil {
		return err
	}
	result.Truncated = result.Truncated || inventory.Truncated
	if persistence != nil {
		if err := commitDocsVerifyResult(cmd.Context(), persistence, persistSummary, result, deps.now); err != nil {
			return err
		}
		persistSummary.Persisted = true
	}
	exitErr := docsVerifyFailure(opts, result)
	envelope := docsVerifyEnvelopeForResult(result, exitErr)
	envelope.Data.Persistence = persistSummary
	return writeDocsVerifyOutput(cmd, opts, result, envelope, exitErr)
}

func docsVerifyCommandTruth(deps docsVerifyDeps) []doctruth.CommandTruth {
	if deps.commandTruth == nil {
		return nil
	}
	return deps.commandTruth()
}

func writeDocsVerifyOutput(
	cmd *cobra.Command,
	opts docsVerifyOptions,
	result doctruth.VerificationResult,
	envelope docsVerifyEnvelope,
	exitErr error,
) error {
	if opts.JSON {
		if err := writeDocsVerifyJSON(cmd.OutOrStdout(), envelope); err != nil {
			return err
		}
		return exitErr
	}
	if err := renderDocsVerifyText(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	return exitErr
}

func docsVerifyOptionsFromCommand(cmd *cobra.Command, args []string) (docsVerifyOptions, error) {
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	maxBytes, err := cmd.Flags().GetInt("max-bytes")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	failOn, err := cmd.Flags().GetString("fail-on")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	persist, err := cmd.Flags().GetBool("persist")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	scopeID, err := cmd.Flags().GetString("scope")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	repo, err := cmd.Flags().GetString("repo")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	imageTruth, err := cmd.Flags().GetString("image-truth")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	imageTruth = strings.TrimSpace(strings.ToLower(imageTruth))
	imageTruth = normalizedDocsVerifyImageTruth(imageTruth)
	switch imageTruth {
	case "auto", "local", "api":
	default:
		return docsVerifyOptions{}, commandExitError{message: "--image-truth must be auto, local, or api", code: 2}
	}
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if limit <= 0 {
		return docsVerifyOptions{}, commandExitError{message: "--limit must be greater than 0", code: 2}
	}
	if maxBytes <= 0 {
		return docsVerifyOptions{}, commandExitError{message: "--max-bytes must be greater than 0", code: 2}
	}
	return docsVerifyOptions{
		Path:             path,
		Limit:            limit,
		MaxDocumentBytes: maxBytes,
		FailOn:           splitCSV(failOn),
		JSON:             jsonOutput,
		Persist:          persist,
		Scope:            scopeID,
		Repo:             repo,
		ImageTruth:       imageTruth,
	}, nil
}

func normalizedDocsVerifyImageTruth(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "auto"
	}
	return value
}

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

func commandUseAllowsArgs(use string) bool {
	return len(strings.Fields(use)) > 1
}

func endpointTruthFromOpenAPI(spec string) []doctruth.HTTPEndpointTruth {
	var raw struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(spec), &raw); err != nil {
		return nil
	}
	out := []doctruth.HTTPEndpointTruth{}
	for path, methods := range raw.Paths {
		for method := range methods {
			method = strings.ToUpper(method)
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				out = append(out, doctruth.HTTPEndpointTruth{Method: method, Path: path})
			}
		}
	}
	return out
}

func docsVerifyHTTPEndpointTruth() []doctruth.HTTPEndpointTruth {
	out := endpointTruthFromOpenAPI(query.OpenAPISpec())
	out = append(
		out,
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/api/v0/docs"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/api/v0/redoc"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/health"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/sse"},
		doctruth.HTTPEndpointTruth{Method: http.MethodPost, Path: "/mcp/message"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/healthz"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/readyz"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/admin/status"},
		doctruth.HTTPEndpointTruth{Method: http.MethodPost, Path: "/admin/replay"},
		doctruth.HTTPEndpointTruth{Method: http.MethodPost, Path: "/admin/refinalize"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/metrics"},
	)
	return out
}

func docsVerifyFailure(opts docsVerifyOptions, result doctruth.VerificationResult) error {
	failOn := map[string]struct{}{}
	for _, status := range opts.FailOn {
		failOn[status] = struct{}{}
	}
	for _, finding := range result.Findings {
		if _, ok := failOn[finding.Status]; ok {
			return commandExitError{
				message: "documentation verification has " + finding.Status + " findings",
				code:    1,
			}
		}
	}
	return nil
}

func docsVerifyEnvelopeForResult(result doctruth.VerificationResult, err error) docsVerifyEnvelope {
	envelope := docsVerifyEnvelope{
		Data: docsVerifyData{
			Findings:        result.Findings,
			EvidencePackets: result.EvidencePackets,
			Summary:         result.Summary,
			Truncated:       result.Truncated,
		},
		Truth: map[string]any{
			"capability": "documentation.verify",
			"basis":      "active documentation claim verification",
			"freshness":  map[string]any{"state": "fresh"},
		},
	}
	if err != nil {
		envelope.Error = &docsVerifyError{Code: "documentation_verification_failed", Message: err.Error()}
	}
	return envelope
}

func renderDocsVerifyText(w io.Writer, result doctruth.VerificationResult) error {
	summary := result.Summary
	if _, err := fmt.Fprintf(
		w,
		"Docs verify: documents=%d claims=%d valid=%d contradicted=%d missing_evidence=%d unsupported=%d truncated=%t\n",
		summary.DocumentsScanned,
		summary.ClaimsChecked,
		summary.Valid,
		summary.Contradicted,
		summary.MissingEvidence,
		summary.UnsupportedClaimType,
		result.Truncated,
	); err != nil {
		return err
	}
	for _, finding := range result.Findings {
		if finding.Status == doctruth.VerificationStatusValid {
			continue
		}
		if _, err := fmt.Fprintf(w, "- %s %s %s\n", finding.Status, finding.ClaimType, finding.NormalizedClaim); err != nil {
			return err
		}
	}
	return nil
}

func writeDocsVerifyJSON(w io.Writer, envelope docsVerifyEnvelope) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
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
