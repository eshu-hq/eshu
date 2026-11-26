// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// Shared fragment-navigation and shape-pin helpers for the lane-2
// catalog contract tests.
func lane2Fragment(t *testing.T, raw, where string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
	}
	return doc
}

func lane2Child(t *testing.T, node map[string]any, key, where string) map[string]any {
	t.Helper()
	next, ok := node[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing or non-object property %q", where, key)
	}
	return next
}

func lane2SchemaAt(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	route, ok := doc[path].(map[string]any)
	if !ok {
		t.Fatalf("fragment paths missing %s", path)
	}
	op := lane2Child(t, route, method, path)
	responses := lane2Child(t, op, "responses", path+" "+method)
	okResp := lane2Child(t, responses, "200", path+" "+method)
	content := lane2Child(t, okResp, "content", path+" "+method)
	body := lane2Child(t, content, "application/json", path+" "+method)
	return lane2Child(t, body, "schema", path+" "+method)
}

func lane2PropsOf(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: object schema has no properties", where)
	}
	return props
}

func lane2ItemsOf(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	items, ok := node["items"].(map[string]any)
	if !ok {
		t.Fatalf("%s: array schema has no items", where)
	}
	return items
}

func lane2RequireExactKeys(t *testing.T, props map[string]any, where string, keys ...string) {
	t.Helper()
	want := make(map[string]bool, len(keys))
	for _, key := range keys {
		want[key] = true
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("%s: missing property %q", where, key)
		}
	}
	for key := range props {
		if !want[key] {
			t.Errorf("%s: phantom property %q not emitted by the owner", where, key)
		}
	}
}

func lane2RequireScalarTypes(t *testing.T, props map[string]any, where string, want map[string]string) {
	t.Helper()
	for key, typ := range want {
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing property %q", where, key)
			continue
		}
		if got, _ := node["type"].(string); got != typ {
			t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
		}
	}
}

func lane2RequireDescribed(t *testing.T, props map[string]any, where string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing property %q", where, key)
			continue
		}
		if desc, _ := node["description"].(string); desc == "" {
			t.Errorf("%s: property %q has no description", where, key)
		}
	}
}

func lane2RequireNoBareObjects(t *testing.T, node map[string]any, where string) {
	t.Helper()
	var walk func(n map[string]any, at string)
	walk = func(n map[string]any, at string) {
		if n["type"] == "object" {
			_, hasProps := n["properties"]
			_, hasAdd := n["additionalProperties"]
			if !hasProps && !hasAdd {
				t.Errorf("%s: bare object at %q (no properties or additionalProperties)", where, at)
			}
		}
		if n["type"] == "array" {
			if items, ok := n["items"].(map[string]any); ok {
				walk(items, at+"[]")
			} else {
				t.Errorf("%s: array at %q has no items", where, at)
			}
		}
		if props, ok := n["properties"].(map[string]any); ok {
			for k, v := range props {
				if vm, ok := v.(map[string]any); ok {
					walk(vm, at+"."+k)
				}
			}
		}
	}
	walk(node, where)
}

