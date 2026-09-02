// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesHardcodedSecretInvestigation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/code/security/secrets/investigate")
	post := querytestutil.MustMapField(t, path, "post")
	body := querytestutil.MustMapField(t, post, "requestBody")
	content := querytestutil.MustMapField(t, body, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	if _, ok := properties["finding_kinds"]; !ok {
		t.Fatal("hardcoded secret request schema missing finding_kinds")
	}
	if _, ok := properties["include_suppressed"]; !ok {
		t.Fatal("hardcoded secret request schema missing include_suppressed")
	}
}
