// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// TestClaimedSourceTruncatedOrgReadWithNoSameOrgRepositoriesIsNotAFanout
// proves a truncated org read whose allowlist holds no repository of the
// target organization (validation permits other-account entries) is not a
// fan-out: nothing is read per repository, the freshness digest stays the one
// a truncated single read produces (truncated=true, no fan-out outcomes), so it
// cannot collide with a genuinely complete read of the same window, and no
// fan-out counter or span attribute is emitted. The org read's own truncation
// still counts on the truncated-reads metric.
func TestClaimedSourceTruncatedOrgReadWithNoSameOrgRepositoriesIsNotAFanout(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	org := securityalerts.GitHubDependabotAlertResult{
		Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")},
		PagesFetched: 3,
		Truncated:    true,
	}
	client := &fanoutClient{org: org}
	envs, got := collectOrgFacts(t, newTracedFanoutSource(
		t, client, []string{"other-org/outside-repo"}, 3, instruments, provider.Tracer("test")))

	if len(client.repoCalls) != 0 {
		t.Fatalf("per-repository calls = %v, want none: no allowlisted repository belongs to the organization", client.repoCalls)
	}
	if len(envs) != 0 {
		t.Fatalf("len(facts) = %d, want 0: the window's only alert is not allowlisted", len(envs))
	}
	// The digest of the same window read without a fan-out: the org window's
	// alerts, truncated, no outcomes. A complete read of the same window must differ from it.
	want := securityAlertFreshnessHint(securityAlertFreshnessInput{alerts: org.Alerts, pagesFetched: 3, truncated: true, orgReadState: true})
	if got != want {
		t.Fatalf("freshness hint = %q, want the truncated single-read digest %q", got, want)
	}
	complete := securityAlertFreshnessHint(securityAlertFreshnessInput{alerts: org.Alerts, pagesFetched: 3, orgReadState: true})
	if got == complete {
		t.Fatal("freshness hint equals a complete read's digest, want the truncation in the digest")
	}

	truncated := "eshu_dp_security_alert_truncated_reads_total"
	if n := counterValue(t, reader, truncated, map[string]string{"operation": "list_org_alerts"}); n != 1 {
		t.Fatalf("%s{operation=list_org_alerts} = %d, want 1", truncated, n)
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	for _, outcome := range []string{
		contract.SecurityAlertFanoutOutcomeComplete, contract.SecurityAlertFanoutOutcomeTruncated,
		contract.SecurityAlertFanoutOutcomeFailed, contract.SecurityAlertFanoutOutcomeAlertsDisabled,
		contract.SecurityAlertFanoutOutcomeRateLimited, contract.SecurityAlertFanoutOutcomeStopped,
		contract.SecurityAlertFanoutOutcomeSkippedCap,
	} {
		if n := counterValue(t, reader, fanout, map[string]string{"outcome": outcome}); n != 0 {
			t.Fatalf("%s{outcome=%s} = %d, want 0 for a claim that fanned out nothing", fanout, outcome, n)
		}
	}
	for key := range spanAttributes(t, recorder) {
		switch key {
		case contract.AttrSecurityAlertFanoutRepositories, contract.AttrSecurityAlertFanoutRequested,
			contract.AttrSecurityAlertFanoutIncomplete, contract.AttrSecurityAlertFanoutRateLimited,
			contract.AttrSecurityAlertFanoutStopped:
			t.Fatalf("span carries %s, want no org_fanout attributes when nothing fanned out", key)
		}
	}
}
