// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"

	cliconfig "github.com/eshu-hq/eshu/go/internal/cli/config"
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

// runConfigValidate is the process-wiring half of `eshu config validate`: it
// reads the two cobra flags, resolves the command's output stream, and snapshots
// the real process environment. The check itself lives in internal/cli/config.
func runConfigValidate(cmd *cobra.Command, _ []string) error {
	registry := envregistry.Default()

	if reference, _ := cmd.Flags().GetBool("reference"); reference {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), registry.RenderMarkdown())
		return nil
	}

	strict, _ := cmd.Flags().GetBool("strict")
	return cliconfig.ValidateEnv(cmd.OutOrStdout(), registry, cliconfig.EnvironMap(os.Environ()), strict)
}
