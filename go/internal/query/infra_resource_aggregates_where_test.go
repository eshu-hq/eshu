// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestInfraResourceAggregateWhereClauseUsesDirectEqualityForIndexedProps
// guards the hot-path index eligibility for the four TerraformResource
// indexes shipped with #690 (provider, environment, resource_service,
// resource_category). A future refactor that wraps the property in
// `coalesce(n.X, ”) = $X` would silently block planner index selection
// even though the test fixture still returns the right rows. This test
// fails as soon as that regression lands.
func TestInfraResourceAggregateWhereClauseUsesDirectEqualityForIndexedProps(t *testing.T) {
	t.Parallel()

	filter := InfraResourceAggregateFilter{
		Provider:         "aws",
		Environment:      "production",
		ResourceService:  "aws.ec2",
		ResourceCategory: "compute",
	}
	where := infraResourceAggregateBranchWhere(filter)

	wantClauses := []string{
		"n.provider = $provider",
		"n.environment = $environment",
		"n.resource_service = $resource_service",
		"n.resource_category = $resource_category",
	}
	for _, want := range wantClauses {
		if !strings.Contains(where, want) {
			t.Fatalf("WHERE clause missing direct-equality predicate %q (index-eligible form): %s", want, where)
		}
	}

	if strings.Contains(where, "coalesce(n.provider") ||
		strings.Contains(where, "coalesce(n.environment") ||
		strings.Contains(where, "coalesce(n.resource_service") ||
		strings.Contains(where, "coalesce(n.resource_category") {
		t.Fatalf("WHERE clause wraps indexed property in coalesce(...); this blocks TerraformResource index usage: %s", where)
	}
}
