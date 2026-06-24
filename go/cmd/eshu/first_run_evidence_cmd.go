// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// evidenceEnvelopeMaxBytes bounds how much of a saved envelope the report
// subcommand reads, so a malformed or hostile stream cannot exhaust memory.
const evidenceEnvelopeMaxBytes = 8 << 20 // 8 MiB

// evidenceFormatMarkdown and evidenceFormatJSON are the accepted artifact
// formats for the evidence report.
const (
	evidenceFormatMarkdown = "md"
	evidenceFormatJSON     = "json"
)

// normalizeEvidenceFormat validates and canonicalizes an artifact format flag.
// It accepts "md"/"markdown" and "json" case-insensitively and returns an error
// listing the supported values otherwise.
func normalizeEvidenceFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", evidenceFormatMarkdown, "markdown":
		return evidenceFormatMarkdown, nil
	case evidenceFormatJSON:
		return evidenceFormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported report format %q: supported formats are md, json", raw)
	}
}

// renderEvidenceArtifact renders the report in the requested format and returns
// the bytes. The report is already redacted, so the bytes are safe to persist.
func renderEvidenceArtifact(report firstRunEvidenceReport, format string) ([]byte, error) {
	normalized, err := normalizeEvidenceFormat(format)
	if err != nil {
		return nil, err
	}
	if normalized == evidenceFormatJSON {
		return renderEvidenceJSON(report)
	}
	markdown, err := renderEvidenceMarkdown(report)
	if err != nil {
		return nil, err
	}
	return []byte(markdown), nil
}

// writeEvidenceArtifact writes the rendered artifact to path with owner-only
// permissions, since a support packet may still contain endpoint hostnames.
func writeEvidenceArtifact(report firstRunEvidenceReport, format, path string) error {
	data, err := renderEvidenceArtifact(report, format)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// newFirstRunReportCmd builds the `eshu first-run report` subcommand. It renders
// a redacted evidence artifact from a saved `eshu first-run --json` envelope so
// an operator can regenerate the support packet without re-running onboarding.
func newFirstRunReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Render a redacted first-run evidence artifact from a saved --json envelope",
		Long: `report renders a redacted first-run evidence artifact (Markdown or JSON)
from a previously captured 'eshu first-run --json' envelope. It re-uses the
result first-run already computed and never re-runs indexing or queries, so it
is safe to run offline against a saved envelope. Tokens, local secrets, and
private endpoints are redacted in every output.`,
		Args: cobra.NoArgs,
		RunE: runFirstRunReport,
	}
	cmd.Flags().String("from", "", "Path to a saved 'eshu first-run --json' envelope (defaults to stdin)")
	cmd.Flags().String("format", "md", "Artifact format: md or json")
	cmd.Flags().String("out", "", "Write the artifact to this path instead of stdout")
	return cmd
}

// runFirstRunReport reads the saved envelope, projects it into the evidence
// report, and renders it in the requested format.
func runFirstRunReport(cmd *cobra.Command, _ []string) error {
	from, _ := cmd.Flags().GetString("from")
	format, _ := cmd.Flags().GetString("format")
	out, _ := cmd.Flags().GetString("out")

	raw, err := readEvidenceEnvelope(cmd, from)
	if err != nil {
		return err
	}
	result, err := firstRunResultFromEnvelope(raw)
	if err != nil {
		return err
	}
	report := buildFirstRunEvidence(result, nil)
	if out != "" {
		if err := writeEvidenceArtifact(report, format, out); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote first-run evidence to %s\n", out)
		return nil
	}
	data, err := renderEvidenceArtifact(report, format)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

// readEvidenceEnvelope reads the saved envelope bytes from a path, or from stdin
// when no path is given.
func readEvidenceEnvelope(cmd *cobra.Command, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), evidenceEnvelopeMaxBytes))
		if err != nil {
			return nil, fmt.Errorf("read first-run envelope from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read first-run envelope: %w", err)
	}
	return data, nil
}

// firstRunResultFromEnvelope decodes a saved first-run envelope into a result,
// restoring the truth metadata onto the result so the rendered report carries
// it. It reuses the canonical firstRunEnvelope shape so the evidence report and
// the onboarding benchmark consume the same persisted contract. The envelope
// must be the object emitted by 'eshu first-run --json'.
func firstRunResultFromEnvelope(raw []byte) (firstRunResult, error) {
	envelope, err := parseFirstRunEnvelope(raw)
	if err != nil {
		return firstRunResult{}, err
	}
	if strings.TrimSpace(envelope.Data.Command) == "" {
		return firstRunResult{}, fmt.Errorf("first-run envelope is missing its data block")
	}
	result := envelope.Data
	if result.Truth == nil {
		result.Truth = envelope.Truth
	}
	return result, nil
}
