// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparity"
	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof"
	"github.com/spf13/cobra"
)

func TestVulnScanProviderParityCommandIsRegisteredWithPrivateSafeFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"vuln-scan", "provider-parity"})
	if err != nil {
		t.Fatalf("rootCmd.Find(vuln-scan provider-parity) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "provider-parity" {
		t.Fatalf("root command = %#v, want vuln-scan provider-parity command", cmd)
	}
	for _, name := range []string{
		"allowlist-file",
		"provider",
		"provider-alerts-file",
		"provider-token-env",
		"supported-ecosystem",
		"limit",
		"json",
		"service-url",
		"api-key",
		"profile",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("vuln-scan provider-parity flag %q missing", name)
		}
	}
}

func TestRunVulnScanProviderParityOutputsAggregateOnlyJSON(t *testing.T) {
	tempDir := t.TempDir()
	allowlistPath := filepath.Join(tempDir, "allowlist.json")
	if err := os.WriteFile(
		allowlistPath,
		[]byte(`{"repositories":[{"provider_repository":"synthetic-owner/synthetic-repo","eshu_repository_id":"repo-synthetic-local"}]}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(allowlist) error = %v, want nil", err)
	}
	providerAlertsPath := filepath.Join(tempDir, "provider-alerts.json")
	if err := os.WriteFile(
		providerAlertsPath,
		[]byte(`{"alerts":[{"repository":"synthetic-owner/synthetic-repo","provider_id":"provider-alert-1","advisory_id":"GHSA-synthetic-local-0001","cve_id":"CVE-2026-SYNTHETIC-0001","ecosystem":"npm","package_name":"synthetic-sensitive-package","package_id":"npm:synthetic-sensitive-package","status":"open","required_evidence":["dependency","advisory"]}]}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(provider alerts) error = %v, want nil", err)
	}

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/supply-chain/impact/findings" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":{"findings":[{"finding_id":"finding-synthetic","advisory_id":"GHSA-synthetic-local-0001","cve_id":"CVE-2026-SYNTHETIC-0001","ecosystem":"npm","package_name":"synthetic-sensitive-package","package_id":"npm:synthetic-sensitive-package","impact_status":"affected_exact","repository_id":"repo-synthetic-local"}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","freshness":"fresh","evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":1,"freshness":"fresh"}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanProviderParityCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("allowlist-file", allowlistPath); err != nil {
		t.Fatalf("Set(allowlist-file) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("provider-alerts-file", providerAlertsPath); err != nil {
		t.Fatalf("Set(provider-alerts-file) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("supported-ecosystem", "npm"); err != nil {
		t.Fatalf("Set(supported-ecosystem) error = %v, want nil", err)
	}

	if err := runVulnScanProviderParity(cmd, nil); err != nil {
		t.Fatalf("runVulnScanProviderParity() error = %v, want nil", err)
	}
	if !strings.Contains(gotQuery, "repository_id=repo-synthetic-local") {
		t.Fatalf("impact query = %q, want repository_id=repo-synthetic-local", gotQuery)
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if payload["error"] != nil {
		t.Fatalf("payload[error] = %#v, want nil", payload["error"])
	}
	data := payload["data"].(map[string]any)
	counts := classCountsFromWire(t, data["counts"])
	if got, want := counts[string(vulnerabilityparityproof.ProviderParityClassMatched)], 1; got != want {
		t.Fatalf("matched count = %d, want %d (counts=%v)", got, want, counts)
	}
	if got, want := data["readiness_state"], "ready_with_findings"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	if got, want := data["freshness_state"], "fresh"; got != want {
		t.Fatalf("data[freshness_state] = %#v, want %#v", got, want)
	}
	mismatchCounts := classCountsFromWire(t, data["mismatch_classes"])
	for _, forbiddenClass := range []string{
		string(vulnerabilityparity.ClassMissingAdvisoryEvidence),
		string(vulnerabilityparity.ClassMissingDependencyEvidence),
		string(vulnerabilityparity.ClassReducerBugCandidate),
		string(vulnerabilityparity.ClassFixedDismissedMismatch),
	} {
		if _, ok := mismatchCounts[forbiddenClass]; ok {
			t.Fatalf("mismatch_classes included internal classifier %q: %v", forbiddenClass, mismatchCounts)
		}
	}
	for _, forbidden := range []string{
		"synthetic-owner",
		"synthetic-repo",
		"repo-synthetic-local",
		"synthetic-sensitive-package",
		"provider-alert",
		"GHSA-synthetic-local",
		"CVE-2026-SYNTHETIC",
		"finding-synthetic",
	} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("provider parity output leaked %q: %s", forbidden, out.String())
		}
	}
}

func TestRenderProviderParitySummaryReportsMismatchRowCount(t *testing.T) {
	data := map[string]any{
		"repositories_checked": 1,
		"provider_alert_count": 4,
		"eshu_finding_count":   3,
		"mismatch_classes": []vulnerabilityparityproof.ClassCount{
			{Class: string(vulnerabilityparity.ClassEshuOnly), Count: 1},
			{Class: string(vulnerabilityparity.ClassProviderOnly), Count: 2},
		},
	}
	out := &bytes.Buffer{}
	if err := renderProviderParitySummary(out, data); err != nil {
		t.Fatalf("renderProviderParitySummary returned error: %v", err)
	}
	if got, want := out.String(), "Provider parity: repositories=1 provider_alerts=4 eshu_findings=3 mismatches=3\n"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func classCountsFromWire(t *testing.T, raw any) map[string]int {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("counts = %T, want []any", raw)
	}
	out := make(map[string]int, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("count row = %T, want map[string]any", item)
		}
		class, _ := row["class"].(string)
		count, _ := row["count"].(float64)
		out[class] = int(count)
	}
	return out
}

func newTestVulnScanProviderParityCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addVulnScanProviderParityFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}
