// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"strings"

	cliconfig "github.com/eshu-hq/eshu/go/internal/cli/config"
	"github.com/eshu-hq/eshu/go/internal/envregistry"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration settings",
}

func init() {
	rootCmd.AddCommand(configCmd)

	configCmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Display current configuration settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := cliconfig.Load()
			if len(settings) == 0 {
				fmt.Printf("No configuration found at %s\n", cliconfig.EnvFilePath())
				return nil
			}
			headers := []string{"Key", "Value"}
			var rows [][]string
			for k, v := range settings {
				rows = append(rows, []string{k, v})
			}
			printTable(headers, rows)
			return nil
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set one configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliconfig.SetValue(args[0], args[1]); err != nil {
				return err
			}
			printSuccess(fmt.Sprintf("Set %s", args[0]))
			return nil
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Reset all configuration to defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print("Reset all configuration to defaults? [y/N] ")
			var answer string
			if _, err := fmt.Scanln(&answer); err != nil {
				return err
			}
			if strings.ToLower(answer) != "y" {
				fmt.Println("Reset cancelled")
				return nil
			}
			return cliconfig.Reset()
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "db <backend>",
		Short: "Switch the default database backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backend, err := cliconfig.ConfigureDatabaseBackend(args[0])
			if err != nil {
				return err
			}
			printSuccess(fmt.Sprintf("Default database switched to %s", backend))
			return nil
		},
	})

	// neo4j setup
	neo4jCmd := &cobra.Command{
		Use:   "neo4j",
		Short: "Neo4j database configuration",
	}
	neo4jCmd.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Configure Neo4j database connection",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Neo4j Database Setup")
			fmt.Println("Set NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD environment variables")
			fmt.Println("or use 'eshu config set NEO4J_URI bolt://localhost:7687'")
			return nil
		},
	})
	rootCmd.AddCommand(neo4jCmd)

	// Shortcut: eshu n -> neo4j setup
	nAlias := &cobra.Command{
		Use:   "n",
		Short: "Shortcut for 'eshu neo4j setup'",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Neo4j Database Setup")
			fmt.Println("Set NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD environment variables")
			fmt.Println("or use 'eshu config set NEO4J_URI bolt://localhost:7687'")
			return nil
		},
	}
	rootCmd.AddCommand(nAlias)
}

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
