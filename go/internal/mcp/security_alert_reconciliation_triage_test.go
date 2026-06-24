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
