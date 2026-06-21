package alertruntime

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceFansOutOrganizationAlertsIntoPerRepositoryFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	client := &recordingOrgClient{result: securityalerts.GitHubDependabotAlertResult{
		Alerts: []securityalerts.GitHubDependabotAlert{
			orgAlert(7, "example-org/alpha-repo"),
			orgAlert(9, "example-org/beta-repo"),
		},
		PagesFetched: 1,
		ObservedAt:   now,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:     ProviderGitHubDependabot,
			Scope:        TargetScopeOrganization,
			ScopeID:      "security-alert:github-org:example-org",
			Organization: "example-org",
			Token:        "github-token",
			MaxPages:     3,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	item := testSecurityAlertWorkItem(now)
	item.ScopeID = "security-alert:github-org:example-org"
	item.AcceptanceUnitID = item.ScopeID
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	// The org endpoint must be hit, not the per-repository endpoint.
	if got, want := client.organization, "example-org"; got != want {
		t.Fatalf("organization requested = %q, want %q", got, want)
	}
	if got, want := client.maxPages, 3; got != want {
		t.Fatalf("maxPages = %d, want %d", got, want)
	}
	if client.repoCalls != 0 {
		t.Fatalf("repoCalls = %d, want 0 (org target must not call the per-repository endpoint)", client.repoCalls)
	}

	envs := drainFacts(collected.Facts)
	if got, want := len(envs), 2; got != want {
		t.Fatalf("len(facts) = %d, want one per repository (%d)", got, want)
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].ScopeID < envs[j].ScopeID })

	want := []struct {
		scopeID        string
		repositoryID   string
		repositoryName string
	}{
		{"security-alert:github:example-org/alpha-repo", "security-alert:github:example-org/alpha-repo", "alpha-repo"},
		{"security-alert:github:example-org/beta-repo", "security-alert:github:example-org/beta-repo", "beta-repo"},
	}
	for i, env := range envs {
		if got := env.FactKind; got != facts.SecurityAlertRepositoryAlertFactKind {
			t.Fatalf("env[%d].FactKind = %q, want %q", i, got, facts.SecurityAlertRepositoryAlertFactKind)
		}
		if got := env.ScopeID; got != want[i].scopeID {
			t.Fatalf("env[%d].ScopeID = %q, want per-repository scope %q", i, got, want[i].scopeID)
		}
		if got := payloadString(env.Payload, "repository_id"); got != want[i].repositoryID {
			t.Fatalf("env[%d].repository_id = %q, want %q", i, got, want[i].repositoryID)
		}
		if got := payloadString(env.Payload, "repository_name"); got != want[i].repositoryName {
			t.Fatalf("env[%d].repository_name = %q, want %q", i, got, want[i].repositoryName)
		}
		// Fan-out facts must carry the same provider + reported confidence as the
		// per-repository path so reducer reconciliation is unchanged.
		if got := payloadString(env.Payload, "provider"); got != ProviderGitHubDependabot {
			t.Fatalf("env[%d].provider = %q, want %q", i, got, ProviderGitHubDependabot)
		}
		if got := env.SourceConfidence; got != facts.SourceConfidenceReported {
			t.Fatalf("env[%d].SourceConfidence = %q, want reported", i, got)
		}
	}

	// The generation scope is the org target scope; facts carry per-repo scopes.
	if got, want := collected.Scope.ScopeKind, scope.KindSecurityAlert; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
}

func TestClaimedSourceSkipsOrganizationAlertsWithUnusableRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	client := &recordingOrgClient{result: securityalerts.GitHubDependabotAlertResult{
		Alerts: []securityalerts.GitHubDependabotAlert{
			orgAlert(7, "example-org/alpha-repo"),
			orgAlert(8, ""), // missing repository: cannot derive a per-repo scope.
		},
		PagesFetched: 1,
		ObservedAt:   now,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:     ProviderGitHubDependabot,
			Scope:        TargetScopeOrganization,
			ScopeID:      "security-alert:github-org:example-org",
			Organization: "example-org",
			Token:        "github-token",
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	item := testSecurityAlertWorkItem(now)
	item.ScopeID = "security-alert:github-org:example-org"
	item.AcceptanceUnitID = item.ScopeID
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	envs := drainFacts(collected.Facts)
	if got, want := len(envs), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d (alert without repository skipped)", got, want)
	}
	if got, want := envs[0].ScopeID, "security-alert:github:example-org/alpha-repo"; got != want {
		t.Fatalf("env.ScopeID = %q, want %q", got, want)
	}
}

func TestValidateOrganizationTargetRequiresOrganization(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider: ProviderGitHubDependabot,
			Scope:    TargetScopeOrganization,
			ScopeID:  "security-alert:github-org:example-org",
			Token:    "github-token",
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{}, nil
		},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want organization-required rejection")
	}
	if !strings.Contains(err.Error(), "organization") {
		t.Fatalf("error = %q, want organization requirement", err)
	}
}

func TestValidateOrganizationTargetRejectsRepositoryFields(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:     ProviderGitHubDependabot,
			Scope:        TargetScopeOrganization,
			ScopeID:      "security-alert:github-org:example-org",
			Organization: "example-org",
			Repository:   "example-org/example-repo",
			Token:        "github-token",
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{}, nil
		},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want repository-field rejection for org target")
	}
}

type recordingOrgClient struct {
	result       securityalerts.GitHubDependabotAlertResult
	err          error
	organization string
	maxPages     int
	repoCalls    int
}

func (c *recordingOrgClient) ListRepositoryAlertsPages(
	context.Context,
	string,
	int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.repoCalls++
	return securityalerts.GitHubDependabotAlertResult{}, nil
}

func (c *recordingOrgClient) ListOrganizationAlertsPages(
	_ context.Context,
	organization string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.organization = organization
	c.maxPages = maxPages
	return c.result, c.err
}

func orgAlert(number int, repoFullName string) securityalerts.GitHubDependabotAlert {
	alert := testDependabotAlert()
	alert.Number = number
	if repoFullName != "" {
		name := repoFullName
		if slash := strings.LastIndex(repoFullName, "/"); slash >= 0 {
			name = repoFullName[slash+1:]
		}
		alert.Repository = securityalerts.GitHubDependabotRepository{
			FullName: repoFullName,
			Name:     name,
		}
	}
	return alert
}

func payloadString(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}

var _ = workflow.WorkItem{}
