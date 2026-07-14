// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
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
					"normalized_resource_id":"` + armID + `",
					"resource_type":"Microsoft.Compute/virtualMachines",
					"tag_value_fingerprints":{"env":"az-env-marker","owner":"az-owner-marker"}
				}`)},
				{facts.AzureTagObservationFactKind, noTagsID, []byte(`{
					"arm_resource_id":"` + noTagsID + `",
					"normalized_resource_id":"` + noTagsID + `",
					"resource_type":"Microsoft.Storage/storageAccounts"
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

// TestPostgresCloudTagEvidenceLoaderMapsGCPTagFacts proves GCP tag evidence
// attaches through the same shared admission shape as Azure while preserving
// the provider full resource name as source identity.
func TestPostgresCloudTagEvidenceLoaderMapsGCPTagFacts(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "gcp:project:proj-1"
		generationID = "gen-1"
	)
	fullResourceName := "//compute.googleapis.com/projects/proj-1/zones/us-central1-a/instances/vm-1"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.GCPTagObservationFactKind, fullResourceName, []byte(`{
					"full_resource_name":"` + fullResourceName + `",
					"asset_type":"compute.googleapis.com/Instance",
					"tag_value_fingerprints":{"env":"gcp-env-marker","owner":"gcp-owner-marker"}
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
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	record := records[0]
	if record.Provider != cloudinventory.ProviderGCP {
		t.Fatalf("provider = %q, want %q", record.Provider, cloudinventory.ProviderGCP)
	}
	if record.RawIdentity != fullResourceName {
		t.Fatalf("raw identity = %q, want %q", record.RawIdentity, fullResourceName)
	}
	if record.TagValueFingerprints["env"] != "gcp-env-marker" ||
		record.TagValueFingerprints["owner"] != "gcp-owner-marker" {
		t.Fatalf("fingerprints = %#v", record.TagValueFingerprints)
	}
}

// TestPostgresCloudTagEvidenceLoaderDropsNonStringFingerprintValues proves the
// loader fails closed, rather than silently coercing, when a tag-evidence
// fact's tag_value_fingerprints values do not match the collector's
// fingerprint-marker contract (a JSON string per key). Before the typed
// factschema decode seam landed, cloudTagEvidenceRecordFromRow read this field
// through a raw map[string]any lookup and coerceJSONString, which formats ANY
// JSON value (a number, in this fixture) into a string instead of rejecting
// it — so a malformed or renamed-shape payload silently attached a coerced,
// never-actually-fingerprinted marker as if it were real tag evidence. The
// typed azurev1.TagObservation/gcpv1.TagObservation decode declares
// TagValueFingerprints as map[string]string, so a non-string JSON value now
// fails decode and the row is dropped and logged like any other undecodable
// fact, never silently attached.
func TestPostgresCloudTagEvidenceLoaderDropsNonStringFingerprintValues(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "cloud:tenant-1"
		generationID = "gen-1"
	)
	armID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-bad-tags"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.AzureTagObservationFactKind, armID, []byte(`{
					"arm_resource_id":"` + armID + `",
					"normalized_resource_id":"` + armID + `",
					"resource_type":"Microsoft.Compute/virtualMachines",
					"tag_value_fingerprints":{"build_number":42}
				}`)},
			}},
		},
	}

	loader := PostgresCloudTagEvidenceLoader{DB: db}
	records, err := loader.LoadCloudTagEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadCloudTagEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 0; got != want {
		t.Fatalf("len(records) = %d, want %d (non-string fingerprint value must be dropped, not coerced): %+v", got, want, records)
	}
}

// TestCloudTagEvidenceQueryIncludesGCPTagFacts proves the SQL allowlist and raw
// identity projection stay in lockstep with the Go fact-kind mapping.
func TestCloudTagEvidenceQueryIncludesGCPTagFacts(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"gcp_tag_observation",
		"full_resource_name",
		"azure_tag_observation",
		"arm_resource_id",
	} {
		if !strings.Contains(listCloudTagEvidenceForGenerationQuery, want) {
			t.Fatalf("listCloudTagEvidenceForGenerationQuery missing %q", want)
		}
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
