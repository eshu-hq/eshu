// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// awsAggregateFixtureRow builds one fake fact_records row shaped like the
// aggregate query's SELECT (fact_kind, fact_id, scope_id, generation_id,
// source_system, observed_at, payload) for an AWS-kind finding.
func awsAggregateFixtureRow(factID string, observedAt time.Time) []any {
	return []any{
		AWSCloudRuntimeDriftFindingFactKind,
		factID,
		"aws:123456789012:us-east-1:ec2",
		"generation:aws-1",
		"aws",
		observedAt,
		[]byte(`{
			"arn":"arn:aws:ec2:us-east-1:123456789012:instance/i-0aws00000000000aa",
			"finding_kind":"orphaned_cloud_resource",
			"management_status":"cloud_only",
			"confidence":1.0,
			"missing_evidence":["terraform_state_resource","terraform_config_resource"],
			"warning_flags":[],
			"recommended_action":"triage_owner_and_import_or_retire",
			"evidence":[]
		}`),
	}
}

// multiCloudAggregateFixtureRow is the provider-neutral-kind sibling of
// awsAggregateFixtureRow.
func multiCloudAggregateFixtureRow(factID string, provider string, observedAt time.Time) []any {
	return []any{
		MultiCloudRuntimeDriftFindingFactKind,
		factID,
		"gcp:project:demo",
		"generation:gcp-1",
		provider,
		observedAt,
		[]byte(`{
			"provider":"` + provider + `",
			"cloud_resource_uid":"cloud_resource:abc",
			"raw_identity":"//compute.googleapis.com/projects/demo/zones/z/instances/orphan",
			"finding_kind":"orphaned_cloud_resource",
			"management_status":"cloud_only",
			"confidence":1.0,
			"missing_evidence":["terraform_state_resource","terraform_config_resource"],
			"warning_flags":[],
			"recommended_action":"triage_owner_and_import_or_retire",
			"evidence":[]
		}`),
	}
}

// TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersReturnsAWSForProviderAWS
// is the #5759 follow-up P1 regression: provider=aws on the aggregate read
// must select the AWS-specific fact kind and return AWS-origin rows, not the
// empty page the pre-fix provider-neutral-only query always returned.
func TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersReturnsAWSForProviderAWS(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{awsAggregateFixtureRow("fact:aws-orphan", observedAt)}},
		},
	}
	store := NewMultiCloudRuntimeDriftFindingStore(db)

	rows, err := store.ListActiveFindingsAcrossProviders(context.Background(), CloudRuntimeDriftAggregateFilter{
		ScopeID:  "aws:123456789012:us-east-1:ec2",
		Provider: "aws",
		Limit:    25,
	})
	if err != nil {
		t.Fatalf("ListActiveFindingsAcrossProviders() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].FactKind, AWSCloudRuntimeDriftFindingFactKind; got != want {
		t.Fatalf("rows[0].FactKind = %q, want %q", got, want)
	}
	if got, want := rows[0].AWS.ARN, "arn:aws:ec2:us-east-1:123456789012:instance/i-0aws00000000000aa"; got != want {
		t.Fatalf("rows[0].AWS.ARN = %q, want %q", got, want)
	}
	if got, want := rows[0].MultiCloud.FactID, ""; got != want {
		t.Fatalf("rows[0].MultiCloud must stay zero-valued for an AWS-kind row, got FactID = %q", got)
	}

	query := db.queries[0].query
	args := db.queries[0].args
	if !strings.Contains(query, "fact.fact_kind = ANY($1)") {
		t.Fatalf("query missing fact_kind ANY predicate: %s", query)
	}
	if !strings.Contains(query, "ORDER BY fact.observed_at DESC, fact.fact_id ASC") {
		t.Fatalf("query missing deterministic order-by: %s", query)
	}
	kinds, ok := args[0].(pgarray.StringArray)
	if !ok {
		t.Fatalf("args[0] is not a string array: %#v", args[0])
	}
	if got, want := []string(kinds), []string{AWSCloudRuntimeDriftFindingFactKind}; !stringSlicesEqual(got, want) {
		t.Fatalf("fact_kind set = %v, want %v (provider=aws must query ONLY the AWS fact kind)", got, want)
	}
}

// TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersUnfilteredQueriesBothKinds
// proves the unfiltered ("all providers") case genuinely spans both fact
// kinds -- the second half of the P1: "an unfiltered all-cloud query is
// genuinely complete across all three providers."
func TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersUnfilteredQueriesBothKinds(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				multiCloudAggregateFixtureRow("fact:gcp-orphan", "gcp", newer),
				awsAggregateFixtureRow("fact:aws-orphan", older),
			}},
		},
	}
	store := NewMultiCloudRuntimeDriftFindingStore(db)

	rows, err := store.ListActiveFindingsAcrossProviders(context.Background(), CloudRuntimeDriftAggregateFilter{
		ScopeID: "gcp:project:demo",
		Limit:   25,
	})
	if err != nil {
		t.Fatalf("ListActiveFindingsAcrossProviders() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}

	args := db.queries[0].args
	kinds, ok := args[0].(pgarray.StringArray)
	if !ok {
		t.Fatalf("args[0] is not a string array: %#v", args[0])
	}
	got := []string(kinds)
	want := []string{MultiCloudRuntimeDriftFindingFactKind, AWSCloudRuntimeDriftFindingFactKind}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("fact_kind set = %v, want %v (unfiltered must query BOTH fact kinds)", got, want)
	}
	// The query itself must carry no provider predicate in the unfiltered case
	// -- a stray payload->>'provider' filter here would silently re-exclude
	// AWS rows even though $1 already selected the AWS fact kind.
	query := db.queries[0].query
	if strings.Contains(query, "payload->>'provider'") {
		t.Fatalf("unfiltered query must not carry a provider predicate: %s", query)
	}
}

// TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersGCPExcludesAWS proves
// provider=gcp still selects only the provider-neutral fact kind (unchanged
// from before this aggregation), so AWS rows are never mixed into a
// gcp-scoped read.
func TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersGCPExcludesAWS(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	store := NewMultiCloudRuntimeDriftFindingStore(db)

	if _, err := store.ListActiveFindingsAcrossProviders(context.Background(), CloudRuntimeDriftAggregateFilter{
		ScopeID:  "gcp:project:demo",
		Provider: "gcp",
		Limit:    25,
	}); err != nil {
		t.Fatalf("ListActiveFindingsAcrossProviders() error = %v, want nil", err)
	}

	args := db.queries[0].args
	kinds, ok := args[0].(pgarray.StringArray)
	if !ok {
		t.Fatalf("args[0] is not a string array: %#v", args[0])
	}
	if got, want := []string(kinds), []string{MultiCloudRuntimeDriftFindingFactKind}; !stringSlicesEqual(got, want) {
		t.Fatalf("fact_kind set = %v, want %v", got, want)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "fact.payload->>'provider' = $3") {
		t.Fatalf("gcp-scoped query must still push the provider predicate: %s", query)
	}
}

func TestMultiCloudRuntimeDriftFindingStoreCountActiveFindingsAcrossProvidersRequiresScope(t *testing.T) {
	t.Parallel()

	store := NewMultiCloudRuntimeDriftFindingStore(&fakeExecQueryer{})
	if _, err := store.CountActiveFindingsAcrossProviders(context.Background(), CloudRuntimeDriftAggregateFilter{}); err == nil {
		t.Fatal("CountActiveFindingsAcrossProviders() error = nil, want scope_id required error")
	}
}

func TestMultiCloudRuntimeDriftFindingStoreListActiveFindingsAcrossProvidersRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := (MultiCloudRuntimeDriftFindingStore{}).ListActiveFindingsAcrossProviders(
		context.Background(),
		CloudRuntimeDriftAggregateFilter{ScopeID: "aws:123456789012:us-east-1:ec2"},
	); err == nil {
		t.Fatal("ListActiveFindingsAcrossProviders() error = nil, want missing database error")
	}
}
