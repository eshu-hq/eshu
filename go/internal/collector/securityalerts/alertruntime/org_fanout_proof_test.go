// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"fmt"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

type alertRef struct {
	repositoryID string
	number       int64
}

func alertRefs(t *testing.T, source *ClaimedSource) []alertRef {
	t.Helper()
	envs, _ := collectOrgFacts(t, source)
	refs := make([]alertRef, 0, len(envs))
	for _, env := range envs {
		refs = append(refs, alertRef{payloadString(env.Payload, "repository_id"), payloadInt(env.Payload, "provider_alert_number")})
	}
	return refs
}

// TestClaimedSourceFanoutCompleteRepositoryReplacesOrgWindowAlerts proves a
// repository whose own read completes replaces its org-window alerts: an alert
// that closed between the two reads is not emitted from the stale org window.
func TestClaimedSourceFanoutCompleteRepositoryReplacesOrgWindowAlerts(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts: []securityalerts.GitHubDependabotAlert{
				orgAlert(7, "example-org/alpha-repo"), // closed before alpha's own read
				orgAlert(3, "example-org/beta-repo"),
			},
			PagesFetched: 1,
			Truncated:    true,
		},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
			"example-org/beta-repo":  {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(4)}, PagesFetched: 1, Truncated: true},
		},
	}
	got := alertRefs(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 1, nil))

	want := []alertRef{
		{"security-alert:github:example-org/alpha-repo", 1}, // alpha#7 dropped: alpha's own read was complete
		{"security-alert:github:example-org/beta-repo", 3},  // beta's own read truncated: org-window alert kept
		{"security-alert:github:example-org/beta-repo", 4},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("facts = %v, want %v", got, want)
	}
}

// TestClaimedSourceFanoutFreshnessHintSeparatesOutcomeOnlyChanges proves the
// freshness hint moves when only the per-repository outcome changes: both
// claims below carry the same alerts (none), the same pages, and the same
// truncated flag, and differ only in which repository failed.
func TestClaimedSourceFanoutFreshnessHintSeparatesOutcomeOnlyChanges(t *testing.T) {
	t.Parallel()

	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo"}
	hint := func(failed string) string {
		client := &fanoutClient{
			org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
			repos: map[string]securityalerts.GitHubDependabotAlertResult{
				"example-org/alpha-repo": {},
				"example-org/beta-repo":  {},
			},
			repoErrs: map[string]error{failed: securityalerts.GitHubDependabotError{StatusCode: 404}},
		}
		envs, hint := collectOrgFacts(t, newFanoutSource(t, client, allowed, 1, nil))
		if len(envs) != 0 {
			t.Fatalf("len(facts) = %d, want 0 so only the outcome can move the hint", len(envs))
		}
		return hint
	}
	alphaFailed, betaFailed := hint("example-org/alpha-repo"), hint("example-org/beta-repo")
	if alphaFailed == "" || betaFailed == "" {
		t.Fatal("freshness hint is empty")
	}
	if alphaFailed == betaFailed {
		t.Fatal("freshness hint identical for alpha-failed and beta-failed, want the outcome in the digest")
	}
	if again := hint("example-org/alpha-repo"); again != alphaFailed {
		t.Fatalf("freshness hint = %q then %q for the same outcome, want stable", alphaFailed, again)
	}
}

// TestClaimedSourceFanoutFreshnessHintMatchesDigestInput pins the fan-out
// hint to the documented digest input: the merged alerts, every page fetched
// across the org and per-repository reads, the claim-level truncated flag, and
// the per-repository outcomes.
func TestClaimedSourceFanoutFreshnessHintMatchesDigestInput(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 2, Truncated: true},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
		},
		repoErrs: map[string]error{"example-org/beta-repo": securityalerts.GitHubDependabotError{StatusCode: 404}},
	}
	_, got := collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 2, nil))

	alpha := repoAlert(1)
	alpha.Repository = fanoutRepositoryObject("example-org/alpha-repo")
	want := securityAlertFreshnessHint(securityAlertFreshnessInput{
		alerts:       []securityalerts.GitHubDependabotAlert{alpha},
		pagesFetched: 3,
		truncated:    true,
		orgReadState: true,
		// The allowlist the fan-out source is configured with (#8838).
		targetSetHash: testTargetSetHash("example-org/alpha-repo", "example-org/beta-repo"),
		fanoutOutcomes: map[string]string{
			"example-org/alpha-repo": contract.SecurityAlertFanoutOutcomeComplete,
			"example-org/beta-repo":  contract.SecurityAlertFanoutOutcomeFailed,
		},
	})
	if got != want {
		t.Fatalf("freshness hint = %q, want %q built from the documented digest input", got, want)
	}
}

