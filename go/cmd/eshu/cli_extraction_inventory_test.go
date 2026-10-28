// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"
	"testing"
)

// TestExtractionInventoryCoversTopLevelCommands pins the #8874 contract
// inventory to the real cobra tree: every visible top-level command must have
// a classified row in cli_extraction_inventory.md, so a new command cannot
// slip in unclassified and silently miss the eshu-cli cutover.
func TestExtractionInventoryCoversTopLevelCommands(t *testing.T) {
	t.Parallel()
	lockCommandTree(t)

	raw, err := os.ReadFile("cli_extraction_inventory.md")
	if err != nil {
		t.Fatalf("ReadFile(cli_extraction_inventory.md) error = %v", err)
	}
	doc := string(raw)

	var missing []string
	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden {
			continue
		}
		name := cmd.Name()
		if name == "help" || name == "completion" {
			continue
		}
		if !strings.Contains(doc, "`"+name+"`") {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("inventory missing top-level commands: %v", missing)
	}
}

// TestExtractionInventoryMarksCoreOnlyAdmin pins the plan's core-only rule:
// the admin family directly operates core-owned schema and recovery state, so
// it must be classified RETAIN, never EXTRACT.
func TestExtractionInventoryMarksCoreOnlyAdmin(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("cli_extraction_inventory.md")
	if err != nil {
		t.Fatalf("ReadFile(cli_extraction_inventory.md) error = %v", err)
	}
	row := inventoryRow(string(raw), "admin")
	if !strings.Contains(row, "RETAIN") {
		t.Fatalf("admin row = %q, want RETAIN classification", row)
	}
}

// TestExtractionInventoryRecordsContracts pins the supporting surfaces the
// cutover must preserve: the shared client flags, the exit-code table, and
// the envelope env vars.
func TestExtractionInventoryRecordsContracts(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("cli_extraction_inventory.md")
	if err != nil {
		t.Fatalf("ReadFile(cli_extraction_inventory.md) error = %v", err)
	}
	doc := string(raw)
	for _, want := range []string{
		"--service-url", "--api-key", "--profile",
		"ESHU_SERVICE_URL", "ESHU_API_KEY",
		"traceExitCode", "commandExitError",
		"GetEnvelope", "application/eshu.envelope+json",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("inventory missing contract %q", want)
		}
	}
}

func inventoryRow(doc, name string) string {
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| `"+name+"`") {
			return line
		}
	}
	return ""
}
