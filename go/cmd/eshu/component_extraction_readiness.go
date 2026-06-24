// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const componentExtractionReadinessVerboseFlag = "verbose"

func init() {
	cmd := &cobra.Command{
		Use:   "extraction-readiness [collector-family]",
		Short: "Explain whether a collector is keep-in-tree, an extraction candidate, blocked, or external-ready",
		Long: "Report the advisory collector extraction readiness checklist. The output is " +
			"informational: it never moves code or changes runtime behavior. With no argument it " +
			"lists every collector family the extraction policy tracks; with a family argument it " +
			"explains that single family's per-criterion checklist.",
		Args: cobra.MaximumNArgs(1),
		RunE: runComponentExtractionReadiness,
	}
	cmd.Flags().Bool(componentJSONFlag, false, "Emit machine-readable JSON")
	cmd.Flags().Bool(componentExtractionReadinessVerboseFlag, false, "Show every criterion, not just blockers")
	componentCmd.AddCommand(cmd)
}

func runComponentExtractionReadiness(cmd *cobra.Command, args []string) error {
	asJSON, err := cmd.Flags().GetBool(componentJSONFlag)
	if err != nil {
		return err
	}
	verbose, err := cmd.Flags().GetBool(componentExtractionReadinessVerboseFlag)
	if err != nil {
		return err
	}

	var rows []extraction.Readiness
	if len(args) == 1 {
		family := scope.CollectorKind(strings.TrimSpace(args[0]))
		row, ok := extraction.Lookup(family)
		if !ok {
			return fmt.Errorf("collector family %q is not tracked by the extraction policy; run without an argument to list tracked families", args[0])
		}
		rows = []extraction.Readiness{row}
	} else {
		rows = extraction.Catalog()
	}

	out := cmd.OutOrStdout()
	if asJSON {
		return writeExtractionReadinessJSON(out, rows)
	}
	return renderExtractionReadiness(out, rows, verbose)
}

func writeExtractionReadinessJSON(w io.Writer, rows []extraction.Readiness) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"collector_extraction_readiness": rows})
}

func renderExtractionReadiness(w io.Writer, rows []extraction.Readiness, verbose bool) error {
	if _, err := fmt.Fprintln(w, "Collector extraction readiness (advisory; does not move code)"); err != nil {
		return err
	}
	for _, row := range rows {
		name := row.DisplayName
		if name == "" {
			name = string(row.Family)
		}
		if _, err := fmt.Fprintf(w, "\n%s [%s] %s\n", row.Family, row.Classification, name); err != nil {
			return err
		}
		if row.Rationale != "" {
			if _, err := fmt.Fprintf(w, "  %s\n", row.Rationale); err != nil {
				return err
			}
		}
		if len(row.Blockers) > 0 {
			if _, err := fmt.Fprintf(w, "  blockers: %s\n", strings.Join(criterionNames(row.Blockers), ", ")); err != nil {
				return err
			}
		}
		if verbose {
			if err := renderExtractionCriteria(w, row.Criteria); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderExtractionCriteria(w io.Writer, criteria []extraction.CriterionResult) error {
	for _, criterion := range criteria {
		line := fmt.Sprintf("  - %s: %s", criterion.Criterion, criterion.State)
		if criterion.Detail != "" {
			line += " (" + criterion.Detail + ")"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// criterionNames returns the criterion identifiers in the order they appear in
// the result slice. Blockers already arrive in canonical policy order, so the
// rendered list lines up with the extraction-criteria table.
func criterionNames(results []extraction.CriterionResult) []string {
	names := make([]string, 0, len(results))
	for _, result := range results {
		names = append(names, string(result.Criterion))
	}
	return names
}
