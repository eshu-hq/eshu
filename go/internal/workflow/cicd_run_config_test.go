// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestValidateCICDRunCollectorConfigurationAcceptsBoundedGitHubActionsTargets(t *testing.T) {
	t.Parallel()

	if err := ValidateCICDRunCollectorConfiguration(testCICDRunConfig()); err != nil {
		t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateCICDRunCollectorConfigurationRejectsUnsafeShape(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw     string
		wantErr string
	}{
		"blank token env": {
			raw:     `{"targets":[{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/repo","repository":"example/repo","allowed_repositories":["example/repo"],"max_runs":1,"max_jobs":10,"max_artifacts":10}]}`,
			wantErr: "token_env is required",
		},
		"repository outside allowlist": {
			raw:     `{"targets":[{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/other"],"max_runs":1,"max_jobs":10,"max_artifacts":10}]}`,
			wantErr: "repository must be listed in allowed_repositories",
		},
		"unbounded artifacts": {
			raw:     `{"targets":[{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/repo"],"max_runs":1,"max_jobs":10,"max_artifacts":501}]}`,
			wantErr: "max_artifacts",
		},
		"credentialed api url": {
			raw:     `{"targets":[{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/repo"],"api_base_url":"https://token@example.com","max_runs":1,"max_jobs":10,"max_artifacts":10}]}`,
			wantErr: "api_base_url must not include credentials",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateCICDRunCollectorConfiguration(tc.raw)
			if err == nil {
				t.Fatal("ValidateCICDRunCollectorConfiguration() error = nil, want non-nil")
			}
			if got := err.Error(); !strings.Contains(got, tc.wantErr) {
				t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %q, want substring %q", got, tc.wantErr)
			}
		})
	}
}

// TestValidateCICDRunCollectorConfigurationAcceptsOmittedMaxRuns proves the
// #5338 PR B default: an omitted/zero max_runs must pass this validation
// layer (the ghactionsruntime collector resolves it to its own default of
// 10 downstream), not be rejected the way it was when max_runs was a
// mandatory field.
func TestValidateCICDRunCollectorConfigurationAcceptsOmittedMaxRuns(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/repo"],"max_jobs":10,"max_artifacts":10}]}`
	if err := ValidateCICDRunCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %v, want nil (omitted max_runs defers to the collector default)", err)
	}
}

// TestValidateCICDRunCollectorConfigurationRejectsNegativeMaxRuns proves the
// omitted-max_runs allowance does not also open the door to an explicit
// negative value.
func TestValidateCICDRunCollectorConfigurationRejectsNegativeMaxRuns(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/repo"],"max_runs":-1,"max_jobs":10,"max_artifacts":10}]}`
	err := ValidateCICDRunCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidateCICDRunCollectorConfiguration() error = nil, want negative max_runs rejection")
	}
	if !strings.Contains(err.Error(), "max_runs") {
		t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %q, want max_runs substring", err)
	}
}

// TestValidateCICDRunCollectorConfigurationAcceptsBoundedGitLabCITargets
// proves a gitlab_ci target — including one nested under a subgroup, which
// GitHub Actions' owner/repo shape has no equivalent for — passes this
// shared validation layer. Before this test (#5427, codex P1 on PR #5778),
// validateCICDRunTargetConfiguration hard-rejected any provider other than
// "github_actions" and required repository to be EXACTLY two "/"-segments,
// so a gitlab_ci target's collector instance configuration would fail
// CollectorInstance.Validate() before parseCICDRunRuntimeConfiguration
// (cmd/collector-cicd-run/config.go) ever got a chance to route it.
func TestValidateCICDRunCollectorConfigurationAcceptsBoundedGitLabCITargets(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/eshu-hq/demo","repository":"eshu-hq/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["eshu-hq/demo"],"max_runs":1,"max_jobs":10}]}`
	if err := ValidateCICDRunCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %v, want nil", err)
	}
}

// TestValidateCICDRunCollectorConfigurationAcceptsGitLabSubgroupProjectPath
// proves a GitLab project nested under a subgroup ("group/subgroup/project",
// three "/"-segments) is accepted — GitHub Actions' owner/repo shape is
// always exactly two segments, but GitLab projects may nest under any
// number of subgroups.
func TestValidateCICDRunCollectorConfigurationAcceptsGitLabSubgroupProjectPath(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/eshu-hq/platform/demo","repository":"eshu-hq/platform/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["eshu-hq/platform/demo"],"max_runs":1,"max_jobs":10}]}`
	if err := ValidateCICDRunCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %v, want nil", err)
	}
}

// TestValidateCICDRunCollectorConfigurationDoesNotRequireMaxArtifactsForGitLab
// proves max_artifacts — a GitHub-Actions-only bound; GitLab reports job
// artifacts inline with no separate paginated endpoint to bound — is not
// required for a gitlab_ci target.
func TestValidateCICDRunCollectorConfigurationDoesNotRequireMaxArtifactsForGitLab(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/eshu-hq/demo","repository":"eshu-hq/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["eshu-hq/demo"],"max_runs":1,"max_jobs":10,"max_artifacts":0}]}`
	if err := ValidateCICDRunCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateCICDRunCollectorConfiguration() error = %v, want nil (max_artifacts is not a GitLab concept)", err)
	}
}

func testCICDRunConfig() string {
	return `{
		"targets": [{
			"provider": "github_actions",
			"scope_id": "ci-cd:github-actions:example/repo",
			"repository": "example/repo",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["example/repo"],
			"max_runs": 1,
			"max_jobs": 100,
			"max_artifacts": 100
		}]
	}`
}

func TestCICDRunCollectorInstanceValidationUsesCICDRunConfig(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 7, 15, 0, 0, 0, time.UTC)
	instance := CollectorInstance{
		InstanceID:     "collector-ci-cd-run",
		CollectorKind:  scope.CollectorCICDRun,
		Mode:           CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testCICDRunConfig(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("CollectorInstance.Validate() error = %v, want nil", err)
	}
}
