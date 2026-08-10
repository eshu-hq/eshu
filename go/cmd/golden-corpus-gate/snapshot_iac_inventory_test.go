// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestGoldenSnapshotIaCInventoryRequiresCurrentSummary(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	const key = "GET /api/v0/iac/resources?limit=50&include_facets=true"
	shape, ok := snap.QueryShapes.HTTP[key]
	if !ok {
		t.Fatalf("query_shapes.http missing %s", key)
	}
	for _, field := range []string{"resources", "count", "summary"} {
		if !containsString(shape.RequiredResponseFields, field) {
			t.Fatalf("%s missing required response field %q", key, field)
		}
	}
	if shape.MinimumResults < 1 {
		t.Fatalf("%s minimum_results = %d, want at least 1", key, shape.MinimumResults)
	}
	for _, path := range []string{
		"resources[].id",
		"summary.total",
		"summary.by_kind.resource",
		"summary.by_kind.module",
		"summary.by_kind.data-source",
	} {
		if !containsString(shape.RequiredJSONPaths, path) {
			t.Fatalf("%s missing required JSON path %q", key, path)
		}
	}
	// Issue #5594 raised these to 13/21/13 (terraform_local_backend_demo's
	// two resource blocks: aws_instance.local_backend_demo,
	// aws_s3_bucket.local_backend_demo). Issue #5572 raises them again to
	// 14/22/14 (terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf's
	// one resource block, aws_security_group.vpc_endpoints). Issue #5954 raises
	// summary.total only, 22 -> 23 (terraform_comprehensive/pagerduty.tf's
	// module "orders_pagerduty_service" is a MODULE block, not a resource
	// block, so count and summary.by_kind.resource hold at 14). PR #6037
	// review round 3 (copilot) added summary.by_kind.module=7 and
	// summary.by_kind.data-source=2: total=23/resource=14 pin their sum at 9
	// (23-14) but not the split between them, so without these two a future
	// regression could shift a count from module to data-source and still
	// pass -- see
	// testdata/golden/e2e-20repo-snapshot.json's own required_json_values
	// comment on this same query shape for the full derivation of all five.
	for path, want := range map[string]any{
		"count":                       float64(14),
		"summary.total":               float64(23),
		"summary.by_kind.resource":    float64(14),
		"summary.by_kind.module":      float64(7),
		"summary.by_kind.data-source": float64(2),
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Fatalf("%s required JSON value %q = %#v, want %#v", key, path, got, want)
		}
	}
}
