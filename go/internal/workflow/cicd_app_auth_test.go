// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

func TestCICDRunGitHubAppConfiguration(t *testing.T) {
	t.Parallel()
	const configuration = `{"targets":[{"provider":"github_actions","scope_id":"ci:example/repository","repository":"example/repository","allowed_repositories":["example/repository"],"auth_mode":"github_app","github_app_id_env":"APP_ID","github_app_installation_id_env":"INSTALLATION_ID","github_app_private_key_env":"PRIVATE_KEY","max_jobs":25,"max_artifacts":25}]}`
	if err := ValidateCICDRunCollectorConfiguration(configuration); err != nil {
		t.Fatalf("valid GitHub App target rejected: %v", err)
	}
}

func TestCICDRunGitHubAppConfigurationRejectsIncompleteOrMixedAuth(t *testing.T) {
	t.Parallel()
	base := cicdRunTargetConfiguration{
		Provider: cicdRunProviderGitHubActions, ScopeID: "ci:example/repo", Repository: "example/repo",
		AllowedRepositories: []string{"example/repo"}, AuthMode: "github_app",
		GitHubAppIDEnv: "APP_ID", GitHubAppInstallationIDEnv: "INSTALL_ID",
		GitHubAppPrivateKeyEnv: "PRIVATE_KEY", MaxJobs: 25, MaxArtifacts: 25,
	}
	cases := []struct {
		name   string
		mutate func(*cicdRunTargetConfiguration)
		want   string
	}{
		{"missing app id", func(target *cicdRunTargetConfiguration) { target.GitHubAppIDEnv = "" }, "github_app_id_env"},
		{"missing installation id", func(target *cicdRunTargetConfiguration) { target.GitHubAppInstallationIDEnv = "" }, "github_app_installation_id_env"},
		{"missing key", func(target *cicdRunTargetConfiguration) { target.GitHubAppPrivateKeyEnv = "" }, "github_app_private_key_env"},
		{"mixed token", func(target *cicdRunTargetConfiguration) { target.TokenEnv = "TOKEN" }, "token_env"},
		{"gitlab still requires token", func(target *cicdRunTargetConfiguration) { target.Provider = cicdRunProviderGitLabCI }, "token_env"},
		{"unknown mode", func(target *cicdRunTargetConfiguration) { target.AuthMode = "other" }, "auth_mode"},
		{"token with app ref", func(target *cicdRunTargetConfiguration) { target.AuthMode = "token_env"; target.TokenEnv = "TOKEN" }, "github_app"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target := base
			test.mutate(&target)
			err := validateCICDRunTargetConfiguration(target)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate target = %v, want named %s error", err, test.want)
			}
		})
	}
}

func TestCICDRunThreeAuthModesRejectMixedCredentials(t *testing.T) {
	t.Parallel()
	base := cicdRunTargetConfiguration{
		Provider: cicdRunProviderGitHubActions, ScopeID: "ci:example/repo",
		Repository: "example/repo", AllowedRepositories: []string{"example/repo"},
		MaxJobs: 25, MaxArtifacts: 25,
	}
	cases := []struct {
		name   string
		mutate func(*cicdRunTargetConfiguration)
		want   string
	}{
		{"default token", func(v *cicdRunTargetConfiguration) { v.TokenEnv = "TOKEN" }, ""},
		{"host file", func(v *cicdRunTargetConfiguration) { v.AuthMode = "host_minted"; v.HostTokenFile = "/token.json" }, ""},
		{"direct app", func(v *cicdRunTargetConfiguration) {
			v.AuthMode = "github_app"
			v.GitHubAppIDEnv = "APP_ID"
			v.GitHubAppInstallationIDEnv = "INSTALL_ID"
			v.GitHubAppPrivateKeyEnv = "KEY"
		}, ""},
		{"token plus host", func(v *cicdRunTargetConfiguration) { v.TokenEnv = "TOKEN"; v.HostTokenFile = "/token.json" }, "host_token_file"},
		{"token plus app", func(v *cicdRunTargetConfiguration) { v.TokenEnv = "TOKEN"; v.GitHubAppIDEnv = "APP_ID" }, "github_app"},
		{"host plus app", func(v *cicdRunTargetConfiguration) {
			v.AuthMode = "host_minted"
			v.HostTokenFile = "/token.json"
			v.GitHubAppIDEnv = "APP_ID"
		}, "github_app"},
		{"app plus host", func(v *cicdRunTargetConfiguration) {
			v.AuthMode = "github_app"
			v.GitHubAppIDEnv = "APP_ID"
			v.GitHubAppInstallationIDEnv = "INSTALL_ID"
			v.GitHubAppPrivateKeyEnv = "KEY"
			v.HostTokenFile = "/token.json"
		}, "host_token_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := base
			tc.mutate(&v)
			err := validateCICDRunTargetConfiguration(v)
			if tc.want == "" && err != nil {
				t.Fatalf("valid mode rejected: %v", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}
