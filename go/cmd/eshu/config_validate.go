// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
	"github.com/spf13/cobra"
)

func init() {
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate ESHU_* environment variables against the central registry",
		Long: "Validate checks the current environment against the code-owned ESHU_*\n" +
			"registry. It reports invalid values for known variables (errors),\n" +
			"deprecated variables (warnings), and unknown variables that resemble a\n" +
			"known name (likely typos). Use --strict to also flag every unrecognized\n" +
			"ESHU_* variable, and --reference to print the generated reference doc.",
		// We print our own diagnostics; do not let cobra echo usage on a
		// validation failure.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runConfigValidate,
	}
	validateCmd.Flags().Bool("strict", false, "Treat every unrecognized ESHU_* variable as a finding")
	validateCmd.Flags().Bool("reference", false, "Print the generated environment-variable reference and exit")
	configCmd.AddCommand(validateCmd)
}

func runConfigValidate(cmd *cobra.Command, _ []string) error {
	registry := envregistry.Default()

	if reference, _ := cmd.Flags().GetBool("reference"); reference {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), registry.RenderMarkdown())
		return nil
	}

	strict, _ := cmd.Flags().GetBool("strict")
	return validateEnv(cmd.OutOrStdout(), registry, environMap(os.Environ()), strict)
}

// validateEnv runs the registry validation against an explicit environment
// snapshot and reports findings. It is the testable seam beneath
// runConfigValidate.
func validateEnv(out io.Writer, registry *envregistry.Registry, env map[string]string, strict bool) error {
	return reportFindings(out, registry.Validate(env, strict))
}

// environMap parses os.Environ()-style "KEY=VALUE" pairs into a map.
func environMap(pairs []string) map[string]string {
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		if key, value, ok := strings.Cut(pair, "="); ok {
			env[key] = value
		}
	}
	return env
}

// reportFindings prints findings grouped into errors and warnings and returns a
// non-nil error when any error-level finding is present, so the command exits
// non-zero.
func reportFindings(out io.Writer, findings []envregistry.Finding) error {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(out, "config validate: OK — no ESHU_* configuration problems found")
		return nil
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Error != findings[j].Error {
			return findings[i].Error // errors first
		}
		return findings[i].Name < findings[j].Name
	})

	var errorCount, warnCount int
	for _, f := range findings {
		label := "WARN "
		if f.Error {
			label = "ERROR"
			errorCount++
		} else {
			warnCount++
		}
		_, _ = fmt.Fprintf(out, "%s %s\n", label, f.Message)
	}
	_, _ = fmt.Fprintf(out, "\nconfig validate: %d error(s), %d warning(s)\n", errorCount, warnCount)

	if errorCount > 0 {
		return fmt.Errorf("configuration invalid: %d error(s)", errorCount)
	}
	return nil
}
