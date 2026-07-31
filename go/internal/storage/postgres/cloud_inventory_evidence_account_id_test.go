// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
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
					"account_id":"111111111111",
					"resource_id":"managed-bucket",
					"region":"us-east-1"
				}`)},
				{facts.GCPCloudResourceFactKind, gcpName, []byte(`{
					"full_resource_name":"` + gcpName + `",
					"asset_type":"compute.googleapis.com/Instance",
					"project_id":"synthetic-gcp-project"
				}`)},
				{facts.AzureCloudResourceFactKind, azureID, []byte(`{
					"arm_resource_id":"` + azureID + `",
					"resource_type":"microsoft.compute/virtualmachines",
					"subscription_id":"11111111-2222-3333-4444-555555555555",
					"location":"eastus"
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

// TestPostgresCloudInventoryEvidenceLoaderRejectsMalformedRequiredIdentityForAWSAndAzure
// is the #5881 review follow-up: AWS account_id and Azure subscription_id are
// REQUIRED, non-optional identity fields in their typed factschema contract
// (sdk/go/factschema/{aws,azure}/v1), unlike GCP's genuinely optional
// project_id. A fact missing the field entirely, or carrying a non-string
// JSON value where the typed struct expects a string, must be REJECTED --
// dropped from the returned records, exactly like any other malformed row
// this loader already drops -- by routing identity resolution through
// factschema.DecodeAWSResource/DecodeAzureCloudResource. The prior raw
// map[string]any + coerceJSONString lookup would have silently tolerated
// every one of these shapes instead (nil -> "", a JSON number -> its
// fmt.Sprint form, a bool -> "true"/"false"), which would undermine the
// #5238 rollout-gap signal's correctness argument: it depends on AWS
// account_id and Azure subscription_id being structurally impossible to
// admit blank (cloud_inventory_rollout_signal.go).
func TestPostgresCloudInventoryEvidenceLoaderRejectsMalformedRequiredIdentityForAWSAndAzure(t *testing.T) {
	t.Parallel()

	awsMissingAccountID := "arn:aws:s3:::missing-account-id"
	awsNonStringAccountID := "arn:aws:s3:::non-string-account-id"
	azureMissingSubscriptionID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/missing-sub"
	azureNonStringSubscriptionID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/non-string-sub"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				// account_id entirely absent; every OTHER required field present, so
				// only the missing identity field can explain the rejection.
				{facts.AWSResourceFactKind, awsMissingAccountID, []byte(`{
					"arn":"` + awsMissingAccountID + `",
					"resource_type":"aws_s3_bucket",
					"resource_id":"missing-account-id",
					"region":"us-east-1"
				}`)},
				// account_id present but a JSON number, not a string. The old
				// coerceJSONString(123456789012) would have returned "123456789012",
				// silently coercing a malformed type into a plausible-looking value.
				{facts.AWSResourceFactKind, awsNonStringAccountID, []byte(`{
					"arn":"` + awsNonStringAccountID + `",
					"resource_type":"aws_s3_bucket",
					"resource_id":"non-string-account-id",
					"region":"us-east-1",
					"account_id":123456789012
				}`)},
				// subscription_id entirely absent.
				{facts.AzureCloudResourceFactKind, azureMissingSubscriptionID, []byte(`{
					"arm_resource_id":"` + azureMissingSubscriptionID + `",
					"resource_type":"microsoft.compute/virtualmachines",
					"location":"eastus"
				}`)},
				// subscription_id present but a JSON bool, not a string. The old
				// coerceJSONString(true) would have returned "true".
				{facts.AzureCloudResourceFactKind, azureNonStringSubscriptionID, []byte(`{
					"arm_resource_id":"` + azureNonStringSubscriptionID + `",
					"resource_type":"microsoft.compute/virtualmachines",
					"location":"eastus",
					"subscription_id":true
				}`)},
			}},
		},
	}

	loader := PostgresCloudInventoryEvidenceLoader{DB: db}
	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 0; got != want {
		t.Fatalf("len(records) = %d, want %d (every row is malformed and must be dropped, not coerced); records = %#v", got, want, records)
	}
}

