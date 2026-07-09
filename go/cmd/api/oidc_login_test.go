// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestNewOIDCLoginHandlerDisabledByDefault(t *testing.T) {
	t.Parallel()

	handler, err := newOIDCLoginHandler(func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("newOIDCLoginHandler() error = %v, want nil", err)
	}
	if handler != nil {
		t.Fatalf("newOIDCLoginHandler() = %#v, want nil when OIDC is unset", handler)
	}
}

// TestNewOIDCLoginHandlerEnabledWithoutConfigFileRequiresDatabase proves
// ESHU_AUTH_OIDC_ENABLED=true with no env config file is a valid activation
// path (#4966, epic #4962: DB-only OIDC providers, no config file needed) as
// long as a database is available — the requirement shifted from "config
// file" to "database", since DB-backed providers are the only possible
// provider source in that combination.
func TestNewOIDCLoginHandlerEnabledWithoutConfigFileRequiresDatabase(t *testing.T) {
	t.Parallel()

	envOnly := func(key string) string {
		if key == envAuthOIDCEnabled {
			return "true"
		}
		return ""
	}

	_, err := newOIDCLoginHandler(envOnly, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("newOIDCLoginHandler() error = %v, want a postgres-required error when enabled with no config file and no db", err)
	}

	handler, err := newOIDCLoginHandler(envOnly, &sql.DB{}, nil, nil)
	if err != nil {
		t.Fatalf("newOIDCLoginHandler() error = %v, want nil when a database is available", err)
	}
	if handler == nil || handler.Service == nil {
		t.Fatalf("newOIDCLoginHandler() = %#v, want a wired DB-only handler", handler)
	}
}

func TestNewOIDCLoginHandlerParsesEnabledWithVarBoolSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     string
		config  string
		wantErr string
		wantNil bool
	}{
		{
			// No config file and no db: numeric "1" still parses as
			// enabled=true (boolean semantics unchanged), and with no
			// possible provider source (no config file, no db for DB-backed
			// providers) newOIDCLoginHandler now fails on the db requirement
			// rather than the old config-file requirement (#4966, epic
			// #4962: a config file is no longer strictly required when
			// enabled).
			name:    "numeric true with no config and no db requires database",
			env:     "1",
			wantErr: "postgres",
		},
		{
			name:    "numeric false disables mounted config",
			env:     "0",
			config:  "/mounted/oidc.json",
			wantNil: true,
		},
		{
			name:    "invalid bool fails closed",
			env:     "sometimes",
			config:  "/mounted/oidc.json",
			wantErr: envAuthOIDCEnabled,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := newOIDCLoginHandler(func(key string) string {
				switch key {
				case envAuthOIDCEnabled:
					return tt.env
				case envAuthOIDCConfigFile:
					return tt.config
				default:
					return ""
				}
			}, nil, nil, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("newOIDCLoginHandler() error = %v, want %s failure", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("newOIDCLoginHandler() error = %v, want nil", err)
			}
			if tt.wantNil && handler != nil {
				t.Fatalf("newOIDCLoginHandler() = %#v, want nil", handler)
			}
		})
	}
}

func TestNewOIDCLoginHandlerLoadsFileBackedConfig(t *testing.T) {
	t.Parallel()

	path := writeOIDCTestConfig(t, `{
  "version": 1,
  "providers": [{
    "provider_config_id": "okta-dev",
    "issuer_url": "https://idp.example.test/oauth2/default",
    "client_id": "client-id",
    "redirect_url": "https://eshu.example.test/api/v0/auth/oidc/callback",
    "tenant_id": "tenant_a",
    "workspace_id": "workspace_a"
  }],
  "group_role_mappings": [{
    "group_sha256": "sha256:group",
    "role_ids": ["developer"]
  }],
  "role_grants": [{
    "role_id": "developer",
    "policy_revision_hash": "sha256:policy",
    "allowed_scope_ids": ["scope_a"],
    "allowed_repository_ids": ["repo_a"]
  }]
}`)
	db := &sql.DB{}
	handler, err := newOIDCLoginHandler(func(key string) string {
		switch key {
		case envAuthOIDCConfigFile:
			return path
		case envAuthOIDCStateTTL:
			return "5m"
		default:
			return ""
		}
	}, db, nil, nil)
	if err != nil {
		t.Fatalf("newOIDCLoginHandler() error = %v, want nil", err)
	}
	if handler == nil || handler.Service == nil || handler.SessionIssuer == nil {
		t.Fatalf("newOIDCLoginHandler() = %#v, want wired handler", handler)
	}
	if handler.SessionRefreshWindow != query.DefaultOIDCSessionRefreshWindow {
		t.Fatalf("refresh window = %v, want default %v", handler.SessionRefreshWindow, query.DefaultOIDCSessionRefreshWindow)
	}
}

