// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestAccEvidenceResponseCodes pins the audited (#acc-evidence) response-code
// contract for every route in the evidence fragments. Each set was diffed
// against the handler emit (not just doc self-consistency):
//
//   - relationships/{resolved_id}: handler writes 400 (blank id),
//     501 (no Postgres read model), 404, 503/500 via writeReadFailure, 200,
//     403 via the scoped-token gate. No 504: the read path is PG-reader-only
//     and ErrGraphReadDeadline (the only 504 arm in WriteGraphReadError) is
//     produced only by graph reads.
//   - admission-decisions: 501 capability gate + 503 nil-store, 400 filter
//     validation, 200 (incl. the empty-grant page), 503/500 via
//     writeReadFailure. Same PG-only no-504 reasoning.
//   - citations: 501 nil content store, 400 bad JSON/handles, 503/500 via
//     writeReadFailure, 200. Same PG-only no-504 reasoning.
//   - bundle: 503 nil status reader, 503/500 via WriteGraphReadError +
//     ServerFailure, 200. Neo4j failures degrade to repository_count 0, so
//     no second error path; no 504 for the same PG-only reason.
//   - deployable-unit packet: 501 capability, 400 scope/generation, 503 nil
//     responder/store, 503/500 via writeReadFailure, 200. No selector
//     resolution runs, so no 404/409; PG-only, so no 504.
//   - drift packet: 501 capability/nil store, 400 normalize, 503/500 via
//     writeReadbackFailure, 200. No selector; PG-only, so no 504.
//   - supply-chain impact packet: 501 capability, 400 unbounded scope,
//     503 nil responder/store, 503/500 via WriteGraphReadError +
//     writeStageFailure, 200 (not-found and ambiguous scopes answer as 200
//     packets). 404/409 come from resolveSupplyChainImpactRepositorySelector
//     (selector.ResolveForRequestWithAccess) when repository_id names an
//     unknown or ambiguous repository; 504 stays because that selector path
//     is graph-backed.
//   - investigations services/{service_name}: 501 capability gate and 409
//     ambiguity from entity.InvestigateService, plus the 503/504 graph-read
//     arms the route already documented.
func TestAccEvidenceResponseCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fragment string
		where    string
		path     string
		method   string
		want     []string
	}{
		{
			Routes, "Routes", "/api/v0/evidence/relationships/{resolved_id}", "get",
			[]string{"200", "400", "403", "404", "500", "501", "503"},
		},
		{
			Routes, "Routes", "/api/v0/evidence/admission-decisions", "get",
			[]string{"200", "400", "403", "500", "501", "503"},
		},
		{
			Routes, "Routes", "/api/v0/evidence/citations", "post",
			[]string{"200", "400", "403", "500", "501", "503"},
		},
		{
			Bundle, "Bundle", "/api/v0/evidence/bundle", "get",
			[]string{"200", "500", "503"},
		},
		{
			Investigations, "Investigations", "/api/v0/investigations/deployable-unit/packet", "get",
			[]string{"200", "400", "403", "500", "501", "503"},
		},
		{
			Investigations, "Investigations", "/api/v0/investigations/drift/packet", "get",
			[]string{"200", "400", "403", "500", "501", "503"},
		},
		{
			Investigations, "Investigations", "/api/v0/investigations/supply-chain/impact/packet", "get",
			[]string{"200", "400", "403", "404", "409", "500", "501", "503", "504"},
		},
		{
			Investigations, "Investigations", "/api/v0/investigations/services/{service_name}", "get",
			[]string{"200", "400", "403", "404", "409", "500", "501", "503", "504"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			doc := accParseFragment(t, tc.fragment, tc.where)
			route, ok := doc[tc.path].(map[string]any)
			if !ok {
				t.Fatalf("%s: fragment paths missing %s", tc.where, tc.path)
			}
			op, ok := route[tc.method].(map[string]any)
			if !ok {
				t.Fatalf("%s %s: missing %s op", tc.where, tc.path, tc.method)
			}
			responses, ok := op["responses"].(map[string]any)
			if !ok {
				t.Fatalf("%s %s: missing responses", tc.where, tc.path)
			}
			var got []string
			for code := range responses {
				got = append(got, code)
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s %s %s: responses = [%s], want [%s]",
					tc.where, tc.path, tc.method, strings.Join(got, ","), strings.Join(want, ","))
			}
		})
	}
}

