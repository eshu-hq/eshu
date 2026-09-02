// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesAdmissionDecisions(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/evidence/admission-decisions")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listAdmissionDecisions"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters := get["parameters"].([]any)
	for _, name := range []string{"domain", "scope_id", "generation_id", "state", "anchor_kind", "anchor_id"} {
		if !openAPIParametersIncludeName(parameters, name) {
			t.Fatalf("parameters missing %q: %#v", name, parameters)
		}
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	if _, ok := responses["501"]; !ok {
		t.Fatalf("responses missing 501 unsupported-capability response: %#v", responses)
	}
	schema := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(
			t,
			querytestutil.MustMapField(
				t,
				querytestutil.MustMapField(t, responses["200"].(map[string]any), "content"),
				"application/json",
			),
			"schema",
		),
		"properties",
	)
	decisions := querytestutil.MustMapField(t, schema, "decisions")
	items := querytestutil.MustMapField(t, decisions, "items")
	itemProps := querytestutil.MustMapField(t, items, "properties")
	for _, name := range []string{"evidence", "evidence_limit", "evidence_truncated"} {
		if _, present := itemProps[name]; !present {
			t.Fatalf("admission decision item schema missing %q: %#v", name, itemProps)
		}
	}
}

func openAPIParametersIncludeName(parameters []any, name string) bool {
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if ok && parameterMap["name"] == name {
			return true
		}
	}
	return false
}
