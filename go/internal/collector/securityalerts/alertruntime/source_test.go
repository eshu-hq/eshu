package alertruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsRepositoryAlertFactsOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:             ProviderGitHubDependabot,
			ScopeID:              "security-alert:github:example-org/example-repo",
			Repository:           "example-org/example-repo",
			Token:                "github-token",
			AllowedRepositories:  []string{"example-org/example-repo"},
			RepositoryAlertLimit: 25,
			MaxPages:             2,
			SourceURI:            "https://github.com/example-org/example-repo/security/dependabot?token=secret",
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{
				Alerts:     []securityalerts.GitHubDependabotAlert{testDependabotAlert()},
				ObservedAt: now,
			}}, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeKind, scope.KindSecurityAlert; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	envs := drainFacts(collected.Facts)
	if got, want := len(envs), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	if got, want := envs[0].FactKind, facts.SecurityAlertRepositoryAlertFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if strings.HasPrefix(envs[0].FactKind, "reducer_") {
		t.Fatalf("FactKind = %q, collector must not emit reducer-owned facts", envs[0].FactKind)
	}
	if got := envs[0].SourceRef.SourceURI; strings.Contains(got, "token=secret") {
		t.Fatalf("SourceURI = %q, want token-bearing query stripped", got)
	}
}

func TestNewClaimedSourceRejectsCredentialBearingAPIBaseURL(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
			APIBaseURL:          "https://user:secret@api.github.com",
		}},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want credential-bearing api_base_url rejection")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("NewClaimedSource() error leaked credential: %q", err)
	}
}

func TestClaimedSourceSetsStableFreshnessHintForUnchangedAlerts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	client := staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{
		Alerts:     []securityalerts.GitHubDependabotAlert{testDependabotAlert()},
		ObservedAt: now,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	first, ok, err := source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
	if err != nil {
		t.Fatalf("NextClaimed() first error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() first ok = false, want true")
	}
	secondItem := testSecurityAlertWorkItem(now.Add(time.Minute))
	secondItem.GenerationID = "security-alert:generation-2"
	secondItem.WorkItemID = "security-alert:security-alert-primary:generation-2"
	second, ok, err := source.NextClaimed(context.Background(), secondItem)
	if err != nil {
		t.Fatalf("NextClaimed() second error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() second ok = false, want true")
	}

	if strings.TrimSpace(first.Generation.FreshnessHint) == "" {
		t.Fatal("FreshnessHint is blank, want stable provider snapshot digest")
	}
	if got, want := second.Generation.FreshnessHint, first.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint changed for unchanged alerts: got %q, want %q", got, want)
	}
}

func TestClaimedSourceMarksProviderCoverageIncompleteWhenOpenAlertPagesAreTruncated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 15, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
			MaxPages:            2,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{result: securityalerts.GitHubDependabotAlertResult{
				Alerts:       []securityalerts.GitHubDependabotAlert{testDependabotAlert()},
				PagesFetched: 2,
				Truncated:    true,
				ObservedAt:   now,
			}}, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	envs := drainFacts(collected.Facts)
	if got, want := len(envs), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := envs[0].Payload
	if got, want := payload["source_freshness"], "partial"; got != want {
		t.Fatalf("Payload[source_freshness] = %#v, want %#v", got, want)
	}
	if got, want := payload["collection_coverage_state"], "incomplete"; got != want {
		t.Fatalf("Payload[collection_coverage_state] = %#v, want %#v", got, want)
	}
	if got, want := payload["collection_truncated"], true; got != want {
		t.Fatalf("Payload[collection_truncated] = %#v, want %#v", got, want)
	}
	if got, want := payload["collection_pages_fetched"], int64(2); got != want {
		t.Fatalf("Payload[collection_pages_fetched] = %#v, want %#v", got, want)
	}
	if got, want := payload["collection_state_filter"], "open"; got != want {
		t.Fatalf("Payload[collection_state_filter] = %#v, want %#v", got, want)
	}
	if got, want := payload["collection_incomplete_reasons"], []string{"provider_open_alert_page_limit_reached"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Payload[collection_incomplete_reasons] = %#v, want %#v", got, want)
	}
}

func TestClaimedSourceReturnsBoundedFailureWithoutRepositoryOrToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 30, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{err: securityalerts.GitHubDependabotError{
				StatusCode: 429,
				Message:    "raw upstream error mentions github-token and example-org/example-repo",
			}}, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want rate-limit failure")
	}
	if strings.Contains(err.Error(), "github-token") || strings.Contains(err.Error(), "example-org/example-repo") {
		t.Fatalf("NextClaimed() error = %q, want bounded redacted message", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("NextClaimed() error = %T, want FailureClass", err)
	}
	if got, want := classified.FailureClass(), FailureRateLimited; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

func TestClaimedSourceClassifiesSDKHTTPError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 25, 16, 45, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) {
			return staticAlertClient{err: sdk.HTTPError{
				Provider:   ProviderGitHubDependabot,
				StatusCode: 404,
				Message:    "not found",
			}}, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), testSecurityAlertWorkItem(now))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want not-found failure")
	}
	var classified interface {
		FailureClass() string
		TerminalFailure() bool
	}
	if !errors.As(err, &classified) {
		t.Fatalf("NextClaimed() error = %T, want classified provider failure", err)
	}
	if got, want := classified.FailureClass(), FailureNotFound; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if !classified.TerminalFailure() {
		t.Fatal("TerminalFailure = false, want true for SDK 404 provider failure")
	}
}