// TestAccEvidenceRelationshipDrilldownShape pins the audited drilldown body:
// every key scanRelationshipEvidenceByResolvedID always sets is required,
// conditional keys stay optional with their presence condition documented.
func TestAccEvidenceRelationshipDrilldownShape(t *testing.T) {
	t.Parallel()

	doc := accParseFragment(t, Routes, "Routes")
	schema := accResponseSchema(t, doc, "/api/v0/evidence/relationships/{resolved_id}", "get")
	props := accProps(t, schema, "relationships drilldown 200")

	for _, key := range []string{
		"lookup_basis", "resolved_id", "generation_id", "source", "target",
		"relationship_type", "confidence", "evidence_count",
		"rationale", "resolution_source", "generation", "details",
		"evidence_preview", "postgres_lookup_id",
	} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("relationships drilldown 200: missing property %q", key)
		}
	}
	accRequireKeys(t, schema, "relationships drilldown 200",
		"lookup_basis", "resolved_id", "generation_id", "source", "target",
		"relationship_type", "confidence", "evidence_count",
		"rationale", "resolution_source", "generation", "details",
		"evidence_preview", "postgres_lookup_id")

	// Conditional keys keep their presence conditions.
	for _, key := range []string{"evidence_kinds", "evidence_type"} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("relationships drilldown 200: missing conditional property %q", key)
		}
	}

	params := accParams(t, doc, "/api/v0/evidence/relationships/{resolved_id}", "get")
	if len(params) != 1 {
		t.Fatalf("relationships drilldown: want exactly 1 param (resolved_id), got %d", len(params))
	}
	if params[0]["name"] != "resolved_id" || params[0]["in"] != "path" || params[0]["required"] != true {
		t.Fatalf("relationships drilldown: resolved_id must be a required path param, got %v", params[0])
	}
}

// TestAccEvidenceAdmissionDecisionsContract pins the audited admission-decision
// query surface: the three required filters, the eight-state enum the handler
// validates, the limit clamp bounds, and the page envelope keys.
func TestAccEvidenceAdmissionDecisionsContract(t *testing.T) {
	t.Parallel()

	doc := accParseFragment(t, Routes, "Routes")
	byName := map[string]map[string]any{}
	for _, p := range accParams(t, doc, "/api/v0/evidence/admission-decisions", "get") {
		name, _ := p["name"].(string)
		byName[name] = p
	}
	for _, name := range []string{"domain", "scope_id", "generation_id"} {
		p, ok := byName[name]
		if !ok {
			t.Fatalf("admission-decisions: missing param %q", name)
		}
		if p["required"] != true {
			t.Errorf("admission-decisions: param %q must be required", name)
		}
	}
	state, ok := byName["state"]
	if !ok {
		t.Fatalf("admission-decisions: missing param state")
	}
	accRequireEnum(t, state, "admission-decisions state",
		"admitted", "rejected", "ambiguous", "stale", "missing_evidence",
		"permission_hidden", "unsupported", "unsafe")

	schema := accResponseSchema(t, doc, "/api/v0/evidence/admission-decisions", "get")
	accRequireKeys(t, schema, "admission-decisions 200",
		"decisions", "count", "limit", "truncated", "recommended_next_calls")
}

// TestAccEvidenceCitationShape pins the audited citation body: rank is always
// emitted (non-omitempty int on evidence.EvidenceCitation), so it joins the
// item required list; the coverage block names all seven keys.
func TestAccEvidenceCitationShape(t *testing.T) {
	t.Parallel()

	doc := accParseFragment(t, Routes, "Routes")
	schema := accResponseSchema(t, doc, "/api/v0/evidence/citations", "post")
	props := accProps(t, schema, "citations 200")
	accRequireKeys(t, schema, "citations 200",
		"citations", "missing_handles", "coverage", "recommended_next_calls")

	item := accItems(t, props, "citations 200", "citations")
	accRequireKeys(t, item, "citations 200 citations[]",
		"citation_id", "rank", "kind", "evidence_family", "confidence", "provenance", "excerpt")
	accRequireKeys(t, accChild(t, accProps(t, item, "citations[]"), "provenance", "citations[]"),
		"citations 200 citations[] provenance", "basis")

	coverage := accChild(t, props, "coverage", "citations 200")
	accRequireKeys(t, coverage, "citations 200 coverage",
		"query_shape", "input_handle_count", "resolved_count", "missing_count",
		"limit", "truncated", "source_backend")
}

// TestAccEvidenceBundleShape pins the audited bundle body: the always-emitted
// top-level keys (evidencebundle.Bundle has no omitempty outside
// active_work_source) are required.
func TestAccEvidenceBundleShape(t *testing.T) {
	t.Parallel()

	doc := accParseFragment(t, Bundle, "Bundle")
	schema := accResponseSchema(t, doc, "/api/v0/evidence/bundle", "get")
	accRequireKeys(t, schema, "bundle 200",
		"schema_version", "bundle_id", "identity", "source", "redaction",
		"contents", "missing_evidence", "reproduce", "bounds", "validation")
}

