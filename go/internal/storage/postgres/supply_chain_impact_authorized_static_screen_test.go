// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestStaticEnvelopeNeedsScanReceiptKeepsGoEvidenceStatic pins the #10030
// protection-screen classification. Image-analyzer scan output is target-only and
// fails closed without a receipt. A go_reachability warning in a go-reachability
// scope belongs with the Go evidence the same scanner emits (module evidence and
// call reachability pass as static non-advisory evidence), so it does not.
func TestStaticEnvelopeNeedsScanReceiptKeepsGoEvidenceStatic(t *testing.T) {
	t.Parallel()

	warning := func(scopeID, analyzer string) facts.Envelope {
		return facts.Envelope{
			FactID:   "warning-" + scopeID,
			ScopeID:  scopeID,
			FactKind: facts.ScannerWorkerWarningFactKind,
			Payload:  map[string]any{"analyzer": analyzer, "repository_id": "repository:r_ea78e8bb"},
		}
	}
	cases := []struct {
		name     string
		envelope facts.Envelope
		want     bool
	}{
		{"go_reachability warning in its go-reachability scope", warning("go-reachability://repository:r_ea78e8bb", "go_reachability"), false},
		{"go_reachability warning in a non-canonical go-reachability scope", warning("go-reachability://https://example.test/repo", "go_reachability"), true},
		{"go_reachability warning in an image scope", warning("scanner-worker://repository/repo-123", "go_reachability"), true},
		{"image analyzer warning in a go-reachability scope", warning("go-reachability://repository:r_ea78e8bb", "trivy"), true},
		{"image analyzer warning in its scan scope", warning("scanner-worker://repository/repo-123", "trivy"), true},
		{"scanner analysis fact", facts.Envelope{FactKind: facts.ScannerWorkerAnalysisFactKind, ScopeID: "go-reachability://repository:r_ea78e8bb"}, true},
		{"os package fact", facts.Envelope{FactKind: "vulnerability.os_package"}, true},
		{"go module evidence is not target-only", facts.Envelope{FactKind: "vulnerability.go_module_evidence", ScopeID: "go-reachability://repository:r_ea78e8bb"}, false},
		{"file fact is not target-only", facts.Envelope{FactKind: "file"}, false},
	}
	for _, tc := range cases {
		if got := staticEnvelopeNeedsScanReceipt(tc.envelope); got != tc.want {
			t.Errorf("%s: staticEnvelopeNeedsScanReceipt() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
