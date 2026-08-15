// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// RunExtractionReadiness reports the advisory collector extraction readiness
// checklist. With no argument it lists every collector family the extraction
// policy tracks; with a family argument it explains that single family's
// per-criterion checklist. The output is informational: it never moves code
// or changes runtime behavior.
func RunExtractionReadiness(w io.Writer, jsonOutput bool, verbose bool, args []string) error {
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

	if jsonOutput {
		return writeExtractionReadinessJSON(w, rows)
	}
	return renderExtractionReadiness(w, rows, verbose)
}

// writeExtractionReadinessJSON keeps its own encoder rather than using this
// package's writeJSON: the readiness surface has always HTML-escaped its
// output (the encoder default), and switching writers would change those
// bytes.
func writeExtractionReadinessJSON(w io.Writer, rows []extraction.Readiness) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"collector_extraction_readiness": rows}) //nolint:wrapcheck // the encode error is the operator-facing text of a failed write; a wrap would change it
}

func renderExtractionReadiness(w io.Writer, rows []extraction.Readiness, verbose bool) error {
	if err := writef(w, "Collector extraction readiness (advisory; does not move code)\n"); err != nil {
		return err
	}
	for _, row := range rows {
		name := row.DisplayName
		if name == "" {
			name = string(row.Family)
		}
		if err := writef(w, "\n%s [%s] %s\n", row.Family, row.Classification, name); err != nil {
			return err
		}
		if row.Rationale != "" {
			if err := writef(w, "  %s\n", row.Rationale); err != nil {
				return err
			}
		}
		if len(row.Blockers) > 0 {
			if err := writef(w, "  blockers: %s\n", strings.Join(criterionNames(row.Blockers), ", ")); err != nil {
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
		if err := writef(w, "%s\n", line); err != nil {
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
