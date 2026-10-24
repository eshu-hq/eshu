// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// disabledAlertsFailure is the failure the GitHub client returns for a 403
// that says Dependabot alerts are disabled and carries no rate-limit signal.
func disabledAlertsFailure() error {
	return securityalerts.GitHubDependabotError{
		StatusCode:     403,
		RateLimit:      securityalerts.GitHubRateLimitInfo{Remaining: 4000, RemainingKnown: true},
		AlertsDisabled: true,
	}
}

func spanAttributes(t *testing.T, recorder *tracetest.SpanRecorder) map[string]attribute.Value {
	t.Helper()
	for _, span := range recorder.Ended() {
		if span.Name() != telemetry.SpanSecurityAlertObserve {
			continue
		}
		got := map[string]attribute.Value{}
		for _, kv := range span.Attributes() {
			got[string(kv.Key)] = kv.Value
		}
		return got
	}
	t.Fatalf("no %s span recorded", telemetry.SpanSecurityAlertObserve)
	return nil
}

// TestClaimedSourceContinuesFanoutPastAlertsDisabledRepository proves a
// repository answering "Dependabot alerts are disabled" is reported and the
// fan-out keeps reading the repositories after it, which complete normally.
func TestClaimedSourceContinuesFanoutPastAlertsDisabledRepository(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(2, "example-org/beta-repo")},
			PagesFetched: 1,
			Truncated:    true,
		},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
			"example-org/delta-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(4)}, PagesFetched: 1},
		},
		repoErrs: map[string]error{"example-org/beta-repo": disabledAlertsFailure()},
	}
	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo", "example-org/delta-repo"}
	envs, _ := collectOrgFacts(t, newTracedFanoutSource(t, client, allowed, 1, instruments, provider.Tracer("test")))

	if !slices.Equal(client.repoCalls, allowed) {
		t.Fatalf("per-repository calls = %v, want %v (read past the disabled repository)", client.repoCalls, allowed)
	}
	if len(envs) != 3 {
		t.Fatalf("len(facts) = %d, want alpha, the disabled repository's org-window fact, and delta", len(envs))
	}
	wantReasons := [][]string{
		nil,
		{collectionOpenPageLimitReachedReason, collectionOrgRepositoryAlertsDisabledReason},
		nil,
	}
	for i, env := range envs {
		got := payloadStrings(env.Payload, "collection_incomplete_reasons")
		if !slices.Equal(got, wantReasons[i]) {
			t.Fatalf("%s reasons = %v, want %v", payloadString(env.Payload, "repository_id"), got, wantReasons[i])
		}
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	for outcome, want := range map[string]int64{
		contract.SecurityAlertFanoutOutcomeAlertsDisabled: 1,
		contract.SecurityAlertFanoutOutcomeComplete:       2,
		contract.SecurityAlertFanoutOutcomeStopped:        0,
	} {
		if got := counterValue(t, reader, fanout, map[string]string{"outcome": outcome}); got != want {
			t.Fatalf("%s{outcome=%s} = %d, want %d", fanout, outcome, got, want)
		}
	}
	attrs := spanAttributes(t, recorder)
	for key, want := range map[string]attribute.Value{
		contract.AttrSecurityAlertFanoutRepositories: attribute.IntValue(3),
		contract.AttrSecurityAlertFanoutRequested:    attribute.IntValue(3),
		contract.AttrSecurityAlertFanoutIncomplete:   attribute.IntValue(1),
		contract.AttrSecurityAlertFanoutRateLimited:  attribute.BoolValue(false),
		contract.AttrSecurityAlertFanoutStopped:      attribute.BoolValue(false),
	} {
		if attrs[key] != want {
			t.Fatalf("span attribute %s = %v, want %v", key, attrs[key], want)
		}
	}
}

// TestClaimedSourceStopsFanoutOnPlain403AfterAlertsDisabled proves the disabled
// 403 does not weaken the stop rule: a later 403 with no disabled marker still
// stops the fan-out.
func TestClaimedSourceStopsFanoutOnPlain403AfterAlertsDisabled(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
		repoErrs: map[string]error{
			"example-org/alpha-repo": disabledAlertsFailure(),
			"example-org/beta-repo": securityalerts.GitHubDependabotError{
				StatusCode: 403,
				RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 4000, RemainingKnown: true},
			},
		},
	}
	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo", "example-org/delta-repo"}
	collectOrgFacts(t, newFanoutSource(t, client, allowed, 1, instruments))

	if want := allowed[:2]; !slices.Equal(client.repoCalls, want) {
		t.Fatalf("per-repository calls = %v, want %v (stop at the plain 403)", client.repoCalls, want)
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	for outcome, want := range map[string]int64{
		contract.SecurityAlertFanoutOutcomeAlertsDisabled: 1,
		contract.SecurityAlertFanoutOutcomeFailed:         1,
		contract.SecurityAlertFanoutOutcomeStopped:        1,
	} {
		if got := counterValue(t, reader, fanout, map[string]string{"outcome": outcome}); got != want {
			t.Fatalf("%s{outcome=%s} = %d, want %d", fanout, outcome, got, want)
		}
	}
}

