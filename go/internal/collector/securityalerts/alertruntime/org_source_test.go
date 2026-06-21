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
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             "security-alert:github-org:example-org",
			Organization:        "example-org",
			Token:               "github-token",
			MaxPages:            3,
			AllowedRepositories: []string{"example-org/alpha-repo", "example-org/beta-repo"},
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
	sort.Slice(envs, func(i, j int) bool {
		return payloadString(envs[i].Payload, "repository_id") < payloadString(envs[j].Payload, "repository_id")
	})

	wantRepos := []struct {
		repositoryID   string
		repositoryName string
	}{
		{"security-alert:github:example-org/alpha-repo", "alpha-repo"},
		{"security-alert:github:example-org/beta-repo", "beta-repo"},
	}
	for i, env := range envs {
		if got := env.FactKind; got != facts.SecurityAlertRepositoryAlertFactKind {
			t.Fatalf("env[%d].FactKind = %q, want %q", i, got, facts.SecurityAlertRepositoryAlertFactKind)
		}
		// All envelopes carry the org generation scope so Postgres streaming
		// writer accepts them (envelope.ScopeID == committed generation scope).
		if got, want := env.ScopeID, "security-alert:github-org:example-org"; got != want {
			t.Fatalf("env[%d].ScopeID = %q, want org scope %q", i, got, want)
		}
		// payload.repository_id carries the per-repo scope for reducer keying.
		if got := payloadString(env.Payload, "repository_id"); got != wantRepos[i].repositoryID {
			t.Fatalf("env[%d].repository_id = %q, want %q", i, got, wantRepos[i].repositoryID)
		}
		if got := payloadString(env.Payload, "repository_name"); got != wantRepos[i].repositoryName {
			t.Fatalf("env[%d].repository_name = %q, want %q", i, got, wantRepos[i].repositoryName)
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

	// The generation scope is the org target scope.
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
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             "security-alert:github-org:example-org",
			Organization:        "example-org",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/alpha-repo"},
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
	// Envelope carries the org generation scope (not per-repo scope) so the
	// Postgres streaming writer accepts it.
	if got, want := envs[0].ScopeID, "security-alert:github-org:example-org"; got != want {
		t.Fatalf("env.ScopeID = %q, want org scope %q", got, want)
	}
	// payload.repository_id carries the per-repo scope for reducer keying.
	if got, want := payloadString(envs[0].Payload, "repository_id"), "security-alert:github:example-org/alpha-repo"; got != want {
		t.Fatalf("env.repository_id = %q, want %q", got, want)
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

// TestValidateOrganizationTargetRequiresAllowedRepositories is the P1 #2
// regression test: org targets without an explicit allowlist must be rejected
// at construction time so no token with org visibility can fan out facts for
// all visible repositories without operator-declared boundaries.
func TestValidateOrganizationTargetRequiresAllowedRepositories(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:     ProviderGitHubDependabot,
			Scope:        TargetScopeOrganization,
			ScopeID:      "security-alert:github-org:example-org",
			Organization: "example-org",
			Token:        "github-token",
			// AllowedRepositories intentionally absent.
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{}, nil
		},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want allowed_repositories-required rejection for org target")
	}
	if !strings.Contains(err.Error(), "allowed_repositories") {
		t.Fatalf("error = %q, want allowed_repositories requirement message", err)
	}
}

// TestClaimedSourceOrgAlertEnvelopeScopeIDMatchesCommittedGenerationScope is
// the P1 #1 regression test: every envelope emitted by an org target must
// carry the org generation scope (not the per-repo scope) so the Postgres
// streaming writer's per-envelope scope check does not fail.
func TestClaimedSourceOrgAlertEnvelopeScopeIDMatchesCommittedGenerationScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             "security-alert:github-org:example-org",
			Organization:        "example-org",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/alpha-repo", "example-org/beta-repo"},
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{
				Alerts: []securityalerts.GitHubDependabotAlert{
					orgAlert(1, "example-org/alpha-repo"),
					orgAlert(2, "example-org/beta-repo"),
				},
				ObservedAt: now,
			}}, nil
		},
		Now: func() time.Time { return now },
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
	if len(envs) == 0 {
		t.Fatal("no facts emitted, want 2")
	}
	// Every envelope must carry the org generation scope, not the per-repo
	// scope. The Postgres streaming writer rejects any envelope whose ScopeID
	// differs from the committed generation scope.
	for i, env := range envs {
		if got, want := env.ScopeID, "security-alert:github-org:example-org"; got != want {
			t.Fatalf("env[%d].ScopeID = %q, want org scope %q (P1 regression: scope mismatch fails commit)", i, got, want)
		}
		// payload.repository_id must still carry the per-repo scope for reducer.
		repoID := payloadString(env.Payload, "repository_id")
		if repoID == "" || repoID == "security-alert:github-org:example-org" {
			t.Fatalf("env[%d].repository_id = %q, want per-repo scope", i, repoID)
		}
	}
}

// TestClaimedSourceFiltersOrgAlertsByAllowlist is the P1 #2 filtering
// regression test: org fan-out must skip alerts for repositories not in
// allowed_repositories even when the provider returns them.
func TestClaimedSourceFiltersOrgAlertsByAllowlist(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:     ProviderGitHubDependabot,
			Scope:        TargetScopeOrganization,
			ScopeID:      "security-alert:github-org:example-org",
			Organization: "example-org",
			Token:        "github-token",
			// Only alpha-repo is allowed; beta-repo must be filtered.
			AllowedRepositories: []string{"example-org/alpha-repo"},
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{
				Alerts: []securityalerts.GitHubDependabotAlert{
					orgAlert(1, "example-org/alpha-repo"),
					orgAlert(2, "example-org/beta-repo"), // not allowlisted
				},
				ObservedAt: now,
			}}, nil
		},
		Now: func() time.Time { return now },
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
		t.Fatalf("len(facts) = %d, want %d (beta-repo must be filtered by allowlist)", got, want)
	}
	if got := payloadString(envs[0].Payload, "repository_id"); got != "security-alert:github:example-org/alpha-repo" {
		t.Fatalf("repository_id = %q, want alpha-repo (beta-repo filtered)", got)
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
