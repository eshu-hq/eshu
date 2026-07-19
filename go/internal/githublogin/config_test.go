// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "github-login.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func TestLoadConfigFileParsesProvidersAndGrantFixture(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{
		"version": 1,
		"default_provider_id": "github-dev",
		"state_ttl": "5m",
		"providers": [{
			"provider_config_id": "github-dev",
			"client_id": "client-id",
			"client_secret_file": "/dev/null",
			"redirect_url": "https://eshu.example.test/api/v0/auth/github/callback",
			"tenant_id": "tenant_a",
			"workspace_id": "workspace_a",
			"allowed_orgs": ["Eshu-HQ"]
		}],
		"group_role_mappings": [{
			"group": "eshu-hq/developers",
			"role_ids": ["developer"]
		}],
		"role_grants": [{
			"role_id": "developer",
			"allowed_scope_ids": ["scope_a"]
		}]
	}`)

	config, resolver, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if len(config.Providers) != 1 || config.Providers[0].ProviderConfigID != "github-dev" {
		t.Fatalf("providers = %#v, want one github-dev provider", config.Providers)
	}
	// allowed_orgs must be lowercased by ValidateConfig's normalization.
	if len(config.Providers[0].AllowedOrgs) != 1 || config.Providers[0].AllowedOrgs[0] != "eshu-hq" {
		t.Fatalf("allowed orgs = %v, want [eshu-hq] (lowercased)", config.Providers[0].AllowedOrgs)
	}

	resolution, ok, err := resolver.ResolveGroupGrants(context.Background(), GrantQuery{
		GroupHashes: []string{SHA256Hash("eshu-hq/developers")},
	})
	if err != nil || !ok {
		t.Fatalf("ResolveGroupGrants() = %#v, %v, %v, want a resolved developer role", resolution, ok, err)
	}
	if len(resolution.RoleIDs) != 1 || resolution.RoleIDs[0] != "developer" {
		t.Fatalf("resolved roles = %v, want [developer]", resolution.RoleIDs)
	}
}

func TestLoadConfigFileRejectsProviderWithNoAllowedOrgs(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{
		"version": 1,
		"providers": [{
			"provider_config_id": "github-dev",
			"client_id": "client-id",
			"redirect_url": "https://eshu.example.test/callback",
			"tenant_id": "tenant_a",
			"workspace_id": "workspace_a"
		}]
	}`)

	if _, _, err := LoadConfigFile(path); err == nil {
		t.Fatal("LoadConfigFile() error = nil, want error for a provider with no allowed_orgs")
	}
}

func TestLoadConfigFileRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{"version": 2, "providers": []}`)
	if _, _, err := LoadConfigFile(path); err == nil {
		t.Fatal("LoadConfigFile() error = nil, want error for unsupported version")
	}
}

func TestLoadConfigFileRejectsMalformedGrantConfig(t *testing.T) {
	t.Parallel()

	// Two role grants sharing the same role_id — StaticGrantResolver's (via
	// ResolveGroupGrants) internal roleGrantIndex construction must reject
	// this as a startup-time config error rather than silently pick one.
	path := writeConfigFile(t, `{
		"version": 1,
		"providers": [{
			"provider_config_id": "github-dev",
			"client_id": "client-id",
			"redirect_url": "https://eshu.example.test/callback",
			"tenant_id": "tenant_a",
			"workspace_id": "workspace_a",
			"allowed_orgs": ["eshu-hq"]
		}],
		"role_grants": [
			{"role_id": "developer"},
			{"role_id": "developer"}
		]
	}`)

	if _, _, err := LoadConfigFile(path); err == nil {
		t.Fatal("LoadConfigFile() error = nil, want error for duplicate role grant ids")
	}
}
