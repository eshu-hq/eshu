// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestAWSCloudRuntimeDriftFindingStoreListsActiveScopedFindings(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"fact:aws-unmanaged-lambda",
				"aws:123456789012:us-east-1:lambda",
				"generation:aws-1",
				"aws",
				observedAt,
				[]byte(`{
					"canonical_id":"arn:aws:lambda:us-east-1:123456789012:function:payments-api",
					"candidate_id":"candidate:lambda:payments-api",
					"arn":"arn:aws:lambda:us-east-1:123456789012:function:payments-api",
					"finding_kind":"unmanaged_cloud_resource",
					"management_status":"terraform_state_only",
					"confidence":0.92,
					"matched_terraform_state_address":"module.app.aws_lambda_function.payments",
					"matched_terraform_config_file":"services/payments/lambda.tf",
					"matched_terraform_module_path":"module.app",
					"service_candidates":["payments"],
					"environment_candidates":["prod"],
					"dependency_paths":["service:payments -> lambda:payments-api"],
					"missing_evidence":["terraform_config_resource"],
					"warning_flags":["security_sensitive_resource"],
					"recommended_action":"restore_config_or_prepare_import_block",
					"evidence":[{
						"id":"evidence:state",
						"source_system":"terraform_state",
						"evidence_type":"terraform_state_resource",
						"scope_id":"tfstate:prod",
						"key":"arn",
						"value":"arn:aws:lambda:us-east-1:123456789012:function:payments-api",
						"confidence":0.95
					}]
				}`),
			}}},
		},
	}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	rows, err := store.ListActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{
		AccountID:    "123456789012",
		Region:       "us-east-1",
		FindingKinds: []string{"unmanaged_cloud_resource"},
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := row.ARN, "arn:aws:lambda:us-east-1:123456789012:function:payments-api"; got != want {
		t.Fatalf("row.ARN = %q, want %q", got, want)
	}
	if got, want := row.FindingKind, "unmanaged_cloud_resource"; got != want {
		t.Fatalf("row.FindingKind = %q, want %q", got, want)
	}
	if got, want := row.ManagementStatus, "terraform_state_only"; got != want {
		t.Fatalf("row.ManagementStatus = %q, want %q", got, want)
	}
	if got, want := row.MatchedTerraformStateAddress, "module.app.aws_lambda_function.payments"; got != want {
		t.Fatalf("row.MatchedTerraformStateAddress = %q, want %q", got, want)
	}
	if got, want := row.WarningFlags, []string{"security_sensitive_resource"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row.WarningFlags = %#v, want %#v", got, want)
	}
	if got, want := len(row.Evidence), 1; got != want {
		t.Fatalf("len(row.Evidence) = %d, want %d", got, want)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM fact_records AS fact",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = fact.generation_id",
		"fact.fact_kind = $1",
		"fact.scope_id LIKE $2",
		"fact.payload->>'finding_kind' IN ($3)",
		"LIMIT $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestAWSCloudRuntimeDriftFindingStoreCountsActiveScopedFindings(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{3}}},
		},
	}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	count, err := store.CountActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{
		ScopeID: "aws:123456789012:us-east-1:lambda",
	})
	if err != nil {
		t.Fatalf("CountActiveFindings() error = %v, want nil", err)
	}
	if got, want := count, 3; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"COUNT(*)",
		"scope.active_generation_id = fact.generation_id",
		"fact.scope_id = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestAWSCloudRuntimeDriftFindingStoreFiltersByARN(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{0}}}},
	}
	store := NewAWSCloudRuntimeDriftFindingStore(db)
	arn := "arn:aws:lambda:us-east-1:123456789012:function:payments-api"

	_, err := store.CountActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{
		AccountID: "123456789012",
		Region:    "us-east-1",
		ARN:       arn,
	})
	if err != nil {
		t.Fatalf("CountActiveFindings() error = %v, want nil", err)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "fact.payload->>'arn' = $3") {
		t.Fatalf("query missing arn predicate: %s", query)
	}
	if got, want := db.queries[0].args[2], arn; got != want {
		t.Fatalf("arn arg = %#v, want %#v", got, want)
	}
}

func TestAWSCloudRuntimeDriftFindingStoreRejectsUnboundedFilters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	for _, tc := range []struct {
		name   string
		filter AWSCloudRuntimeDriftFindingFilter
	}{
		{name: "missing scope", filter: AWSCloudRuntimeDriftFindingFilter{}},
		{name: "wildcard account", filter: AWSCloudRuntimeDriftFindingFilter{AccountID: "%"}},
		{name: "short account", filter: AWSCloudRuntimeDriftFindingFilter{AccountID: "123"}},
		{name: "wildcard region", filter: AWSCloudRuntimeDriftFindingFilter{
			AccountID: "123456789012",
			Region:    "us-_-1",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.ListActiveFindings(context.Background(), tc.filter); err == nil {
				t.Fatal("ListActiveFindings() error = nil, want bounded filter error")
			}
			if _, err := store.CountActiveFindings(context.Background(), tc.filter); err == nil {
				t.Fatal("CountActiveFindings() error = nil, want bounded filter error")
			}
		})
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("query count = %d, want 0", got)
	}
}

