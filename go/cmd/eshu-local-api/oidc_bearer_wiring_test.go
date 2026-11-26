// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
)

// TestNewOIDCBearerResolverDisabledWhenAudienceUnset proves ESHU_AUTH_RESOURCE_URI
// unset disables the IdP bearer resolver entirely (issue #5162): no
// resolver is constructed, no error, and — critically — no database access
// is attempted, so a token-only deployment that never sets this variable
// pays nothing for the feature.
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

// TestNewOIDCBearerResolverRequiresDatabaseWhenAudienceSet proves that once
// ESHU_AUTH_RESOURCE_URI is configured, a missing database fails wiring
// closed with an actionable error rather than silently disabling the
// resolver or panicking.
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

// TestLoadOIDCBearerEnvConfigEmptyPathIsValid proves an unset
// ESHU_AUTH_OIDC_CONFIG_FILE is a valid, zero-value configuration (DB-backed
// providers alone are a fully supported deployment shape), not an error.
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

// TestLoadOIDCBearerEnvConfigMissingFileErrors proves a configured but
// unreadable config file fails closed with a wrapped error rather than
// silently falling back to zero providers.
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
