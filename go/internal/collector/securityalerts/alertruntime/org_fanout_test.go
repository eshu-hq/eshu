// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// TestClaimedSourceFansOutPerRepositoryWhenOrgReadTruncated is the #8805
// acceptance test: a truncated org read issues one bounded per-repository read
// for each allowlisted repository and merges the results into the same
// generation and scope id.
func TestClaimedSourceFansOutPerRepositoryWhenOrgReadTruncated(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts: []securityalerts.GitHubDependabotAlert{
				orgAlert(1, "example-org/alpha-repo"),
				orgAlert(40, "example-org/gamma-repo"), // not allowlisted
			},
			PagesFetched: 3,
			Truncated:    true,
		},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1), repoAlert(2)}, PagesFetched: 1},
			"example-org/beta-repo":  {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(5)}, PagesFetched: 1},
		},
	}
	source := newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 3, nil)

	envs, _ := collectOrgFacts(t, source)

	if got, want := client.repoCalls, []string{"example-org/alpha-repo", "example-org/beta-repo"}; !slices.Equal(got, want) {
		t.Fatalf("per-repository calls = %v, want %v", got, want)
	}
	if got, want := client.repoPages, []int{3, 3}; !slices.Equal(got, want) {
		t.Fatalf("per-repository max_pages = %v, want each bounded by max_pages %v", got, want)
	}
	type key struct {
		repositoryID string
		number       int64
	}
	var got []key
	for _, env := range envs {
		if env.ScopeID != fanoutTestOrgScopeID {
			t.Fatalf("env.ScopeID = %q, want org generation scope %q", env.ScopeID, fanoutTestOrgScopeID)
		}
		if env.GenerationID != "security-alert:generation-1" {
			t.Fatalf("env.GenerationID = %q, want the claimed generation", env.GenerationID)
		}
		if state := payloadString(env.Payload, "collection_coverage_state"); state != collectionCoverageComplete {
			t.Fatalf("collection_coverage_state = %q, want complete after an untruncated per-repository read", state)
		}
		if reasons := payloadStrings(env.Payload, "collection_incomplete_reasons"); len(reasons) != 0 {
			t.Fatalf("collection_incomplete_reasons = %v, want none", reasons)
		}
		got = append(got, key{payloadString(env.Payload, "repository_id"), payloadInt(env.Payload, "provider_alert_number")})
	}
	want := []key{
		{"security-alert:github:example-org/alpha-repo", 1},
		{"security-alert:github:example-org/alpha-repo", 2},
		{"security-alert:github:example-org/beta-repo", 5},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("facts = %v, want %v (deduped alpha#1, gamma filtered)", got, want)
	}
	if name := payloadString(envs[2].Payload, "repository_name"); name != "beta-repo" {
		t.Fatalf("repository_name = %q, want beta-repo for a fan-out-only fact", name)
	}
}

// TestClaimedSourceOrgFanoutFactsKeepOrgPathStableKey proves facts are
// idempotent across the org path and the per-repository path: the same
// provider alert keeps one stable key and fact id whichever read produced it,
// and that key matches a repository-scope target's key for the repository.
func TestClaimedSourceOrgFanoutFactsKeepOrgPathStableKey(t *testing.T) {
	t.Parallel()

	allowed := []string{"example-org/alpha-repo"}
	orgOnly := &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{
		Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")},
		PagesFetched: 1,
	}}
	orgEnvs, _ := collectOrgFacts(t, newFanoutSource(t, orgOnly, allowed, 1, nil))

	fanout := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
		},
	}
	fanoutEnvs, _ := collectOrgFacts(t, newFanoutSource(t, fanout, allowed, 1, nil))

	if len(orgEnvs) != 1 || len(fanoutEnvs) != 1 {
		t.Fatalf("facts = %d org-path, %d fan-out path, want 1 each", len(orgEnvs), len(fanoutEnvs))
	}
	if orgEnvs[0].StableFactKey != fanoutEnvs[0].StableFactKey {
		t.Fatalf("stable key org path %q != fan-out path %q", orgEnvs[0].StableFactKey, fanoutEnvs[0].StableFactKey)
	}
	if orgEnvs[0].FactID != fanoutEnvs[0].FactID {
		t.Fatalf("fact id org path %q != fan-out path %q", orgEnvs[0].FactID, fanoutEnvs[0].FactID)
	}
	repoScoped, err := securityalerts.NewGitHubDependabotAlertEnvelope(securityalerts.EnvelopeContext{
		ScopeID:             "security-alert:github:example-org/alpha-repo",
		GenerationID:        "security-alert:generation-1",
		CollectorInstanceID: "security-alert-primary",
		FencingToken:        42,
		ObservedAt:          fanoutEnvs[0].ObservedAt,
	}, repoAlert(1))
	if err != nil {
		t.Fatalf("NewGitHubDependabotAlertEnvelope() error = %v", err)
	}
	if repoScoped.StableFactKey != fanoutEnvs[0].StableFactKey {
		t.Fatalf("stable key repository target %q != fan-out path %q", repoScoped.StableFactKey, fanoutEnvs[0].StableFactKey)
	}
}

