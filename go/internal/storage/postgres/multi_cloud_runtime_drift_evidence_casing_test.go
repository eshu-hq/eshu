// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderAzureStateCaseInsensitiveJoin
// proves the Azure-side state-to-observed join is case-insensitive while AWS and
// GCP stay case-significant. Azure ARM ids are case-insensitive per Azure and the
// shared cloud_resource_uid lower-cases them before hashing, so a
// terraform_state_resource whose attributes.id differs only in casing from the
// observed arm_resource_id must still join and classify as unmanaged (cloud +
// state, no config) rather than read as an orphaned cloud resource. AWS ARNs and
// GCP full resource names are case-significant: a state row whose identity
// differs only in casing must NOT collapse onto a distinct observed identity, so
// those resources stay orphaned.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderAzureStateCaseInsensitiveJoin(t *testing.T) {
	t.Parallel()

	const (
		scopeID       = "cloud:tenant-1"
		generationID  = "gen-1"
		stateScopeID  = "state_snapshot:s3:hash-xyz"
		stateGen      = "state-gen-1"
		configScopeID = "repository:infra"
		configGen     = "config-gen-1"
	)
	// Azure: observed arm_resource_id and the Terraform state attributes.id name
	// the same resource but differ only in casing. They resolve to one uid.
	azureObservedID := "/subscriptions/Sub-1/resourceGroups/RG/providers/Microsoft.Compute/virtualMachines/Managed"
	azureStateID := "/subscriptions/sub-1/resourcegroups/rg/providers/microsoft.compute/virtualmachines/managed"
	// AWS and GCP: observed and state identities differ only in casing. ARNs and
	// full resource names are case-significant, so these resolve to DISTINCT uids
	// and must not join.
	awsObservedARN := "arn:aws:s3:::Example-Bucket"
	awsStateARN := "arn:aws:s3:::example-bucket"
	gcpObservedName := "//compute.googleapis.com/projects/p/zones/z/instances/Instance-A"
	gcpStateName := "//compute.googleapis.com/projects/p/zones/z/instances/instance-a"

	azureUID := mustResolveUID(t, cloudinventory.ProviderAzure, azureObservedID)
	if other := mustResolveUID(t, cloudinventory.ProviderAzure, azureStateID); other != azureUID {
		t.Fatalf("azure casing produced distinct uids %q != %q", azureUID, other)
	}
	awsUID := mustResolveUID(t, cloudinventory.ProviderAWS, awsObservedARN)
	if other := mustResolveUID(t, cloudinventory.ProviderAWS, awsStateARN); other == awsUID {
		t.Fatalf("aws casing collapsed distinct uids %q", awsUID)
	}
	gcpUID := mustResolveUID(t, cloudinventory.ProviderGCP, gcpObservedName)
	if other := mustResolveUID(t, cloudinventory.ProviderGCP, gcpStateName); other == gcpUID {
		t.Fatalf("gcp casing collapsed distinct uids %q", gcpUID)
	}

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// Observed inventory facts carry the mixed-case identities.
			{rows: [][]any{
				{facts.AWSResourceFactKind, awsObservedARN, []byte(`{"arn":"` + awsObservedARN + `","resource_type":"aws_s3_bucket"}`)},
				{facts.GCPCloudResourceFactKind, gcpObservedName, []byte(`{"full_resource_name":"` + gcpObservedName + `","asset_type":"compute.googleapis.com/Instance"}`)},
				{facts.AzureCloudResourceFactKind, azureObservedID, []byte(`{"arm_resource_id":"` + azureObservedID + `","resource_type":"microsoft.compute/virtualmachines"}`)},
			}},
			// Active Terraform state rows carry the differently-cased identities.
			// In production the SQL surfaces the Azure row via a case-folded
			// /subscriptions/ match and the AWS/GCP rows only on an exact match;
			// the fake returns all three so the Go fold proves it joins Azure and
			// drops the case-mismatched AWS/GCP rows rather than collapsing them.
			{rows: [][]any{
				{stateScopeID, stateGen, "aws_s3_bucket.example", awsStateARN, fixtureMultiCloudStatePayload(
					"aws_s3_bucket.example", "aws_s3_bucket", "arn", awsStateARN,
				)},
				{stateScopeID, stateGen, "google_compute_instance.a", gcpStateName, fixtureMultiCloudStatePayload(
					"google_compute_instance.a", "google_compute_instance", "self_link", gcpStateName,
				)},
				{stateScopeID, stateGen, "azurerm_virtual_machine.managed", azureStateID, fixtureMultiCloudStatePayload(
					"azurerm_virtual_machine.managed", "azurerm_virtual_machine", "id", azureStateID,
				)},
			}},
			// Module-prefix walk for the Azure state backend owner.
			{rows: [][]any{}},
			// Config-side terraform_resources rows: empty, so the Azure address is
			// not declared and the joined resource reads as unmanaged.
			{rows: [][]any{{fixtureConfigResourcesArray()}}},
		},
	}
	resolver := &stubAWSRuntimeDriftConfigResolver{
		anchors: map[string]tfstatebackend.CommitAnchor{
			"s3:hash-xyz": {
				RepoID:      "infra",
				ScopeID:     configScopeID,
				CommitID:    configGen,
				BackendKind: "s3",
				LocatorHash: "hash-xyz",
			},
		},
	}
	loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{DB: db, ConfigResolver: resolver}

	rows, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadMultiCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	for _, row := range rows {
		uid, ok := row.ResolveUID()
		if !ok {
			t.Fatalf("row uid did not resolve: %#v", row)
		}
		switch uid {
		case azureUID:
			if row.State == nil {
				t.Fatalf("azure row did not join state despite case-only id difference: %#v", row)
			}
			if got, want := row.State.Address, "azurerm_virtual_machine.managed"; got != want {
				t.Fatalf("azure state address = %q, want %q", got, want)
			}
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindUnmanagedCloudResource {
				t.Fatalf("azure finding = %q, want unmanaged", got)
			}
		case awsUID:
			if row.State != nil {
				t.Fatalf("aws case-only difference must not join state: %#v", row)
			}
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindOrphanedCloudResource {
				t.Fatalf("aws finding = %q, want orphaned", got)
			}
		case gcpUID:
			if row.State != nil {
				t.Fatalf("gcp case-only difference must not join state: %#v", row)
			}
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindOrphanedCloudResource {
				t.Fatalf("gcp finding = %q, want orphaned", got)
			}
		default:
			t.Fatalf("unexpected row uid %q", uid)
		}
	}

	// The state join SQL must case-fold only the Azure /subscriptions/ keyspace.
	stateJoin := db.queries[1]
	if !strings.Contains(stateJoin.query, "lower(") || !strings.Contains(stateJoin.query, "/subscriptions/") {
		t.Fatalf("state join SQL does not case-fold the Azure keyspace:\n%s", stateJoin.query)
	}
}
