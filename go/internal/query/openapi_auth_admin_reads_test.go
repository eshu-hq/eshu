// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOpenAPIAuthAdminReadPaths verifies the admin identity read endpoints are
// present in the spec as GET operations, document the all-scope admin and
// tenant-scoping contract, and never advertise a secret-shaped response field.
func TestOpenAPIAuthAdminReadPaths(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	paths := mustMapField(t, spec, "paths")

	adminPaths := []string{
		"/api/v0/auth/local/invitations",
		"/api/v0/auth/admin/role-assignments",
		"/api/v0/auth/admin/roles",
		"/api/v0/auth/admin/idp-providers",
		"/api/v0/auth/admin/idp-group-mappings",
		"/api/v0/auth/admin/api-tokens",
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
	}
	for _, path := range adminPaths {
		entry, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI admin path %q missing", path)
		}
		get := mustMapField(t, entry, "get")
		description, ok := get["description"].(string)
		if !ok || !strings.Contains(description, "All-scopes admin route") {
			t.Fatalf("admin path %q missing all-scopes admin contract: %v", path, get["description"])
		}
	}

	// The provider list must document that issuer/metadata/entity/client hashes
	// and credential handles are never returned.
	providers := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/admin/idp-providers"), "get")
	providerDescription, _ := providers["description"].(string)
	if !strings.Contains(providerDescription, "credential handles") {
		t.Fatalf("idp-providers description missing no-secret contract: %v", providers["description"])
	}

	// The group mappings list must document that external_group_hash is never
	// returned.
	mappings := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/admin/idp-group-mappings"), "get")
	mappingDescription, _ := mappings["description"].(string)
	if !strings.Contains(mappingDescription, "external group hash") {
		t.Fatalf("idp-group-mappings description missing no-secret contract: %v", mappings["description"])
	}

	// No admin read GET operation may advertise a secret-shaped response field.
	// Only the GET is scanned: a path key may also host an unrelated mutation
	// (for example the POST create-invitation route legitimately documents the
	// one-time invite_code), which is out of scope for these read endpoints.
	for _, path := range adminPaths {
		entry, _ := paths[path].(map[string]any)
		get := mustMapField(t, entry, "get")
		raw, err := json.Marshal(get)
		if err != nil {
			t.Fatalf("marshal admin path %q get: %v", path, err)
		}
		body := string(raw)
		for _, forbidden := range []string{
			"_hash", "invite_code", "credential_handle", "external_group_hash", "token_hash",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("admin path %q GET advertises secret-shaped field %q: %s", path, forbidden, body)
			}
		}
	}
}