// TestAccEvidenceVizDerive pins the audited derive contract: source_response
// is optional (an absent body derives the unsupported packet, 200), so only
// view is required; the only failure is 400.
func TestAccEvidenceVizDerive(t *testing.T) {
	t.Parallel()

	doc := accParseFragment(t, VisualizationPackets, "VisualizationPackets")
	route, ok := doc["/api/v0/visualizations/derive"].(map[string]any)
	if !ok {
		t.Fatal("VisualizationPackets: fragment paths missing /api/v0/visualizations/derive")
	}
	op, ok := route["post"].(map[string]any)
	if !ok {
		t.Fatal("VisualizationPackets /api/v0/visualizations/derive: missing post op")
	}
	body, _ := op["requestBody"].(map[string]any)
	content, _ := body["content"].(map[string]any)
	jsonBody, _ := content["application/json"].(map[string]any)
	reqSchema, _ := jsonBody["schema"].(map[string]any)
	required, _ := reqSchema["required"].([]any)
	if len(required) != 1 || required[0] != "view" {
		t.Fatalf("visualizations/derive: required = %v, want [view] (source_response is optional)", required)
	}
	props, _ := reqSchema["properties"].(map[string]any)
	if _, ok := props["source_response"].(map[string]any); !ok {
		t.Fatal("visualizations/derive: request schema must still document source_response")
	}
	responses, _ := op["responses"].(map[string]any)
	var got []string
	for code := range responses {
		got = append(got, code)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "200,400" {
		t.Fatalf("visualizations/derive: responses = [%s], want [200,400]", strings.Join(got, ","))
	}
}

func accParseFragment(t *testing.T, raw, where string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
	}
	return doc
}

func accChild(t *testing.T, node map[string]any, key, where string) map[string]any {
	t.Helper()
	next, ok := node[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing or non-object %q", where, key)
	}
	return next
}

func accProps(t *testing.T, node map[string]any, where string) map[string]any {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: object schema has no properties", where)
	}
	return props
}

func accItems(t *testing.T, props map[string]any, where, key string) map[string]any {
	t.Helper()
	arr, ok := props[key].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing property %q", where, key)
	}
	items, ok := arr["items"].(map[string]any)
	if !ok {
		t.Fatalf("%s: %q has no items", where, key)
	}
	return items
}

func accParams(t *testing.T, doc map[string]any, path, method string) []map[string]any {
	t.Helper()
	route, ok := doc[path].(map[string]any)
	if !ok {
		t.Fatalf("fragment paths missing %s", path)
	}
	op, ok := route[method].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing %s op", path, method)
	}
	raw, ok := op["parameters"].([]any)
	if !ok {
		t.Fatalf("%s %s: missing parameters", path, method)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, p := range raw {
		m, ok := p.(map[string]any)
		if !ok {
			t.Fatalf("%s %s: parameter is not an object", path, method)
		}
		out = append(out, m)
	}
	return out
}

func accResponseSchema(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	route, ok := doc[path].(map[string]any)
	if !ok {
		t.Fatalf("fragment paths missing %s", path)
	}
	op, ok := route[method].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing %s op", path, method)
	}
	responses := accChild(t, op, "responses", path+" "+method)
	okResp := accChild(t, responses, "200", path+" "+method)
	content, ok := okResp["content"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s 200 response has no content", path, method)
	}
	jsonBody := accChild(t, content, "application/json", path+" "+method)
	return accChild(t, jsonBody, "schema", path+" "+method)
}

func accRequireKeys(t *testing.T, node map[string]any, where string, keys ...string) {
	t.Helper()
	raw, ok := node["required"].([]any)
	if !ok {
		t.Fatalf("%s: schema has no required list", where)
	}
	have := map[string]bool{}
	for _, r := range raw {
		if s, ok := r.(string); ok {
			have[s] = true
		}
	}
	for _, key := range keys {
		if !have[key] {
			t.Errorf("%s: required list missing %q (got %v)", where, key, raw)
		}
	}
}

func accRequireEnum(t *testing.T, node map[string]any, where string, values ...string) {
	t.Helper()
	schema, ok := node["schema"].(map[string]any)
	if !ok {
		t.Fatalf("%s: param has no schema", where)
	}
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatalf("%s: schema has no enum", where)
	}
	have := map[string]bool{}
	for _, r := range raw {
		if s, ok := r.(string); ok {
			have[s] = true
		}
	}
	for _, v := range values {
		if !have[v] {
			t.Errorf("%s: enum missing %q (got %v)", where, v, raw)
		}
	}
}
