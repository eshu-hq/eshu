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

	handler, err := newOIDCLoginHandler(func(string) string { return "" }, nil, nil)
	if err != nil {
		t.Fatalf("newOIDCLoginHandler() error = %v, want nil", err)
	}
	if handler != nil {
		t.Fatalf("newOIDCLoginHandler() = %#v, want nil when OIDC is unset", handler)
	}
}

func TestNewOIDCLoginHandlerEnabledRequiresConfigFile(t *testing.T) {
	t.Parallel()

	_, err := newOIDCLoginHandler(func(key string) string {
		if key == envAuthOIDCEnabled {
			return "true"
		}
		return ""
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), envAuthOIDCConfigFile) {
		t.Fatalf("newOIDCLoginHandler() error = %v, want config file requirement", err)
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
			name:    "numeric true requires config",
			env:     "1",
			wantErr: envAuthOIDCConfigFile,
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
			}, nil, nil)
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
	}, db, nil)
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
	}, &sql.DB{}, nil)
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
			}, &sql.DB{}, nil)
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
	}, &sql.DB{}, nil)
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
