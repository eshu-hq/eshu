// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIIaCManagementSafetyGateFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")

	unmanagedPath := testutil.MustMapField(t, paths, "/api/v0/iac/unmanaged-resources")
	unmanagedPost := testutil.MustMapField(t, unmanagedPath, "post")
	unmanagedOK := testutil.MustMapField(t, testutil.MustMapField(t, unmanagedPost, "responses"), "200")
	unmanagedProps := testutil.MustMapField(
		t,
		testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, unmanagedOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := unmanagedProps["safety_summary"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing safety_summary")
	}
	findings := testutil.MustMapField(t, unmanagedProps, "findings")
	findingProps := testutil.MustMapField(t, testutil.MustMapField(t, findings, "items"), "properties")
	if _, ok := findingProps["safety_gate"]; !ok {
		t.Fatal("iac/unmanaged-resources finding schema missing safety_gate")
	}

	statusPath := testutil.MustMapField(t, paths, "/api/v0/iac/management-status")
	statusPost := testutil.MustMapField(t, statusPath, "post")
	statusOK := testutil.MustMapField(t, testutil.MustMapField(t, statusPost, "responses"), "200")
	statusProps := testutil.MustMapField(
		t,
		testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, statusOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := statusProps["safety_gate"]; !ok {
		t.Fatal("iac/management-status response schema missing safety_gate")
	}

	explainPath := testutil.MustMapField(t, paths, "/api/v0/iac/management-status/explain")
	explainPost := testutil.MustMapField(t, explainPath, "post")
	explainOK := testutil.MustMapField(t, testutil.MustMapField(t, explainPost, "responses"), "200")
	explainProps := testutil.MustMapField(
		t,
		testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, explainOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := explainProps["safety_gate"]; !ok {
		t.Fatal("iac/management-status/explain response schema missing safety_gate")
	}
}
