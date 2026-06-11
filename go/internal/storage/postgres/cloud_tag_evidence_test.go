package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestPostgresCloudTagEvidenceLoaderMapsTagFacts proves the loader reads
// azure_tag_observation facts for one generation, maps the arm_resource_id and
// keyed tag value fingerprints into the shared admission record, and drops rows
// that carry no usable fingerprints.
func TestPostgresCloudTagEvidenceLoaderMapsTagFacts(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "cloud:tenant-1"
		generationID = "gen-1"
	)
	armID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	noTagsID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.AzureTagObservationFactKind, armID, []byte(`{
					"arm_resource_id":"` + armID + `",
					"tag_value_fingerprints":{"env":"az-env-marker","owner":"az-owner-marker"}
				}`)},
				{facts.AzureTagObservationFactKind, noTagsID, []byte(`{
					"arm_resource_id":"` + noTagsID + `"
				}`)},
			}},
		},
	}

	loader := PostgresCloudTagEvidenceLoader{DB: db}
	records, err := loader.LoadCloudTagEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadCloudTagEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("len(records) = %d, want %d (blank-fingerprint row dropped)", got, want)
	}
	record := records[0]
	if record.Provider != cloudinventory.ProviderAzure {
		t.Fatalf("provider = %q, want %q", record.Provider, cloudinventory.ProviderAzure)
	}
	if record.RawIdentity != armID {
		t.Fatalf("raw identity = %q, want %q", record.RawIdentity, armID)
	}
	if record.TagValueFingerprints["env"] != "az-env-marker" ||
		record.TagValueFingerprints["owner"] != "az-owner-marker" {
		t.Fatalf("fingerprints = %#v", record.TagValueFingerprints)
	}
}

// TestPostgresCloudTagEvidenceLoaderRequiresScopeAndGeneration proves the loader
// rejects blank scope or generation rather than scanning the whole table.
func TestPostgresCloudTagEvidenceLoaderRequiresScopeAndGeneration(t *testing.T) {
	t.Parallel()

	loader := PostgresCloudTagEvidenceLoader{DB: &fakeExecQueryer{}}
	if _, err := loader.LoadCloudTagEvidence(context.Background(), "", "gen-1"); err == nil {
		t.Fatal("blank scope: error = nil, want non-nil")
	}
	if _, err := loader.LoadCloudTagEvidence(context.Background(), "scope-1", ""); err == nil {
		t.Fatal("blank generation: error = nil, want non-nil")
	}
}
