// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// capturedStates records every read state the source hands to its writer
// factory, and the work item generation it was built for.
type capturedStates struct {
	states []securityalerts.TargetReadState
}

func (c *capturedStates) factory(state securityalerts.TargetReadState) collector.GenerationStateWriter {
	c.states = append(c.states, state)
	return func(context.Context, collector.CommitExecutor) error { return nil }
}

func newStateSource(
	t *testing.T,
	client RepositoryAlertClient,
	target TargetConfig,
	factory func(securityalerts.TargetReadState) collector.GenerationStateWriter,
) (*ClaimedSource, workflow.WorkItem) {
	t.Helper()
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets:             []TargetConfig{target},
		ClientFactory:       func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:                 func() time.Time { return now },
		ReadStateWriter:     factory,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	item := testSecurityAlertWorkItem(now)
	item.ScopeID = target.ScopeID
	item.AcceptanceUnitID = target.ScopeID
	return source, item
}

func orgStateTarget(allowed ...string) TargetConfig {
	return TargetConfig{
		Provider:            ProviderGitHubDependabot,
		Scope:               TargetScopeOrganization,
		ScopeID:             fanoutTestOrgScopeID,
		Organization:        "example-org",
		Token:               "github-token",
		MaxPages:            1,
		AllowedRepositories: allowed,
	}
}

func unreadByRepository(state securityalerts.TargetReadState) map[string]string {
	out := make(map[string]string, len(state.Unread))
	for _, row := range state.Unread {
		out[row.Repository] = row.Reason
	}
	return out
}

func collectState(t *testing.T, source *ClaimedSource, item workflow.WorkItem) collector.CollectedGeneration {
	t.Helper()
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v, err %v, want a collected generation", ok, err)
	}
	return collected
}

// TestClaimedSourceRecordsUnreadRepositoriesOfATruncatedOrgRead proves the
// state names every allowlisted repository the fan-out did not read fully with
// the collector's own reason, and leaves a repository read in full out.
func TestClaimedSourceRecordsUnreadRepositoriesOfATruncatedOrgRead(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts:       []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")},
			PagesFetched: 1,
			Truncated:    true,
		},
		repos: map[string]securityalerts.GitHubDependabotAlertResult{
			"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1},
			"example-org/delta-repo": {PagesFetched: 1, Truncated: true},
		},
		repoErrs: map[string]error{"example-org/beta-repo": securityalerts.GitHubDependabotError{StatusCode: 404}},
	}
	captured := &capturedStates{}
	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo", "example-org/delta-repo", "other-org/gamma-repo"}
	source, item := newStateSource(t, client, orgStateTarget(allowed...), captured.factory)
	collected := collectState(t, source, item)

	if collected.StateWriter == nil {
		t.Fatal("StateWriter = nil, want the writer for the org read state")
	}
	if len(captured.states) != 1 {
		t.Fatalf("read states built = %d, want 1", len(captured.states))
	}
	state := captured.states[0]
	if state.GenerationID != item.GenerationID || state.ScopeID != fanoutTestOrgScopeID {
		t.Errorf("state identity = %q/%q, want %q/%q", state.GenerationID, state.ScopeID, item.GenerationID, fanoutTestOrgScopeID)
	}
	if !state.ObservedAt.Equal(collected.Generation.ObservedAt) {
		t.Errorf("state observed at = %v, want the generation's %v", state.ObservedAt, collected.Generation.ObservedAt)
	}
	if !state.OrgReadTruncated {
		t.Error("OrgReadTruncated = false, want true: the org stream hit max_pages")
	}
	if state.RepositoriesTotal != 3 {
		t.Errorf("RepositoriesTotal = %d, want 3 (the other-org entry never appears in this org's stream)", state.RepositoriesTotal)
	}
	want := map[string]string{
		"example-org/beta-repo":  collectionOrgRepositoryReadFailedReason,
		"example-org/delta-repo": collectionOpenPageLimitReachedReason,
	}
	got := unreadByRepository(state)
	if len(got) != len(want) {
		t.Fatalf("unread = %v, want %v (alpha was read in full)", got, want)
	}
	for repository, reason := range want {
		if got[repository] != reason {
			t.Errorf("unread[%s] = %q, want %q", repository, got[repository], reason)
		}
	}
}

func TestClaimedSourceRecordsEveryFanoutStopReason(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{
		org: securityalerts.GitHubDependabotAlertResult{
			Alerts: []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")}, PagesFetched: 1, Truncated: true,
		},
		repoErrs: map[string]error{"example-org/alpha-repo": securityalerts.GitHubDependabotError{
			StatusCode: 403,
			RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 0, RemainingKnown: true},
		}},
	}
	captured := &capturedStates{}
	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo"}
	source, item := newStateSource(t, client, orgStateTarget(allowed...), captured.factory)
	collectState(t, source, item)

	got := unreadByRepository(captured.states[0])
	for _, repository := range allowed {
		if got[repository] != collectionOrgRepositoryRateLimitedReason {
			t.Errorf("unread[%s] = %q, want %q", repository, got[repository], collectionOrgRepositoryRateLimitedReason)
		}
	}
}

