// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB11 pins the lane-B11 shape-completeness fixes: the
// 200-response schemas in this lane must not carry bare {"type": "object"}
// shapes, and every property on them must carry a description naming its
// emitting owner. Each entry names the Go owner that emits the keys the
// contract asserts. Request schemas are out of scope for this lane.
func TestContractShapesLaneB11(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components object")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec components has no schemas object")
	}

	schemaOf := func(t *testing.T, name string) map[string]any {
		t.Helper()
		node, ok := schemas[name].(map[string]any)
		if !ok {
			t.Fatalf("spec schemas missing %q", name)
		}
		return node
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
	}

	// requireDescribedTypes pins that every listed property exists, carries
	// a description naming its owner, and has the owner's JSON scalar type.
	// Presence-only checks pass a boolean where the server emits an integer,
	// so every hand-typed scalar needs its type pinned.
	requireDescribedTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
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
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
			if node["type"] == "object" {
				_, hasProps := node["properties"]
				_, hasAdditional := node["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
			if node["type"] == "array" {
				items, ok := node["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: property %q is an array with no items", where, key)
					continue
				}
				if items["type"] == "object" {
					_, hasProps := items["properties"]
					_, hasAdditional := items["additionalProperties"]
					if !hasProps && !hasAdditional {
						t.Errorf("%s: property %q has bare object items", where, key)
					}
				}
			}
		}
	}

	// requireExactKeys pins the closed property set of an object whose owner
	// emits a fixed key list: missing keys and phantom additions both fail.
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
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

	t.Run("why trail steps are open maps", func(t *testing.T) {
		t.Parallel()
		rel := schemaOf(t, "Relationship")
		prov, ok := propsOf(t, rel, "Relationship")["provenance"].(map[string]any)
		if !ok {
			t.Fatal("Relationship has no provenance property")
		}
		trail, ok := propsOf(t, prov, "Relationship.provenance")["why_trail"].(map[string]any)
		if !ok {
			t.Fatal("Relationship.provenance has no why_trail property")
		}
		items := itemsOf(t, trail, "Relationship.provenance.why_trail")
		if got, _ := items["type"].(string); got != "object" {
			t.Fatalf("why_trail items type = %q, want object", got)
		}
		// Owner codemodel.RelationshipStoryProvenance decodes why_trail_json
		// into []map[string]any: a genuine open map, so the contract carries
		// additionalProperties with the value type plus key semantics.
		if _, ok := items["additionalProperties"]; !ok {
			t.Error("why_trail items is a bare object with no additionalProperties")
		}
		if desc, _ := items["description"].(string); desc == "" {
			t.Error("why_trail items has no description")
		}
	})

	t.Run("collector readiness counts", func(t *testing.T) {
		t.Parallel()
		env := schemaOf(t, "CollectorReadinessEnvelope")
		props := propsOf(t, env, "CollectorReadinessEnvelope")
		counts, ok := props["counts"].(map[string]any)
		if !ok {
			t.Fatal("CollectorReadinessEnvelope has no counts property")
		}
		if desc, _ := counts["description"].(string); desc == "" {
			t.Error("CollectorReadinessEnvelope.counts has no description")
		}
		// Owner querycontract.BuildCollectorListReadiness always emits both
		// keys of CollectorListReadinessCounts.
		countProps := propsOf(t, counts, "CollectorReadinessEnvelope.counts")
		requireExactKeys(t, countProps, "CollectorReadinessEnvelope.counts",
			"results_returned", "results_truncated")
		requireDescribedTypes(t, countProps, "CollectorReadinessEnvelope.counts", map[string]string{
			"results_returned":  "integer",
			"results_truncated": "boolean",
		})
	})

	t.Run("semantic evidence row", func(t *testing.T) {
		t.Parallel()
		// Owner query.semanticEvidencePublicRow projects the durable fact
		// record; source-ACL posture via query.surfaceSourceACLState and
		// query.applySourceACLDisclosure.
		row := schemaOf(t, "SemanticEvidenceRow")
		props := propsOf(t, row, "SemanticEvidenceRow")
		requireDescribedTypes(t, props, "SemanticEvidenceRow", map[string]string{
			"fact_id": "string", "fact_kind": "string",
			"truth_basis": "string", "provider_profile_id": "string",
			"provider_kind": "string", "prompt_version": "string",
			"redaction_version": "string", "policy_state": "string",
			"freshness_state": "string", "admission_state": "string",
			"corroboration_state": "string", "source": "object",
			"chunk": "object", "provider": "object",
		})
	})

	t.Run("code search shapes", func(t *testing.T) {
		t.Parallel()
		// Row owners: codequery.CodeHandler search builders
		// (searchGraphEntitiesWithExact for graph rows,
		// searchEntityContentWithExact for content rows), clips via
		// querycontract.ClipRowsSourceCache/ClipRowsDocstring, semantic
		// copies via entitysemantics.AttachSemanticSummary, hybrid marker
		// via the codemodel hybrid rerank.
		result := schemaOf(t, "CodeSearchResult")
		requireDescribedTypes(t, propsOf(t, result, "CodeSearchResult"), "CodeSearchResult", map[string]string{
			"entity_id": "string", "entity_name": "string",
			"entity_type": "string", "name": "string",
			"labels": "array", "file_path": "string",
			"start_line": "integer", "end_line": "integer",
			"language": "string", "repo_id": "string",
			"repo_name": "string",
		})
		// Envelope owner codemodel.CodeSearchPagePayloadFromPage; rank_walk
		// added by codequery.writeRankWalkPage on the rank_cursor path.
		resp := schemaOf(t, "CodeSearchResponse")
		respProps := propsOf(t, resp, "CodeSearchResponse")
		requireDescribedTypes(t, respProps, "CodeSearchResponse", map[string]string{
			"source": "string", "source_backend": "string",
			"query": "string", "repo_id": "string",
			"results": "array", "limit": "integer",
		})
		if desc, _ := respProps["rank_walk"].(map[string]any)["description"].(string); desc == "" {
			t.Error("CodeSearchResponse.rank_walk has no description")
		}
	})

	t.Run("symbol search shapes", func(t *testing.T) {
		t.Parallel()
		// Row owners codequery.symbolEntityResults (content path) and
		// codequery.normalizeSymbolGraphResults (graph path); envelope
		// owner codequery.handleSymbolSearch.
		result := schemaOf(t, "SymbolSearchResult")
		requireDescribedTypes(t, propsOf(t, result, "SymbolSearchResult"), "SymbolSearchResult", map[string]string{
			"entity_id": "string", "name": "string",
			"entity_name": "string", "entity_type": "string",
			"file_path": "string", "relative_path": "string",
			"repo_id": "string", "language": "string",
			"start_line": "integer", "end_line": "integer",
			"classification": "string", "match_kind": "string",
			"rank": "integer", "semantic_summary": "string",
			"semantic_profile": "object",
		})
		resp := schemaOf(t, "SymbolSearchResponse")
		respProps := propsOf(t, resp, "SymbolSearchResponse")
		requireDescribedTypes(t, respProps, "SymbolSearchResponse", map[string]string{
			"symbol": "string", "query": "string",
			"match_mode": "string", "repo_id": "string",
			"language": "string", "entity_types": "array",
			"limit": "integer", "offset": "integer",
			"count": "integer", "truncated": "boolean",
			"source_backend": "string", "ambiguity": "object",
			"results": "array",
		})
	})

	t.Run("language query response", func(t *testing.T) {
		t.Parallel()
		// Owner language.languageQueryResponseBody (echo keys) with
		// source_backend set by language.writeLanguageQueryResult and
		// language.writeLanguageQueryEmptyGrantResult.
		resp := schemaOf(t, "LanguageQueryResponse")
		respProps := propsOf(t, resp, "LanguageQueryResponse")
		requireDescribedTypes(t, respProps, "LanguageQueryResponse", map[string]string{
			"language": "string", "entity_type": "string",
			"query": "string", "results": "array",
		})
		requireExactKeys(t, respProps, "LanguageQueryResponse",
			"language", "entity_type", "query", "results", "source_backend")
	})

	t.Run("local identity responses", func(t *testing.T) {
		t.Parallel()
		// Owners local.localIdentityAPITokenResponse (token routes) and
		// local.IdentitySessionResponse (session writer).
		token := schemaOf(t, "LocalIdentityAPITokenResponse")
		requireDescribedTypes(t, propsOf(t, token, "LocalIdentityAPITokenResponse"), "LocalIdentityAPITokenResponse", map[string]string{
			"status": "string", "token_id": "string",
			"token_class": "string", "issued_at": "string",
			"expires_at": "string",
		})
		session := schemaOf(t, "LocalIdentitySessionResponse")
		sessionProps := propsOf(t, session, "LocalIdentitySessionResponse")
		requireDescribedTypes(t, sessionProps, "LocalIdentitySessionResponse", map[string]string{
			"status": "string", "csrf_token": "string",
			"idle_expires_at": "string", "absolute_expires_at": "string",
			"locked_until": "string",
		})
		if desc, _ := sessionProps["auth"].(map[string]any)["description"].(string); desc == "" {
			t.Error("LocalIdentitySessionResponse.auth has no description")
		}
		// Owner governanceaudit.Count via
		// query.normalizeGovernanceAuditCounts always emits both keys.
		audit := schemaOf(t, "AdminAuditCountList")
		items := itemsOf(t, audit, "AdminAuditCountList")
		itemProps := propsOf(t, items, "AdminAuditCountList[]")
		requireExactKeys(t, itemProps, "AdminAuditCountList[]", "name", "count")
		requireDescribedTypes(t, itemProps, "AdminAuditCountList[]", map[string]string{
			"name": "string", "count": "integer",
		})
	})

	t.Run("provider config responses", func(t *testing.T) {
		t.Parallel()
		// Owners config.providerConfigDetailJSON (reads.go) and
		// config.providerConfigWriteResponse (audit.go).
		cfg := schemaOf(t, "AdminProviderConfig")
		cfgProps := propsOf(t, cfg, "AdminProviderConfig")
		requireExactKeys(t, cfgProps, "AdminProviderConfig",
			"provider_config_id", "provider_kind", "status",
			"active_revision_id", "configuration", "has_secret",
			"secret_fingerprint", "key_id", "shadowed_by_environment",
			"managed_by", "created_at", "updated_at")
		requireDescribedTypes(t, cfgProps, "AdminProviderConfig", map[string]string{
			"provider_config_id": "string", "provider_kind": "string",
			"status": "string", "active_revision_id": "string",
			"configuration": "object", "has_secret": "boolean",
			"secret_fingerprint": "string", "key_id": "string",
			"created_at": "string", "updated_at": "string",
		})
		// Owner Detail.Configuration is map[string]any: a genuine open
		// map, so the contract carries additionalProperties plus key
		// semantics instead of fixed properties.
		configuration, ok := cfgProps["configuration"].(map[string]any)
		if !ok {
			t.Fatal("AdminProviderConfig has no configuration property")
		}
		if _, ok := configuration["additionalProperties"]; !ok {
			t.Error("AdminProviderConfig.configuration is a bare object with no additionalProperties")
		}
		result := schemaOf(t, "AdminProviderConfigWriteResult")
		resultProps := propsOf(t, result, "AdminProviderConfigWriteResult")
		requireExactKeys(t, resultProps, "AdminProviderConfigWriteResult",
			"provider_config_id", "revision_id", "status", "changed")
		requireDescribedTypes(t, resultProps, "AdminProviderConfigWriteResult", map[string]string{
			"provider_config_id": "string", "revision_id": "string",
			"status": "string", "changed": "boolean",
		})
	})

	t.Run("sign in policy response", func(t *testing.T) {
		t.Parallel()
		// Owner query.signInPolicyDetailJSON projects SignInPolicy.
		policy := schemaOf(t, "AdminSignInPolicy")
		policyProps := propsOf(t, policy, "AdminSignInPolicy")
		requireExactKeys(t, policyProps, "AdminSignInPolicy",
			"tenant_id", "require_sso", "allow_local_user_creation",
			"require_mfa_for_all_users", "idle_timeout_seconds",
			"absolute_timeout_seconds", "sso_admin_verified_at",
			"sso_admin_verified_provider_config_id", "policy_revision_hash",
			"updated_at")
		requireDescribedTypes(t, policyProps, "AdminSignInPolicy", map[string]string{
			"tenant_id": "string", "sso_admin_verified_at": "string",
			"sso_admin_verified_provider_config_id": "string",
			"policy_revision_hash":                  "string", "updated_at": "string",
		})
	})

	t.Run("completed lane stays described", func(t *testing.T) {
		t.Parallel()
		// Recovery, pacing, and work-source envelopes already carry owner
		// descriptions; pin them so this lane cannot regress them.
		for schema, keys := range map[string][]string{
			"DeltaActiveScopesReport":      {"total", "by_outcome", "sample_scope_ids", "detail"},
			"ReindexRequestsWrittenReport": {"count", "scope_ids"},
			"SourcePacing": {
				"source_system", "slowed", "reason", "oldest_claimable_age",
				"oldest_claimable_age_seconds", "claimable_depth", "engaged_since",
				"engaged_duration", "engaged_duration_seconds", "stuck",
			},
			"ActiveWorkSource":     {"source", "reason", "as_of", "age_seconds", "stale"},
			"TerraformStateSource": {"source", "reason", "as_of", "age_seconds", "stale"},
			"CoordinatorSource":    {"source", "reason", "as_of", "age_seconds", "stale"},
		} {
			node := schemaOf(t, schema)
			props := propsOf(t, node, schema)
			for _, key := range keys {
				prop, ok := props[key].(map[string]any)
				if !ok {
					t.Errorf("%s: missing property %q", schema, key)
					continue
				}
				if desc, _ := prop["description"].(string); desc == "" {
					t.Errorf("%s: property %q has no description", schema, key)
				}
			}
		}
	})
}
