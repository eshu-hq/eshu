// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"strings"
	"testing"
)

// TestContractShapesAccStatus pins the accuracy-lane status fixes: every
// P1/P2 plus the cheap mechanical P3s from the spec-versus-handler audit of
// the status, CI/CD, freshness, service, impact, catalog, and CLI fragments.
// Each subtest names the handler owner that emits the pinned contract. All
// changes are fragment-side only; no handler behavior changes.
func TestContractShapesAccStatus(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("ingesters list documents no dead 500", func(t *testing.T) {
		t.Parallel()
		// Owner: query/status.go listIngesters degrades a reader failure to
		// 200 (err == nil gate); no path writes 500.
		for _, path := range []string{"/api/v0/status/ingesters", "/api/v0/ingesters"} {
			responses := accStatusResponses(t, paths, path, "get")
			if _, ok := responses["500"]; ok {
				t.Errorf("%s: documents unreachable 500", path)
			}
			if _, ok := responses["200"]; !ok {
				t.Errorf("%s: missing 200", path)
			}
		}
	})

	t.Run("collectors names unscoped keys and scoped aggregates", func(t *testing.T) {
		t.Parallel()
		// Owner: query/status_collectors.go collectorRuntimeStatusesToSlice
		// emits the five unscoped keys; scopedCollectorRuntimeStatusesToSlice
		// emits the per-kind aggregates for scoped callers.
		schema := lane4SchemaAt(t, paths, "/api/v0/status/collectors", "get", "200")
		props := lane4PropsOf(t, schema, "collectors")
		rows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "collectors", "collectors"), "rows"), "rows[]")
		lane4RequireKeys(t, rows, "collectors[]",
			"bootstrap", "display_name", "last_observed_at", "updated_at", "deactivated_at",
			"collector_count", "coordinator_registered_count", "enabled_count",
			"bootstrap_count", "claims_enabled_count")
		lane4RequireDescribed(t, rows, "collectors[]",
			"bootstrap", "display_name", "last_observed_at", "updated_at", "deactivated_at",
			"collector_count", "coordinator_registered_count", "enabled_count",
			"bootstrap_count", "claims_enabled_count")
		op := lane4Child(t, paths["/api/v0/status/collectors"].(map[string]any), "get", "collectors")
		desc, _ := op["description"].(string)
		if !strings.Contains(desc, "per-kind") {
			t.Error("collectors description does not name the scoped per-kind aggregate shape")
		}
	})

	t.Run("hosted readiness notes unvalidated format fallback", func(t *testing.T) {
		t.Parallel()
		// Owner: query/status_hosted_readiness.go only honors format=text;
		// any other value serves the JSON 200.
		route := paths["/api/v0/status/hosted-readiness"].(map[string]any)
		op := lane4Child(t, route, "get", "hosted-readiness")
		params, ok := op["parameters"].([]any)
		if !ok || len(params) == 0 {
			t.Fatal("hosted-readiness has no parameters")
		}
		param, ok := params[0].(map[string]any)
		if !ok {
			t.Fatal("hosted-readiness first parameter is not an object")
		}
		desc, _ := param["description"].(string)
		if !strings.Contains(desc, "JSON 200") {
			t.Error("hosted-readiness format description does not note the unvalidated fallback serves JSON 200")
		}
	})

	t.Run("retirement documents 504", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/retirement/handler.go writeRetirementLookupFailure via
		// WriteGraphReadError maps a graph read deadline to 504.
		responses := accStatusResponses(t, paths, "/api/v0/admin/repository-retirements", "post")
		accStatusRequireCodes(t, responses, "retirement", "504")
	})

	t.Run("causality required and nullable timestamps", func(t *testing.T) {
		t.Parallel()
		// Owner: query/status_mappers.go withActiveWorkSource omits the key
		// when the reader reports no source; freshnessTransitionsToSlice
		// emits null timestamps via nullableRFC3339.
		schema := lane4SchemaAt(t, paths, "/api/v0/status/freshness-causality", "get", "200")
		required := accStatusRequired(t, schema, "causality")
		for _, key := range required {
			if key == "active_work_source" {
				t.Error("causality still requires conditionally-absent active_work_source")
			}
		}
		desc := accStatusDescription(t, lane4Child(t, lane4PropsOf(t, schema, "causality"), "active_work_source", "causality"), "causality.active_work_source")
		if !strings.Contains(desc, "only when") && !strings.Contains(desc, "absent when") {
			t.Error("causality active_work_source description does not note its conditional presence")
		}
		props := lane4PropsOf(t, schema, "causality")
		transitions := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, props, "recent_transitions", "causality"), "transitions"), "transitions[]")
		for _, key := range []string{"observed_at", "superseded_at"} {
			node, ok := transitions[key].(map[string]any)
			if !ok {
				t.Errorf("causality.transitions[] missing %q", key)
				continue
			}
			if !accStatusNullable(node) {
				t.Errorf("causality.transitions[] %q is not nullable", key)
			}
		}
	})

	t.Run("cicd run correlations documents selector errors", func(t *testing.T) {
		t.Parallel()
		// Owner: query/selector/selector.go ResolveForRequestWithAccess
		// answers 404 on no match and 409 on ambiguity.
		responses := accStatusResponses(t, paths, "/api/v0/ci-cd/run-correlations", "get")
		accStatusRequireCodes(t, responses, "run-correlations", "404", "409")
		for _, path := range []string{
			"/api/v0/ci-cd/run-correlations/count",
			"/api/v0/ci-cd/run-correlations/inventory",
		} {
			responses := accStatusResponses(t, paths, path, "get")
			accStatusRequireCodes(t, responses, path,
				"400", "404", "409", "500", "501", "403", "503")
		}
	})

	t.Run("service story documents 501", func(t *testing.T) {
		t.Parallel()
		// Owner: query/entity/service_story_seam.go BuildServiceStoryEnvelope
		// answers 501 on an unsupported capability.
		responses := accStatusResponses(t, paths, "/api/v0/services/{service_name}/story", "get")
		accStatusRequireCodes(t, responses, "story", "501")
	})

	t.Run("service catalog anchors and documents selector errors", func(t *testing.T) {
		t.Parallel()
		// Owner: query/service/catalog.go 400s without an anchor and resolves
		// via selector.ResolveForRequestWithAccess (404/409).
		route := paths["/api/v0/service-catalog/correlations"].(map[string]any)
		op := lane4Child(t, route, "get", "service-catalog")
		desc, _ := op["description"].(string)
		if !strings.Contains(desc, "anchor") || !strings.Contains(desc, "400") {
			t.Error("service-catalog description does not name the required anchor (400 without one)")
		}
		responses := accStatusResponses(t, paths, "/api/v0/service-catalog/correlations", "get")
		accStatusRequireCodes(t, responses, "service-catalog", "404", "409")
	})

	t.Run("catalog count names repositories and workloads", func(t *testing.T) {
		t.Parallel()
		// Owner: query/repository/catalog.go finish sets Count to
		// repositories plus workloads; services are excluded from Count.
		schema := lane4SchemaAt(t, paths, "/api/v0/catalog", "get", "200")
		count, ok := lane4PropsOf(t, schema, "catalog")["count"].(map[string]any)
		if !ok {
			t.Fatal("catalog schema has no count property")
		}
		desc, _ := count["description"].(string)
		if strings.Contains(desc, "across collections") {
			t.Error("catalog count still claims a total across collections")
		}
		if !strings.Contains(desc, "workloads") {
			t.Error("catalog count description does not name workloads")
		}
	})

	t.Run("cli validate and score mark absent bodies present-when", func(t *testing.T) {
		t.Parallel()
		// Owner: cli/support/handler.go validateBundle answers 200 valid:false
		// on an absent bundle; scoreEvidence scores an absent prompts set as
		// empty (only version mismatch 400s via ParseEvidence).
		validate := accStatusRequestSchema(t, paths, "/api/v0/report/bundle/validate", "post")
		if required := accStatusRequired(t, validate, "validate request"); accStatusHas(required, "bundle") {
			t.Error("validate request still requires bundle, but absent bundle answers 200 valid:false")
		}
		bundleDesc := accStatusDescription(t, lane4Child(t, lane4PropsOf(t, validate, "validate request"), "bundle", "validate request"), "validate request.bundle")
		if !strings.Contains(bundleDesc, "200") || !strings.Contains(bundleDesc, "valid:false") {
			t.Error("validate bundle description does not note the absent-bundle 200 valid:false")
		}
		score := accStatusRequestSchema(t, paths, "/api/v0/answer-quality/score", "post")
		if required := accStatusRequired(t, score, "score request"); accStatusHas(required, "prompts") {
			t.Error("score request still requires prompts, but absent prompts score as empty with 200")
		}
		if required := accStatusRequired(t, score, "score request"); !accStatusHas(required, "version") {
			t.Error("score request must still require version (ParseEvidence 400s otherwise)")
		}
		promptsDesc := accStatusDescription(t, lane4Child(t, lane4PropsOf(t, score, "score request"), "prompts", "score request"), "score request.prompts")
		if !strings.Contains(promptsDesc, "200") {
			t.Error("score prompts description does not note the absent-prompts 200")
		}
	})

	t.Run("cli request schemas reject unknown fields", func(t *testing.T) {
		t.Parallel()
		// Owner: cli/support/handler.go decodeBody (and the score strict
		// decoder) DisallowUnknownFields, answering 400 on unknown fields.
		for _, path := range []string{
			"/api/v0/investigation/packet/render",
			"/api/v0/service/report/compose",
			"/api/v0/report/bundle/capture",
			"/api/v0/report/bundle/validate",
			"/api/v0/answer-quality/score",
		} {
			schema := accStatusRequestSchema(t, paths, path, "post")
			if schema["additionalProperties"] != false {
				t.Errorf("%s request schema does not declare additionalProperties:false", path)
			}
		}
	})

	t.Run("trace deployment chain defaults direct_only false", func(t *testing.T) {
		t.Parallel()
		// Owner: query/impact/trace_deployment.go decodes an absent
		// direct_only to false, so enrichment runs by default.
		schema := accStatusRequestSchema(t, paths, "/api/v0/impact/trace-deployment-chain", "post")
		direct, ok := lane4PropsOf(t, schema, "trace request")["direct_only"].(map[string]any)
		if !ok {
			t.Fatal("trace request has no direct_only property")
		}
		if def, _ := direct["default"].(bool); def {
			t.Error("trace direct_only still defaults true; the handler decodes absent to false")
		}
		desc, _ := direct["description"].(string)
		if !strings.Contains(desc, "false") {
			t.Error("trace direct_only description does not state the absent-field behavior")
		}
		responses := accStatusResponses(t, paths, "/api/v0/impact/trace-deployment-chain", "post")
		accStatusRequireCodes(t, responses, "trace-deployment-chain", "501")
	})

	t.Run("impact change surfaces document 404 and 501s", func(t *testing.T) {
		t.Parallel()
		// Owner: query/impact/change_surface_investigation.go answers 404
		// on an ungranted repo_id; every capability-gated impact route
		// answers 501 via CapabilityUnsupported.
		investigate := accStatusResponses(t, paths, "/api/v0/impact/change-surface/investigate", "post")
		accStatusRequireCodes(t, investigate, "investigate", "404")
		for _, path := range []string{
			"/api/v0/impact/trace-deployment-chain",
			"/api/v0/impact/change-surface",
			"/api/v0/impact/deployment-config-influence",
			"/api/v0/impact/trace-resource-to-code",
			"/api/v0/impact/explain-dependency-path",
			"/api/v0/impact/blast-radius",
		} {
			responses := accStatusResponses(t, paths, path, "post")
			accStatusRequireCodes(t, responses, path, "501")
		}
	})
}

