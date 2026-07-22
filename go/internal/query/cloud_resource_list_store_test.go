// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildCloudResourceIdentityListQueryAppliesAuthorizationBeforeLimit(t *testing.T) {
	t.Parallel()

	query, args := buildCloudResourceIdentityListQuery(CloudResourceListPageFilter{
		Provider:             "aws",
		ResourceType:         "aws_iam_role",
		Region:               "us-east-1",
		AccountID:            "account-a",
		AfterResourceType:    "aws_iam_role",
		AfterID:              "uid-a",
		Limit:                51,
		AllowedRepositoryIDs: []string{"repository:allowed"},
		AllowedScopeIDs:      []string{"scope:allowed"},
	})

	for _, want := range []string{
		"FROM graph_node_owner AS owner",
		"fact.fact_id = owner.winning_row->>'source_fact_id'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"scope.scope_kind = 'repository'",
		"scope.source_key = ANY(",
		"fact.scope_id = ANY(",
		"LIMIT 1",
		"owner.winning_row->>'collector_kind' =",
		"owner.winning_row->>'resource_type' =",
		"owner.winning_row->>'region' =",
		"owner.winning_row->>'account_id' =",
		"ORDER BY owner.winning_row->>'resource_type', owner.uid",
		"LIMIT",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q:\n%s", want, query)
		}
	}
	authAt := strings.Index(query, "scope.source_key = ANY(")
	outerLimitAt := strings.LastIndex(query, "LIMIT")
	if authAt < 0 || outerLimitAt < 0 || authAt > outerLimitAt {
		t.Fatalf("authorization must precede outer page bound:\n%s", query)
	}
	if got, want := len(args), 9; got != want {
		t.Fatalf("args len = %d, want %d: %#v", got, want, args)
	}
}

func TestBuildCloudResourceIdentityListQueryCoversEveryProductionVariant(t *testing.T) {
	t.Parallel()

	for mask := 0; mask < 32; mask++ {
		filter := CloudResourceListPageFilter{Limit: 51, AllScopes: true}
		wants := map[string]bool{
			"collector_kind": mask&1 != 0,
			"resource_type":  mask&2 != 0,
			"region":         mask&4 != 0,
			"account_id":     mask&8 != 0,
			"cursor":         mask&16 != 0,
		}
		if wants["collector_kind"] {
			filter.Provider = "aws"
		}
		if wants["resource_type"] {
			filter.ResourceType = "aws_iam_role"
		}
		if wants["region"] {
			filter.Region = "us-east-1"
		}
		if wants["account_id"] {
			filter.AccountID = "account-a"
		}
		if wants["cursor"] {
			filter.AfterResourceType = "aws_iam_role"
			filter.AfterID = "uid-a"
		}

		query, args := buildCloudResourceIdentityListQuery(filter)
		checks := map[string]string{
			"collector_kind": "owner.winning_row->>'collector_kind' =",
			"resource_type":  "owner.winning_row->>'resource_type' =",
			"region":         "owner.winning_row->>'region' =",
			"account_id":     "owner.winning_row->>'account_id' =",
			"cursor":         "(owner.winning_row->>'resource_type', owner.uid) >",
		}
		for name, fragment := range checks {
			if got := strings.Contains(query, fragment); got != wants[name] {
				t.Errorf("variant %02d %s present=%t, want %t:\n%s", mask, name, got, wants[name], query)
			}
		}
		if strings.Contains(query, "scope.source_key = ANY(") || strings.Contains(query, "fact.scope_id = ANY(") {
			t.Errorf("variant %02d unscoped query contains grant predicate:\n%s", mask, query)
		}
		if strings.Count(query, "LIMIT 1") != 1 {
			t.Errorf("variant %02d must retain the correlated scalar LIMIT 1:\n%s", mask, query)
		}
		if !strings.HasSuffix(strings.TrimSpace(query), fmt.Sprintf("LIMIT $%d", len(args))) {
			t.Errorf("variant %02d outer limit is not the final operation:\n%s", mask, query)
		}
	}
}

func TestBuildCloudResourceIdentityListQueryBindsValues(t *testing.T) {
	t.Parallel()

	query, _ := buildCloudResourceIdentityListQuery(CloudResourceListPageFilter{
		Provider:  "aws' OR TRUE --",
		AccountID: "secret-account",
		Limit:     2,
		AllScopes: true,
	})
	if strings.Contains(query, "aws' OR TRUE --") || strings.Contains(query, "secret-account") {
		t.Fatalf("filter value interpolated into SQL:\n%s", query)
	}
}