// TestContractShapesLane2Catalog pins the lane-2 shape-completeness fixes
// for the component-extensions and capabilities fragments.
func TestContractShapesLane2Catalog(t *testing.T) {
	t.Parallel()

	t.Run("component extensions", func(t *testing.T) {
		t.Parallel()
		// Owners: ComponentExtensionsHandler.listComponentExtensions and
		// getComponentExtensionDiagnostics (query/component_extensions.go)
		// emit ComponentExtensionInventoryResponse /
		// ComponentExtensionDiagnosticsResponse and the nested
		// ComponentExtension* rows.
		doc := lane2Fragment(t, ComponentExtensions, "ComponentExtensions")
		schema := lane2SchemaAt(t, doc, "/api/v0/component-extensions", "get")
		props := lane2PropsOf(t, schema, "extensions")
		lane2RequireExactKeys(t, props, "extensions",
			"schema_version", "status", "component_home_configured",
			"components", "count", "total_count", "limit", "truncated",
			"policy")
		lane2RequireScalarTypes(t, props, "extensions", map[string]string{
			"schema_version": "string", "status": "string",
			"component_home_configured": "boolean", "components": "array",
			"count": "integer", "total_count": "integer", "limit": "integer",
			"truncated": "boolean", "policy": "object",
		})
		lane2RequireDescribed(t, props, "extensions",
			"schema_version", "status", "component_home_configured",
			"components", "count", "total_count", "limit", "truncated",
			"policy")

		comp := lane2ItemsOf(t, lane2Child(t, props, "components", "extensions"), "extensions[]")
		compProps := lane2PropsOf(t, comp, "extensions[]")
		lane2RequireExactKeys(t, compProps, "extensions[]",
			"id", "name", "publisher", "version", "manifest_digest",
			"verified", "trust_mode", "installed_at", "states",
			"activations", "diagnostics", "trust_decision", "policy_gate",
			"last_conformance_proof", "scheduler_state",
			"read_model_availability", "error")
		lane2RequireScalarTypes(t, compProps, "extensions[]", map[string]string{
			"id": "string", "verified": "boolean",
		})
		lane2RequireDescribed(t, compProps, "extensions[]",
			"id", "name", "publisher", "version", "manifest_digest",
			"verified", "trust_mode", "installed_at", "states",
			"activations", "diagnostics", "trust_decision", "policy_gate",
			"last_conformance_proof", "scheduler_state",
			"read_model_availability", "error")

		// Owner: ComponentExtensionActivation.
		act := lane2ItemsOf(t, lane2Child(t, compProps, "activations", "extensions.act"), "extensions.act[]")
		actProps := lane2PropsOf(t, act, "extensions.act[]")
		lane2RequireExactKeys(t, actProps, "extensions.act[]",
			"instance_id", "mode", "claims_enabled", "config_handle",
			"enabled_at")
		lane2RequireScalarTypes(t, actProps, "extensions.act[]", map[string]string{
			"claims_enabled": "boolean",
		})
		lane2RequireDescribed(t, actProps, "extensions.act[]",
			"instance_id", "mode", "claims_enabled", "config_handle",
			"enabled_at")

		// Owner: ComponentExtensionPolicyDiagnostics.
		diag := lane2Child(t, compProps, "diagnostics", "extensions.diag")
		diagProps := lane2PropsOf(t, diag, "extensions.diag")
		lane2RequireExactKeys(t, diagProps, "extensions.diag",
			"policy_configured", "policy_allowed", "policy_mode",
			"policy_code", "policy_reason")
		lane2RequireScalarTypes(t, diagProps, "extensions.diag", map[string]string{
			"policy_configured": "boolean", "policy_allowed": "boolean",
		})
		lane2RequireDescribed(t, diagProps, "extensions.diag",
			"policy_configured", "policy_allowed", "policy_mode",
			"policy_code", "policy_reason")

		// Owner: ComponentExtensionTrustDecision.
		trust := lane2Child(t, compProps, "trust_decision", "extensions.trust")
		trustProps := lane2PropsOf(t, trust, "extensions.trust")
		lane2RequireExactKeys(t, trustProps, "extensions.trust",
			"decision", "code", "reason")
		lane2RequireDescribed(t, trustProps, "extensions.trust",
			"decision", "code", "reason")

		// Owner: ComponentExtensionPolicyGate.
		gate := lane2Child(t, compProps, "policy_gate", "extensions.gate")
		gateProps := lane2PropsOf(t, gate, "extensions.gate")
		lane2RequireExactKeys(t, gateProps, "extensions.gate",
			"state", "mode", "code")
		lane2RequireDescribed(t, gateProps, "extensions.gate",
			"state", "mode", "code")

		// Owner: ComponentExtensionConformanceProof.
		proof := lane2Child(t, compProps, "last_conformance_proof", "extensions.proof")
		proofProps := lane2PropsOf(t, proof, "extensions.proof")
		lane2RequireExactKeys(t, proofProps, "extensions.proof",
			"status", "reason")
		lane2RequireDescribed(t, proofProps, "extensions.proof",
			"status", "reason")

		// Owner: ComponentExtensionSchedulerState.
		sched := lane2Child(t, compProps, "scheduler_state", "extensions.sched")
		schedProps := lane2PropsOf(t, sched, "extensions.sched")
		lane2RequireExactKeys(t, schedProps, "extensions.sched",
			"state", "reason")
		lane2RequireDescribed(t, schedProps, "extensions.sched",
			"state", "reason")

		// Owner: ComponentExtensionReadModelAvailability.
		avail := lane2Child(t, compProps, "read_model_availability", "extensions.avail")
		availProps := lane2PropsOf(t, avail, "extensions.avail")
		lane2RequireExactKeys(t, availProps, "extensions.avail",
			"state", "unavailable_reason")
		lane2RequireDescribed(t, availProps, "extensions.avail",
			"state", "unavailable_reason")

		// Owner: ComponentExtensionError.
		errNode := lane2Child(t, compProps, "error", "extensions.error")
		errProps := lane2PropsOf(t, errNode, "extensions.error")
		lane2RequireExactKeys(t, errProps, "extensions.error",
			"code", "message")
		lane2RequireDescribed(t, errProps, "extensions.error",
			"code", "message")

		// Owner: ComponentExtensionPolicyStatus.
		policy := lane2Child(t, props, "policy", "extensions.policy")
		policyProps := lane2PropsOf(t, policy, "extensions.policy")
		lane2RequireExactKeys(t, policyProps, "extensions.policy",
			"mode", "allow_ids_configured", "allow_publishers_configured",
			"revoke_ids_configured", "revoke_publishers_configured",
			"core_version_configured")
		lane2RequireScalarTypes(t, policyProps, "extensions.policy", map[string]string{
			"allow_ids_configured":         "boolean",
			"allow_publishers_configured":  "boolean",
			"revoke_ids_configured":        "boolean",
			"revoke_publishers_configured": "boolean",
			"core_version_configured":      "boolean",
		})
		lane2RequireDescribed(t, policyProps, "extensions.policy",
			"mode", "allow_ids_configured", "allow_publishers_configured",
			"revoke_ids_configured", "revoke_publishers_configured",
			"core_version_configured")

		// Owner: ComponentExtensionDiagnosticsResponse carries one
		// ComponentExtensionComponent plus the same policy status.
		diagSchema := lane2SchemaAt(t, doc, "/api/v0/component-extensions/{component_id}/diagnostics", "get")
		diagRouteProps := lane2PropsOf(t, diagSchema, "diagnostics")
		lane2RequireExactKeys(t, diagRouteProps, "diagnostics",
			"schema_version", "status", "component_home_configured",
			"component", "policy")
		lane2RequireDescribed(t, diagRouteProps, "diagnostics",
			"schema_version", "status", "component_home_configured",
			"component", "policy")
		one := lane2Child(t, diagRouteProps, "component", "diagnostics.component")
		oneProps := lane2PropsOf(t, one, "diagnostics.component")
		lane2RequireExactKeys(t, oneProps, "diagnostics.component",
			"id", "name", "publisher", "version", "manifest_digest",
			"verified", "trust_mode", "installed_at", "states",
			"activations", "diagnostics", "trust_decision", "policy_gate",
			"last_conformance_proof", "scheduler_state",
			"read_model_availability", "error")
		lane2RequireDescribed(t, oneProps, "diagnostics.component",
			"id", "name", "publisher", "version", "manifest_digest",
			"verified", "trust_mode", "installed_at", "states",
			"activations", "diagnostics", "trust_decision", "policy_gate",
			"last_conformance_proof", "scheduler_state",
			"read_model_availability", "error")

		lane2RequireNoBareObjects(t, schema, "extensions")
		lane2RequireNoBareObjects(t, diagSchema, "diagnostics")
	})

	t.Run("capabilities", func(t *testing.T) {
		t.Parallel()
		// Owner: capability.Handler.list (query/capability/handler.go)
		// serializes capabilitycatalog.Entry rows (compact view projects
		// CompactEntry) plus the AuthorizationCatalog when
		// include_authorization is true.
		doc := lane2Fragment(t, Capabilities, "Capabilities")
		schema := lane2SchemaAt(t, doc, "/api/v0/capabilities", "get")
		props := lane2PropsOf(t, schema, "capabilities")
		lane2RequireExactKeys(t, props, "capabilities",
			"version", "authorization", "total", "limit", "offset",
			"truncated", "next_offset", "capabilities")
		lane2RequireScalarTypes(t, props, "capabilities", map[string]string{
			"version": "string", "authorization": "object", "total": "integer",
			"limit": "integer", "offset": "integer", "truncated": "boolean",
			"next_offset": "integer", "capabilities": "array",
		})
		lane2RequireDescribed(t, props, "capabilities",
			"version", "authorization", "total", "limit", "offset",
			"truncated", "next_offset", "capabilities")

		// Owner: capabilitycatalog.AuthorizationCatalog.
		auth := lane2Child(t, props, "authorization", "capabilities.auth")
		authProps := lane2PropsOf(t, auth, "capabilities.auth")
		lane2RequireExactKeys(t, authProps, "capabilities.auth",
			"version", "roles", "data_classes", "permission_families",
			"bootstrap_owner", "custom_policy")
		lane2RequireDescribed(t, authProps, "capabilities.auth",
			"version", "roles", "data_classes", "permission_families",
			"bootstrap_owner", "custom_policy")
		role := lane2ItemsOf(t, lane2Child(t, authProps, "roles", "capabilities.role"), "capabilities.role[]")
		roleProps := lane2PropsOf(t, role, "capabilities.role[]")
		lane2RequireExactKeys(t, roleProps, "capabilities.role[]",
			"role", "display_name", "description", "bootstrap_default",
			"grants")
		lane2RequireScalarTypes(t, roleProps, "capabilities.role[]", map[string]string{
			"bootstrap_default": "boolean",
		})
		lane2RequireDescribed(t, roleProps, "capabilities.role[]",
			"role", "display_name", "description", "bootstrap_default",
			"grants")
		grant := lane2ItemsOf(t, lane2Child(t, roleProps, "grants", "capabilities.grant"), "capabilities.grant[]")
		grantProps := lane2PropsOf(t, grant, "capabilities.grant[]")
		lane2RequireExactKeys(t, grantProps, "capabilities.grant[]",
			"action", "data_classes", "scope_levels")
		lane2RequireDescribed(t, grantProps, "capabilities.grant[]",
			"action", "data_classes", "scope_levels")
		dc := lane2ItemsOf(t, lane2Child(t, authProps, "data_classes", "capabilities.dc"), "capabilities.dc[]")
		dcProps := lane2PropsOf(t, dc, "capabilities.dc[]")
		lane2RequireExactKeys(t, dcProps, "capabilities.dc[]",
			"data_class", "sensitivity", "description")
		lane2RequireDescribed(t, dcProps, "capabilities.dc[]",
			"data_class", "sensitivity", "description")
		fam := lane2ItemsOf(t, lane2Child(t, authProps, "permission_families", "capabilities.fam"), "capabilities.fam[]")
		famProps := lane2PropsOf(t, fam, "capabilities.fam[]")
		lane2RequireExactKeys(t, famProps, "capabilities.fam[]",
			"family", "description", "planned", "capability_prefixes",
			"action", "data_classes", "scope_levels", "default_roles")
		lane2RequireScalarTypes(t, famProps, "capabilities.fam[]", map[string]string{
			"planned": "boolean",
		})
		lane2RequireDescribed(t, famProps, "capabilities.fam[]",
			"family", "description", "planned", "capability_prefixes",
			"action", "data_classes", "scope_levels", "default_roles")
		owner := lane2Child(t, authProps, "bootstrap_owner", "capabilities.owner")
		ownerProps := lane2PropsOf(t, owner, "capabilities.owner")
		lane2RequireExactKeys(t, ownerProps, "capabilities.owner",
			"role", "starts_with_admin", "starts_with_sensitive_data_grants",
			"delegable_roles")
		lane2RequireScalarTypes(t, ownerProps, "capabilities.owner", map[string]string{
			"starts_with_admin":                 "boolean",
			"starts_with_sensitive_data_grants": "boolean",
		})
		lane2RequireDescribed(t, ownerProps, "capabilities.owner",
			"role", "starts_with_admin", "starts_with_sensitive_data_grants",
			"delegable_roles")
		custom := lane2Child(t, authProps, "custom_policy", "capabilities.custom")
		customProps := lane2PropsOf(t, custom, "capabilities.custom")
		lane2RequireExactKeys(t, customProps, "capabilities.custom",
			"status", "note")
		lane2RequireDescribed(t, customProps, "capabilities.custom",
			"status", "note")

		// Owner: capabilitycatalog.Entry in full view, CompactEntry in
		// compact view; the contract documents the union.
		entry := lane2ItemsOf(t, lane2Child(t, props, "capabilities", "capabilities"), "capabilities[]")
		entryProps := lane2PropsOf(t, entry, "capabilities[]")
		lane2RequireExactKeys(t, entryProps, "capabilities[]",
			"capability", "answering_stores", "display_name", "owner_package",
			"maturity", "derived_maturity", "maturity_reason", "profiles",
			"surfaces", "proof_signals", "known_gaps", "linked_issues",
			"docs", "console", "authorization")
		lane2RequireScalarTypes(t, entryProps, "capabilities[]", map[string]string{
			"console": "boolean",
		})
		lane2RequireDescribed(t, entryProps, "capabilities[]",
			"capability", "answering_stores", "display_name", "owner_package",
			"maturity", "derived_maturity", "maturity_reason", "profiles",
			"surfaces", "proof_signals", "known_gaps", "linked_issues",
			"docs", "console", "authorization")

		// Owner: capabilitycatalog.Surface.
		surface := lane2ItemsOf(t, lane2Child(t, entryProps, "surfaces", "capabilities.surface"), "capabilities.surface[]")
		surfaceProps := lane2PropsOf(t, surface, "capabilities.surface[]")
		lane2RequireExactKeys(t, surfaceProps, "capabilities.surface[]",
			"tool", "kind")
		lane2RequireDescribed(t, surfaceProps, "capabilities.surface[]",
			"tool", "kind")

		// Owner: capabilitycatalog.ProofSignal.
		signal := lane2ItemsOf(t, lane2Child(t, entryProps, "proof_signals", "capabilities.signal"), "capabilities.signal[]")
		signalProps := lane2PropsOf(t, signal, "capabilities.signal[]")
		lane2RequireExactKeys(t, signalProps, "capabilities.signal[]",
			"kind", "ref")
		lane2RequireDescribed(t, signalProps, "capabilities.signal[]",
			"kind", "ref")

		// Owner: capabilitycatalog.CapabilityAuthorization.
		entryAuth := lane2Child(t, entryProps, "authorization", "capabilities.entryauth")
		entryAuthProps := lane2PropsOf(t, entryAuth, "capabilities.entryauth")
		lane2RequireExactKeys(t, entryAuthProps, "capabilities.entryauth",
			"family", "action", "data_classes", "scope_levels",
			"default_roles", "sensitive_data")
		lane2RequireScalarTypes(t, entryAuthProps, "capabilities.entryauth", map[string]string{
			"sensitive_data": "boolean",
		})
		lane2RequireDescribed(t, entryAuthProps, "capabilities.entryauth",
			"family", "action", "data_classes", "scope_levels",
			"default_roles", "sensitive_data")

		// Owner: capabilitycatalog.EntryProfile keyed by runtime profile.
		profiles := lane2Child(t, entryProps, "profiles", "capabilities.profiles")
		add, ok := profiles["additionalProperties"].(map[string]any)
		if !ok {
			t.Fatalf("capabilities.profiles: missing additionalProperties for the per-profile map")
		}
		profileProps, ok := add["properties"].(map[string]any)
		if !ok {
			t.Fatalf("capabilities.profiles: additionalProperties has no properties")
		}
		lane2RequireExactKeys(t, profileProps, "capabilities.profiles.items",
			"status", "max_truth_level", "required_runtime",
			"p95_latency_ms", "max_scope_size")
		lane2RequireScalarTypes(t, profileProps, "capabilities.profiles.items", map[string]string{
			"p95_latency_ms": "integer",
		})
		lane2RequireDescribed(t, profileProps, "capabilities.profiles.items",
			"status", "max_truth_level", "required_runtime",
			"p95_latency_ms", "max_scope_size")

		lane2RequireNoBareObjects(t, schema, "capabilities")
	})
}
