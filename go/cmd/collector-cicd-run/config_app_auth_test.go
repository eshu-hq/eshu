// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestLoadClaimedRuntimeConfigResolvesGitHubAppReferences(t *testing.T) {
	t.Parallel()
	const privateKey = "private-key-value-canary"
	instanceJSON := `[{"instance_id":"cicd-app","collector_kind":"ci_cd_run","mode":"continuous","enabled":true,"claims_enabled":true,"configuration":{"targets":[{"provider":"github_actions","scope_id":"ci:example/repo","repository":"example/repo","allowed_repositories":["example/repo"],"auth_mode":"github_app","github_app_id_env":"APP_ID","github_app_installation_id_env":"INSTALL_ID","github_app_private_key_env":"PRIVATE_KEY","max_jobs":25,"max_artifacts":25,"max_deployments":3}]}}]`
	getenv := func(key string) string {
		switch key {
		case envCollectorInstances:
			return instanceJSON
		case "APP_ID":
			return "123"
		case "INSTALL_ID":
			return "456"
		case "PRIVATE_KEY":
			return privateKey
		default:
			return ""
		}
	}
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		if strings.Contains(err.Error(), privateKey) {
			t.Fatalf("credential value leaked: %v", err)
		}
		t.Fatalf("valid App target rejected: %v", err)
	}
	if len(config.GitHubSource.Targets) != 1 {
		t.Fatalf("GitHub target count = %d, want 1", len(config.GitHubSource.Targets))
	}
	target := config.GitHubSource.Targets[0]
	if target.MaxDeployments != 3 {
		t.Fatalf("MaxDeployments = %d, want configured bound 3", target.MaxDeployments)
	}
	if target.Token != "" {
		t.Fatalf("static token unexpectedly set for App target")
	}
	if target.GitHubAppID != "123" || target.GitHubAppInstallationID != "456" || target.GitHubAppPrivateKey != privateKey {
		t.Fatalf("App env references were not resolved into runtime target")
	}
}

func TestLoadClaimedRuntimeConfigRejectsUnresolvedAppEnv(t *testing.T) {
	t.Parallel()
	const config = `[{"instance_id":"cicd-app","collector_kind":"ci_cd_run","mode":"continuous","enabled":true,"claims_enabled":true,"configuration":{"targets":[{"provider":"github_actions","scope_id":"ci:example/repo","repository":"example/repo","allowed_repositories":["example/repo"],"auth_mode":"github_app","github_app_id_env":"APP_ID","github_app_installation_id_env":"INSTALL_ID","github_app_private_key_env":"PRIVATE_KEY","max_jobs":25,"max_artifacts":25}]}}]`
	for _, test := range []struct{ missing, field string }{
		{"APP_ID", "github_app_id_env"},
		{"INSTALL_ID", "github_app_installation_id_env"},
		{"PRIVATE_KEY", "github_app_private_key_env"},
	} {
		t.Run(test.missing, func(t *testing.T) {
			t.Parallel()
			_, err := loadClaimedRuntimeConfig(func(key string) string {
				if key == envCollectorInstances {
					return config
				}
				if key == test.missing {
					return ""
				}
				switch key {
				case "APP_ID", "INSTALL_ID", "PRIVATE_KEY":
					return "secret-value-canary"
				default:
					return ""
				}
			})
			if err == nil || !strings.Contains(err.Error(), test.field) || strings.Contains(err.Error(), "secret-value-canary") {
				t.Fatalf("load config error = %v, want named field without value", err)
			}
		})
	}
}

func TestMapTargetRejectsAppReferencesMixedWithOtherModes(t *testing.T) {
	t.Parallel()
	base := targetJSON{ScopeID: "ci:example/repo", Repository: "example/repo", AllowedRepositories: []string{"example/repo"}}
	cases := []struct {
		name     string
		mode     string
		tokenEnv string
		hostFile string
		appIDEnv string
		want     string
	}{
		{"static with app", "token_env", "TOKEN", "", "APP_ID", "github_app"},
		{"host with app", "host_minted", "", "/token.json", "APP_ID", "github_app"},
		{"app with host", "github_app", "", "/token.json", "APP_ID", "host_token_file"},
		{"app with static", "github_app", "TOKEN", "", "APP_ID", "token_env"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := base
			v.AuthMode, v.TokenEnv, v.HostTokenFile, v.GitHubAppIDEnv = tc.mode, tc.tokenEnv, tc.hostFile, tc.appIDEnv
			v.GitHubAppInstallationIDEnv, v.GitHubAppPrivateKeyEnv = "INSTALL_ID", "KEY"
			_, err := mapTarget(v, func(string) string { return "credential-canary" })
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "credential-canary") {
				t.Fatalf("map target = %v, want %s without secret", err, tc.want)
			}
		})
	}
}