// TestClaimedSourceFanoutSpanCarriesFanoutAttributes proves the
// security_alert.observe span carries the five org_fanout attributes, on a
// rate-limited stop and on a capped fan-out.
func TestClaimedSourceFanoutSpanCarriesFanoutAttributes(t *testing.T) {
	t.Parallel()

	rateLimited := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
		repoErrs: map[string]error{
			"example-org/alpha-repo": securityalerts.GitHubDependabotError{
				StatusCode: 403,
				RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 0, RemainingKnown: true},
				Operation:  "list_repo_alerts",
			},
		},
	}
	capped := make([]string, 0, maxOrganizationFanoutRepositories+2)
	for i := range maxOrganizationFanoutRepositories + 2 {
		capped = append(capped, fmt.Sprintf("example-org/repo-%03d", i))
	}
	tests := []struct {
		name    string
		client  *fanoutClient
		allowed []string
		want    map[string]attribute.Value
	}{
		{
			name:    "rate limited stop",
			client:  rateLimited,
			allowed: []string{"example-org/alpha-repo", "example-org/beta-repo"},
			want: map[string]attribute.Value{
				contract.AttrSecurityAlertFanoutRepositories: attribute.IntValue(2),
				contract.AttrSecurityAlertFanoutRequested:    attribute.IntValue(1),
				contract.AttrSecurityAlertFanoutIncomplete:   attribute.IntValue(2),
				contract.AttrSecurityAlertFanoutRateLimited:  attribute.BoolValue(true),
				contract.AttrSecurityAlertFanoutStopped:      attribute.BoolValue(true),
			},
		},
		{
			name:    "cap reached",
			client:  &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true}},
			allowed: capped,
			want: map[string]attribute.Value{
				contract.AttrSecurityAlertFanoutRepositories: attribute.IntValue(maxOrganizationFanoutRepositories + 2),
				contract.AttrSecurityAlertFanoutRequested:    attribute.IntValue(maxOrganizationFanoutRepositories),
				contract.AttrSecurityAlertFanoutIncomplete:   attribute.IntValue(2),
				contract.AttrSecurityAlertFanoutRateLimited:  attribute.BoolValue(false),
				contract.AttrSecurityAlertFanoutStopped:      attribute.BoolValue(false),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			source := newTracedFanoutSource(t, tt.client, tt.allowed, 1, nil, provider.Tracer("test"))
			collectOrgFacts(t, source)

			var observe sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended() {
				if span.Name() == telemetry.SpanSecurityAlertObserve {
					observe = span
				}
			}
			if observe == nil {
				t.Fatalf("no %s span recorded", telemetry.SpanSecurityAlertObserve)
			}
			got := map[string]attribute.Value{}
			for _, kv := range observe.Attributes() {
				got[string(kv.Key)] = kv.Value
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Fatalf("span attribute %s = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}

// TestClaimedSourceRecordsOrgReadFetchBeforeFanout proves the org read's own
// success sample lands before the first per-repository read, so the one
// success sample for the org read does not span the whole fan-out.
func TestClaimedSourceRecordsOrgReadFetchBeforeFanout(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	requests := "eshu_dp_security_alert_provider_requests_total"
	var atFirstRepoRead int64 = -1
	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {}, "example-org/beta-repo": {},
		},
	}
	client.onRepoCall = func() {
		if atFirstRepoRead < 0 {
			atFirstRepoRead = counterValue(t, reader, requests, map[string]string{"status_class": "success"})
		}
	}
	collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 1, instruments))

	if atFirstRepoRead != 1 {
		t.Fatalf("success samples at the first per-repository read = %d, want the org read's 1", atFirstRepoRead)
	}
	if got := counterValue(t, reader, requests, map[string]string{"status_class": "success"}); got != 3 {
		t.Fatalf("success samples after the claim = %d, want 3 (org read + 2 repositories)", got)
	}
}
