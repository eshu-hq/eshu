package main

import "github.com/spf13/cobra"

var ecosystemCmd = &cobra.Command{
	Use:   "ecosystem",
	Short: "Ecosystem-wide analysis (migrated to Go)",
}

func init() {
	rootCmd.AddCommand(ecosystemCmd)

	ecosystemCmd.AddCommand(&cobra.Command{
		Use:   "index",
		Short: "Removed: use the supported local or admin indexing flows instead",
		RunE:  runEcosystemIndex,
	})

	ecosystemCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Removed: no dedicated ecosystem status command is supported",
		RunE:  runEcosystemStatus,
	})

	ecosystemCmd.AddCommand(&cobra.Command{
		Use:   "overview",
		Short: "Show ecosystem overview",
		RunE:  runEcosystemOverview,
	})
}

func runEcosystemIndex(cmd *cobra.Command, args []string) error {
	return removedCommandError(
		"eshu ecosystem index",
		"Use `eshu index <path>` or the Go admin indexing flows until a dedicated ecosystem index contract exists.",
	)
}

func runEcosystemStatus(cmd *cobra.Command, args []string) error {
	return removedCommandError(
		"eshu ecosystem status",
		"Use `eshu index-status` or the Go admin/status APIs until a dedicated ecosystem status contract exists.",
	)
}

func runEcosystemOverview(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	if err := client.Get("/api/v0/ecosystem/overview", &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}