func accStatusResponses(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	route, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("spec paths missing %s", path)
	}
	op := lane4Child(t, route, method, path)
	return lane4Child(t, op, "responses", path+" "+method)
}

func accStatusRequireCodes(t *testing.T, responses map[string]any, where string, codes ...string) {
	t.Helper()
	for _, code := range codes {
		if _, ok := responses[code]; !ok {
			t.Errorf("%s: missing %s response", where, code)
		}
	}
}

func accStatusRequestSchema(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	route, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("spec paths missing %s", path)
	}
	op := lane4Child(t, route, method, path)
	body := lane4Child(t, op, "requestBody", path+" "+method)
	content := lane4Child(t, body, "content", path+" "+method)
	jsonBody := lane4Child(t, content, "application/json", path+" "+method)
	return lane4Child(t, jsonBody, "schema", path+" "+method)
}

func accStatusRequired(t *testing.T, schema map[string]any, where string) []string {
	t.Helper()
	raw, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s: non-string required entry %v", where, v)
		}
		out = append(out, s)
	}
	return out
}

func accStatusHas(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

func accStatusDescription(t *testing.T, node map[string]any, where string) string {
	t.Helper()
	desc, _ := node["description"].(string)
	if desc == "" {
		t.Errorf("%s: missing description", where)
	}
	return desc
}

func accStatusNullable(node map[string]any) bool {
	typ, ok := node["type"]
	if !ok {
		return false
	}
	if s, ok := typ.(string); ok {
		return s == "null"
	}
	types, ok := typ.([]any)
	if !ok {
		return false
	}
	var hasString, hasNull bool
	for _, v := range types {
		if s, ok := v.(string); ok {
			hasString = hasString || s == "string"
			hasNull = hasNull || s == "null"
		}
	}
	return hasString && hasNull
}
