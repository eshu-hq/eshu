// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// staticScreenBenchEnvelopes builds one authorized load's static envelopes: mostly
// package and consumption facts, a few Go evidence facts, go_reachability warnings
// in a go-reachability scope, and image-analyzer warnings.
func staticScreenBenchEnvelopes() []facts.Envelope {
	out := make([]facts.Envelope, 0, 1000)
	for i := 0; i < 1000; i++ {
		switch i % 10 {
		case 0:
			out = append(out, facts.Envelope{
				FactID: fmt.Sprintf("warn-go-%d", i), FactKind: facts.ScannerWorkerWarningFactKind,
				ScopeID: "go-reachability://repository:r_ea78e8bb", Payload: map[string]any{"analyzer": "go_reachability", "target_kind": "repository", "target_locator_hash": "sha256:510d18e9994bab03f03457cb59d2cc94b0bb2084d1ae8f8aedb69b6425b5b776", "reason": "toolchain_unsupported", "warning_class": "scanner_worker_warning", "analysis_status": "not_scanned", "coverage_status": "unsupported"},
			})
		case 1:
			out = append(out, facts.Envelope{
				FactID: fmt.Sprintf("warn-img-%d", i), FactKind: facts.ScannerWorkerWarningFactKind,
				ScopeID: "scanner-worker://repository/repo-123", Payload: map[string]any{"analyzer": "trivy", "target_kind": "repository", "target_locator_hash": "sha256:510d18e9994bab03f03457cb59d2cc94b0bb2084d1ae8f8aedb69b6425b5b776", "reason": "toolchain_unsupported", "warning_class": "scanner_worker_warning", "analysis_status": "not_scanned", "coverage_status": "unsupported"},
			})
		case 2:
			out = append(out, facts.Envelope{
				FactID: fmt.Sprintf("go-mod-%d", i), FactKind: "vulnerability.go_module_evidence",
				ScopeID: "go-reachability://repository:r_ea78e8bb",
			})
		default:
			out = append(out, facts.Envelope{
				FactID: fmt.Sprintf("pkg-%d", i), FactKind: "reducer_package_consumption_correlation",
				ScopeID: "vulnerability_intelligence:orders-api:go-alias",
			})
		}
	}
	return out
}

// BenchmarkStaticEnvelopeScreen runs the engaged loader's static-envelope screen
// over one 1,000-envelope load. It measures the per-envelope predicate only and
// fails if the screen stops failing closed on exactly the image-analyzer warnings.
func BenchmarkStaticEnvelopeScreen(b *testing.B) {
	envelopes := staticScreenBenchEnvelopes()
	b.ReportAllocs()
	b.ResetTimer()
	failClosed := 0
	for i := 0; i < b.N; i++ {
		failClosed = 0
		for _, envelope := range envelopes {
			if staticEnvelopeNeedsScanReceipt(envelope) {
				failClosed++
			}
		}
	}
	// 10 percent of the input is image-analyzer warnings, the only envelopes that
	// still fail closed on this branch (a go_reachability warning in a
	// go-reachability scope passes as static Go evidence).
	if failClosed != 100 {
		b.Fatalf("fail-closed envelopes = %d, want 100 (the image-analyzer warnings)", failClosed)
	}
}
