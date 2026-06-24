// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOpenAPIAuthAdminMutationPaths verifies the admin identity mutation
// endpoints are present in the spec under the correct unsafe method, document
// the all-scopes admin contract, and never advertise a secret-shaped request or
// response field (no raw invite code, credential handle, token hash, or raw
// external group beyond the documented write-only external_group input).
func TestOpenAPIAuthAdminMutationPaths(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	paths := mustMapField(t, spec, "paths")

	// Each mutation route keyed by the operation method it must expose.
	mutations := []struct {
		path   string
		method string
	}{
		{"/api/v0/auth/local/invitations/{invite_id}/revoke", "post"},
		{"/api/v0/auth/admin/role-assignments", "post"},
		{"/api/v0/auth/admin/role-assignments/revoke", "post"},
		{"/api/v0/auth/admin/idp-group-mappings", "post"},
		{"/api/v0/auth/admin/idp-group-mappings/{mapping_ref}", "delete"},
	}
	for _, m := range mutations {
		entry, ok := paths[m.path].(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI mutation path %q missing", m.path)
		}
		op := mustMapField(t, entry, m.method)
		description, ok := op["description"].(string)
		if !ok || !strings.Contains(description, "All-scopes admin route") {
			t.Fatalf("mutation %s %q missing all-scopes admin contract: %v", m.method, m.path, op["description"])
		}
		if !strings.Contains(description, "audit") {
			t.Fatalf("mutation %s %q description must note the governance audit event: %v", m.method, m.path, description)
		}
		// The mutation operation must not advertise a secret-shaped response field.
		// external_group is the one allowed write-only input (raw group name in the
		// request body); it is explicitly documented as never stored or returned, so
		// it is excluded from the forbidden scan below.
		raw, err := json.Marshal(op)
		if err != nil {
			t.Fatalf("marshal %s %q: %v", m.method, m.path, err)
		}
		body := string(raw)
		for _, forbidden := range []string{
			"_hash", "invite_code", "credential_handle", "token_hash", "external_group_hash",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("mutation %s %q advertises secret-shaped field %q: %s", m.method, m.path, forbidden, body)
			}
		}
	}

	// The create-mapping POST documents external_group as a write-only raw input
	// that is hashed server-side and never returned, and returns only the opaque
	// mapping_ref.
	createMapping := mustMapField(t, mustMapField(t, paths, "/api/v0/auth/admin/idp-group-mappings"), "post")
	createRaw, _ := json.Marshal(createMapping)
	if !strings.Contains(string(createRaw), "mapping_ref") {
		t.Fatalf("create mapping must return mapping_ref: %s", string(createRaw))
	}
	if !strings.Contains(string(createRaw), "never stored or returned") {
		t.Fatalf("create mapping must document external_group as never stored or returned: %s", string(createRaw))
	}
}
