// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

func init() {
	rootCmd.AddCommand(newEvidenceCommand())
}

func newEvidenceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "evidence",
		Short:         "Work with portable evidence artifacts",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newEvidenceBundleCommand())
	return cmd
}

func newEvidenceBundleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "bundle",
		Short:         "Export and validate portable evidence bundles",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newEvidenceBundleExportCommand())
	cmd.AddCommand(newEvidenceBundleValidateCommand())
	return cmd
}

func newEvidenceBundleExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "export",
		Short:         "Export a deterministic share-safe evidence bundle",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runEvidenceBundleExport,
	}
	cmd.Flags().String("scope", "repo:demo/service", "Share-safe scope handle for the bundle")
	cmd.Flags().String("out", "", "Path to write the bundle JSON; stdout when omitted")
	return cmd
}

func newEvidenceBundleValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate a portable evidence bundle",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runEvidenceBundleValidate,
	}
	cmd.Flags().String("from", "", "Path to a bundle JSON file; stdin when omitted")
	return cmd
}

func runEvidenceBundleExport(cmd *cobra.Command, _ []string) error {
	scope, err := cmd.Flags().GetString("scope")
	if err != nil {
		return err
	}
	outPath, err := cmd.Flags().GetString("out")
	if err != nil {
		return err
	}
	bundle := evidencebundle.BuildDemoBundle(evidencebundle.DemoBundleOptions{ScopeID: scope})
	if err := evidencebundle.Validate(bundle); err != nil {
		return fmt.Errorf("validate generated evidence bundle: %w", err)
	}
	raw, err := evidencebundle.RenderJSON(bundle)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outPath) != "" {
		if err := os.WriteFile(outPath, raw, 0o600); err != nil {
			return fmt.Errorf("write evidence bundle: %w", err)
		}
		return nil
	}
	if _, err := cmd.OutOrStdout().Write(raw); err != nil {
		return fmt.Errorf("write evidence bundle: %w", err)
	}
	return nil
}

func runEvidenceBundleValidate(cmd *cobra.Command, _ []string) error {
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	raw, err := readEvidenceBundleInput(cmd.InOrStdin(), from)
	if err != nil {
		return err
	}
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return fmt.Errorf("decode evidence bundle: %w", err)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "evidence bundle validation: failed")
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "evidence bundle validation: passed")
	return nil
}

func readEvidenceBundleInput(in io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local validation path, not an HTTP request param //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read evidence bundle: %w", err)
		}
		return raw, nil
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("read evidence bundle stdin: %w", err)
	}
	return raw, nil
}
