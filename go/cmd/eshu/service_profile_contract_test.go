// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// TestMCPStartProfileOverridesContract pins the --profile flag to environment
// mapping behind `eshu mcp start --profile`, including the lightweight
// backend-clearing rule and the rejection of non-local profiles.
func TestMCPStartProfileOverridesContract(t *testing.T) {
	t.Run("empty leaves the owner default alone", func(t *testing.T) {
		got, err := mcpStartProfileOverrides("")
		if err != nil {
			t.Fatalf("mcpStartProfileOverrides() error = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Fatalf("mcpStartProfileOverrides() = %#v, want empty", got)
		}
	})

	t.Run("lightweight pins the profile and clears the backend", func(t *testing.T) {
		got, err := mcpStartProfileOverrides("local_lightweight")
		if err != nil {
			t.Fatalf("mcpStartProfileOverrides() error = %v, want nil", err)
		}
		if got["ESHU_QUERY_PROFILE"] != "local_lightweight" {
			t.Fatalf("ESHU_QUERY_PROFILE = %q, want local_lightweight", got["ESHU_QUERY_PROFILE"])
		}
		if backend, ok := got["ESHU_GRAPH_BACKEND"]; !ok || backend != "" {
			t.Fatalf("ESHU_GRAPH_BACKEND = %q, want present-but-empty", backend)
		}
	})

	t.Run("authoritative pins the profile and the nornicdb backend", func(t *testing.T) {
		got, err := mcpStartProfileOverrides("local_authoritative")
		if err != nil {
			t.Fatalf("mcpStartProfileOverrides() error = %v, want nil", err)
		}
		if got["ESHU_QUERY_PROFILE"] != "local_authoritative" {
			t.Fatalf("ESHU_QUERY_PROFILE = %q, want local_authoritative", got["ESHU_QUERY_PROFILE"])
		}
		if got["ESHU_GRAPH_BACKEND"] != "nornicdb" {
			t.Fatalf("ESHU_GRAPH_BACKEND = %q, want nornicdb", got["ESHU_GRAPH_BACKEND"])
		}
	})

	t.Run("surrounding whitespace is tolerated", func(t *testing.T) {
		got, err := mcpStartProfileOverrides("  local_lightweight  ")
		if err != nil {
			t.Fatalf("mcpStartProfileOverrides() error = %v, want nil", err)
		}
		if got["ESHU_QUERY_PROFILE"] != "local_lightweight" {
			t.Fatalf("ESHU_QUERY_PROFILE = %q, want local_lightweight", got["ESHU_QUERY_PROFILE"])
		}
	})

	t.Run("production is rejected with the supported profiles", func(t *testing.T) {
		_, err := mcpStartProfileOverrides("production")
		if err == nil {
			t.Fatal("mcpStartProfileOverrides() error = nil, want rejection")
		}
		for _, want := range []string{"local_lightweight", "local_authoritative"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("mcpStartProfileOverrides() error = %q, want it to name %q", err.Error(), want)
			}
		}
	})

	t.Run("unknown profile reports a parse failure", func(t *testing.T) {
		_, err := mcpStartProfileOverrides("bogus_profile")
		if err == nil {
			t.Fatal("mcpStartProfileOverrides() error = nil, want parse failure")
		}
		if !strings.Contains(err.Error(), "parse --profile") {
			t.Fatalf("mcpStartProfileOverrides() error = %q, want parse context", err.Error())
		}
	})
}
