// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// TestClaimedSourceStopsFanoutOnNonNotFoundFailure proves only not_found lets
// the fan-out continue: an auth_denied 403 without rate-limit headers, a 5xx,
// a terminal 4xx, or a transport failure stops it after the first read, so the
// fan-out never keeps calling a provider that is refusing or limiting it.
func TestClaimedSourceStopsFanoutOnNonNotFoundFailure(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"auth_denied_403_without_rate_limit_headers": securityalerts.GitHubDependabotError{
			StatusCode: 403,
			RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 4000, RemainingKnown: true},
		},
		"retryable_503":   securityalerts.GitHubDependabotError{StatusCode: 503},
		"terminal_422":    securityalerts.GitHubDependabotError{StatusCode: 422},
		"transport_error": errors.New("connection reset"),
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instruments, reader := newTestInstruments(t)
			client := &fanoutClient{
				org: securityalerts.GitHubDependabotAlertResult{
					Alerts: []securityalerts.GitHubDependabotAlert{
						orgAlert(1, "example-org/alpha-repo"),
						orgAlert(2, "example-org/beta-repo"),
						orgAlert(3, "example-org/delta-repo"),
					},
					PagesFetched: 1,
					Truncated:    true,
				},
				repoErrs: map[string]error{"example-org/alpha-repo": failure},
			}
			allowed := []string{"example-org/alpha-repo", "example-org/beta-repo", "example-org/delta-repo"}
			envs, _ := collectOrgFacts(t, newFanoutSource(t, client, allowed, 1, instruments))

			if got, want := client.repoCalls, []string{"example-org/alpha-repo"}; !slices.Equal(got, want) {
				t.Fatalf("per-repository calls = %v, want %v (stop after the first failure)", got, want)
			}
			if len(envs) != 3 {
				t.Fatalf("len(facts) = %d, want the 3 org-window facts kept", len(envs))
			}
			wantReasons := [][]string{
				{collectionOpenPageLimitReachedReason, collectionOrgRepositoryReadFailedReason},
				{collectionOpenPageLimitReachedReason, collectionOrgFanoutStoppedReason},
				{collectionOpenPageLimitReachedReason, collectionOrgFanoutStoppedReason},
			}
			for i, env := range envs {
				if got := payloadStrings(env.Payload, "collection_incomplete_reasons"); !slices.Equal(got, wantReasons[i]) {
					t.Fatalf("%s reasons = %v, want %v", payloadString(env.Payload, "repository_id"), got, wantReasons[i])
				}
			}
			fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
			for outcome, want := range map[string]int64{
				contract.SecurityAlertFanoutOutcomeFailed:  1,
				contract.SecurityAlertFanoutOutcomeStopped: 2,
			} {
				if got := counterValue(t, reader, fanout, map[string]string{"outcome": outcome}); got != want {
					t.Fatalf("%s{outcome=%s} = %d, want %d", fanout, outcome, got, want)
				}
			}
		})
	}
}

// cancelingClient cancels the claim context during the first per-repository
// read, the way a lost lease or shutdown does mid fan-out.
type cancelingClient struct {
	fanoutClient
	cancel context.CancelFunc
}

func (c *cancelingClient) ListRepositoryAlertsPages(
	ctx context.Context,
	repository string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.cancel()
	_, _ = c.fanoutClient.ListRepositoryAlertsPages(ctx, repository, maxPages)
	return securityalerts.GitHubDependabotAlertResult{}, ctx.Err()
}

// TestClaimedSourceFailsClaimWhenContextCancelledDuringFanout proves a claim
// whose context is cancelled mid fan-out returns the error and no partial
// generation, instead of committing a half-read organization.
func TestClaimedSourceFailsClaimWhenContextCancelledDuringFanout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &cancelingClient{
		fanoutClient: fanoutClient{org: securityalerts.GitHubDependabotAlertResult{
			Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")},
			PagesFetched: 1,
			Truncated:    true,
		}},
		cancel: cancel,
	}
	source := newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 1, nil)

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	item := testSecurityAlertWorkItem(now)
	item.ScopeID = fanoutTestOrgScopeID
	item.AcceptanceUnitID = fanoutTestOrgScopeID
	collected, ok, err := source.NextClaimed(ctx, item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want the cancelled-claim failure")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false for a cancelled claim")
	}
	if collected.Facts != nil {
		t.Fatal("NextClaimed() returned a fact stream, want no partial generation")
	}
	if got, want := client.repoCalls, []string{"example-org/alpha-repo"}; !slices.Equal(got, want) {
		t.Fatalf("per-repository calls = %v, want %v (no read after cancellation)", got, want)
	}
}
