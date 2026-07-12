// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

// Collector fault-injection coverage (C-14, #4367) — incident, work-item, and
// security-alert collector boundaries. See fault_collectors_test.go for the
// shared helper, the fault-injection rationale, and the active skills.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/jira"
	"github.com/eshu-hq/eshu/go/internal/collector/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// TestJiraCollectorSurfacesInjectedTimeout drives the real Jira work-item client
// into a boundary timeout.
func TestJiraCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := jira.NewHTTPClient(jira.HTTPClientConfig{
			BaseURL: "https://jira.invalid",
			Email:   "collector@example.com",
			Token:   "jira-token",
			Client:  timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		_, err = client.CollectWorkItemEvidence(context.Background(), jira.TargetConfig{
			Provider:   "jira",
			ScopeID:    "jira:site:prod",
			SiteID:     "site-prod",
			BaseURL:    "https://jira.invalid",
			Email:      "collector@example.com",
			Token:      "jira-token",
			JQL:        "project = OPS",
			IssueLimit: 25,
		}, jira.CollectionWindow{Until: time.Now().UTC()})
		return err
	})
}

// TestPagerDutyCollectorSurfacesInjectedTimeout drives the real PagerDuty
// incident client into a boundary timeout.
func TestPagerDutyCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := pagerduty.NewHTTPClient(pagerduty.HTTPClientConfig{
			BaseURL: "https://pagerduty.invalid",
			Token:   "pagerduty-token",
			Client:  timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		_, err = client.CollectIncidentEvidence(context.Background(), pagerduty.TargetConfig{
			Provider:      "pagerduty",
			ScopeID:       "pagerduty:account:prod",
			AccountID:     "account-prod",
			Token:         "pagerduty-token",
			APIBaseURL:    "https://pagerduty.invalid",
			IncidentLimit: 25,
		}, pagerduty.CollectionWindow{Until: time.Now().UTC()})
		return err
	})
}

// TestSecurityAlertsCollectorSurfacesInjectedTimeout drives the real GitHub
// Dependabot alert client into a boundary timeout.
func TestSecurityAlertsCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client := securityalerts.NewGitHubDependabotClient(securityalerts.GitHubDependabotClientConfig{
			BaseURL:              "https://api.github.invalid",
			Token:                "github-token",
			AllowedRepositories:  []string{"octo-org/checkout"},
			RepositoryAlertLimit: 25,
			HTTPClient:           timeoutFaultClient(),
		})
		_, err := client.ListRepositoryAlerts(context.Background(), "octo-org/checkout")
		return err
	})
}