func TestAWSCloudRuntimeDriftFindingStoreCapsLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	_, err := store.ListActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{
		ScopeID: "aws:123456789012:us-east-1:lambda",
		Limit:   5000,
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	if got, want := db.queries[0].args[2], 500; got != want {
		t.Fatalf("limit arg = %#v, want %#v", got, want)
	}
}

// TestAWSCloudRuntimeDriftFindingStoreScopedGrantBindsScopePredicate is the
// #5167 W4 direct-SQL proof that the store's tenant-isolation mechanism is the
// emitted query, not a mock. It exercises the real
// buildAWSCloudRuntimeDriftFindingQuery through CountActiveFindings with
// Scoped=true and asserts the `fact.scope_id = ANY($N)` grant predicate is
// (1) present, (2) AND-combined with the account predicate (not OR), (3) at
// the correct positional-parameter index, and (4) bound to the exact
// pq.StringArray grant value. The handler-layer fakeIaCManagementStore proofs
// reimplement this filter one layer above the real store and never invoke
// this builder, so a mis-ordered $N, an OR-instead-of-AND, or a dropped
// predicate would ship undetected without this test.
//
// Mutation proof: deleting the `if filter.Scoped { ... ANY ... }` block in
// buildAWSCloudRuntimeDriftFindingQuery turns this test red (the query no
// longer contains the ANY predicate).
func TestAWSCloudRuntimeDriftFindingStoreScopedGrantBindsScopePredicate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{0}}}},
	}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	grant := []string{"aws:123456789012:us-east-1:lambda"}
	_, err := store.CountActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{
		AccountID:       "123456789012",
		Scoped:          true,
		AllowedScopeIDs: grant,
	})
	if err != nil {
		t.Fatalf("CountActiveFindings() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	// The account predicate lands at $2, so the scoped grant predicate is the
	// next positional parameter, $3. Asserting the literal "AND ... = ANY($3)"
	// substring proves position (3), AND-combination (2), and presence (1) in
	// one check -- an OR-combined or wrong-index predicate fails it.
	if !strings.Contains(query, "AND fact.scope_id = ANY($3)") {
		t.Fatalf("query missing AND-combined scope grant predicate at $3: %s", query)
	}
	if strings.Contains(query, " OR ") {
		t.Fatalf("query must not OR-combine the scope grant predicate: %s", query)
	}
	if got, want := db.queries[0].args[2], pq.StringArray(grant); !reflect.DeepEqual(got, want) {
		t.Fatalf("scope grant arg = %#v, want %#v", got, want)
	}
}

// TestAWSCloudRuntimeDriftFindingStoreEmptyScopedGrantSkipsQuery is the #5167
// W4 direct-SQL proof of the empty-grant short-circuit: a scoped read with no
// granted AWS collector scope must return zero rows WITHOUT issuing any query,
// so a scoped caller with only repository grants can never observe another
// tenant's cloud drift findings even by existence or volume. Both
// ListActiveFindings and CountActiveFindings are covered because each carries
// its own guard.
//
// Mutation proof: removing either short-circuit turns the corresponding
// assertion red (a query is issued where none must be).
func TestAWSCloudRuntimeDriftFindingStoreEmptyScopedGrantSkipsQuery(t *testing.T) {
	t.Parallel()

	filter := AWSCloudRuntimeDriftFindingFilter{
		AccountID:       "123456789012",
		Scoped:          true,
		AllowedScopeIDs: nil,
	}

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		db := &fakeExecQueryer{}
		store := NewAWSCloudRuntimeDriftFindingStore(db)
		rows, err := store.ListActiveFindings(context.Background(), filter)
		if err != nil {
			t.Fatalf("ListActiveFindings() error = %v, want nil", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %#v, want empty for an empty scoped grant", rows)
		}
		if got := len(db.queries); got != 0 {
			t.Fatalf("query count = %d, want 0 -- an empty scoped grant must issue no query", got)
		}
	})

	t.Run("count", func(t *testing.T) {
		t.Parallel()
		db := &fakeExecQueryer{}
		store := NewAWSCloudRuntimeDriftFindingStore(db)
		count, err := store.CountActiveFindings(context.Background(), filter)
		if err != nil {
			t.Fatalf("CountActiveFindings() error = %v, want nil", err)
		}
		if count != 0 {
			t.Fatalf("count = %d, want 0 for an empty scoped grant", count)
		}
		if got := len(db.queries); got != 0 {
			t.Fatalf("query count = %d, want 0 -- an empty scoped grant must issue no query", got)
		}
	})
}