// TestGCPProjectIDFromFullResourceNameMatchesCollectorDerivation is a parity
// pin between this loader's local gcpProjectIDFromFullResourceName and the
// collector's canonical go/internal/collector/gcpcloud.ProjectIDFromFullName.
// The two are intentionally independent, small, duplicated implementations of
// the same parsing rule (this package does not import the collector package
// for this purpose, keeping the storage layer independent of collector
// internals); this test is what keeps them from silently drifting apart.
func TestGCPProjectIDFromFullResourceNameMatchesCollectorDerivation(t *testing.T) {
	t.Parallel()

	cases := []string{
		"//compute.googleapis.com/projects/p/zones/z/instances/i",
		"//cloudresourcemanager.googleapis.com/organizations/123456",
		"//cloudresourcemanager.googleapis.com/folders/789",
		"",
		"projects/only-project-segment",
		"//bigquery.googleapis.com/projects/proj-a/datasets/d/tables/t",
		// Trailing slash immediately after the project id: the id itself is
		// still a distinct segment before the trailing empty one.
		"//compute.googleapis.com/projects/trailing-project/",
		// Multiple "projects/" segments in one name: both implementations must
		// agree on which one wins (the FIRST occurrence, since both loops return
		// on first match) rather than silently diverging on the later one.
		"//storage.googleapis.com/projects/outer-project/buckets/b/projects/inner-project",
		// Empty id immediately after "projects/": a malformed name whose segment
		// right after "projects" is itself empty. Both implementations must
		// return the same (empty) string rather than one panicking or one
		// returning a different sentinel.
		"//compute.googleapis.com/projects//zones/z/instances/i",
		// Unusual casing: "Projects" (capital P) must NOT match "projects" in
		// either implementation -- both must agree on returning "" rather than
		// one being case-sensitive and the other not.
		"//compute.googleapis.com/Projects/p/zones/z/instances/i",
	}
	for _, fullResourceName := range cases {
		want := gcpcloud.ProjectIDFromFullName(fullResourceName)
		got := gcpProjectIDFromFullResourceName(fullResourceName)
		if got != want {
			t.Errorf("gcpProjectIDFromFullResourceName(%q) = %q, want %q (collector derivation)", fullResourceName, got, want)
		}
	}
}

// TestPostgresCloudInventoryEvidenceLoaderGCPBlankProjectIDDerivesFromFullResourceName
// is the #5238 GCP-specific fix: a resource whose gcp_cloud_resource payload
// carries project_id="" (the collector's documented "always emitted but may be
// empty" case -- sdk/go/factschema/gcp/v1/resource.go) but whose
// full_resource_name DOES embed a "projects/<id>" segment must still resolve a
// non-blank AccountID via the fallback, matching what the collector's own
// go/internal/collector/gcpcloud.ProjectIDFromFullName would have derived at
// emission time. Without this, a resource the collector's own derivation would
// have identified vanishes from every project_id-filtered read while still
// appearing under an unscoped provider=gcp read -- a real, closable gap, not
// the genuine org/folder exclusion the next test documents.
func TestPostgresCloudInventoryEvidenceLoaderGCPBlankProjectIDDerivesFromFullResourceName(t *testing.T) {
	t.Parallel()

	gcpName := "//compute.googleapis.com/projects/derivable-project/zones/z/instances/i"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.GCPCloudResourceFactKind, gcpName, []byte(`{
					"full_resource_name":"` + gcpName + `",
					"asset_type":"compute.googleapis.com/Instance",
					"project_id":""
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
	if got, want := records[0].AccountID, "derivable-project"; got != want {
		t.Fatalf("AccountID = %q, want %q derived from full_resource_name", got, want)
	}
}

// TestPostgresCloudInventoryEvidenceLoaderGCPBlankProjectIDWithNoDerivableSegment
// is the documented residual gap: an organization- or folder-level Cloud Asset
// Inventory asset has NO project by definition -- its full_resource_name
// carries no "projects/<id>" segment at all, so there is nothing to derive.
// AccountID correctly stays blank; this resource is genuinely absent from
// every project_id-filtered read (though still visible under an unscoped
// provider=gcp read -- proven end to end at the query layer by
// TestCloudInventoryHandlerGCPBlankProjectIDExcludedFromFilterButVisibleUnscoped
// in go/internal/query/cloud_inventory_account_alias_test.go).
func TestPostgresCloudInventoryEvidenceLoaderGCPBlankProjectIDWithNoDerivableSegment(t *testing.T) {
	t.Parallel()

	gcpName := "//cloudresourcemanager.googleapis.com/organizations/123456"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.GCPCloudResourceFactKind, gcpName, []byte(`{
					"full_resource_name":"` + gcpName + `",
					"asset_type":"cloudresourcemanager.googleapis.com/Organization",
					"project_id":""
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
		t.Fatalf("AccountID = %q, want empty string for an org-level asset with no derivable project segment", got)
	}
}
