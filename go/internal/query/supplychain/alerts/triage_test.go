// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

func TestDecodeSecurityAlertReconciliationRowPreservesTriageDetails(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": float64(42),
		"provider_state":        "open",
		"repository_id":         "repo://github/example-org/payments-api",
		"package_id":            "npm://registry.npmjs.org/no-owned-evidence",
		"reconciliation_status": "provider_only",
		"reason":                "provider alert has no matching owned dependency evidence",
		"reason_code":           "owned_dependency_missing",
		"missing_evidence": []any{
			map[string]any{
				"kind":   "owned_dependency",
				"reason": "no_owned_dependency_evidence",
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeRow("reconciliation-42", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeRow() error = %v, want nil", err)
	}
	if got, want := row.ReasonCode, "owned_dependency_missing"; got != want {
		t.Fatalf("ReasonCode = %q, want %q", got, want)
	}
	wantMissing := []supplychain.SecurityAlertMissingEvidence{{
		Kind:   "owned_dependency",
		Reason: "no_owned_dependency_evidence",
	}}
	if !reflect.DeepEqual(row.MissingEvidence, wantMissing) {
		t.Fatalf("MissingEvidence = %#v, want %#v", row.MissingEvidence, wantMissing)
	}
}
