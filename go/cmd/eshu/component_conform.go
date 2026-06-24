// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/extensionconformance"
)

func runComponentConform(cmd *cobra.Command, args []string) error {
	fixtures, err := cmd.Flags().GetStringSlice(componentFixtureFlag)
	if err != nil {
		return err
	}
	mode, err := cmd.Flags().GetString(componentModeFlag)
	if err != nil {
		return err
	}

	report, runErr := extensionconformance.Run(cmd.Context(), extensionconformance.Request{
		ManifestPath:  args[0],
		FixturePaths:  fixtures,
		Mode:          extensionconformance.Mode(mode),
		ComponentHome: componentHomeFromFlags(cmd),
	})
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("conform", componentConformanceStatus(report))
		payload.Conformance = &report
		if runErr != nil {
			payload.Error = componentErrorPayload(component.WrapError(
				component.ErrorCodeConformanceFailed,
				runErr.Error(),
				runErr,
			))
		}
		if writeErr := writeComponentJSON(cmd.OutOrStdout(), payload); writeErr != nil {
			return writeErr
		}
	}
	if runErr != nil {
		return runErr
	}
	if componentJSONEnabled(cmd) {
		return nil
	}
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"conformance passed %s@%s fixtures=%d facts=%d\n",
		report.ComponentID,
		report.ComponentVersion,
		report.Summary.FixtureCount,
		report.Summary.FactCount,
	)
	return err
}

func componentConformanceStatus(report extensionconformance.Report) string {
	if report.Status == extensionconformance.StatusPassed {
		return "passed"
	}
	return "failed"
}
