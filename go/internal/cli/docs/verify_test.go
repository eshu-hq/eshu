// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

func TestHTTPEndpointTruthIncludesMountedNonOpenAPIRoutes(t *testing.T) {
	t.Parallel()

	endpoints := map[string]struct{}{}
	for _, endpoint := range HTTPEndpointTruth() {
		endpoints[endpoint.Method+" "+endpoint.Path] = struct{}{}
	}
	for _, want := range []string{
		"GET /api/v0/docs",
		"GET /api/v0/redoc",
		"GET /health",
		"GET /sse",
		"POST /mcp/message",
		"GET /healthz",
		"GET /readyz",
		"GET /admin/status",
		"POST /admin/replay",
		"POST /admin/refinalize",
		"GET /metrics",
	} {
		if _, ok := endpoints[want]; !ok {
			t.Fatalf("HTTPEndpointTruth() missing %s", want)
		}
	}
}

func TestInventoryFreshnessHintSeparatesImageTruthSources(t *testing.T) {
	t.Parallel()

	documents := []doctruth.DocumentInput{{
		Path:       "README.md",
		SourceURI:  "file:///repo/README.md",
		RevisionID: "sha256:doc",
	}}
	local := InventoryFreshnessHint(documents, 256*1024, 50, "local")
	api := InventoryFreshnessHint(documents, 256*1024, 50, "api")
	if local == api {
		t.Fatalf("freshness local = freshness api = %q, want image truth source in fingerprint", local)
	}
}

func TestNormalizeImageTruthModeDefaultsToAuto(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "", want: "auto"},
		{in: "   ", want: "auto"},
		{in: "API", want: "api"},
		{in: " Local ", want: "local"},
		{in: "nonsense", want: "nonsense"},
	} {
		if got := NormalizeImageTruthMode(tc.in); got != tc.want {
			t.Fatalf("NormalizeImageTruthMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
