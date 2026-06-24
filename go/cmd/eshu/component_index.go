// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/componentindex"
)

func init() {
	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Manage component extension index metadata",
	}
	indexVerifyCmd := &cobra.Command{
		Use:   "verify <index>",
		Short: "Verify a component extension index for local or CI publication gates",
		Args:  cobra.ExactArgs(1),
		RunE:  runComponentIndexVerify,
	}
	addComponentJSONFlag(indexVerifyCmd)
	indexCmd.AddCommand(indexVerifyCmd)
	componentCmd.AddCommand(indexCmd)
}

func runComponentIndexVerify(cmd *cobra.Command, args []string) error {
	index, err := loadComponentIndex(args[0])
	if err != nil {
		return renderComponentError(cmd, "index verify", err)
	}
	report := componentindex.Validate(index)
	if !report.Valid {
		return renderComponentIndexReport(cmd, index, report, componentIndexVerificationFailure(report))
	}
	return renderComponentIndexReport(cmd, index, report, nil)
}

func loadComponentIndex(path string) (componentindex.Index, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return componentindex.Index{}, component.NewError(component.ErrorCodeInvalidManifest, "read component index failed")
	}
	var index componentindex.Index
	if err := yaml.Unmarshal(raw, &index); err != nil {
		return componentindex.Index{}, component.Errorf(component.ErrorCodeInvalidManifest, "decode component index: %v", err)
	}
	return index, nil
}

func renderComponentIndexReport(
	cmd *cobra.Command,
	index componentindex.Index,
	report componentindex.Report,
	err error,
) error {
	if componentJSONEnabled(cmd) {
		status := "verified"
		if err != nil {
			status = "failed"
		}
		payload := newComponentCLIOutput("index verify", status)
		payload.IndexReport = &report
		if err != nil {
			payload.Error = componentErrorPayload(err)
		}
		if writeErr := writeComponentJSON(cmd.OutOrStdout(), payload); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "component index verification failed with %d issues\n", len(report.Issues)); writeErr != nil {
			return writeErr
		}
		for _, issue := range report.Issues {
			if _, writeErr := fmt.Fprintf(
				cmd.OutOrStdout(),
				"%s\t%s\t%s\t%s\n",
				issue.Code,
				issue.ComponentID,
				issue.Field,
				issue.Message,
			); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	_, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "verified component index with %d entries\n", len(index.Entries))
	return writeErr
}

func componentIndexVerificationFailure(report componentindex.Report) error {
	return component.Errorf(component.ErrorCodeInvalidManifest, "component index verification failed: %d issue(s)", len(report.Issues))
}
