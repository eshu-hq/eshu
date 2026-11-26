// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesAccAuth pins the auth+admin OpenAPI accuracy fixes: every
// entry names the Go handler owner that emits the keys or status codes.
func TestContractShapesAccAuth(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components object")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec components have no schemas object")
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object %q", where, key)
		}
		return next
	}
	opAt := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		return child(t, route, method, path)
	}
	responsesOf := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		return child(t, opAt(t, path, method), "responses", path+" "+method)
	}
	requireCode := func(t *testing.T, path, method, code string) {
		t.Helper()
		responses := responsesOf(t, path, method)
		if _, ok := responses[code].(map[string]any); !ok {
			t.Errorf("%s %s: missing %s response", path, method, code)
		}
	}
	requireNoCode := func(t *testing.T, path, method, code string) {
		t.Helper()
		responses := responsesOf(t, path, method)
		if _, ok := responses[code]; ok {
			t.Errorf("%s %s: phantom %s response with no handler path", path, method, code)
		}
	}
	resolveRef := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		ref, _ := node["$ref"].(string)
		if ref == "" {
			return node
		}
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		target, ok := schemas[name].(map[string]any)
		if !ok {
			t.Fatalf("%s: $ref %q does not resolve", where, ref)
		}
		return target
	}
	bodySchemaOf := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		op := opAt(t, path, method)
		body := child(t, op, "requestBody", path+" "+method)
		content := child(t, body, "content", path+" "+method)
		media := child(t, content, "application/json", path+" "+method)
		return resolveRef(t, child(t, media, "schema", path+" "+method), path+" "+method)
	}
	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}
	requiredOf := func(t *testing.T, node map[string]any) map[string]bool {
		t.Helper()
		got := map[string]bool{}
		raw, ok := node["required"].([]any)
		if !ok {
			return got
		}
		for _, v := range raw {
			s, ok := v.(string)
			if !ok {
				t.Fatalf("required entry is not a string: %v", v)
			}
			got[s] = true
		}
		return got
	}
	requireRequired := func(t *testing.T, node map[string]any, where string, keys ...string) {
		t.Helper()
		raw, ok := node["required"]
		if len(keys) == 0 {
			if ok {
				if arr, ok := raw.([]any); ok && len(arr) > 0 {
					t.Errorf("%s: expected no required keys, got %v", where, arr)
				}
			}
			return
		}
		if !ok {
			t.Fatalf("%s: object schema has no required array", where)
		}
		got := requiredOf(t, node)
		for _, key := range keys {
			if !got[key] {
				t.Errorf("%s: required is missing %q", where, key)
			}
		}
		for key := range got {
			found := false
			for _, want := range keys {
				if key == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: required carries phantom %q", where, key)
			}
		}
	}

	t.Run("login accepts totp and names decode and session-create failures", func(t *testing.T) {
		t.Parallel()
		// Owner: local/requests.go localIdentityLoginRequest carries
		// totp_code; local/handler.go handleLogin answers 400 on decode
		// failure; local/helpers.go IssueSessionCookies answers 500 when
		// the session create fails.
		login := child(t, schemas, "LocalIdentityLoginRequest", "LocalIdentityLoginRequest")
		loginProps := propsOf(t, login, "LocalIdentityLoginRequest")
		if _, ok := loginProps["totp_code"]; !ok {
			t.Errorf("LocalIdentityLoginRequest: missing totp_code property")
		}
		requireRequired(t, login, "LocalIdentityLoginRequest", "login_id", "password")
		requireCode(t, "/api/v0/auth/local/login", "post", "400")
		requireCode(t, "/api/v0/auth/local/login", "post", "500")
	})

	t.Run("login 401 and 403 carry the session body variant", func(t *testing.T) {
		t.Parallel()
		// Owner: local/helpers.go handleLogin answers 401 with
		// IdentitySessionResponse{invalid} and 403 with
		// IdentitySessionResponse{disabled} alongside the error envelope.
		for _, code := range []string{"401", "403"} {
			resp := child(t, responsesOf(t, "/api/v0/auth/local/login", "post"), code, "login "+code)
			content, ok := resp["content"].(map[string]any)
			if !ok {
				t.Fatalf("login %s: no content block for the session body variant", code)
			}
			media := child(t, content, "application/json", "login "+code)
			schema := child(t, media, "schema", "login "+code)
			raw, err := json.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "LocalIdentitySessionResponse") {
				t.Errorf("login %s: content does not name the LocalIdentitySessionResponse variant", code)
			}
		}
		desc, _ := child(t, responsesOf(t, "/api/v0/auth/local/login", "post"), "202", "login 202")["description"].(string)
		if !strings.Contains(desc, "must_change_password") {
			t.Errorf("login 202: description does not name must_change_password: %q", desc)
		}
	})

	t.Run("rotate accepts totp and honors require_sso", func(t *testing.T) {
		t.Parallel()
		// Owner: local/requests.go localIdentityPasswordRotationRequest
		// carries totp_code; local/handler.go handleRotatePassword denies
		// require_sso tenants with 403.
		rotate := child(t, schemas, "LocalIdentityPasswordRotationRequest", "LocalIdentityPasswordRotationRequest")
		if _, ok := propsOf(t, rotate, "LocalIdentityPasswordRotationRequest")["totp_code"]; !ok {
			t.Errorf("LocalIdentityPasswordRotationRequest: missing totp_code property")
		}
		requireCode(t, "/api/v0/auth/local/password/rotate", "post", "403")
	})

	t.Run("break-glass session names decode failure", func(t *testing.T) {
		t.Parallel()
		// Owner: local/helpers.go handleBreakGlassSession answers 400 on
		// bad JSON.
		requireCode(t, "/api/v0/auth/local/break-glass/session", "post", "400")
	})

	t.Run("totp begin names invalid requests", func(t *testing.T) {
		t.Parallel()
		// Owner: local/totp.go handleBeginTOTPEnrollment answers 400 on
		// an invalid request and on begin failure.
		requireCode(t, "/api/v0/auth/local/mfa/totp/begin", "post", "400")
	})

	t.Run("token create names auth and resolve failures", func(t *testing.T) {
		t.Parallel()
		// Owner: local/api_tokens_self_service.go authorizeTokenMutation
		// answers 401 for subject-less callers; local/api_tokens.go
		// answers 500 when the user resolve fails. token_class defaults
		// to personal when absent.
		requireCode(t, "/api/v0/auth/local/api-tokens", "post", "401")
		requireCode(t, "/api/v0/auth/local/api-tokens", "post", "500")
		create := child(t, schemas, "LocalIdentityAPITokenCreateRequest", "LocalIdentityAPITokenCreateRequest")
		if requiredOf(t, create)["token_class"] {
			t.Errorf("LocalIdentityAPITokenCreateRequest: token_class must be optional (defaults to personal)")
		}
		classProp, ok := propsOf(t, create, "LocalIdentityAPITokenCreateRequest")["token_class"].(map[string]any)
		if !ok {
			t.Fatalf("LocalIdentityAPITokenCreateRequest: missing token_class property")
		}
		if def, _ := classProp["default"].(string); def != "personal" {
			t.Errorf("LocalIdentityAPITokenCreateRequest: token_class default = %q, want personal", def)
		}
		requireCode(t, "/api/v0/auth/local/api-tokens/{token_id}/revoke", "post", "401")
		requireCode(t, "/api/v0/auth/local/api-tokens/{token_id}/rotate", "post", "401")
	})

	t.Run("token list drops optional class from required", func(t *testing.T) {
		t.Parallel()
		// Owner: local/api_tokens.go tokenJSON tags token_class
		// omitempty, so rows from a store with an empty class omit it.
		op := opAt(t, "/api/v0/auth/local/api-tokens", "get")
		resp := child(t, child(t, op, "responses", "token list"), "200", "token list")
		raw, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		schema := decoded["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		items := schema["properties"].(map[string]any)["tokens"].(map[string]any)["items"].(map[string]any)
		if requiredOf(t, items)["token_class"] {
			t.Errorf("token list items: token_class must not be required (omitempty)")
		}
	})

	t.Run("audit events names bad limits", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/identity/audit.go handleListAuditEvents answers
		// 400 on a bad limit.
		requireCode(t, "/api/v0/auth/admin/audit/events", "get", "400")
	})

	t.Run("profile mfa drops absent factor kind", func(t *testing.T) {
		t.Parallel()
		// Owner: profile_handler.go tags factor_kind omitempty: callers
		// without MFA carry no factor_kind.
		op := opAt(t, "/api/v0/auth/profile", "get")
		resp := child(t, child(t, op, "responses", "profile"), "200", "profile")
		raw, _ := json.Marshal(resp)
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		schema := decoded["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		mfa := schema["properties"].(map[string]any)["mfa"].(map[string]any)
		requireRequired(t, mfa, "profile.mfa", "has_active_mfa")
	})

	t.Run("session rows drop absent tenant scope", func(t *testing.T) {
		t.Parallel()
		// Owner: browser_session_list.go sessionJSON tags tenant_id and
		// workspace_id omitempty.
		op := opAt(t, "/api/v0/auth/sessions", "get")
		resp := child(t, child(t, op, "responses", "sessions"), "200", "sessions")
		raw, _ := json.Marshal(resp)
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		schema := decoded["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		items := schema["properties"].(map[string]any)["sessions"].(map[string]any)["items"].(map[string]any)
		got := requiredOf(t, items)
		for _, key := range []string{"tenant_id", "workspace_id"} {
			if got[key] {
				t.Errorf("sessions items: %q must not be required (omitempty)", key)
			}
		}
	})

	t.Run("oidc and github drop phantom unauthorized", func(t *testing.T) {
		t.Parallel()
		// Owner: oidc_login_handler.go writeOIDCLoginError and
		// github_login_handler.go writeGitHubLoginError emit only 400,
		// 403, 500, 503.
		for _, path := range []string{
			"/api/v0/auth/oidc/login",
			"/api/v0/auth/oidc/callback",
			"/api/v0/auth/github/login",
			"/api/v0/auth/github/callback",
		} {
			requireNoCode(t, path, "get", "401")
		}
		requireCode(t, "/api/v0/auth/oidc/login", "get", "403")
		requireCode(t, "/api/v0/auth/github/login", "get", "403")
	})

	t.Run("setup names store and completion failures", func(t *testing.T) {
		t.Parallel()
		// Owner: setup_handler.go answers 500 on SetupNeeded, credential
		// check, owner resolve, and password persist failures;
		// setup_mfa_handler.go answers 409 on concurrent completion and
		// 500 on code generation, session, and commit failures.
		requireCode(t, "/api/v0/auth/setup-state", "get", "500")
		requireCode(t, "/api/v0/auth/setup/claim", "post", "500")
		requireCode(t, "/api/v0/auth/setup/admin", "post", "500")
		requireCode(t, "/api/v0/auth/setup/mfa", "post", "409")
		requireCode(t, "/api/v0/auth/setup/mfa", "post", "500")
	})

	t.Run("provider enable names revision races", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/provider/config/audit.go maps ErrRevisionChanged
		// to 409.
		requireCode(t, "/api/v0/auth/admin/provider-configs/{provider_config_id}/enable", "post", "409")
	})

	t.Run("bootstrap names id-generation failure", func(t *testing.T) {
		t.Parallel()
		// Owner: local/handler.go handleBootstrap answers 500 only when
		// server-side ID generation fails.
		requireCode(t, "/api/v0/auth/local/bootstrap", "post", "500")
	})

	t.Run("admin request bodies name required selectors", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/handler.go refinalize requires scope_ids;
		// admin/facts.go listWorkItems takes an all-optional filter body,
		// listDecisions requires repository_id and source_run_id,
		// deadLetter requires one of work_item_ids, scope_id, stage,
		// skip requires repository_id, backfill requires scope_id or
		// generation_id, listReplayEvents takes an all-optional body.
		// Every body is required: querycontract.ReadJSON errors on empty.
		refinalize := bodySchemaOf(t, "/api/v0/admin/refinalize", "post")
		requireRequired(t, refinalize, "refinalize body", "scope_ids")

		workItems := bodySchemaOf(t, "/api/v0/admin/work-items/query", "post")
		workItemProps := propsOf(t, workItems, "work-items body")
		for _, key := range []string{"statuses", "scope_id", "stage", "failure_class", "limit"} {
			if _, ok := workItemProps[key]; !ok {
				t.Errorf("work-items body: missing filter property %q", key)
			}
		}
		requireRequired(t, workItems, "work-items body")

		decisions := bodySchemaOf(t, "/api/v0/admin/decisions/query", "post")
		requireRequired(t, decisions, "decisions body", "repository_id", "source_run_id")

		deadLetter := bodySchemaOf(t, "/api/v0/admin/dead-letter", "post")
		deadLetterProps := propsOf(t, deadLetter, "dead-letter body")
		for _, key := range []string{"work_item_ids", "scope_id", "stage"} {
			if _, ok := deadLetterProps[key]; !ok {
				t.Errorf("dead-letter body: missing selector property %q", key)
			}
		}

		skip := bodySchemaOf(t, "/api/v0/admin/skip", "post")
		requireRequired(t, skip, "skip body", "repository_id")

		backfill := bodySchemaOf(t, "/api/v0/admin/backfill", "post")
		backfillProps := propsOf(t, backfill, "backfill body")
		for _, key := range []string{"scope_id", "generation_id"} {
			if _, ok := backfillProps[key]; !ok {
				t.Errorf("backfill body: missing selector property %q", key)
			}
		}

		replayEvents := bodySchemaOf(t, "/api/v0/admin/replay-events/query", "post")
		replayProps := propsOf(t, replayEvents, "replay-events body")
		for _, key := range []string{"scope_id", "work_item_id", "failure_class", "limit"} {
			if _, ok := replayProps[key]; !ok {
				t.Errorf("replay-events body: missing filter property %q", key)
			}
		}
		requireRequired(t, replayEvents, "replay-events body")
	})
}
