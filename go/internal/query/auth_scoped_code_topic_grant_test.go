// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// Code-topic SQL-builder proof that lives in package query: it calls the
// root-owned codeTopicFilters builder directly, which codequery cannot name
// without importing the root back (#6060). Split from
// codequery/auth_scoped_code_topic_grant_test.go at the lane-A move; the
// fake-driven topic route proofs stay there.

func TestCodeTopicFiltersBindTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	filters, args, _ := codeTopicFilters(CodeTopicInvestigationRequest{
		AllowedRepositoryIDs: []string{codeGrantGrantedRepo},
	})
	if !slices.Contains(filters, "repo_id = ANY($1)") {
		t.Fatalf("codeTopicFilters() = %#v, want a repo_id = ANY($1) grant predicate; without it a scoped caller's grant is resolved but never applied", filters)
	}
	querytestutil.AssertBoundRepositoryGrantArray(t, args, []string{codeGrantGrantedRepo})

	unscoped, _, _ := codeTopicFilters(CodeTopicInvestigationRequest{})
	for _, filter := range unscoped {
		if strings.Contains(filter, "repo_id = ANY(") {
			t.Fatalf("codeTopicFilters() = %#v, want no grant predicate for an unscoped caller", unscoped)
		}
	}
}
