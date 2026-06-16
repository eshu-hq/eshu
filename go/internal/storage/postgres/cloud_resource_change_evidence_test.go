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
		scopeID      = "azure:tenant:subscription:sub-1:all:all:resourcechanges"
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
		"azure_resource_change",
		"fact.scope_id = $1",
		"fact.generation_id = $2",
		"fact.is_tombstone = FALSE",
		"target_arm_resource_id",
	} {
		if !strings.Contains(listCloudResourceChangeEvidenceForGenerationQuery, want) {
			t.Fatalf("listCloudResourceChangeEvidenceForGenerationQuery missing %q", want)
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
