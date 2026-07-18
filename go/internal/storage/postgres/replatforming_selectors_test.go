// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestAWSCloudRuntimeDriftFindingStoreListsActiveReplatformingScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{
		{"aws:123456789012:us-east-1:lambda", "123456789012", "us-east-1", "lambda", 3},
		{"aws:123456789012:us-west-2:s3", "123456789012", "us-west-2", "s3", 0},
		{"aws:210987654321:us-east-2:ec2", "210987654321", "us-east-2", "ec2", 1},
	}}}}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	page, err := store.ListActiveReplatformingScopes(context.Background(), 2, nil)
	if err != nil {
		t.Fatalf("ListActiveReplatformingScopes() error = %v, want nil", err)
	}
	if got, want := len(page.Scopes), 2; got != want {
		t.Fatalf("len(page.Scopes) = %d, want %d", got, want)
	}
	if !page.Truncated {
		t.Fatal("page.Truncated = false, want true")
	}
	if got, want := page.Scopes[0].FindingCount, 3; got != want {
		t.Fatalf("page.Scopes[0].FindingCount = %d, want %d", got, want)
	}
	if got, want := page.Scopes[1].FindingCount, 0; got != want {
		t.Fatalf("page.Scopes[1].FindingCount = %d, want authoritative empty scope", got)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, required := range []string{
		"FROM ingestion_scopes AS scope",
		"scope.source_system = 'aws'",
		"scope.collector_kind = 'aws'",
		"scope.scope_kind = 'region'",
		"scope.status = 'active'",
		"scope.active_generation_id IS NOT NULL",
		"fact.scope_id = scope.scope_id",
		"fact.generation_id = scope.active_generation_id",
		"fact.fact_kind = $1",
		"ORDER BY scope.scope_id",
		"LIMIT $2",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("selector query missing %q:\n%s", required, query)
		}
	}
	if got, want := db.queries[0].args, []any{AWSCloudRuntimeDriftFindingFactKind, 3}; !equalAnySlice(got, want) {
		t.Fatalf("query args = %#v, want %#v", got, want)
	}
}

func TestAWSCloudRuntimeDriftFindingStoreScopesReplatformingSelectorsToExactGrants(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewAWSCloudRuntimeDriftFindingStore(db)
	allowedScopeIDs := []string{
		"aws:123456789012:us-east-1:lambda",
		"aws:210987654321:us-west-2:s3",
	}

	if _, err := store.ListActiveReplatformingScopes(context.Background(), 25, allowedScopeIDs); err != nil {
		t.Fatalf("ListActiveReplatformingScopes() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, required := range []string{"scope.scope_id = ANY($2)", "LIMIT $3"} {
		if !strings.Contains(query, required) {
			t.Fatalf("scoped selector query missing %q:\n%s", required, query)
		}
	}
	args := db.queries[0].args
	if got, want := len(args), 3; got != want {
		t.Fatalf("query args length = %d, want %d", got, want)
	}
	if got, want := args[0], any(AWSCloudRuntimeDriftFindingFactKind); got != want {
		t.Fatalf("query arg[0] = %#v, want %#v", got, want)
	}
	grants, ok := args[1].(pq.StringArray)
	if !ok {
		t.Fatalf("query arg[1] type = %T, want pq.StringArray", args[1])
	}
	if got, want := []string(grants), allowedScopeIDs; !equalStringSlice(got, want) {
		t.Fatalf("query arg[1] = %#v, want %#v", got, want)
	}
	if got, want := args[2], any(26); got != want {
		t.Fatalf("query arg[2] = %#v, want %#v", got, want)
	}
}

func equalStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalAnySlice(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
