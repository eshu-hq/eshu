// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeTagHistoryReader is a minimal query.GraphQuery double for MCP dispatch
// tests: it returns canned rows and records the last query for assertions.
type fakeTagHistoryReader struct {
	rows       []map[string]any
	lastCypher string
	lastParams map[string]any
}

func (f *fakeTagHistoryReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.lastCypher = cypher
	f.lastParams = params
	return f.rows, nil
}

func (*fakeTagHistoryReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestListContainerImageTagHistoryToolIsRegistered proves the #5459 MCP tool
// is advertised through ReadOnlyTools().
func TestListContainerImageTagHistoryToolIsRegistered(t *testing.T) {
	t.Parallel()

	_ = requireToolDefinition(t, "list_container_image_tag_history")
}

// TestListContainerImageTagHistorySchema proves the advertised input schema
// carries the required repository_id and tag selector fields plus limit.
func TestListContainerImageTagHistorySchema(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_container_image_tag_history")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"repository_id", "tag", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("list_container_image_tag_history schema missing %q", field)
		}
	}
}

// TestResolveRouteMapsContainerImageTagHistoryToBoundedQuery proves
// resolveRoute composes the GET /api/v0/images/tag-history route with the
// repository_id, tag, limit, and offset query params from the tool args.
func TestResolveRouteMapsContainerImageTagHistoryToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_container_image_tag_history", map[string]any{
		"repository_id": "oci-registry://ghcr.io/eshu-hq/demo",
		"tag":           "1.0.0",
		"limit":         float64(25),
		"offset":        float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/images/tag-history"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["repository_id"], "oci-registry://ghcr.io/eshu-hq/demo"; got != want {
		t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["tag"], "1.0.0"; got != want {
		t.Fatalf("route.query[tag] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
	if got, want := route.query["offset"], "5"; got != want {
		t.Fatalf("route.query[offset] = %#v, want %#v", got, want)
	}
}

// TestDispatchToolListContainerImageTagHistoryReturnsOrderedRows proves the
// MCP tool dispatches end-to-end through query.TagHistoryHandler: the
// repository_id/tag args compose the image_ref anchor server-side, and the
// bounded, ordered tag_history rows come back through the canonical envelope.
//
// This mirrors the shape of dispatch_container_image_identity_authz_test.go
// (proving a real AuthContext-carrying dispatch), but does not exercise a
// scoped-bearer-token round trip: GET /api/v0/images/tag-history follows
// GET /api/v0/images' own precedent (see openapi_paths_images.go) of not
// carrying the "x-scoped-token-support" marker, so it is not part of the
// scoped-token allowlist today. That is a deliberate, documented decision for
// this change, not an oversight; see the executor's completion report for
// #5459 for the reasoning it followed.
func TestDispatchToolListContainerImageTagHistoryReturnsOrderedRows(t *testing.T) {
	t.Parallel()

	reader := &fakeTagHistoryReader{rows: []map[string]any{
		{
			"tag":               "1.0.0",
			"resolved_digest":   "sha256:aaa",
			"mutated":           false,
			"first_observed_at": "2026-06-25T00:00:00Z",
			"repository_id":     "oci-registry://ghcr.io/eshu-hq/demo",
			"identity_strength": "weak_tag",
			"uid":               "uid-1",
		},
	}}
	mux := http.NewServeMux()
	handler := &query.TagHistoryHandler{
		Neo4j:   reader,
		Profile: query.ProfileProduction,
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_container_image_tag_history",
		map[string]any{
			"repository_id": "oci-registry://ghcr.io/eshu-hq/demo",
			"tag":           "1.0.0",
			"limit":         float64(10),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope == nil || result.Envelope.Truth == nil {
		t.Fatalf("envelope = %#v, want truth envelope", result.Envelope)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := data["image_ref"], "ghcr.io/eshu-hq/demo:1.0.0"; got != want {
		t.Fatalf("image_ref = %#v, want %#v", got, want)
	}
	history, ok := data["tag_history"].([]any)
	if !ok || len(history) != 1 {
		t.Fatalf("tag_history = %#v, want 1 row", data["tag_history"])
	}
	if got, want := reader.lastParams["image_ref"], "ghcr.io/eshu-hq/demo:1.0.0"; got != want {
		t.Fatalf("query image_ref param = %#v, want %#v", got, want)
	}
	if !strings.Contains(reader.lastCypher, "ORDER BY t.first_observed_at, t.uid") {
		t.Fatalf("cypher missing deterministic ORDER BY, got:\n%s", reader.lastCypher)
	}
}

// TestDispatchToolListContainerImageTagHistoryMissingSelectorReturnsError
// proves a missing repository_id or tag surfaces as a dispatch error rather
// than a silent empty-but-200 page.
func TestDispatchToolListContainerImageTagHistoryMissingSelectorReturnsError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := &query.TagHistoryHandler{
		Neo4j:   &fakeTagHistoryReader{},
		Profile: query.ProfileProduction,
	}
	handler.Mount(mux)

	_, err := dispatchTool(
		context.Background(),
		mux,
		"list_container_image_tag_history",
		map[string]any{"repository_id": "oci-registry://ghcr.io/eshu-hq/demo"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err == nil {
		t.Fatal("dispatchTool() error = nil, want an error for a missing tag selector")
	}
}
