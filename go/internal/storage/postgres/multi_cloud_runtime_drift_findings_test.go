// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMultiCloudRuntimeDriftFindingStoreListsActiveScopedFindings(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"fact:gcp-orphan",
				"gcp:proj:z",
				"generation:gcp-1",
				"gcp",
				observedAt,
				[]byte(`{
					"canonical_id":"canonical:multi_cloud_runtime_drift:gcp:proj:z:orphaned_cloud_resource:cloud_resource:abc",
					"candidate_id":"candidate:gcp:orphan",
					"provider":"gcp",
					"cloud_resource_uid":"cloud_resource:abc",
					"raw_identity":"//compute.googleapis.com/projects/proj/zones/z/instances/orphan",
					"finding_kind":"orphaned_cloud_resource",
					"management_status":"cloud_only",
					"confidence":1.0,
					"missing_evidence":["terraform_state_resource","terraform_config_resource"],
					"warning_flags":[],
					"recommended_action":"triage_owner_and_import_or_retire",
					"evidence":[{
						"id":"evidence:uid",
						"source_system":"reducer/multi_cloud_runtime_drift",
						"evidence_type":"cloud_resource_uid",
						"scope_id":"gcp:proj:z",
						"key":"cloud_resource_uid",
						"value":"cloud_resource:abc",
						"confidence":1.0
					}]
				}`),
			}}},
		},
	}
	store := NewMultiCloudRuntimeDriftFindingStore(db)

	rows, err := store.ListActiveFindings(context.Background(), MultiCloudRuntimeDriftFindingFilter{
		ScopeID:      "gcp:proj:z",
		Provider:     "gcp",
		FindingKinds: []string{"orphaned_cloud_resource"},
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := row.Provider, "gcp"; got != want {
		t.Fatalf("row.Provider = %q, want %q", got, want)
	}
	if got, want := row.CloudResourceUID, "cloud_resource:abc"; got != want {
		t.Fatalf("row.CloudResourceUID = %q, want %q", got, want)
	}
	if got, want := row.FindingKind, "orphaned_cloud_resource"; got != want {
		t.Fatalf("row.FindingKind = %q, want %q", got, want)
	}
	if got, want := row.ManagementStatus, "cloud_only"; got != want {
		t.Fatalf("row.ManagementStatus = %q, want %q", got, want)
	}
	if got, want := row.MissingEvidence, []string{"terraform_state_resource", "terraform_config_resource"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row.MissingEvidence = %#v, want %#v", got, want)
	}
	if got, want := len(row.Evidence), 1; got != want {
		t.Fatalf("len(row.Evidence) = %d, want %d", got, want)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"FROM fact_records AS fact",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = fact.generation_id",
		"fact.fact_kind = $1",
		"fact.scope_id = $2",
		"fact.payload->>'provider' = $3",
		"fact.payload->>'finding_kind' IN ($4)",
		"LIMIT $5",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestMultiCloudRuntimeDriftFindingStoreCountsActiveScopedFindings(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{4}}}}}
	store := NewMultiCloudRuntimeDriftFindingStore(db)

	count, err := store.CountActiveFindings(context.Background(), MultiCloudRuntimeDriftFindingFilter{
		ScopeID: "azure:sub:rg",
	})
	if err != nil {
		t.Fatalf("CountActiveFindings() error = %v, want nil", err)
	}
	if got, want := count, 4; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{"COUNT(*)", "fact.scope_id = $2"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestMultiCloudRuntimeDriftFindingStoreRejectsUnscopedFilter(t *testing.T) {
	t.Parallel()

	store := NewMultiCloudRuntimeDriftFindingStore(&fakeExecQueryer{})
	if _, err := store.ListActiveFindings(context.Background(), MultiCloudRuntimeDriftFindingFilter{}); err == nil {
		t.Fatal("ListActiveFindings() error = nil, want scope_id required error")
	}
}

func TestMultiCloudRuntimeDriftFindingStoreFiltersByUID(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	store := NewMultiCloudRuntimeDriftFindingStore(db)

	if _, err := store.ListActiveFindings(context.Background(), MultiCloudRuntimeDriftFindingFilter{
		ScopeID:          "gcp:proj:z",
		CloudResourceUID: "cloud_resource:abc",
	}); err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	query := db.queries[0].query
	if !strings.Contains(query, "fact.payload->>'cloud_resource_uid' = $3") {
		t.Fatalf("query missing cloud_resource_uid filter: %s", query)
	}
}

func TestMultiCloudRuntimeDriftFindingStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := (MultiCloudRuntimeDriftFindingStore{}).ListActiveFindings(
		context.Background(),
		MultiCloudRuntimeDriftFindingFilter{ScopeID: "gcp:proj:z"},
	); err == nil {
		t.Fatal("ListActiveFindings() error = nil, want missing database error")
	}
}
