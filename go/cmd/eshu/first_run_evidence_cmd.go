// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
)

// evidenceEnvelopeMaxBytes bounds how much of a saved envelope the report
// subcommand reads, so a malformed or hostile stream cannot exhaust memory.
const evidenceEnvelopeMaxBytes = 8 << 20 // 8 MiB

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
is safe to run offline against a saved envelope.

Every endpoint, path, and free-text field is scrubbed before it is rendered: an
embedded 'user:password@' credential and a credential-shaped query parameter are
both replaced, and an absolute path is reduced to its final element. A secret in
a URL path segment, or written as bare prose with no key beside it, is not
detected. See docs/public/reference/first-run-evidence.md for the full limits.`,
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
	report := firstrun.BuildEvidence(result, nil)
	if out != "" {
		if err := firstrun.WriteEvidenceArtifact(report, format, out); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote first-run evidence to %s\n", out)
		return nil
	}
	data, err := firstrun.RenderEvidenceArtifact(report, format)
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
	data, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied CLI flag pointing to a local first-run envelope file, not an HTTP request param
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
func firstRunResultFromEnvelope(raw []byte) (firstrun.Result, error) {
	envelope, err := parseFirstRunEnvelope(raw)
	if err != nil {
		return firstrun.Result{}, err
	}
	if strings.TrimSpace(envelope.Data.Command) == "" {
		return firstrun.Result{}, fmt.Errorf("first-run envelope is missing its data block")
	}
	result := envelope.Data
	if result.Truth == nil {
		result.Truth = envelope.Truth
	}
	return result, nil
}
