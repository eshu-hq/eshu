// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9Infrastructure pins the lane-B9 shape-completeness
// fixes for the infrastructure family: the graph-backed infra search,
// relationships, resource aggregates, ecosystem overview, graph summary,
// relationships catalog/edges, Kubernetes correlations, observability
// coverage correlations, and the five secrets/IAM reads. Bare
// {"type": "object"} shapes gain properties (or a described open-map shape),
// and every 200-response property carries a description naming its Go owner.
func TestContractShapesLaneB9Infrastructure(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
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

	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
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

	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
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

	schema200 := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			t.Fatal("spec has no paths object")
		}
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content := child(t, okResp, "content", path+" "+method+" 200")
		body := child(t, content, "application/json", path+" "+method+" 200")
		return child(t, body, "schema", path+" "+method+" 200")
	}

	t.Run("infra resource search", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/infra/resources/search"
		top := propsOf(t, schema200(t, "/api/v0/infra/resources/search", "post"), where)
		requireExactKeys(t, top, where, "results", "count", "limit", "truncated")
		requireDescribed(t, top, where, "results", "count", "limit", "truncated")
		items := itemsOf(t, top["results"].(map[string]any), where+" results")
		rprops := propsOf(t, items, where+" results[]")
		// Owner: query infra search result projection in infra.go.
		requireKeys(t, rprops, where+" results[]",
			"id", "name", "labels", "kind", "provider", "environment",
			"source", "config_path")
		requireDescribed(t, rprops, where+" results[]",
			"id", "name", "labels", "kind", "provider", "environment",
			"source", "config_path", "resource_type", "resource_service",
			"resource_category", "resource_id", "arn", "account_id", "region",
			"service_kind", "running_image_ref", "running_image_digest")
	})

	t.Run("infra relationships", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/infra/relationships"
		top := propsOf(t, schema200(t, "/api/v0/infra/relationships", "post"), where)
		requireExactKeys(t, top, where, "id", "name", "labels", "outgoing", "incoming")
		requireDescribed(t, top, where, "id", "name", "labels", "outgoing", "incoming")
		// outgoing/incoming items are the shared Relationship component $ref
		// outside this lane's scope; the route pins the arrays plus their
		// descriptions here.
		requireKeys(t, top, where, "outgoing", "incoming")
	})

	t.Run("ecosystem overview", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/ecosystem/overview"
		top := propsOf(t, schema200(t, "/api/v0/ecosystem/overview", "get"), where)
		// Owner: query.runEcosystemOverviewCounts fields.
		requireExactKeys(t, top, where,
			"repo_count", "workload_count", "platform_count", "instance_count")
		requireDescribed(t, top, where,
			"repo_count", "workload_count", "platform_count", "instance_count")
	})

	t.Run("ecosystem graph summary", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/ecosystem/graph-summary"
		top := propsOf(t, schema200(t, "/api/v0/ecosystem/graph-summary", "post"), where)
		requireExactKeys(t, top, where,
			"scope", "repo_id", "hot_entities", "hot_entities_truncated",
			"key_relationships", "ecosystem_map", "note")
		requireDescribed(t, top, where,
			"scope", "repo_id", "hot_entities", "hot_entities_truncated",
			"key_relationships", "ecosystem_map", "note")
		hot := itemsOf(t, top["hot_entities"].(map[string]any), where+" hot_entities")
		hprops := propsOf(t, hot, where+" hot_entities[]")
		// Owner: query.graphSummaryHotEntitiesNeo4j / codemodel.CallGraphMetricsRows.
		requireExactKeys(t, hprops, where+" hot_entities[]",
			"function_id", "function_name", "file_path",
			"incoming_calls", "outgoing_calls", "total_degree")
		requireDescribed(t, hprops, where+" hot_entities[]",
			"function_id", "function_name", "file_path",
			"incoming_calls", "outgoing_calls", "total_degree")
		// key_relationships is a genuine open map: edge type -> bounded count
		// over the fixed CALLS/IMPORTS/INHERITS/OVERRIDES/REFERENCES set.
		rel := top["key_relationships"].(map[string]any)
		if _, ok := rel["additionalProperties"]; !ok {
			t.Errorf("%s: key_relationships must carry additionalProperties", where)
		}
		// ecosystem_map is a genuine open map: structural count fields plus
		// the languages list on the repo-scoped packet.
		eco := top["ecosystem_map"].(map[string]any)
		if _, ok := eco["additionalProperties"]; !ok {
			t.Errorf("%s: ecosystem_map must carry additionalProperties", where)
		}
	})

	t.Run("relationships catalog", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/relationships/catalog"
		top := propsOf(t, schema200(t, "/api/v0/relationships/catalog", "post"), where)
		requireExactKeys(t, top, where, "verbs", "verb_count", "total_edges", "layer_count")
		requireDescribed(t, top, where, "verbs", "verb_count", "total_edges", "layer_count")
		items := itemsOf(t, top["verbs"].(map[string]any), where+" verbs")
		vprops := propsOf(t, items, where+" verbs[]")
		// Owner: query.relationshipVerbTile.
		requireExactKeys(t, vprops, where+" verbs[]",
			"verb", "layer", "count", "evidence", "detail", "source_tools")
		requireDescribed(t, vprops, where+" verbs[]",
			"verb", "layer", "count", "evidence", "detail", "source_tools")
		// source_tools is a genuine open map: source-tool token -> edge count
		// for Tier-2 verbs that stamp source_tool.
		st := vprops["source_tools"].(map[string]any)
		if _, ok := st["additionalProperties"]; !ok {
			t.Errorf("%s: verbs[].source_tools must carry additionalProperties", where)
		}
	})

	t.Run("relationship edges", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/relationships/edges"
		top := propsOf(t, schema200(t, "/api/v0/relationships/edges", "post"), where)
		requireExactKeys(t, top, where,
			"verb", "layer", "evidence", "detail", "edges", "truncated", "limit", "source_tool")
		requireDescribed(t, top, where,
			"verb", "layer", "evidence", "detail", "edges", "truncated", "limit", "source_tool")
		items := itemsOf(t, top["edges"].(map[string]any), where+" edges")
		eprops := propsOf(t, items, where+" edges[]")
		// Owner: query.relationshipEdge.
		requireExactKeys(t, eprops, where+" edges[]",
			"source_id", "source_name", "target_id", "target_name", "evidence", "source_tool")
		requireDescribed(t, eprops, where+" edges[]",
			"source_id", "source_name", "target_id", "target_name", "evidence", "source_tool")
	})

	t.Run("infra resource aggregates", func(t *testing.T) {
		t.Parallel()
		count := propsOf(t, schema200(t, "/api/v0/infra/resources/count", "get"), "GET /api/v0/infra/resources/count")
		// Owner: query infra aggregates handler count body.
		requireExactKeys(t, count, "GET /api/v0/infra/resources/count",
			"total_resources", "by_provider", "by_environment", "by_label", "scope")
		requireDescribed(t, count, "GET /api/v0/infra/resources/count",
			"total_resources", "by_provider", "by_environment", "by_label", "scope")
		for _, key := range []string{"by_provider", "by_environment", "by_label"} {
			node := count[key].(map[string]any)
			if _, ok := node["additionalProperties"]; !ok {
				t.Errorf("GET /api/v0/infra/resources/count: %s must carry additionalProperties", key)
			}
		}
		// scope is a genuine filter-echo string map: query.infraResourceAggregateScope.
		scope := count["scope"].(map[string]any)
		if _, ok := scope["additionalProperties"]; !ok {
			t.Errorf("GET /api/v0/infra/resources/count: scope must carry additionalProperties")
		}
		inv := propsOf(t, schema200(t, "/api/v0/infra/resources/inventory", "get"), "GET /api/v0/infra/resources/inventory")
		// Owner: query infra aggregates handler inventory body.
		requireExactKeys(t, inv, "GET /api/v0/infra/resources/inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated", "next_offset", "scope")
		requireDescribed(t, inv, "GET /api/v0/infra/resources/inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated", "next_offset", "scope")
		bitems := itemsOf(t, inv["buckets"].(map[string]any), "GET /api/v0/infra/resources/inventory buckets")
		bprops := propsOf(t, bitems, "GET /api/v0/infra/resources/inventory buckets[]")
		// Owner: query.InfraResourceInventoryRow.
		requireExactKeys(t, bprops, "GET /api/v0/infra/resources/inventory buckets[]",
			"dimension", "value", "count")
		requireDescribed(t, bprops, "GET /api/v0/infra/resources/inventory buckets[]",
			"dimension", "value", "count")
		iscope := inv["scope"].(map[string]any)
		if _, ok := iscope["additionalProperties"]; !ok {
			t.Errorf("GET /api/v0/infra/resources/inventory: scope must carry additionalProperties")
		}
	})

	t.Run("kubernetes correlations", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/kubernetes/correlations"
		top := propsOf(t, schema200(t, "/api/v0/kubernetes/correlations", "get"), where)
		requireExactKeys(t, top, where, "correlations", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where, "correlations", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["correlations"].(map[string]any), where+" correlations")
		cprops := propsOf(t, items, where+" correlations[]")
		// Owner: kubernetes.CorrelationResult.
		requireExactKeys(t, cprops, where+" correlations[]",
			"correlation_id", "cluster_id", "workload_object_id", "namespace",
			"workload_name", "workload_uid", "image_ref", "source_digest",
			"join_mode", "identity_edge_key", "relationship_type", "outcome",
			"drift_kind", "reason", "non_promotion", "provenance_only",
			"candidate_source_digests", "warnings", "evidence_fact_ids")
		requireDescribed(t, cprops, where+" correlations[]",
			"correlation_id", "cluster_id", "workload_object_id", "namespace",
			"workload_name", "workload_uid", "image_ref", "source_digest",
			"join_mode", "identity_edge_key", "relationship_type", "outcome",
			"drift_kind", "reason", "non_promotion", "provenance_only",
			"candidate_source_digests", "warnings", "evidence_fact_ids")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_correlation_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_correlation_id")
	})

	t.Run("observability coverage correlations", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/observability/coverage/correlations"
		top := propsOf(t, schema200(t, "/api/v0/observability/coverage/correlations", "get"), where)
		requireExactKeys(t, top, where, "correlations", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where, "correlations", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["correlations"].(map[string]any), where+" correlations")
		cprops := propsOf(t, items, where+" correlations[]")
		// Owner: coverage.CorrelationResult.
		requireExactKeys(t, cprops, where+" correlations[]",
			"correlation_id", "provider", "coverage_signal", "observability_object_ref",
			"observability_resource_uid", "target_uid", "target_service_ref",
			"outcome", "reason", "coverage_status", "provenance_only",
			"resolution_mode", "source_class", "source_classes", "source_kind",
			"source_kinds", "source_outcome", "source_outcomes", "resource_class",
			"freshness_state", "candidate_target_uids", "evidence_fact_ids")
		requireDescribed(t, cprops, where+" correlations[]",
			"correlation_id", "provider", "coverage_signal", "observability_object_ref",
			"observability_resource_uid", "target_uid", "target_service_ref",
			"outcome", "reason", "coverage_status", "provenance_only",
			"resolution_mode", "source_class", "source_classes", "source_kind",
			"source_kinds", "source_outcome", "source_outcomes", "resource_class",
			"freshness_state", "candidate_target_uids", "evidence_fact_ids")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_correlation_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_correlation_id")
	})

	t.Run("secrets-iam identity trust chains", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/secrets-iam/identity-trust-chains"
		top := propsOf(t, schema200(t, "/api/v0/secrets-iam/identity-trust-chains", "get"), where)
		requireExactKeys(t, top, where,
			"identity_trust_chains", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where,
			"identity_trust_chains", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["identity_trust_chains"].(map[string]any), where+" identity_trust_chains")
		cprops := propsOf(t, items, where+" identity_trust_chains[]")
		// Owner: secrets.IAMIdentityTrustChainResult.
		requireExactKeys(t, cprops, where+" identity_trust_chains[]",
			"chain_id", "state", "confidence", "service_account_join_key",
			"workload_object_id", "workload_kind", "iam_role_fingerprint",
			"vault_role_join_key", "vault_mount_join_key", "vault_policy_join_keys",
			"evidence_fact_ids", "missing_evidence", "source_scopes", "source_generations")
		requireDescribed(t, cprops, where+" identity_trust_chains[]",
			"chain_id", "state", "confidence", "service_account_join_key",
			"workload_object_id", "workload_kind", "iam_role_fingerprint",
			"vault_role_join_key", "vault_mount_join_key", "vault_policy_join_keys",
			"evidence_fact_ids", "missing_evidence", "source_scopes", "source_generations")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_chain_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_chain_id")
	})

	t.Run("secrets-iam posture gaps", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/secrets-iam/posture-gaps"
		top := propsOf(t, schema200(t, "/api/v0/secrets-iam/posture-gaps", "get"), where)
		requireExactKeys(t, top, where, "posture_gaps", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where, "posture_gaps", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["posture_gaps"].(map[string]any), where+" posture_gaps")
		gprops := propsOf(t, items, where+" posture_gaps[]")
		// Owner: secrets.IAMPostureGapResult.
		requireExactKeys(t, gprops, where+" posture_gaps[]",
			"gap_id", "gap_type", "state", "reason", "service_account_join_key",
			"evidence_fact_ids", "missing_evidence", "unsupported_layers")
		requireDescribed(t, gprops, where+" posture_gaps[]",
			"gap_id", "gap_type", "state", "reason", "service_account_join_key",
			"evidence_fact_ids", "missing_evidence", "unsupported_layers")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_gap_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_gap_id")
	})

	t.Run("secrets-iam posture summary", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/secrets-iam/posture-summary"
		top := propsOf(t, schema200(t, "/api/v0/secrets-iam/posture-summary", "get"), where)
		requireExactKeys(t, top, where, "scope_id", "summary")
		requireDescribed(t, top, where, "scope_id", "summary")
		summary := propsOf(t, top["summary"].(map[string]any), where+" summary")
		// Owner: secrets.IAMPostureSummary.
		requireExactKeys(t, summary, where+" summary",
			"identity_trust_chains_by_state", "privilege_observations_by_risk_type",
			"privilege_observations_by_severity", "secret_access_paths_by_state",
			"posture_gaps_by_gap_type", "s3_external_principal_grant_posture")
		requireDescribed(t, summary, where+" summary",
			"identity_trust_chains_by_state", "privilege_observations_by_risk_type",
			"privilege_observations_by_severity", "secret_access_paths_by_state",
			"posture_gaps_by_gap_type", "s3_external_principal_grant_posture")
		for _, bucket := range []string{
			"identity_trust_chains_by_state", "privilege_observations_by_risk_type",
			"privilege_observations_by_severity", "secret_access_paths_by_state",
			"posture_gaps_by_gap_type",
		} {
			bitems := itemsOf(t, summary[bucket].(map[string]any), where+" summary."+bucket)
			bprops := propsOf(t, bitems, where+" summary."+bucket+"[]")
			// Owner: secrets.IAMBucketCount.
			requireExactKeys(t, bprops, where+" summary."+bucket+"[]", "bucket", "count")
			requireDescribed(t, bprops, where+" summary."+bucket+"[]", "bucket", "count")
		}
		grant := propsOf(t, summary["s3_external_principal_grant_posture"].(map[string]any), where+" summary.s3")
		// Owner: secrets grant-posture view (grant_posture.go).
		requireExactKeys(t, grant, where+" summary.s3",
			"total_grants", "grants_by_outcome", "grants_by_resolution_mode",
			"public_grants", "cross_account_grants", "service_principal_grants")
		requireDescribed(t, grant, where+" summary.s3",
			"total_grants", "grants_by_outcome", "grants_by_resolution_mode",
			"public_grants", "cross_account_grants", "service_principal_grants")
		for _, bucket := range []string{"grants_by_outcome", "grants_by_resolution_mode"} {
			bitems := itemsOf(t, grant[bucket].(map[string]any), where+" summary.s3."+bucket)
			bprops := propsOf(t, bitems, where+" summary.s3."+bucket+"[]")
			requireExactKeys(t, bprops, where+" summary.s3."+bucket+"[]", "bucket", "count")
			requireDescribed(t, bprops, where+" summary.s3."+bucket+"[]", "bucket", "count")
		}
	})

	t.Run("secrets-iam privilege posture observations", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/secrets-iam/privilege-posture-observations"
		top := propsOf(t, schema200(t, "/api/v0/secrets-iam/privilege-posture-observations", "get"), where)
		requireExactKeys(t, top, where,
			"privilege_posture_observations", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where,
			"privilege_posture_observations", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["privilege_posture_observations"].(map[string]any), where+" privilege_posture_observations")
		oprops := propsOf(t, items, where+" privilege_posture_observations[]")
		// Owner: secrets.IAMPrivilegePostureObservationResult.
		requireExactKeys(t, oprops, where+" privilege_posture_observations[]",
			"observation_id", "risk_type", "severity", "state", "confidence",
			"subject_fingerprint", "reason", "evidence_fact_ids")
		requireDescribed(t, oprops, where+" privilege_posture_observations[]",
			"observation_id", "risk_type", "severity", "state", "confidence",
			"subject_fingerprint", "reason", "evidence_fact_ids")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_observation_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_observation_id")
	})

	t.Run("secrets-iam secret access paths", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/secrets-iam/secret-access-paths"
		top := propsOf(t, schema200(t, "/api/v0/secrets-iam/secret-access-paths", "get"), where)
		requireExactKeys(t, top, where,
			"secret_access_paths", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, top, where,
			"secret_access_paths", "count", "limit", "truncated", "next_cursor")
		items := itemsOf(t, top["secret_access_paths"].(map[string]any), where+" secret_access_paths")
		pprops := propsOf(t, items, where+" secret_access_paths[]")
		// Owner: secrets.IAMSecretAccessPathResult.
		requireExactKeys(t, pprops, where+" secret_access_paths[]",
			"path_id", "chain_id", "state", "confidence", "kv_path_fingerprint",
			"vault_mount_join_key", "vault_policy_join_key", "capabilities",
			"evidence_fact_ids")
		requireDescribed(t, pprops, where+" secret_access_paths[]",
			"path_id", "chain_id", "state", "confidence", "kv_path_fingerprint",
			"vault_mount_join_key", "vault_policy_join_key", "capabilities",
			"evidence_fact_ids")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		requireExactKeys(t, cursor, where+" next_cursor", "after_path_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_path_id")
	})
}
