// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestAccSearchResponseCodes pins the audited (#acc-evidence) response-code
// contract for the search fragments. Each set was diffed against the handler
// emit:
//
//   - search/semantic: 403 permission gates, 501 capability gate
//     (local_lightweight unsupported), 400 request validation, 200, and the
//     read-path 404/409/503/500 behind the retrieval seam. No 504: the read
//     path is PG-reader/index-only and ErrGraphReadDeadline is produced only
//     by graph reads.
//   - ask: 403 scoped-denial, 400 invalid argument, 401 shared-auth, 200,
//     503 disabled/unconfigured. Unknown format values never 400:
//     render.DetectFormat falls through to question inference.
//   - services/{service_name}/story: the BuildServiceStoryEnvelope seam
//     writes 501 (capability), 400 (service_name/evidence_detail/
//     downstream_read_limit), 404, 409, 503 (incl. the content-indexes
//     503), 504 graph deadline, 500, 200, plus 403 via the scoped-token
//     gate. The doc already carried 503/504; 501 was the gap.
func TestAccSearchResponseCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fragment string
		where    string
		path     string
		method   string
		want     []string
	}{
		{
			Semantic, "Semantic", "/api/v0/search/semantic", "post",
			[]string{"200", "400", "403", "404", "409", "500", "501", "503"},
		},
		{
			Ask, "Ask", "/api/v0/ask", "post",
			[]string{"200", "400", "401", "403", "503"},
		},
		{
			ServiceStory, "ServiceStory", "/api/v0/services/{service_name}/story", "get",
			[]string{"200", "400", "403", "404", "409", "500", "501", "503", "504"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			doc := accSearchParse(t, tc.fragment, tc.where)
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

// TestAccSearchAskFormatFallback pins the audited ask P3: the format enum is
// not a closed validation set, so the doc must say an unknown value falls
// back to question inference instead of reading as a 400.
func TestAccSearchAskFormatFallback(t *testing.T) {
	t.Parallel()

	doc := accSearchParse(t, Ask, "Ask")
	route := doc["/api/v0/ask"].(map[string]any)
	op := route["post"].(map[string]any)
	reqBody := op["requestBody"].(map[string]any)
	content := reqBody["content"].(map[string]any)
	jsonBody := content["application/json"].(map[string]any)
	reqSchema := jsonBody["schema"].(map[string]any)
	props := reqSchema["properties"].(map[string]any)
	format, ok := props["format"].(map[string]any)
	if !ok {
		t.Fatal("ask: request schema has no format property")
	}
	desc, _ := format["description"].(string)
	if !strings.Contains(strings.ToLower(desc), "falls back") {
		t.Fatalf("ask: format description must document the unknown-value fallback, got %q", desc)
	}
}

// TestAccSearchSemanticBody pins the audited semantic body: the response
// names the keys semanticsearch.semanticSearchResponseFromRetrieval emits,
// the rerank block is absent (not "disabled") unless rerank was requested so
// the state enum never carries "disabled", and ranking basis survives only
// under rerank.
func TestAccSearchSemanticBody(t *testing.T) {
	t.Parallel()

	doc := accSearchParse(t, Semantic, "Semantic")
	route := doc["/api/v0/search/semantic"].(map[string]any)
	op := route["post"].(map[string]any)
	responses := op["responses"].(map[string]any)
	okResp := responses["200"].(map[string]any)
	content := okResp["content"].(map[string]any)
	jsonBody := content["application/json"].(map[string]any)
	schema := jsonBody["schema"].(map[string]any)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("search/semantic 200: schema has no properties")
	}
	for _, key := range []string{
		"query", "repo_id", "anchor", "mode", "search_mode", "limit",
		"timeout_ms", "results", "truncated", "retrieval_state",
		"corpus_state", "facets", "rerank", "recommended_next_calls",
	} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("search/semantic 200: missing property %q", key)
		}
	}
	rerank, ok := props["rerank"].(map[string]any)
	if !ok {
		t.Fatal("search/semantic 200: missing rerank block")
	}
	rerankProps, ok := rerank["properties"].(map[string]any)
	if !ok {
		t.Fatal("search/semantic 200 rerank: no properties")
	}
	state, ok := rerankProps["state"].(map[string]any)
	if !ok {
		t.Fatal("search/semantic 200 rerank: no state")
	}
	var enumVals []string
	for _, v := range state["enum"].([]any) {
		s, _ := v.(string)
		enumVals = append(enumVals, s)
	}
	sort.Strings(enumVals)
	if strings.Join(enumVals, ",") != "applied,inactive,stale_skipped" {
		t.Fatalf("search/semantic 200 rerank.state enum = [%s], want [applied,inactive,stale_skipped]",
			strings.Join(enumVals, ","))
	}
}

