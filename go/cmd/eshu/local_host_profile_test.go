// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestResolveLocalHostRuntimeConfig(t *testing.T) {
	t.Run("defaults to lightweight profile", func(t *testing.T) {
		got, err := resolveLocalHostRuntimeConfig(func(string) string { return "" })
		if err != nil {
			t.Fatalf("resolveLocalHostRuntimeConfig() error = %v, want nil", err)
		}
		if got.Profile != query.ProfileLocalLightweight {
			t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalLightweight)
		}
		if got.GraphBackend != "" {
			t.Fatalf("GraphBackend = %q, want empty", got.GraphBackend)
		}
	})

	t.Run("authoritative defaults to nornicdb", func(t *testing.T) {
		got, err := resolveLocalHostRuntimeConfig(func(key string) string {
			if key == "ESHU_QUERY_PROFILE" {
				return string(query.ProfileLocalAuthoritative)
			}
			return ""
		})
		if err != nil {
			t.Fatalf("resolveLocalHostRuntimeConfig() error = %v, want nil", err)
		}
		if got.Profile != query.ProfileLocalAuthoritative {
			t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalAuthoritative)
		}
		if got.GraphBackend != query.GraphBackendNornicDB {
			t.Fatalf("GraphBackend = %q, want %q", got.GraphBackend, query.GraphBackendNornicDB)
		}
	})

	t.Run("rejects unsupported profiles", func(t *testing.T) {
		_, err := resolveLocalHostRuntimeConfig(func(key string) string {
			if key == "ESHU_QUERY_PROFILE" {
				return string(query.ProfileProduction)
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "local Eshu service supports only") {
			t.Fatalf("resolveLocalHostRuntimeConfig() error = %v, want unsupported profile error", err)
		}
	})

	t.Run("rejects graph backend override in lightweight mode", func(t *testing.T) {
		_, err := resolveLocalHostRuntimeConfig(func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return string(query.GraphBackendNornicDB)
			}
			return ""
		})
		if err == nil || !strings.Contains(err.Error(), "ESHU_GRAPH_BACKEND") {
			t.Fatalf("resolveLocalHostRuntimeConfig() error = %v, want graph-backend override error", err)
		}
	})
}

// TestDefaultProfileForMode pins the top-of-funnel default: an MCP stdio owner
// boots the authoritative (embedded-graph) profile so graph-backed questions
// like "who calls this function" are answerable on a fresh install, while the
// watch owner used by the lightweight indexer stays Postgres-only.
func TestDefaultProfileForMode(t *testing.T) {
	if got := defaultProfileForMode(localHostModeMCPStdio); got != query.ProfileLocalAuthoritative {
		t.Fatalf("defaultProfileForMode(mcp_stdio) = %q, want %q", got, query.ProfileLocalAuthoritative)
	}
	if got := defaultProfileForMode(localHostModeWatch); got != query.ProfileLocalLightweight {
		t.Fatalf("defaultProfileForMode(watch) = %q, want %q", got, query.ProfileLocalLightweight)
	}
}

// TestRequestedAttachRuntimeConfig pins the mcp-stdio attach contract: an empty
// environment is not an explicit request (any owner attaches), while a
// graph-backend-only signal resolves to authoritative rather than erroring as an
// invalid lightweight combination, so it matches a running authoritative owner.
func TestRequestedAttachRuntimeConfig(t *testing.T) {
	t.Run("empty environment is not explicit", func(t *testing.T) {
		_, explicit, err := requestedAttachRuntimeConfig(func(string) string { return "" })
		if err != nil {
			t.Fatalf("requestedAttachRuntimeConfig() error = %v, want nil", err)
		}
		if explicit {
			t.Fatalf("explicit = true, want false for empty environment")
		}
	})

	t.Run("graph backend only resolves authoritative", func(t *testing.T) {
		got, explicit, err := requestedAttachRuntimeConfig(func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return string(query.GraphBackendNornicDB)
			}
			return ""
		})
		if err != nil {
			t.Fatalf("requestedAttachRuntimeConfig() error = %v, want nil", err)
		}
		if !explicit {
			t.Fatalf("explicit = false, want true when ESHU_GRAPH_BACKEND is set")
		}
		if got.Profile != query.ProfileLocalAuthoritative {
			t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalAuthoritative)
		}
		if got.GraphBackend != query.GraphBackendNornicDB {
			t.Fatalf("GraphBackend = %q, want %q", got.GraphBackend, query.GraphBackendNornicDB)
		}
	})
}

func TestResolveLocalHostRuntimeConfigWithDefault(t *testing.T) {
	t.Run("authoritative default selects nornicdb when env unset", func(t *testing.T) {
		got, err := resolveLocalHostRuntimeConfigWithDefault(func(string) string { return "" }, query.ProfileLocalAuthoritative)
		if err != nil {
			t.Fatalf("resolveLocalHostRuntimeConfigWithDefault() error = %v, want nil", err)
		}
		if got.Profile != query.ProfileLocalAuthoritative {
			t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalAuthoritative)
		}
		if got.GraphBackend != query.GraphBackendNornicDB {
			t.Fatalf("GraphBackend = %q, want %q", got.GraphBackend, query.GraphBackendNornicDB)
		}
	})

	t.Run("explicit lightweight env overrides authoritative default", func(t *testing.T) {
		got, err := resolveLocalHostRuntimeConfigWithDefault(func(key string) string {
			if key == "ESHU_QUERY_PROFILE" {
				return string(query.ProfileLocalLightweight)
			}
			return ""
		}, query.ProfileLocalAuthoritative)
		if err != nil {
			t.Fatalf("resolveLocalHostRuntimeConfigWithDefault() error = %v, want nil", err)
		}
		if got.Profile != query.ProfileLocalLightweight {
			t.Fatalf("Profile = %q, want %q", got.Profile, query.ProfileLocalLightweight)
		}
		if got.GraphBackend != "" {
			t.Fatalf("GraphBackend = %q, want empty", got.GraphBackend)
		}
	})
}
