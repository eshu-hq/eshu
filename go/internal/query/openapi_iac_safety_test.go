// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIIaCManagementSafetyGateFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")

	unmanagedPath := querytestutil.MustMapField(t, paths, "/api/v0/iac/unmanaged-resources")
	unmanagedPost := querytestutil.MustMapField(t, unmanagedPath, "post")
	unmanagedOK := querytestutil.MustMapField(t, querytestutil.MustMapField(t, unmanagedPost, "responses"), "200")
	unmanagedProps := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, unmanagedOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := unmanagedProps["safety_summary"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing safety_summary")
	}
	findings := querytestutil.MustMapField(t, unmanagedProps, "findings")
	findingProps := querytestutil.MustMapField(t, querytestutil.MustMapField(t, findings, "items"), "properties")
	if _, ok := findingProps["safety_gate"]; !ok {
		t.Fatal("iac/unmanaged-resources finding schema missing safety_gate")
	}

	statusPath := querytestutil.MustMapField(t, paths, "/api/v0/iac/management-status")
	statusPost := querytestutil.MustMapField(t, statusPath, "post")
	statusOK := querytestutil.MustMapField(t, querytestutil.MustMapField(t, statusPost, "responses"), "200")
	statusProps := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, statusOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := statusProps["safety_gate"]; !ok {
		t.Fatal("iac/management-status response schema missing safety_gate")
	}

	explainPath := querytestutil.MustMapField(t, paths, "/api/v0/iac/management-status/explain")
	explainPost := querytestutil.MustMapField(t, explainPath, "post")
	explainOK := querytestutil.MustMapField(t, querytestutil.MustMapField(t, explainPost, "responses"), "200")
	explainProps := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, explainOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := explainProps["safety_gate"]; !ok {
		t.Fatal("iac/management-status/explain response schema missing safety_gate")
	}
}
