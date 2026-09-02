// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestOpenAPISpecIncludesLiveEvidenceBundle proves GET /api/v0/evidence/bundle
// (#4045) is documented in the served OpenAPI spec, and that it deliberately
// carries neither tenant-scope marker: the bundle is stack-wide, the same
// posture as its two stack-wide source routes GET /api/v0/status/index and
// GET /api/v0/status/pipeline.
func TestOpenAPISpecIncludesLiveEvidenceBundle(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/evidence/bundle")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "getLiveEvidenceBundle"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	if _, marked := get["x-scoped-token-support"]; marked {
		t.Fatal(`get carries "x-scoped-token-support", but the bundle is stack-wide and must not advertise scoped-token support`)
	}
	if _, marked := get["x-browser-session-only"]; marked {
		t.Fatal(`get carries "x-browser-session-only", but the bundle is not a browser-session identity route`)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	schema := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(
			t,
			querytestutil.MustMapField(t, responses["200"].(map[string]any), "content"),
			"application/json",
		),
		"schema",
	)
	properties := querytestutil.MustMapField(t, schema, "properties")
	for _, name := range []string{"schema_version", "bundle_id", "identity", "contents", "reproduce", "validation"} {
		if _, present := properties[name]; !present {
			t.Fatalf("live evidence bundle schema missing %q: %#v", name, properties)
		}
	}
}