func TestClaimedSourcePreflightProviderAccessIsBoundedAndRedacted(t *testing.T) {
	t.Parallel()

	client := &recordingAlertClient{
		err: securityalerts.GitHubDependabotError{
			StatusCode: 403,
			Message:    "raw upstream error mentions github-token and example-org/example-repo",
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			ScopeID:             "security-alert:github:example-org/example-repo",
			Repository:          "example-org/example-repo",
			Token:               "github-token",
			AllowedRepositories: []string{"example-org/example-repo"},
			MaxPages:            25,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	result, err := source.PreflightProviderAccess(context.Background())
	if err == nil {
		t.Fatal("PreflightProviderAccess() error = nil, want auth-denied failure")
	}
	if got, want := result.TargetCount, 1; got != want {
		t.Fatalf("TargetCount = %d, want %d", got, want)
	}
	if got, want := client.maxPages, 1; got != want {
		t.Fatalf("preflight maxPages = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "github-token") || strings.Contains(err.Error(), "example-org/example-repo") {
		t.Fatalf("PreflightProviderAccess() error = %q, want bounded redacted message", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("PreflightProviderAccess() error = %T, want FailureClass", err)
	}
	if got, want := classified.FailureClass(), FailureAuthDenied; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

type staticAlertClient struct {
	result securityalerts.GitHubDependabotAlertResult
	err    error
}

func (c staticAlertClient) ListRepositoryAlertsPages(
	context.Context,
	string,
	int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	return c.result, c.err
}

func (c staticAlertClient) ListOrganizationAlertsPages(
	context.Context,
	string,
	int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	return c.result, c.err
}

type recordingAlertClient struct {
	result     securityalerts.GitHubDependabotAlertResult
	err        error
	repository string
	maxPages   int
}

func (c *recordingAlertClient) ListRepositoryAlertsPages(
	_ context.Context,
	repository string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.repository = repository
	c.maxPages = maxPages
	return c.result, c.err
}

func (c *recordingAlertClient) ListOrganizationAlertsPages(
	_ context.Context,
	organization string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.repository = organization
	c.maxPages = maxPages
	return c.result, c.err
}

func testSecurityAlertWorkItem(now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "security-alert:security-alert-primary:generation-1",
		RunID:               "security-alert:security-alert-primary:schedule-1",
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: "security-alert-primary",
		SourceSystem:        string(scope.CollectorSecurityAlert),
		ScopeID:             "security-alert:github:example-org/example-repo",
		AcceptanceUnitID:    "security-alert:github:example-org/example-repo",
		SourceRunID:         "security-alert:generation-1",
		GenerationID:        "security-alert:generation-1",
		FairnessKey:         "security-alert:github_dependabot",
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 42,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func testDependabotAlert() securityalerts.GitHubDependabotAlert {
	return securityalerts.GitHubDependabotAlert{
		Number: 7,
		State:  "open",
		Dependency: securityalerts.GitHubDependabotDependency{
			Package:      securityalerts.GitHubDependabotPackage{Ecosystem: "npm", Name: "left-pad"},
			ManifestPath: "package-lock.json",
			Scope:        "runtime",
			Relationship: "direct",
		},
		SecurityAdvisory: securityalerts.GitHubDependabotSecurityAdvisory{
			GHSAID:   "GHSA-abcd-1234",
			CVEID:    "CVE-2026-0001",
			Severity: "high",
		},
		SecurityVulnerability: securityalerts.GitHubDependabotSecurityVulnerability{
			Package:                securityalerts.GitHubDependabotPackage{Ecosystem: "npm", Name: "left-pad"},
			VulnerableVersionRange: "< 1.0.1",
			FirstPatchedVersion:    securityalerts.GitHubDependabotVersion{Identifier: "1.0.1"},
		},
		HTMLURL:   "https://github.com/example-org/example-repo/security/dependabot/7?token=secret",
		CreatedAt: "2026-05-25T14:00:00Z",
		UpdatedAt: "2026-05-25T15:00:00Z",
	}
}

func drainFacts(in <-chan facts.Envelope) []facts.Envelope {
	var envs []facts.Envelope
	for env := range in {
		envs = append(envs, env)
	}
	return envs
}
