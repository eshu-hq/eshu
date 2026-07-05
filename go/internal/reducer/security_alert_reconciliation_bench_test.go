// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkSecurityAlertEnvelopes builds n realistic security_alert.repository_alert
// envelopes carrying the full field set the GitHub Dependabot / alert-runtime
// collectors emit (provider identity, advisory ids, CVSS/EPSS/CWE containers,
// and the collection-coverage fields), so the benchmark exercises the same
// container-normalization work the hot extractor does per fact.
func benchmarkSecurityAlertEnvelopes(n int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, n)
	for i := 0; i < n; i++ {
		repoID := fmt.Sprintf("repo://github/acme/service-%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:        fmt.Sprintf("alert-%d", i),
			ScopeID:       repoID,
			GenerationID:  "generation-bench",
			FactKind:      facts.SecurityAlertRepositoryAlertFactKind,
			SchemaVersion: facts.SecurityAlertSchemaVersionV1,
			Payload: map[string]any{
				"repository_id":         repoID,
				"provider":              "github_dependabot",
				"provider_alert_id":     fmt.Sprintf("github_dependabot:%s:%d", repoID, i),
				"provider_alert_number": int64(i),
				"provider_state":        "open",
				"repository_name":       fmt.Sprintf("service-%d", i),
				"package_id":            fmt.Sprintf("npm://registry.npmjs.org/pkg-%d", i),
				"ecosystem":             "npm",
				"package_name":          fmt.Sprintf("pkg-%d", i),
				"manifest_path":         "package-lock.json",
				"dependency_scope":      "runtime",
				"relationship":          "direct",
				"ghsa_ids":              []any{fmt.Sprintf("GHSA-bench-%04d", i)},
				"cve_ids":               []any{fmt.Sprintf("CVE-2026-%04d", i)},
				"vulnerable_range":      "< 1.3.0",
				"patched_version":       "1.3.0",
				"severity":              "high",
				"cvss": map[string]any{
					"score":  7.5,
					"vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H",
				},
				"epss": map[string]any{
					"percentage": "0.0123",
					"percentile": "0.456",
				},
				"cwes": []any{
					map[string]any{"cwe_id": "CWE-400", "name": "Uncontrolled Resource Consumption"},
				},
				"summary":                       "Denial of service",
				"source_url":                    "https://github.com/advisories/GHSA-bench",
				"created_at":                    "2026-05-20T09:00:00Z",
				"updated_at":                    "2026-05-23T10:15:00Z",
				"source_freshness":              "partial",
				"collection_coverage_state":     "incomplete",
				"collection_truncated":          true,
				"collection_pages_fetched":      int64(2),
				"collection_state_filter":       "open",
				"collection_incomplete_reasons": []any{"provider_open_alert_page_limit_reached"},
			},
		})
	}
	return envelopes
}

// BenchmarkExtractProviderSecurityAlerts measures the typed-decode extraction
// path on the touched hot extractor. It is the no-regression benchmark for the
// Wave 4e typed-payload migration: run it on this branch and against the
// pre-typing raw-map read (via the TDD shim) on the same corpus to confirm the
// typed path stays within the diagnostic-rigor ~10% band.
func BenchmarkExtractProviderSecurityAlerts(b *testing.B) {
	envelopes := benchmarkSecurityAlertEnvelopes(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alerts, quarantined, err := extractProviderSecurityAlertsWithQuarantine(envelopes)
		if err != nil {
			b.Fatalf("extract error = %v", err)
		}
		if len(alerts) != len(envelopes) || len(quarantined) != 0 {
			b.Fatalf("extract produced alerts=%d quarantined=%d, want alerts=%d quarantined=0", len(alerts), len(quarantined), len(envelopes))
		}
	}
}