func TestClaimedSourceDoesNotFanOutWhenOrgReadComplete(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{
		Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")},
		PagesFetched: 1,
	}}
	envs, _ := collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 2, nil))
	if len(client.repoCalls) != 0 {
		t.Fatalf("per-repository calls = %v, want none for an untruncated org read", client.repoCalls)
	}
	if len(envs) != 1 {
		t.Fatalf("len(facts) = %d, want 1", len(envs))
	}
}

// TestClaimedSourceReportsRepositoryStillTruncatedAfterFanout proves a
// repository whose own bounded read still truncates is reported through
// collection_incomplete_reasons and the truncation metric.
func TestClaimedSourceReportsRepositoryStillTruncatedAfterFanout(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 2, Truncated: true},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
			"example-org/beta-repo":  {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(9)}, PagesFetched: 2, Truncated: true},
		},
	}
	envs, _ := collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 2, instruments))
	if len(envs) != 2 {
		t.Fatalf("len(facts) = %d, want 2", len(envs))
	}
	alpha, beta := envs[0], envs[1]
	if state := payloadString(alpha.Payload, "collection_coverage_state"); state != collectionCoverageComplete {
		t.Fatalf("alpha coverage = %q, want complete", state)
	}
	if state := payloadString(beta.Payload, "collection_coverage_state"); state != collectionCoverageIncomplete {
		t.Fatalf("beta coverage = %q, want incomplete", state)
	}
	if got := payloadStrings(beta.Payload, "collection_incomplete_reasons"); !slices.Equal(got, []string{collectionOpenPageLimitReachedReason}) {
		t.Fatalf("beta collection_incomplete_reasons = %v, want [%s]", got, collectionOpenPageLimitReachedReason)
	}
	if got := payloadString(beta.Payload, "source_freshness"); got != securityAlertSourceFreshnessPartial {
		t.Fatalf("beta source_freshness = %q, want partial", got)
	}
	if got := payloadInt(beta.Payload, "collection_pages_fetched"); got != 2 {
		t.Fatalf("beta collection_pages_fetched = %d, want its own per-repository pages (2)", got)
	}

	truncated := "eshu_dp_security_alert_truncated_reads_total"
	if got := counterValue(t, reader, truncated, map[string]string{"operation": "list_org_alerts"}); got != 1 {
		t.Fatalf("%s{operation=list_org_alerts} = %d, want 1", truncated, got)
	}
	if got := counterValue(t, reader, truncated, map[string]string{"operation": "list_repo_alerts"}); got != 1 {
		t.Fatalf("%s{operation=list_repo_alerts} = %d, want 1", truncated, got)
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	for outcome, want := range map[string]int64{
		contract.SecurityAlertFanoutOutcomeComplete:  1,
		contract.SecurityAlertFanoutOutcomeTruncated: 1,
	} {
		if got := counterValue(t, reader, fanout, map[string]string{"outcome": outcome}); got != want {
			t.Fatalf("%s{outcome=%s} = %d, want %d", fanout, outcome, got, want)
		}
	}
}

// TestClaimedSourceStopsFanoutOnRateLimit proves the fan-out stops at the
// first rate-limited per-repository read instead of looping: the remaining
// repositories are not requested and their org-window facts report why.
func TestClaimedSourceStopsFanoutOnRateLimit(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts: []securityalerts.GitHubDependabotAlert{
				orgAlert(1, "example-org/alpha-repo"),
				orgAlert(3, "example-org/beta-repo"),
			},
			PagesFetched: 1,
			Truncated:    true,
		},
		repoErrs: map[string]error{
			"example-org/alpha-repo": securityalerts.GitHubDependabotError{
				StatusCode: 403,
				RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 0, RemainingKnown: true},
				Operation:  "list_repo_alerts",
			},
		},
	}
	envs, _ := collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 1, instruments))

	if got, want := client.repoCalls, []string{"example-org/alpha-repo"}; !slices.Equal(got, want) {
		t.Fatalf("per-repository calls = %v, want %v (stop at rate limit)", got, want)
	}
	if len(envs) != 2 {
		t.Fatalf("len(facts) = %d, want the 2 org-window facts kept", len(envs))
	}
	for _, env := range envs {
		want := []string{collectionOpenPageLimitReachedReason, collectionOrgRepositoryRateLimitedReason}
		if got := payloadStrings(env.Payload, "collection_incomplete_reasons"); !slices.Equal(got, want) {
			t.Fatalf("collection_incomplete_reasons = %v, want %v", got, want)
		}
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	if got := counterValue(t, reader, fanout, map[string]string{"outcome": contract.SecurityAlertFanoutOutcomeRateLimited}); got != 2 {
		t.Fatalf("%s{outcome=rate_limited} = %d, want 2", fanout, got)
	}
	if got := counterValue(t, reader, "eshu_dp_security_alert_rate_limited_total", nil); got != 1 {
		t.Fatalf("rate_limited_total = %d, want 1", got)
	}
}

