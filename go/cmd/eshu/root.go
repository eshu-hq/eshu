package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

var (
	globalDatabase string
	globalVisual   bool
)

var rootCmd = &cobra.Command{
	Use:     "eshu",
	Short:   "Eshu -- code-to-cloud context graph",
	Version: buildinfo.AppVersion(),
	Long: `Eshu is both an MCP server and a CLI toolkit for code analysis.

For MCP Server Mode (AI assistants):
  1. Run 'eshu mcp setup' to configure your IDE
  2. Run 'eshu mcp start' to launch the server

For CLI Toolkit Mode (direct usage):
  eshu index .     -- Index your current directory
  eshu list        -- List indexed repositories

Run 'eshu help' to see all available commands.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if globalDatabase != "" {
			if err := os.Setenv("ESHU_RUNTIME_DB_TYPE", globalDatabase); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalDatabase, "database", "", "Temporarily override database backend")
	rootCmd.PersistentFlags().BoolVarP(&globalVisual, "visual", "V", false, "Show results as interactive graph visualization")
	rootCmd.Flags().BoolP("version", "v", false, "Show the installed application version")
	rootCmd.SetVersionTemplate("Eshu {{.Version}}\n")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(helpCmd)
	rootCmd.AddCommand(doctorCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the installed application version",
	RunE: func(cmd *cobra.Command, args []string) error {
		return printVersion(cmd)
	},
}

func printVersion(cmd *cobra.Command) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Eshu %s\n", buildinfo.AppVersion())
	return err
}

var helpCmd = &cobra.Command{
	Use:   "help",
	Short: "Show the main help message",
	RunE:  func(cmd *cobra.Command, args []string) error { return rootCmd.Help() },
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostics to check system health and configuration",
	RunE:  runDoctor,
}
