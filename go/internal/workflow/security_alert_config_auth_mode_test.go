// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

// TestValidateSecurityAlertAuthModes pins the admission mirror of the runtime
// dual-mode table: token_env stays the back-compatible default, github_app
// requires all three App env references, the two shapes never mix, and an
// unknown auth_mode fails.
func TestValidateSecurityAlertAuthModes(t *testing.T) {
	t.Parallel()

	target := func(authMode, tokenEnv, appIDEnv, installationEnv, keyEnv string) string {
		return `{"targets": [{
			"provider": "github_dependabot",
			"scope_id": "security-alert:github:example-org/example-repo",
			"repository": "example-org/example-repo",
			"auth_mode": "` + authMode + `",
			"token_env": "` + tokenEnv + `",
			"github_app_id_env": "` + appIDEnv + `",
			"github_app_installation_id_env": "` + installationEnv + `",
			"github_app_private_key_env": "` + keyEnv + `",
			"allowed_repositories": ["example-org/example-repo"]
		}]}`
	}
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "token default valid",
			raw:  target("", "GITHUB_TOKEN", "", "", ""),
		},
		{
			name: "token explicit valid",
			raw:  target("token_env", "GITHUB_TOKEN", "", "", ""),
		},
		{
			name: "github app valid",
			raw:  target("github_app", "", "GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY"),
		},
		{
			name:    "token mode missing token_env",
			raw:     target("", "", "", "", ""),
			wantErr: "token_env is required",
		},
		{
			name:    "token mode refuses app id env",
			raw:     target("", "GITHUB_TOKEN", "GITHUB_APP_ID", "", ""),
			wantErr: "github_app",
		},
		{
			name:    "token mode refuses private key env",
			raw:     target("token_env", "GITHUB_TOKEN", "", "", "GITHUB_APP_PRIVATE_KEY"),
			wantErr: "github_app",
		},
		{
			name:    "app mode refuses token_env",
			raw:     target("github_app", "GITHUB_TOKEN", "GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY"),
			wantErr: "token_env must be empty",
		},
		{
			name:    "app mode missing app id env",
			raw:     target("github_app", "", "", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY"),
			wantErr: "github_app_id_env",
		},
		{
			name:    "app mode missing installation env",
			raw:     target("github_app", "", "GITHUB_APP_ID", "", "GITHUB_APP_PRIVATE_KEY"),
			wantErr: "github_app_installation_id_env",
		},
		{
			name:    "app mode missing private key env",
			raw:     target("github_app", "", "GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", ""),
			wantErr: "github_app_private_key_env",
		},
		{
			name:    "unknown auth mode",
			raw:     target("magic", "GITHUB_TOKEN", "", "", ""),
			wantErr: "unsupported security alert auth_mode",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSecurityAlertCollectorConfiguration(tt.raw)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSecurityAlertCollectorConfiguration() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSecurityAlertCollectorConfiguration() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
