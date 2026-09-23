// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// BackendOverride pins the rows one backend returns today for a read case
// whose correct answer, ReadCase.WantRows, that backend does not give.
//
// An override is a known defect written down, not a second correct answer.
// The case stays deterministic on both lanes: the backend with the override
// must return exactly the pinned rows, and every other backend must return
// WantRows. When the defect is fixed, the overridden backend starts returning
// WantRows, fails its pinned rows, and the override must be deleted in the fix.
type BackendOverride struct {
	// Divergence names the issue that tracks the defect, as "#NNNN",
	// optionally followed by a short description. It is required.
	Divergence string
	// WantRows are the exact rows the backend returns today, compared the
	// same way as ReadCase.WantRows. It must be non-nil and differ from the
	// case's WantRows.
	WantRows []map[string]any
}

// divergenceIssuePattern matches an issue reference such as "#6968".
var divergenceIssuePattern = regexp.MustCompile(`(^|\s)#[0-9]+\b`)

// RunReadCorpusFor runs read cases against graph as backend and returns a
// report with row counts per case. A case with an override for backend is
// held to the override's rows; every other exact-row case to its WantRows.
// An empty backend applies no override. Every case is validated before any
// query runs, so an invalid override cannot let earlier cases pass silently.
func RunReadCorpusFor(ctx context.Context, graph GraphQuery, backend BackendID, cases []ReadCase) (Report, error) {
	if graph == nil {
		return Report{}, fmt.Errorf("graph query is required")
	}
	if len(cases) == 0 {
		return Report{}, fmt.Errorf("read corpus must include at least one case")
	}
	for _, tc := range cases {
		if err := validateReadCase(tc); err != nil {
			return Report{}, err
		}
	}

	report := Report{Results: make([]CaseResult, 0, len(cases))}
	for _, tc := range cases {
		caseCtx, cancel := context.WithTimeout(ctx, corpusTimeout)
		rows, err := graph.Run(caseCtx, tc.Cypher, tc.Parameters)
		cancel()
		if err != nil {
			return Report{}, fmt.Errorf("read case %q: %w", tc.Name, err)
		}
		if tc.MinRows > 0 && len(rows) < tc.MinRows {
			return Report{}, fmt.Errorf("read case %q returned %d rows, want at least %d", tc.Name, len(rows), tc.MinRows)
		}
		if err := checkReadRows(tc, backend, rows); err != nil {
			return Report{}, err
		}
		result := CaseResult{
			Name:       tc.Name,
			Capability: tc.Capability,
			Rows:       len(rows),
		}
		if override, ok := tc.Overrides[backend]; ok && backend != "" {
			result.Divergence = override.Divergence
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

// checkReadRows compares rows with the expectation that applies to backend.
// The error names the backend and, for an override, its tracking issue and the
// correct rows; compareReadRows adds the returned rows, so a CI log alone is
// enough to correct a pinned expectation.
func checkReadRows(tc ReadCase, backend BackendID, rows []map[string]any) error {
	if tc.WantRows == nil {
		return nil
	}
	if override, ok := tc.Overrides[backend]; ok && backend != "" {
		if err := compareReadRows(rows, override.WantRows); err != nil {
			correct, _ := normalizedRows(tc.WantRows)
			return fmt.Errorf("read case %q on %s (pinned divergence %s; correct rows (%d): %s): %w",
				tc.Name, backend, override.Divergence, len(correct), strings.Join(correct, " "), err)
		}
		return nil
	}
	if err := compareReadRows(rows, tc.WantRows); err != nil {
		if backend == "" {
			return fmt.Errorf("read case %q: %w", tc.Name, err)
		}
		return fmt.Errorf("read case %q on %s: %w", tc.Name, backend, err)
	}
	return nil
}

// validateReadCaseOverrides rejects overrides that would hide a check: one
// with no tracking issue, nil rows (which disable the comparison), rows equal
// to the correct rows, an unknown backend, or no correct rows to diverge from.
func validateReadCaseOverrides(tc ReadCase) error {
	if len(tc.Overrides) == 0 {
		return nil
	}
	if tc.WantRows == nil {
		return fmt.Errorf("read case %q has backend overrides but no WantRows to diverge from", tc.Name)
	}
	correct, err := normalizedRows(tc.WantRows)
	if err != nil {
		return fmt.Errorf("read case %q: %w", tc.Name, err)
	}
	for backend, override := range tc.Overrides {
		if backend != BackendNeo4j && backend != BackendNornicDB {
			return fmt.Errorf("read case %q overrides unknown backend %q", tc.Name, backend)
		}
		if !divergenceIssuePattern.MatchString(strings.TrimSpace(override.Divergence)) {
			return fmt.Errorf("read case %q override for %s must name its tracking issue as #NNNN in Divergence, got %q",
				tc.Name, backend, override.Divergence)
		}
		if override.WantRows == nil {
			return fmt.Errorf("read case %q override for %s (%s) has nil WantRows", tc.Name, backend, override.Divergence)
		}
		pinned, err := normalizedRows(override.WantRows)
		if err != nil {
			return fmt.Errorf("read case %q override for %s: %w", tc.Name, backend, err)
		}
		if slices.Equal(pinned, correct) {
			return fmt.Errorf("read case %q override for %s (%s) equals the correct WantRows; delete it",
				tc.Name, backend, override.Divergence)
		}
	}
	return nil
}
