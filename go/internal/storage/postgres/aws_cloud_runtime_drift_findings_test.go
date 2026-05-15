package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
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
					"confidence":0.92,
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

func TestAWSCloudRuntimeDriftFindingStoreRejectsUnboundedFilters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	if _, err := store.ListActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{}); err == nil {
		t.Fatal("ListActiveFindings() error = nil, want unbounded filter error")
	}
	if _, err := store.CountActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{}); err == nil {
		t.Fatal("CountActiveFindings() error = nil, want unbounded filter error")
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
