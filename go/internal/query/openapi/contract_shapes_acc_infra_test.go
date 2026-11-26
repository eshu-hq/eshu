// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesAccInfra pins the accuracy-lane fragment fixes for the
// iac, cloud, infrastructure, repository, and supply families. Every pin
// below mirrors a handler-verified mismatch: the drift finding-kind vocab,
// always-emitted drift keys, error codes the handlers write but the docs
// omit, anchor rules enforced with 400, and conditional keys the docs
// overstate as required.
func TestContractShapesAccInfra(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}

	opOf := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op, ok := route[method].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing method %q", path, method)
		}
		return op
	}

	responsesOf := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		op := opOf(t, path, method)
		responses, ok := op["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s: missing responses", path, method)
		}
		return responses
	}

	requireCode := func(t *testing.T, path, method, code string) {
		t.Helper()
		if _, ok := responsesOf(t, path, method)[code]; !ok {
			t.Errorf("%s %s: missing %s response", path, method, code)
		}
	}

	schema200 := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		responses := responsesOf(t, path, method)
		okResp, ok := responses["200"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s: missing 200 response", path, method)
		}
		content := okResp["content"].(map[string]any)
		body := content["application/json"].(map[string]any)
		schema, ok := body["schema"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s: missing 200 schema", path, method)
		}
		return schema
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

	strOf := func(t *testing.T, node map[string]any, key, where string) string {
		t.Helper()
		child, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing property %q", where, key)
		}
		s, _ := child["description"].(string)
		return s
	}

	requireDescContains := func(t *testing.T, props map[string]any, key string, needles ...string) {
		t.Helper()
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("missing property %q", key)
			return
		}
		desc, _ := node["description"].(string)
		lower := strings.ToLower(desc)
		for _, needle := range needles {
			if !strings.Contains(lower, strings.ToLower(needle)) {
				t.Errorf("property %q description missing %q (got %q)", key, needle, desc)
			}
		}
	}

	requiredOf := func(t *testing.T, node map[string]any, where string) []string {
		t.Helper()
		raw, ok := node["required"].([]any)
		if !ok {
			t.Fatalf("%s: missing required array", where)
		}
		var out []string
		for _, v := range raw {
			s, _ := v.(string)
			out = append(out, s)
		}
		return out
	}

	requireRequiredHas := func(t *testing.T, node map[string]any, where string, keys ...string) {
		t.Helper()
		have := map[string]bool{}
		for _, k := range requiredOf(t, node, where) {
			have[k] = true
		}
		for _, k := range keys {
			if !have[k] {
				t.Errorf("%s: required missing %q (have %v)", where, k, have)
			}
		}
	}

	requireRequiredLacks := func(t *testing.T, node map[string]any, where string, keys ...string) {
		t.Helper()
		have := map[string]bool{}
		for _, k := range requiredOf(t, node, where) {
			have[k] = true
		}
		for _, k := range keys {
			if have[k] {
				t.Errorf("%s: required must not list conditional key %q", where, k)
			}
		}
	}

	t.Run("iac finding kinds six", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{"/api/v0/iac/unmanaged-resources", "/api/v0/iac/terraform-import-plan/candidates"} {
			op := opOf(t, path, "post")
			body := op["requestBody"].(map[string]any)
			content := body["content"].(map[string]any)
			payload := content["application/json"].(map[string]any)
			schema := payload["schema"].(map[string]any)
			props := propsOf(t, schema, path+" request")
			desc := strOf(t, props, "finding_kinds", path+" request")
			for _, kind := range []string{"orphaned_cloud_resource", "unmanaged_cloud_resource", "unknown_cloud_resource", "ambiguous_cloud_resource", "image_version_drift", "value_comparison_inconclusive"} {
				if !strings.Contains(desc, kind) {
					t.Errorf("%s: finding_kinds description missing %q", path, kind)
				}
			}
		}
	})

	t.Run("cloud drift required keys", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/cloud/runtime-drift/findings"
		top := propsOf(t, schema200(t, "/api/v0/cloud/runtime-drift/findings", "post"), where)
		items := itemsOf(t, top["drift_findings"].(map[string]any), where+" drift_findings")
		requireRequiredHas(t, items, where+" drift_findings[]", "generation_id", "cloud_resource_uid")
	})

	t.Run("ssm 500", func(t *testing.T) {
		t.Parallel()
		requireCode(t, "/api/v0/cloud/ssm-host-inventory", "get", "500")
	})

	t.Run("aws findings conditional keys", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/aws/runtime-drift/findings"
		top := propsOf(t, schema200(t, "/api/v0/aws/runtime-drift/findings", "post"), where)
		items := itemsOf(t, top["drift_findings"].(map[string]any), where+" drift_findings")
		fprops := propsOf(t, items, where+" drift_findings[]")
		for _, key := range []string{"resource_type", "resource_id", "tags", "candidate_id", "matched_terraform_state_address", "matched_terraform_config_file", "matched_terraform_module_path", "matched_other_iac_source"} {
			if _, ok := fprops[key].(map[string]any); !ok {
				t.Errorf("%s: drift_findings[] missing conditional property %q", where, key)
			}
		}
	})

	t.Run("infra error codes", func(t *testing.T) {
		t.Parallel()
		requireCode(t, "/api/v0/infra/resources/search", "post", "501")
		requireCode(t, "/api/v0/infra/relationships", "post", "501")
		requireCode(t, "/api/v0/ecosystem/overview", "get", "501")
		requireCode(t, "/api/v0/ecosystem/graph-summary", "post", "404")
		requireCode(t, "/api/v0/ecosystem/graph-summary", "post", "422")
		for _, path := range []string{"/api/v0/infra/resources/count", "/api/v0/infra/resources/inventory"} {
			requireCode(t, path, "get", "400")
			requireCode(t, path, "get", "501")
		}
	})

	t.Run("k8s and observability anchor prose", func(t *testing.T) {
		t.Parallel()
		k8s := opOf(t, "/api/v0/kubernetes/correlations", "get")
		desc, _ := k8s["description"].(string)
		if !strings.Contains(desc, "is required") && !strings.Contains(desc, "anchor") {
			t.Errorf("k8s description does not state the anchor-required 400 rule")
		}
		obs := opOf(t, "/api/v0/observability/coverage/correlations", "get")
		odesc, _ := obs["description"].(string)
		if !strings.Contains(odesc, "is required") && !strings.Contains(odesc, "anchor") {
			t.Errorf("observability description does not state the anchor-required 400 rule")
		}
	})

	t.Run("repo tree size note", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/tree"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/tree", "get"), where)
		items := itemsOf(t, top["entries"].(map[string]any), where+" entries")
		fprops := propsOf(t, items, where+" entries[]")
		requireDescContains(t, fprops, "size", "omit")
	})

	t.Run("repo branches fallback and 409s", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/branches"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/branches", "get"), where)
		branchItems := itemsOf(t, top["branches"].(map[string]any), where+" branches")
		requireDescContains(t, propsOf(t, branchItems, where+" branches[]"), "name", "fallback")
		for _, path := range []string{
			"/api/v0/repositories/{repo_id}/branches",
			"/api/v0/repositories/{repo_id}/freshness",
			"/api/v0/repositories/{repo_id}/stats",
			"/api/v0/repositories/{repo_id}/coverage",
			"/api/v0/repositories/{repo_id}/context",
			"/api/v0/repositories/{repo_id}/story",
		} {
			requireCode(t, path, "get", "409")
		}
	})

	t.Run("repo coverage summary required", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/repositories/{repo_id}/coverage"
		top := propsOf(t, schema200(t, "/api/v0/repositories/{repo_id}/coverage", "get"), where)
		summary := top["summary"].(map[string]any)
		sprops := propsOf(t, summary, where+" summary")
		requireRequiredHas(t, summary, where+" summary",
			"graph_file_count", "graph_entity_count", "content_file_count", "content_entity_count")
		requireRequiredLacks(t, summary, where+" summary",
			"graph_gap_count", "content_gap_count", "completeness_state")
		requireDescContains(t, sprops, "graph_gap_count", "available")
	})

	t.Run("repo context story 501", func(t *testing.T) {
		t.Parallel()
		requireCode(t, "/api/v0/repositories/{repo_id}/context", "get", "501")
		requireCode(t, "/api/v0/repositories/{repo_id}/story", "get", "501")
	})

	t.Run("package registry aggregates errors", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{"/api/v0/package-registry/packages/count", "/api/v0/package-registry/packages/inventory"} {
			requireCode(t, path, "get", "400")
			requireCode(t, path, "get", "501")
			requireCode(t, path, "get", "500")
		}
	})

	t.Run("advisories absent notes and 500", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/supply-chain/advisories"
		top := propsOf(t, schema200(t, "/api/v0/supply-chain/advisories", "get"), where)
		items := itemsOf(t, top["advisories"].(map[string]any), where+" advisories")
		aprops := propsOf(t, items, where+" advisories[]")
		requireDescContains(t, aprops, "severity_label", "absent")
		requireDescContains(t, aprops, "cvss_score", "absent")
		requireCode(t, "/api/v0/supply-chain/advisories", "get", "500")
	})

	t.Run("advisory evidence and vuln errors", func(t *testing.T) {
		t.Parallel()
		requireCode(t, "/api/v0/supply-chain/advisories/evidence", "get", "501")
		requireCode(t, "/api/v0/supply-chain/advisories/evidence", "get", "500")
		requireCode(t, "/api/v0/supply-chain/vulnerabilities/{advisory_id}", "get", "500")
	})

	t.Run("container identities anchor and errors", func(t *testing.T) {
		t.Parallel()
		op := opOf(t, "/api/v0/supply-chain/container-images/identities", "get")
		desc, _ := op["description"].(string)
		if desc == "" {
			t.Errorf("container identities route needs a description stating the anchor-required 400 rule")
		} else if !strings.Contains(desc, "is required") && !strings.Contains(desc, "anchor") {
			t.Errorf("container identities description does not state the anchor-required 400 rule")
		}
		for _, path := range []string{
			"/api/v0/supply-chain/container-images/identities",
			"/api/v0/supply-chain/container-images/identities/count",
			"/api/v0/supply-chain/container-images/identities/inventory",
		} {
			requireCode(t, path, "get", "400")
			requireCode(t, path, "get", "501")
			requireCode(t, path, "get", "500")
		}
	})

	t.Run("impact findings anchor and trio errors", func(t *testing.T) {
		t.Parallel()
		op := opOf(t, "/api/v0/supply-chain/impact/findings", "get")
		desc, _ := op["description"].(string)
		if !strings.Contains(desc, "is required") && !strings.Contains(desc, "anchor") {
			t.Errorf("impact findings description does not state the anchor-required 400 rule")
		}
		for _, path := range []string{
			"/api/v0/supply-chain/impact/findings",
			"/api/v0/supply-chain/impact/findings/count",
			"/api/v0/supply-chain/impact/inventory",
		} {
			requireCode(t, path, "get", "400")
			requireCode(t, path, "get", "501")
			requireCode(t, path, "get", "500")
		}
	})

	t.Run("impact explain anchor and errors", func(t *testing.T) {
		t.Parallel()
		op := opOf(t, "/api/v0/supply-chain/impact/explain", "get")
		desc, _ := op["description"].(string)
		if !strings.Contains(desc, "is required") && !strings.Contains(desc, "anchor") {
			t.Errorf("impact explain description does not state the anchor-required 400 rule")
		}
		requireCode(t, "/api/v0/supply-chain/impact/explain", "get", "400")
		requireCode(t, "/api/v0/supply-chain/impact/explain", "get", "501")
		requireCode(t, "/api/v0/supply-chain/impact/explain", "get", "500")
	})

	t.Run("security and sbom aggregate errors", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{
			"/api/v0/supply-chain/security-alerts/reconciliations",
			"/api/v0/supply-chain/security-alerts/reconciliations/count",
			"/api/v0/supply-chain/security-alerts/reconciliations/inventory",
			"/api/v0/supply-chain/sbom-attestations/attachments",
			"/api/v0/supply-chain/sbom-attestations/attachments/count",
			"/api/v0/supply-chain/sbom-attestations/attachments/inventory",
		} {
			requireCode(t, path, "get", "400")
			requireCode(t, path, "get", "501")
			requireCode(t, path, "get", "500")
		}
		op := opOf(t, "/api/v0/supply-chain/sbom-attestations/attachments", "get")
		desc, _ := op["description"].(string)
		if desc == "" {
			t.Errorf("sbom attachments route needs a description stating the anchor-required 400 rule")
		} else if !strings.Contains(desc, "is required") && !strings.Contains(desc, "anchor") {
			t.Errorf("sbom attachments description does not state the anchor-required 400 rule")
		}
	})

	t.Run("scanner contract 400", func(t *testing.T) {
		t.Parallel()
		requireCode(t, "/api/v0/supply-chain/vulnerability-scanner/contract", "get", "400")
	})

	t.Run("tag history conditional note", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/images/tag-history"
		top := propsOf(t, schema200(t, "/api/v0/images/tag-history", "get"), where)
		items := itemsOf(t, top["tag_history"].(map[string]any), where+" tag_history")
		tprops := propsOf(t, items, where+" tag_history[]")
		requireDescContains(t, tprops, "first_observed_at", "absent")
	})
}