func TestClaimedSourceContinuesFanoutPastFailedRepository(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")},
			PagesFetched: 1,
			Truncated:    true,
		},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/beta-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(5)}, PagesFetched: 1},
		},
		repoErrs: map[string]error{
			"example-org/alpha-repo": securityalerts.GitHubDependabotError{StatusCode: 404, Operation: "list_repo_alerts"},
		},
	}
	envs, _ := collectOrgFacts(t, newFanoutSource(t, client, []string{"example-org/alpha-repo", "example-org/beta-repo"}, 1, nil))
	if got, want := client.repoCalls, []string{"example-org/alpha-repo", "example-org/beta-repo"}; !slices.Equal(got, want) {
		t.Fatalf("per-repository calls = %v, want %v", got, want)
	}
	if len(envs) != 2 {
		t.Fatalf("len(facts) = %d, want 2", len(envs))
	}
	if got, want := payloadStrings(envs[0].Payload, "collection_incomplete_reasons"),
		[]string{collectionOpenPageLimitReachedReason, collectionOrgRepositoryReadFailedReason}; !slices.Equal(got, want) {
		t.Fatalf("alpha reasons = %v, want %v", got, want)
	}
	if got := payloadString(envs[1].Payload, "collection_coverage_state"); got != collectionCoverageComplete {
		t.Fatalf("beta coverage = %q, want complete", got)
	}
}

// TestClaimedSourceBoundsFanoutRepositories proves the per-claim fan-out cap:
// allowlisted repositories past maxOrganizationFanoutRepositories are not
// requested and are counted as skipped, not looped over.
func TestClaimedSourceBoundsFanoutRepositories(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	allowed := make([]string, 0, maxOrganizationFanoutRepositories+2)
	for i := range maxOrganizationFanoutRepositories + 2 {
		allowed = append(allowed, fmt.Sprintf("example-org/repo-%03d", i))
	}
	allowed = append(allowed, "other-org/outside-repo") // never read through an org target
	last := allowed[maxOrganizationFanoutRepositories+1]
	client := &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{
		Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, last)},
		PagesFetched: 1,
		Truncated:    true,
	}}
	envs, _ := collectOrgFacts(t, newFanoutSource(t, client, allowed, 1, instruments))

	if got := len(client.repoCalls); got != maxOrganizationFanoutRepositories {
		t.Fatalf("per-repository calls = %d, want the cap %d", got, maxOrganizationFanoutRepositories)
	}
	if slices.Contains(client.repoCalls, "other-org/outside-repo") {
		t.Fatal("fan-out read a repository outside the target organization")
	}
	if len(envs) != 1 {
		t.Fatalf("len(facts) = %d, want the skipped repository's org-window fact", len(envs))
	}
	want := []string{collectionOpenPageLimitReachedReason, collectionOrgFanoutCapReachedReason}
	if got := payloadStrings(envs[0].Payload, "collection_incomplete_reasons"); !slices.Equal(got, want) {
		t.Fatalf("collection_incomplete_reasons = %v, want %v", got, want)
	}
	fanout := "eshu_dp_security_alert_org_fanout_repositories_total"
	if got := counterValue(t, reader, fanout, map[string]string{"outcome": contract.SecurityAlertFanoutOutcomeSkippedCap}); got != 2 {
		t.Fatalf("%s{outcome=skipped_cap} = %d, want 2", fanout, got)
	}
}

// TestClaimedSourceFanoutFreshnessHintTracksOutcomes proves an unchanged
// fan-out keeps its freshness digest while a changed per-repository outcome
// moves it, so a refresh is skipped only when provider truth is unchanged.
func TestClaimedSourceFanoutFreshnessHintTracksOutcomes(t *testing.T) {
	t.Parallel()

	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo"}
	build := func(betaErr error) *fanoutClient {
		client := &fanoutClient{
			org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
			repos: map[string]securityalerts.GitHubDependabotAlertResult{
				"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
				"example-org/beta-repo":  {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
			},
		}
		if betaErr != nil {
			client.repoErrs = map[string]error{"example-org/beta-repo": betaErr}
		}
		return client
	}
	_, first := collectOrgFacts(t, newFanoutSource(t, build(nil), allowed, 1, nil))
	_, second := collectOrgFacts(t, newFanoutSource(t, build(nil), allowed, 1, nil))
	_, failed := collectOrgFacts(t, newFanoutSource(t, build(securityalerts.GitHubDependabotError{StatusCode: 404}), allowed, 1, nil))
	if first == "" || first != second {
		t.Fatalf("freshness hint = %q then %q, want a stable non-empty digest", first, second)
	}
	if first == failed {
		t.Fatal("freshness hint unchanged after a per-repository read failed, want a new boundary")
	}
}
