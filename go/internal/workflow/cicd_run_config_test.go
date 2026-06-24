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
