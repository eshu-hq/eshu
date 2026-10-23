// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
)

func appModeTestPrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func appModeInstancesJSON(targets string) string {
	return `[{
		"instance_id": "security-alert-primary",
		"collector_kind": "security_alert",
		"mode": "continuous",
		"enabled": true,
		"claims_enabled": true,
		"configuration": {"targets": [` + targets + `]}
	}]`
}

func appModeTokenTarget(apiBaseURL string) string {
	return `{
		"provider": "github_dependabot",
		"scope_id": "security-alert:github:example-org/example-repo",
		"repository": "example-org/example-repo",
		"token_env": "GITHUB_TOKEN",
		"allowed_repositories": ["example-org/example-repo"],
		"api_base_url": "` + apiBaseURL + `"
	}`
}

func appModeGitHubAppTarget(apiBaseURL string) string {
	return `{
		"provider": "github_dependabot",
		"auth_mode": "github_app",
		"scope_id": "security-alert:github:example-org/example-repo",
		"repository": "example-org/example-repo",
		"github_app_id_env": "GITHUB_APP_ID",
		"github_app_installation_id_env": "GITHUB_APP_INSTALLATION_ID",
		"github_app_private_key_env": "GITHUB_APP_PRIVATE_KEY",
		"allowed_repositories": ["example-org/example-repo"],
		"api_base_url": "` + apiBaseURL + `"
	}`
}

func appModeGetenv(t *testing.T, targets string, extra map[string]string) func(string) string {
	t.Helper()
	keyPEM := appModeTestPrivateKey(t)
	values := map[string]string{
		envCollectorInstances:        appModeInstancesJSON(targets),
		envCollectorInstanceID:       "security-alert-primary",
		envOwnerID:                   "pod-security-alerts",
		"GITHUB_TOKEN":               "static-token-value",
		"GITHUB_APP_ID":              "42",
		"GITHUB_APP_INSTALLATION_ID": "777",
		"GITHUB_APP_PRIVATE_KEY":     keyPEM,
	}
	for k, v := range extra {
		values[k] = v
	}
	return func(key string) string { return values[key] }
}

// TestLoadClaimedRuntimeConfigResolvesGitHubAppEnvs pins env resolution for
// github_app targets: the three App references resolve to runtime values,
// the static token stays empty, and the mode survives onto the target.
func TestLoadClaimedRuntimeConfigResolvesGitHubAppEnvs(t *testing.T) {
	t.Parallel()

	getenv := appModeGetenv(t, appModeGitHubAppTarget(""), nil)
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got := len(config.Source.Targets); got != 1 {
		t.Fatalf("targets = %d, want 1", got)
	}
	target := config.Source.Targets[0]
	if target.AuthMode != alertruntime.AuthModeGitHubApp {
		t.Fatalf("AuthMode = %q, want github_app", target.AuthMode)
	}
	if target.Token != "" {
		t.Fatal("Token is set for a github_app target; the minted token must never be persisted")
	}
	if target.GitHubAppID != "42" || target.GitHubAppInstallationID != "777" || target.GitHubAppPrivateKey == "" {
		t.Fatalf("app credentials not resolved: id=%q installation=%q key-empty=%v",
			target.GitHubAppID, target.GitHubAppInstallationID, target.GitHubAppPrivateKey == "")
	}
}

// TestLoadClaimedRuntimeConfigRejectsMixedCredentialShapes pins the env
// resolution mirror: cross-mode references and unresolvable envs fail
// closed with no silent fallback between modes.
func TestLoadClaimedRuntimeConfigRejectsMixedCredentialShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets func(apiBase string) string
		extra   map[string]string
		wantErr string
	}{
		{
			name: "token mode refuses app id env",
			targets: func(apiBase string) string {
				return `{"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"github_app_id_env": "GITHUB_APP_ID",
					"allowed_repositories": ["example-org/example-repo"],
					"api_base_url": "` + apiBase + `"}`
			},
			wantErr: "github_app",
		},
		{
			name: "app mode refuses token env",
			targets: func(apiBase string) string {
				return `{"provider": "github_dependabot",
					"auth_mode": "github_app",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"github_app_id_env": "GITHUB_APP_ID",
					"github_app_installation_id_env": "GITHUB_APP_INSTALLATION_ID",
					"github_app_private_key_env": "GITHUB_APP_PRIVATE_KEY",
					"allowed_repositories": ["example-org/example-repo"],
					"api_base_url": "` + apiBase + `"}`
			},
			wantErr: "token_env",
		},
		{
			name:    "app mode unresolvable private key",
			targets: appModeGitHubAppTarget,
			extra:   map[string]string{"GITHUB_APP_PRIVATE_KEY": ""},
			wantErr: "did not resolve",
		},
		{
			name:    "token mode unresolvable token",
			targets: appModeTokenTarget,
			extra:   map[string]string{"GITHUB_TOKEN": ""},
			wantErr: "did not resolve",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := appModeGetenv(t, tt.targets(""), tt.extra)
			_, err := loadClaimedRuntimeConfig(getenv)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadClaimedRuntimeConfig() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestProviderAccessPreflightHandsAppTargetToFactory pins the binary
// preflight wiring for github_app targets: env references resolve through to
// the runtime target the factory receives (mode, App values, no static
// token), and the bounded preflight read runs against it. The HTTP mint plus
// alert-read exchange itself is proven at the runtime layer, where the
// coordinator's https-only admission does not block fake bases.
func TestProviderAccessPreflightHandsAppTargetToFactory(t *testing.T) {
	t.Parallel()

	getenv := appModeGetenv(t, appModeGitHubAppTarget("https://api.github.example"), nil)
	var seen []alertruntime.TargetConfig
	client := &preflightAlertClient{}
	err := runProviderAccessPreflightWithSignals(
		context.Background(),
		getenv,
		func(target alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
			seen = append(seen, target)
			return client, nil
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runProviderAccessPreflightWithSignals() error = %v, want nil", err)
	}
	if len(seen) != 1 {
		t.Fatalf("factory targets = %d, want 1", len(seen))
	}
	target := seen[0]
	if target.AuthMode != alertruntime.AuthModeGitHubApp {
		t.Fatalf("AuthMode = %q, want github_app", target.AuthMode)
	}
	if target.Token != "" {
		t.Fatal("Token is set for a github_app target; the minted token must never be persisted")
	}
	if target.GitHubAppID != "42" || target.GitHubAppInstallationID != "777" || target.GitHubAppPrivateKey == "" {
		t.Fatalf("app credentials not wired: id=%q installation=%q key-empty=%v",
			target.GitHubAppID, target.GitHubAppInstallationID, target.GitHubAppPrivateKey == "")
	}
	if got := client.maxPages; got != 1 {
		t.Fatalf("preflight maxPages = %d, want 1", got)
	}
}
