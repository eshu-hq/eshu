// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

// TestNewOIDCBearerResolverDisabledWhenAudienceUnset mirrors cmd/api's
// identically named test: ESHU_AUTH_RESOURCE_URI unset disables the IdP
// bearer resolver entirely on mcp-server too (issue #5162), with no
// database access attempted.
func TestNewOIDCBearerResolverDisabledWhenAudienceUnset(t *testing.T) {
	t.Parallel()
	resolver, err := newOIDCBearerResolver(context.Background(), func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("newOIDCBearerResolver() error = %v, want nil", err)
	}
	if resolver != nil {
		t.Fatalf("newOIDCBearerResolver() = %#v, want nil when ESHU_AUTH_RESOURCE_URI is unset", resolver)
	}
}

// TestNewOIDCBearerResolverRequiresDatabaseWhenAudienceSet mirrors cmd/api's
// identically named test.
func TestNewOIDCBearerResolverRequiresDatabaseWhenAudienceSet(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == envAuthResourceURI {
			return "https://eshu.example.test"
		}
		return ""
	}
	_, err := newOIDCBearerResolver(context.Background(), getenv, nil, nil, nil)
	if err == nil {
		t.Fatal("newOIDCBearerResolver() error = nil, want a postgres-required error")
	}
}

// TestLoadOIDCBearerEnvConfigEmptyPathIsValid mirrors cmd/api's identically
// named test: mcp-server reads the same ESHU_AUTH_OIDC_CONFIG_FILE
// convention, and an unset path is a valid (DB-providers-only) shape.
func TestLoadOIDCBearerEnvConfigEmptyPathIsValid(t *testing.T) {
	t.Parallel()
	config, staticResolver, err := loadOIDCBearerEnvConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadOIDCBearerEnvConfig() error = %v, want nil", err)
	}
	if len(config.Providers) != 0 {
		t.Fatalf("config.Providers = %v, want empty", config.Providers)
	}
	if staticResolver.HasIgnoredPolicyRevisionHash() {
		t.Fatal("staticResolver unexpectedly carries state from a zero-value config")
	}
}

// TestLoadOIDCBearerEnvConfigMissingFileErrors mirrors cmd/api's identically
// named test.
func TestLoadOIDCBearerEnvConfigMissingFileErrors(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == envAuthOIDCConfigFile {
			return "/nonexistent/oidc-config-file-for-test.json"
		}
		return ""
	}
	if _, _, err := loadOIDCBearerEnvConfig(getenv); err == nil {
		t.Fatal("loadOIDCBearerEnvConfig() error = nil, want a file-read error")
	}
}
