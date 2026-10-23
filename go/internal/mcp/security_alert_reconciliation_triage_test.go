// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestSecurityAlertReconciliationToolAdvertisesTriageFields(t *testing.T) {
	t.Parallel()

	var description string
	for _, tool := range supplyChainTools() {
		if tool.Name == "list_security_alert_reconciliations" {
			description = tool.Description
			break
		}
	}
	if description == "" {
		t.Fatal("list_security_alert_reconciliations tool not found")
	}
	for _, want := range []string{"reason_code", "missing_evidence", "unsupported", "ambiguous"} {
		if !strings.Contains(description, want) {
			t.Fatalf("tool description missing %q: %s", want, description)
		}
	}
}

// TestSecurityAlertToolsAdvertiseEmptyAnswerCoverage keeps the list, count,
// and inventory tool descriptions in step with #8803: an agent must learn from
// the tool itself that an empty answer carries coverage.reasons, that an
// unanchored complete is target health only, that a grantless token gets
// no_authorized_repositories, and that a zero is clean only with no pending or
// dead-lettered reducer work, instead of reading zero rows as a clean result.
func TestSecurityAlertToolsAdvertiseEmptyAnswerCoverage(t *testing.T) {
	t.Parallel()

	descriptions := map[string]string{}
	for _, tool := range ReadOnlyTools() {
		descriptions[tool.Name] = tool.Description
	}
	for _, name := range []string{
		"list_security_alert_reconciliations",
		"count_security_alert_reconciliations",
		"get_security_alert_reconciliation_inventory",
	} {
		description, ok := descriptions[name]
		if !ok {
			t.Fatalf("%s tool not found", name)
		}
		for _, want := range []string{
			"coverage.reasons",
			"empty",
			// D3: a zero is clean only with no pending reducer work.
			"A zero-row answer is clean only when coverage.state is complete and the reducer's security_alert_reconciliation domain has no pending or dead-lettered work for the scope (get_freshness_causality).",
			// D1: unanchored complete is target health only.
			"anchor by repository_id",
			// F1: a grantless token gets its own reason.
			"no_authorized_repositories",
		} {
			if !strings.Contains(description, want) {
				t.Fatalf("%s description missing %q: %s", name, want, description)
			}
		}
	}
}