// TestAccSearchStoryParams pins the audited story query surface: the handler
// reads exactly service_id, repo, environment, evidence_detail, and
// downstream_read_limit (plus the path service_name), so the doc lists no
// more and no fewer.
func TestAccSearchStoryParams(t *testing.T) {
	t.Parallel()

	doc := accSearchParse(t, ServiceStory, "ServiceStory")
	route := doc["/api/v0/services/{service_name}/story"].(map[string]any)
	op := route["get"].(map[string]any)
	raw, ok := op["parameters"].([]any)
	if !ok {
		t.Fatal("story: missing parameters")
	}
	names := map[string]bool{}
	for _, p := range raw {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := m["$ref"].(string); ok {
			names[ref] = true
			continue
		}
		name, _ := m["name"].(string)
		names[name] = true
	}
	for _, want := range []string{
		"#/components/parameters/ServiceName",
		"service_id", "repo", "environment", "evidence_detail", "downstream_read_limit",
	} {
		if !names[want] {
			t.Errorf("story: missing param %q (got %v)", want, names)
		}
	}
	if len(names) != 6 {
		t.Errorf("story: want exactly 6 params, got %v", names)
	}
}

// TestAccSearchSemanticEvidenceParams pins the audited semantic-evidence
// filter surface: both routes share one reader
// (query.semanticEvidenceRequestFilter) and one SQL builder
// (query.buildSemanticEvidenceSQL applies every non-empty filter field on
// both fact kinds), so every honored filter is documented on both routes.
func TestAccSearchSemanticEvidenceParams(t *testing.T) {
	t.Parallel()

	doc := accSearchParse(t, SemanticEvidence, "SemanticEvidence")
	namesOf := func(path string) map[string]bool {
		t.Helper()
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("SemanticEvidence: fragment paths missing %s", path)
		}
		op := route["get"].(map[string]any)
		raw, ok := op["parameters"].([]any)
		if !ok {
			t.Fatalf("%s: missing parameters", path)
		}
		out := map[string]bool{}
		for _, p := range raw {
			m, _ := p.(map[string]any)
			name, _ := m["name"].(string)
			out[name] = true
		}
		return out
	}

	obs := namesOf("/api/v0/semantic/documentation-observations")
	for _, want := range []string{
		"fact_id", "scope_id", "generation_id", "repo", "target_kind",
		"target_id", "service_id", "source_class", "source_id", "document_id",
		"section_id", "relative_path", "entity_id", "provider_profile_id",
		"provider_kind", "prompt_version", "redaction_version",
		"extraction_mode", "policy_state", "redaction_state", "freshness_state",
		"admission_state", "corroboration_state", "observation_type",
		"hint_type", "relationship_kind", "q", "updated_since", "limit", "cursor",
	} {
		if !obs[want] {
			t.Errorf("documentation-observations: missing honored param %q", want)
		}
	}

	hints := namesOf("/api/v0/semantic/code-hints")
	for _, want := range []string{
		"fact_id", "scope_id", "generation_id", "repo", "target_kind",
		"target_id", "service_id", "source_class", "source_id",
		"document_id", "section_id", "relative_path", "entity_id",
		"provider_profile_id", "provider_kind", "prompt_version",
		"redaction_version", "extraction_mode", "policy_state",
		"redaction_state", "freshness_state", "admission_state",
		"corroboration_state", "observation_type", "hint_type",
		"relationship_kind", "q", "updated_since", "limit", "cursor",
	} {
		if !hints[want] {
			t.Errorf("code-hints: missing honored param %q", want)
		}
	}
}

// TestAccSearchSemanticEvidenceErrors pins the audited error sets: 400
// (bad limit/cursor/updated_since/no filter), 403 scoped gate, 500/503 via
// the read failure (PG-reader-only, so no 504 arm), 501 capability + nil
// read model.
func TestAccSearchSemanticEvidenceErrors(t *testing.T) {
	t.Parallel()

	doc := accSearchParse(t, SemanticEvidence, "SemanticEvidence")
	for _, path := range []string{
		"/api/v0/semantic/documentation-observations",
		"/api/v0/semantic/code-hints",
	} {
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("SemanticEvidence: fragment paths missing %s", path)
		}
		op := route["get"].(map[string]any)
		responses, ok := op["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing responses", path)
		}
		var got []string
		for code := range responses {
			got = append(got, code)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != "200,400,403,500,501,503" {
			t.Errorf("%s: responses = [%s], want [200,400,403,500,501,503]",
				path, strings.Join(got, ","))
		}
	}
}

func accSearchParse(t *testing.T, raw, where string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
	}
	return doc
}
