// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPostgresCloudResourceChangeEvidenceLoaderMapsAzureChangeFacts(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "azure:tenant:subscription:sub-1:all:all:resource_changes"
		generationID = "gen-changes-1"
		armID        = "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	)
	changeTime := time.Date(2026, time.June, 16, 10, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{
					facts.AzureResourceChangeFactKind,
					armID,
					"change-stable-1",
					[]byte(`{
						"target_arm_resource_id":"` + armID + `",
						"change_type":"deleted",
						"change_time":"` + changeTime.Format(time.RFC3339Nano) + `",
						"operation":"Microsoft.Compute/virtualMachines/delete",
						"client_type":"AzurePortal",
						"actor_class":"user",
						"actor_fingerprint":"actor-marker",
						"changed_property_paths":["properties.storageProfile.imageReference","properties.provisioningState"],
						"changed_property_truncated":true,
						"is_tombstone_candidate":true,
						"changedBy":"raw-actor-must-not-leak"
					}`),
				},
				{
					facts.AzureResourceChangeFactKind,
					"not-an-arm-id",
					"change-stable-bad",
					[]byte(`{
						"target_arm_resource_id":"not-an-arm-id",
						"change_type":"updated",
						"change_time":"` + changeTime.Format(time.RFC3339Nano) + `"
					}`),
				},
			}},
		},
	}

	loader := PostgresCloudResourceChangeEvidenceLoader{DB: db}
	records, err := loader.LoadCloudResourceChangeEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadCloudResourceChangeEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("len(records) = %d, want %d (malformed ARM identity dropped)", got, want)
	}
	record := records[0]
	if record.Provider != cloudinventory.ProviderAzure || record.RawIdentity != armID {
		t.Fatalf("provider/raw identity = (%q,%q)", record.Provider, record.RawIdentity)
	}
	if record.ChangeType != "deleted" || !record.TombstoneCandidate {
		t.Fatalf("change type/tombstone = (%q,%v), want deleted candidate", record.ChangeType, record.TombstoneCandidate)
	}
	if !record.ChangeTime.Equal(changeTime) {
		t.Fatalf("change time = %s, want %s", record.ChangeTime, changeTime)
	}
	if record.ActorFingerprint != "actor-marker" || record.ActorClass != "user" {
		t.Fatalf("actor evidence = %#v", record)
	}
	if len(record.ChangedPropertyPaths) != 2 || !record.ChangedPropertyTruncated {
		t.Fatalf("changed paths/truncated = %#v/%v", record.ChangedPropertyPaths, record.ChangedPropertyTruncated)
	}
	if strings.Contains(record.ActorFingerprint, "raw-actor") {
		t.Fatalf("raw actor leaked into fingerprint: %#v", record)
	}
}

func TestCloudResourceChangeEvidenceQueryIsBounded(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"requested_inventory_generation AS",
		"active_resource_change_generation AS",
		"azure_resource_change",
		"generation.scope_id = $1",
		"generation.generation_id = $2",
		"source.scope_id LIKE 'azure:%:%:%:%:%:resource_graph'",
		"source.scope_id LIKE 'azure:%:%:%:%:%:arm_fallback'",
		"generation.status = 'active'",
		"fact.scope_id = generation.scope_id",
		"fact.generation_id = generation.generation_id",
		"fact.is_tombstone = FALSE",
		"target_arm_resource_id",
	} {
		if !strings.Contains(listCloudResourceChangeEvidenceForGenerationQuery, want) {
			t.Fatalf("listCloudResourceChangeEvidenceForGenerationQuery missing %q", want)
		}
	}
}

func TestPostgresCloudResourceChangeEvidenceLoaderResolvesSiblingLane(t *testing.T) {
	t.Parallel()

	const (
		inventoryScopeID      = "azure:tenant:subscription:sub-1:all:all:resource_graph"
		inventoryGenerationID = "gen-inventory-1"
	)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	loader := PostgresCloudResourceChangeEvidenceLoader{DB: db}

	if _, err := loader.LoadCloudResourceChangeEvidence(
		context.Background(),
		inventoryScopeID,
		inventoryGenerationID,
	); err != nil {
		t.Fatalf("LoadCloudResourceChangeEvidence() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	q := db.queries[0]
	if got, want := q.args[0], inventoryScopeID; got != want {
		t.Fatalf("query scope arg = %v, want %v", got, want)
	}
	if got, want := q.args[1], inventoryGenerationID; got != want {
		t.Fatalf("query generation arg = %v, want %v", got, want)
	}
	for _, want := range []string{
		":resource_graph",
		":resource_changes",
		"active_resource_change_generation",
	} {
		if !strings.Contains(q.query, want) {
			t.Fatalf("query missing sibling lane fragment %q: %s", want, q.query)
		}
	}
}

func TestPostgresCloudResourceChangeEvidenceLoaderRequiresScopeAndGeneration(t *testing.T) {
	t.Parallel()

	loader := PostgresCloudResourceChangeEvidenceLoader{DB: &fakeExecQueryer{}}
	if _, err := loader.LoadCloudResourceChangeEvidence(context.Background(), "", "gen-1"); err == nil {
		t.Fatal("blank scope: error = nil, want non-nil")
	}
	if _, err := loader.LoadCloudResourceChangeEvidence(context.Background(), "scope-1", ""); err == nil {
		t.Fatal("blank generation: error = nil, want non-nil")
	}
}
