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
	// aws_s3_bucket.local_backend_demo). Issue #5572 raised them again to
	// 14/22/14 (terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf's
	// one resource block, aws_security_group.vpc_endpoints). Issue #5861 raised
	// them to 15/23/15 (terraform_comprehensive/lambda_partial.tf's one resource
	// block, aws_lambda_function.supply-chain-demo-partial -- the corpus's only
	// partially comparable runtime-drift pair).
	//
	// Issue #5954 then adds terraform_comprehensive/pagerduty.tf's
	// module "orders_pagerduty_service". That is a MODULE block, not a resource
	// block, so it moves summary.by_kind.module 6 -> 7 and summary.total
	// 23 -> 24 while count and summary.by_kind.resource hold at 15.
	//
	// Both changes landed independently against a 22/14 base and each raised
	// summary.total to 23 on its own, for different reasons -- #5861 by adding
	// a resource, #5954 by adding a module. With both fixtures staged the
	// totals compose rather than collide: 15 resources + 7 modules +
	// 2 data-sources = 24.
	//
	// summary.by_kind.module and summary.by_kind.data-source are pinned
	// (PR #6037 review round 3, copilot) because total=24/resource=15 fixes
	// their sum at 9 but not the split, so without them a regression could
	// shift a count from module to data-source and still pass -- see
	// testdata/golden/e2e-20repo-snapshot.json's own required_json_values
	// comment on this same query shape for the full derivation of all five.
	for path, want := range map[string]any{
		"count":                       float64(15),
		"summary.total":               float64(24),
		"summary.by_kind.resource":    float64(15),
		"summary.by_kind.module":      float64(7),
		"summary.by_kind.data-source": float64(2),
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Fatalf("%s required JSON value %q = %#v, want %#v", key, path, got, want)
		}
	}
}
