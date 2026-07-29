// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestPostgresCloudInventoryEvidenceLoaderExtractsPerProviderAccountID is the
// #5238 per-provider regression: the loader must read each provider's own raw
// account/tenant identifier straight from the source fact's required identity
// field -- aws_resource.account_id, gcp_cloud_resource.project_id,
// azure_cloud_resource.subscription_id (sdk/go/factschema/{aws,gcp,azure}/v1)
// -- into CloudInventoryRecord.AccountID. This is the field the shared
// admission path (go/internal/reducer/cloud_inventory_admission.go) persists
// onto the canonical reducer_cloud_resource_identity payload as a uniform
// "account_id" key, which the readback's account_id/project_id/subscription_id
// selectors filter directly (go/internal/query/cloud_inventory_read_model.go).
// Before this fix GCP and Azure had no path to that value at all: the readback
// compared the caller's alias value against the derived, opaque scope_id
// instead, which never equals the raw project/subscription number.
func TestPostgresCloudInventoryEvidenceLoaderExtractsPerProviderAccountID(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "cloud:tenant-1"
		generationID = "gen-1"
	)
	awsARN := "arn:aws:s3:::managed-bucket"
	gcpName := "//compute.googleapis.com/projects/p/zones/z/instances/i"
	azureID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.AWSResourceFactKind, awsARN, []byte(`{
					"arn":"` + awsARN + `",
					"resource_type":"aws_s3_bucket",
					"account_id":"111111111111"
				}`)},
				{facts.GCPCloudResourceFactKind, gcpName, []byte(`{
					"full_resource_name":"` + gcpName + `",
					"asset_type":"compute.googleapis.com/Instance",
					"project_id":"synthetic-gcp-project"
				}`)},
				{facts.AzureCloudResourceFactKind, azureID, []byte(`{
					"arm_resource_id":"` + azureID + `",
					"resource_type":"microsoft.compute/virtualmachines",
					"subscription_id":"11111111-2222-3333-4444-555555555555"
				}`)},
			}},
		},
	}

	loader := PostgresCloudInventoryEvidenceLoader{DB: db}
	records, err := loader.LoadCloudInventoryEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 3; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}

	byProvider := make(map[string]reducer.CloudInventoryRecord, len(records))
	for _, record := range records {
		byProvider[record.Provider] = record
	}

	if got, want := byProvider["aws"].AccountID, "111111111111"; got != want {
		t.Fatalf("aws AccountID = %q, want %q", got, want)
	}
	if got, want := byProvider["gcp"].AccountID, "synthetic-gcp-project"; got != want {
		t.Fatalf("gcp AccountID = %q, want %q", got, want)
	}
	if got, want := byProvider["azure"].AccountID, "11111111-2222-3333-4444-555555555555"; got != want {
		t.Fatalf("azure AccountID = %q, want %q", got, want)
	}
}

// TestPostgresCloudInventoryEvidenceLoaderMissingAccountIDYieldsEmptyString
// proves a record whose source fact is missing (or has a blank) account
// identity decodes to an empty AccountID rather than erroring or fabricating a
// value -- the admission path already treats an unresolved identity field as
// absent evidence elsewhere in this loader (see coerceJSONString(nil) == "").
func TestPostgresCloudInventoryEvidenceLoaderMissingAccountIDYieldsEmptyString(t *testing.T) {
	t.Parallel()

	awsARN := "arn:aws:s3:::no-account-field"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.AWSResourceFactKind, awsARN, []byte(`{
					"arn":"` + awsARN + `",
					"resource_type":"aws_s3_bucket"
				}`)},
			}},
		},
	}

	loader := PostgresCloudInventoryEvidenceLoader{DB: db}
	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := records[0].AccountID, ""; got != want {
		t.Fatalf("AccountID = %q, want empty string for a payload with no account_id key", got)
	}
}