func TestNewOIDCLoginHandlerParsesSessionRefreshWindow(t *testing.T) {
	t.Parallel()

	path := writeOIDCTestConfig(t, `{
  "version": 1,
  "providers": [{
    "provider_config_id": "okta-dev",
    "issuer_url": "https://idp.example.test/oauth2/default",
    "client_id": "client-id",
    "redirect_url": "https://eshu.example.test/api/v0/auth/oidc/callback",
    "tenant_id": "tenant_a",
    "workspace_id": "workspace_a"
  }]
}`)
	handler, err := newOIDCLoginHandler(func(key string) string {
		switch key {
		case envAuthOIDCConfigFile:
			return path
		case envAuthOIDCSessionRefreshWindow:
			return "20m"
		default:
			return ""
		}
	}, &sql.DB{}, nil, nil)
	if err != nil {
		t.Fatalf("newOIDCLoginHandler() error = %v, want nil", err)
	}
	if got, want := handler.SessionRefreshWindow, 20*time.Minute; got != want {
		t.Fatalf("refresh window = %v, want %v", got, want)
	}
}

func TestNewOIDCLoginHandlerInvalidSessionRefreshWindowFailsStartup(t *testing.T) {
	t.Parallel()

	path := writeOIDCTestConfig(t, `{
  "version": 1,
  "providers": [{
    "provider_config_id": "okta-dev",
    "issuer_url": "https://idp.example.test/oauth2/default",
    "client_id": "client-id",
    "redirect_url": "https://eshu.example.test/api/v0/auth/oidc/callback",
    "tenant_id": "tenant_a",
    "workspace_id": "workspace_a"
  }]
}`)
	for _, raw := range []string{"0", "-1m", "not-a-duration"} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			_, err := newOIDCLoginHandler(func(key string) string {
				switch key {
				case envAuthOIDCConfigFile:
					return path
				case envAuthOIDCSessionRefreshWindow:
					return raw
				default:
					return ""
				}
			}, &sql.DB{}, nil, nil)
			if err == nil || !strings.Contains(err.Error(), envAuthOIDCSessionRefreshWindow) {
				t.Fatalf("newOIDCLoginHandler() error = %v, want refresh window failure", err)
			}
		})
	}
}

func TestNewOIDCLoginHandlerInvalidProviderOverrideFailsStartup(t *testing.T) {
	t.Parallel()

	path := writeOIDCTestConfig(t, `{
  "version": 1,
  "providers": [{
    "provider_config_id": "okta-dev",
    "issuer_url": "https://idp.example.test/oauth2/default",
    "client_id": "client-id",
    "redirect_url": "https://eshu.example.test/api/v0/auth/oidc/callback",
    "tenant_id": "tenant_a",
    "workspace_id": "workspace_a"
  }]
}`)
	_, err := newOIDCLoginHandler(func(key string) string {
		switch key {
		case envAuthOIDCConfigFile:
			return path
		case envAuthOIDCProviderID:
			return "missing-provider"
		default:
			return ""
		}
	}, &sql.DB{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "validate oidc login config") {
		t.Fatalf("newOIDCLoginHandler() error = %v, want invalid provider override", err)
	}
}

func writeOIDCTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/oidc.json"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write oidc config: %v", err)
	}
	return path
}