// TestClaimedSourceCountsAlertsDisabledRepositoriesTowardFanoutCap proves
// repositories that answer "alerts disabled" still count toward the per-claim
// cap, so they cannot extend the claim past maxOrganizationFanoutRepositories.
func TestClaimedSourceCountsAlertsDisabledRepositoriesTowardFanoutCap(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	allowed := make([]string, 0, maxOrganizationFanoutRepositories+2)
	repoErrs := map[string]error{}
	for i := range maxOrganizationFanoutRepositories + 2 {
		name := fmt.Sprintf("example-org/repo-%03d", i)
		allowed = append(allowed, name)
		if i < 3 {
			repoErrs[name] = disabledAlertsFailure()
		}
	}
	client := &fanoutClient{
		org:      securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
		repoErrs: repoErrs,
	}
	collectOrgFacts(t, newFanoutSource(t, client, allowed, 1, instruments))

	if got := len(client.repoCalls); got != maxOrganizationFanoutRepositories {
		t.Fatalf("per-repository calls = %d, want the cap %d", got, maxOrganizationFanoutRepositories)
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	for outcome, want := range map[string]int64{
		contract.SecurityAlertFanoutOutcomeAlertsDisabled: 3,
		contract.SecurityAlertFanoutOutcomeComplete:       int64(maxOrganizationFanoutRepositories - 3),
		contract.SecurityAlertFanoutOutcomeSkippedCap:     2,
	} {
		if got := counterValue(t, reader, fanout, map[string]string{"outcome": outcome}); got != want {
			t.Fatalf("%s{outcome=%s} = %d, want %d", fanout, outcome, got, want)
		}
	}
}

// TestClaimedSourceFanoutFreshnessHintSeparatesAlertsDisabledFromFailed proves
// a repository whose alerts were switched off is a different refresh boundary
// than one that merely failed to read, with identical alerts and pages.
func TestClaimedSourceFanoutFreshnessHintSeparatesAlertsDisabledFromFailed(t *testing.T) {
	t.Parallel()

	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo"}
	hint := func(alphaErr error) string {
		client := &fanoutClient{
			org:      securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
			repos:    map[string]securityalerts.GitHubDependabotAlertResult{"example-org/beta-repo": {}},
			repoErrs: map[string]error{"example-org/alpha-repo": alphaErr},
		}
		_, got := collectOrgFacts(t, newFanoutSource(t, client, allowed, 1, nil))
		return got
	}
	disabled := hint(disabledAlertsFailure())
	failed := hint(securityalerts.GitHubDependabotError{StatusCode: 404})
	if disabled == "" || failed == "" {
		t.Fatal("freshness hint is empty")
	}
	if disabled == failed {
		t.Fatal("freshness hint identical for alerts_disabled and failed, want the outcome in the digest")
	}
}

// TestClaimedSourceIgnoresAlertsDisabledMarkerOnOtherClasses proves the marker
// only changes an auth_denied failure: a rate-limited or retryable failure that
// somehow carries it still stops the fan-out.
func TestClaimedSourceIgnoresAlertsDisabledMarkerOnOtherClasses(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"rate_limited": securityalerts.GitHubDependabotError{
			StatusCode:     403,
			RateLimit:      securityalerts.GitHubRateLimitInfo{Remaining: 0, RemainingKnown: true},
			AlertsDisabled: true,
		},
		"retryable_503": securityalerts.GitHubDependabotError{StatusCode: 503, AlertsDisabled: true},
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &fanoutClient{
				org:      securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
				repoErrs: map[string]error{"example-org/alpha-repo": failure},
			}
			collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 1, nil))
			if want := []string{"example-org/alpha-repo"}; !slices.Equal(client.repoCalls, want) {
				t.Fatalf("per-repository calls = %v, want %v (marker must not continue the fan-out)", client.repoCalls, want)
			}
		})
	}
}

// TestClaimedSourceRepositoryTargetClassifiesAlertsDisabledAsNotFound proves a
// repository-scope target that gets the disabled 403 fails with terminal
// not_found, not auth_denied, so readiness does not blame a working
// credential (#8947).
func TestClaimedSourceRepositoryTargetClassifiesAlertsDisabledAsNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	client := &recordingAlertClient{err: disabledAlertsFailure()}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
			MaxPages:            1,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	_, ok, err := source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
	if ok || err == nil {
		t.Fatalf("NextClaimed() ok = %t, err = %v, want a failure", ok, err)
	}
	var classified interface {
		FailureClass() string
		TerminalFailure() bool
	}
	if !errors.As(err, &classified) {
		t.Fatalf("NextClaimed() error = %T, want classified provider failure", err)
	}
	if classified.FailureClass() != FailureNotFound || !classified.TerminalFailure() {
		t.Fatalf("class = %q terminal = %t, want terminal not_found", classified.FailureClass(), classified.TerminalFailure())
	}
}