func TestClaimedSourceRecordsRepositoriesPastTheFanoutCap(t *testing.T) {
	t.Parallel()

	allowed := make([]string, maxOrganizationFanoutRepositories+2)
	repos := make(map[string]securityalerts.GitHubDependabotAlertResult, len(allowed))
	for i := range allowed {
		allowed[i] = fmt.Sprintf("example-org/repo-%03d", i)
		repos[allowed[i]] = securityalerts.GitHubDependabotAlertResult{PagesFetched: 1}
	}
	client := &fanoutClient{
		org:   securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true},
		repos: repos,
	}
	captured := &capturedStates{}
	source, item := newStateSource(t, client, orgStateTarget(allowed...), captured.factory)
	collectState(t, source, item)

	state := captured.states[0]
	if state.RepositoriesTotal != len(allowed) {
		t.Errorf("RepositoriesTotal = %d, want %d", state.RepositoriesTotal, len(allowed))
	}
	got := unreadByRepository(state)
	wantUnread := allowed[maxOrganizationFanoutRepositories:]
	gotNames := make([]string, 0, len(got))
	for repository, reason := range got {
		gotNames = append(gotNames, repository)
		if reason != collectionOrgFanoutCapReachedReason {
			t.Errorf("unread[%s] = %q, want %q", repository, reason, collectionOrgFanoutCapReachedReason)
		}
	}
	slices.Sort(gotNames)
	if !slices.Equal(gotNames, wantUnread) {
		t.Errorf("unread repositories = %v, want the %d past the cap: %v", gotNames, len(wantUnread), wantUnread)
	}
}

// TestClaimedSourceRecordsACompleteOrgReadWithNothingUnread proves the carrier
// row is also written when the org stream was not truncated, so the read path
// can tell "read in full" from "no record".
func TestClaimedSourceRecordsACompleteOrgReadWithNothingUnread(t *testing.T) {
	t.Parallel()

	client := &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{
		Alerts: []securityalerts.GitHubDependabotAlert{orgAlert(1, "example-org/alpha-repo")}, PagesFetched: 1,
	}}
	captured := &capturedStates{}
	allowed := []string{"example-org/alpha-repo", "example-org/beta-repo"}
	source, item := newStateSource(t, client, orgStateTarget(allowed...), captured.factory)
	collected := collectState(t, source, item)

	if collected.StateWriter == nil || len(captured.states) != 1 {
		t.Fatalf("StateWriter = %v, states = %d, want one state for a complete org read", collected.StateWriter != nil, len(captured.states))
	}
	state := captured.states[0]
	if state.OrgReadTruncated || len(state.Unread) != 0 || state.RepositoriesTotal != 2 {
		t.Errorf("state = %+v, want not truncated, nothing unread, 2 repositories", state)
	}
	if len(client.repoCalls) != 0 {
		t.Errorf("per-repository calls = %v, want none for a complete org read", client.repoCalls)
	}
}

func TestClaimedSourceWritesNoReadStateForRepositoryTargetsOrWithoutAWriter(t *testing.T) {
	t.Parallel()

	repoTarget := TargetConfig{
		Provider: ProviderGitHubDependabot, Scope: TargetScopeRepository, ScopeID: "security-alert:github:example-org/alpha-repo",
		Repository: "example-org/alpha-repo", Token: "github-token", MaxPages: 1,
		AllowedRepositories: []string{"example-org/alpha-repo"},
	}
	client := &fanoutClient{repos: map[string]securityalerts.GitHubDependabotAlertResult{
		"example-org/alpha-repo": {Alerts: []securityalerts.GitHubDependabotAlert{repoAlert(1)}, PagesFetched: 1, Truncated: true},
	}}
	captured := &capturedStates{}
	source, item := newStateSource(t, client, repoTarget, captured.factory)
	if collected := collectState(t, source, item); collected.StateWriter != nil || len(captured.states) != 0 {
		t.Errorf("repository target built a read state: writer %v, states %d", collected.StateWriter != nil, len(captured.states))
	}

	orgClient := &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1}}
	source, item = newStateSource(t, orgClient, orgStateTarget("example-org/alpha-repo"), nil)
	if collected := collectState(t, source, item); collected.StateWriter != nil {
		t.Error("StateWriter set without a ReadStateWriter configured")
	}
}
