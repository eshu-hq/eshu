package workflow

import (
	"strings"
	"testing"
)

func TestValidateSecurityAlertCollectorConfigurationAcceptsGitHubDependabotTarget(t *testing.T) {
	t.Parallel()

	raw := `{
		"targets": [
			{
				"provider": "github_dependabot",
				"scope_id": "security-alert:github:example-org/example-repo",
				"repository": "example-org/example-repo",
				"token_env": "GITHUB_TOKEN",
				"allowed_repositories": ["example-org/example-repo"],
				"api_base_url": "https://api.github.example",
				"source_uri": "http://fixtures.example/security-alerts.json",
				"repository_alert_limit": 50,
				"max_pages": 2
			}
		]
	}`

	if err := ValidateSecurityAlertCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateSecurityAlertCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateSecurityAlertCollectorConfigurationAcceptsOrganizationTarget(t *testing.T) {
	t.Parallel()

	raw := `{
		"targets": [
			{
				"provider": "github_dependabot",
				"scope": "org",
				"scope_id": "security-alert:github-org:example-org",
				"organization": "example-org",
				"token_env": "GITHUB_TOKEN",
				"max_pages": 5
			}
		]
	}`

	if err := ValidateSecurityAlertCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateSecurityAlertCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateSecurityAlertCollectorConfigurationRejectsInvalidOrganizationTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "missing organization",
			raw: `{
				"targets": [{
					"provider": "github_dependabot",
					"scope": "org",
					"scope_id": "security-alert:github-org:example-org",
					"token_env": "GITHUB_TOKEN"
				}]
			}`,
			wantErr: "organization is required",
		},
		{
			name: "repository set for org scope",
			raw: `{
				"targets": [{
					"provider": "github_dependabot",
					"scope": "org",
					"scope_id": "security-alert:github-org:example-org",
					"organization": "example-org",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN"
				}]
			}`,
			wantErr: "repository must be empty",
		},
		{
			name: "unsupported scope",
			raw: `{
				"targets": [{
					"provider": "github_dependabot",
					"scope": "enterprise",
					"scope_id": "security-alert:github-ent:example-ent",
					"token_env": "GITHUB_TOKEN"
				}]
			}`,
			wantErr: "unsupported security alert scope",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSecurityAlertCollectorConfiguration(tt.raw)
			if err == nil {
				t.Fatal("ValidateSecurityAlertCollectorConfiguration() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSecurityAlertCollectorConfigurationRequiresCredentialEnvAndAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "missing token env",
			raw: `{
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"allowed_repositories": ["example-org/example-repo"]
				}]
			}`,
			wantErr: "token_env is required",
		},
		{
			name: "repository outside allowlist",
			raw: `{
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["other-org/other-repo"]
				}]
			}`,
			wantErr: "repository must be listed in allowed_repositories",
		},
		{
			name: "unbounded page count",
			raw: `{
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"],
					"max_pages": 101
				}]
			}`,
			wantErr: "max_pages must be between 1 and 100",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSecurityAlertCollectorConfiguration(tt.raw)
			if err == nil {
				t.Fatal("ValidateSecurityAlertCollectorConfiguration() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSecurityAlertCollectorConfiguration() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSecurityAlertCollectorConfigurationRequiresHTTPSAPIBaseURL(t *testing.T) {
	t.Parallel()

	raw := `{
		"targets": [{
			"provider": "github_dependabot",
			"scope_id": "security-alert:github:example-org/example-repo",
			"repository": "example-org/example-repo",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["example-org/example-repo"],
			"api_base_url": "http://api.github.example",
			"source_uri": "http://fixtures.example/security-alerts.json"
		}]
	}`

	err := ValidateSecurityAlertCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidateSecurityAlertCollectorConfiguration() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "api_base_url must use https") {
		t.Fatalf("ValidateSecurityAlertCollectorConfiguration() error = %q, want HTTPS api_base_url error", err)
	}
}
