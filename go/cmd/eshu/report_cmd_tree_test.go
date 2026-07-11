// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestRootCommandTree_ReportSubtreeIsSingleAndComplete builds against the REAL
// rootCmd wiring (the actual init()-assembled command tree the eshu binary
// runs) and asserts that:
//
//  1. exactly ONE top-level command is named "report" — a second root-level
//     `report` would silently shadow the first in cobra's name lookup, making
//     one of the two features unreachable at runtime;
//  2. `report capture` and `report validate` (this feature) are reachable
//     through that single report command; and
//  3. the pre-existing operator-digest report behavior is still reachable —
//     the report command still carries its --scope flag and a RunE.
//
// Direct-instantiation command tests never exercise the real root tree, so
// they cannot catch a duplicate root registration; this test can.
func TestRootCommandTree_ReportSubtreeIsSingleAndComplete(t *testing.T) {
	reportCommands := make([]*cobra.Command, 0, 1)
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "report" {
			reportCommands = append(reportCommands, cmd)
		}
	}
	if len(reportCommands) != 1 {
		t.Fatalf("rootCmd has %d top-level commands named \"report\", want exactly 1 (a second registration shadows the first)", len(reportCommands))
	}
	report := reportCommands[0]

	// (2) report capture / report validate are reachable through the tree.
	for _, sub := range []string{"capture", "validate"} {
		found, _, err := rootCmd.Find([]string{"report", sub})
		if err != nil {
			t.Fatalf("rootCmd.Find([report %s]) error = %v", sub, err)
		}
		if found == nil || found.Name() != sub {
			t.Fatalf("report %s is not reachable through the root command tree (resolved to %v)", sub, found)
		}
	}

	// (3) the pre-existing operator-digest report is still reachable: the same
	// report command still owns its --scope flag and can run on its own.
	if report.Flags().Lookup("scope") == nil {
		t.Fatalf("report command lost its --scope flag; the operator-digest report path is no longer reachable")
	}
	if report.RunE == nil {
		t.Fatalf("report command has no RunE; the operator-digest report path is no longer runnable as `eshu report`")
	}
}
