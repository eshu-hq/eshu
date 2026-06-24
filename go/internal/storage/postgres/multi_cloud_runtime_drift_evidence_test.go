// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// fixtureMultiCloudStatePayload renders a terraform_state_resource payload whose
// attributes carry the provider-native identity under the same key the shared
// cloud_resource_uid keyspace expects for that provider: AWS arn, Azure id (ARM
// id), GCP self_link/id. The loader must resolve these into the same uid as the
// observed provider inventory fact so observed and Terraform join on one key.
func fixtureMultiCloudStatePayload(address, resourceType, identityKey, identity string) []byte {
	return []byte(`{
		"address":"` + address + `",
		"type":"` + resourceType + `",
		"attributes":{"` + identityKey + `":"` + identity + `"}
	}`)
}

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderJoinsObservedStateConfigByUID
// proves the loader keys observed cloud resources, Terraform state, and Terraform
// config on one canonical cloud_resource_uid across AWS, GCP, and Azure. A
// cloud-only resource is orphaned, a cloud+state-without-config resource is
// unmanaged, and a cloud+state+config resource converges (no finding). The
// Terraform state identity must resolve to the same uid as the observed fact.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderJoinsObservedStateConfigByUID(t *testing.T) {
	t.Parallel()

	const (
		scopeID       = "cloud:tenant-1"
		generationID  = "gen-1"
		stateScopeID  = "state_snapshot:s3:hash-xyz"
		stateGen      = "state-gen-1"
		configScopeID = "repository:infra"
		configGen     = "config-gen-1"
	)
	// AWS orphan: observed only.
	orphanARN := "arn:aws:iam::123456789012:role/orphan"
	// GCP unmanaged: observed + state, no config.
	gcpName := "//compute.googleapis.com/projects/p/zones/z/instances/unmanaged"
	// Azure managed: observed + state + config (converges, no finding).
	azureID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/managed"

	orphanUID := mustResolveUID(t, cloudinventory.ProviderAWS, orphanARN)
	gcpUID := mustResolveUID(t, cloudinventory.ProviderGCP, gcpName)
	azureUID := mustResolveUID(t, cloudinventory.ProviderAzure, azureID)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// Observed inventory facts for the three providers.
			{rows: [][]any{
				{facts.AWSResourceFactKind, orphanARN, []byte(`{"arn":"` + orphanARN + `","resource_type":"aws_iam_role","tags":{"Environment":"prod"}}`)},
				{facts.GCPCloudResourceFactKind, gcpName, []byte(`{"full_resource_name":"` + gcpName + `","asset_type":"compute.googleapis.com/Instance"}`)},
				{facts.AzureCloudResourceFactKind, azureID, []byte(`{"arm_resource_id":"` + azureID + `","resource_type":"microsoft.compute/virtualmachines"}`)},
			}},
			// Active Terraform state resources: GCP unmanaged + Azure managed.
			// The AWS orphan has no state row.
			{rows: [][]any{
				{stateScopeID, stateGen, "google_compute_instance.unmanaged", gcpName, fixtureMultiCloudStatePayload(
					"google_compute_instance.unmanaged", "google_compute_instance", "self_link", gcpName,
				)},
				{stateScopeID, stateGen, "azurerm_virtual_machine.managed", azureID, fixtureMultiCloudStatePayload(
					"azurerm_virtual_machine.managed", "azurerm_virtual_machine", "id", azureID,
				)},
			}},
			// Module-prefix walk for configScopeID/configGen.
			{rows: [][]any{}},
			// Config-side terraform_resources rows. Only the Azure managed state
			// address is declared; GCP stays cloud+state without config.
			{rows: [][]any{{
				fixtureConfigResourcesArray(fixtureConfigParserRow("azurerm_virtual_machine", "managed")),
			}}},
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
	loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{
		DB:             db,
		ConfigResolver: resolver,
	}

	rows, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadMultiCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}

	byUID := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		uid, ok := row.ResolveUID()
		if !ok {
			t.Fatalf("row uid did not resolve: %#v", row)
		}
		byUID[uid] = struct{}{}
		switch uid {
		case orphanUID:
			if row.Provider != cloudinventory.ProviderAWS {
				t.Fatalf("orphan provider = %q, want aws", row.Provider)
			}
			if row.Cloud == nil || row.State != nil || row.Config != nil {
				t.Fatalf("orphan row = %#v, want cloud only", row)
			}
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindOrphanedCloudResource {
				t.Fatalf("orphan finding = %q, want orphaned", got)
			}
		case gcpUID:
			if row.Provider != cloudinventory.ProviderGCP {
				t.Fatalf("gcp provider = %q, want gcp", row.Provider)
			}
			if row.Cloud == nil || row.State == nil || row.Config != nil {
				t.Fatalf("gcp row = %#v, want cloud+state only", row)
			}
			if got, want := row.State.Address, "google_compute_instance.unmanaged"; got != want {
				t.Fatalf("gcp state address = %q, want %q", got, want)
			}
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindUnmanagedCloudResource {
				t.Fatalf("gcp finding = %q, want unmanaged", got)
			}
		case azureUID:
			if row.Provider != cloudinventory.ProviderAzure {
				t.Fatalf("azure provider = %q, want azure", row.Provider)
			}
			if row.Cloud == nil || row.State == nil || row.Config == nil {
				t.Fatalf("azure row = %#v, want cloud+state+config", row)
			}
			if got := row.EffectiveFindingKind(); got != "" {
				t.Fatalf("azure finding = %q, want empty (converged)", got)
			}
		default:
			t.Fatalf("unexpected row uid %q", uid)
		}
	}
	for _, uid := range []string{orphanUID, gcpUID, azureUID} {
		if _, ok := byUID[uid]; !ok {
			t.Fatalf("missing row for uid %q", uid)
		}
	}

	// The observed scan must be bound to scope+generation, and the state join
	// must be bounded by the observed raw-identity allowlist so a stale
	// generation cannot widen the join.
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	observed := db.queries[0]
	if !strings.Contains(observed.query, "scope_id = $1") || !strings.Contains(observed.query, "generation_id = $2") {
		t.Fatalf("observed scan not bound to scope+generation:\n%s", observed.query)
	}
	stateJoin := db.queries[1]
	if !strings.Contains(stateJoin.query, "jsonb_array_elements_text($1::jsonb)") {
		t.Fatalf("state join is not bounded by raw-identity allowlist:\n%s", stateJoin.query)
	}
}

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderMarksUnknownAndAmbiguous proves
// the loader surfaces unresolved config ownership as unknown and conflicting
// Terraform state owners for one uid as ambiguous, never promoting them to
// managed.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderMarksUnknownAndAmbiguous(t *testing.T) {
	t.Parallel()

	const (
		scopeID          = "cloud:tenant-1"
		generationID     = "gen-1"
		unknownScopeID   = "state_snapshot:s3:missing-owner"
		ambiguousScopeID = "state_snapshot:s3:ambiguous-owner"
		stateGen         = "state-gen-1"
	)
	unknownName := "//compute.googleapis.com/projects/p/zones/z/instances/unknown"
	ambiguousID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/ambiguous"

	unknownUID := mustResolveUID(t, cloudinventory.ProviderGCP, unknownName)
	ambiguousUID := mustResolveUID(t, cloudinventory.ProviderAzure, ambiguousID)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.GCPCloudResourceFactKind, unknownName, []byte(`{"full_resource_name":"` + unknownName + `","asset_type":"compute.googleapis.com/Instance"}`)},
				{facts.AzureCloudResourceFactKind, ambiguousID, []byte(`{"arm_resource_id":"` + ambiguousID + `","resource_type":"microsoft.storage/storageaccounts"}`)},
			}},
			{rows: [][]any{
				{unknownScopeID, stateGen, "google_compute_instance.unknown", unknownName, fixtureMultiCloudStatePayload(
					"google_compute_instance.unknown", "google_compute_instance", "self_link", unknownName,
				)},
				{ambiguousScopeID, stateGen, "azurerm_storage_account.ambiguous_a", ambiguousID, fixtureMultiCloudStatePayload(
					"azurerm_storage_account.ambiguous_a", "azurerm_storage_account", "id", ambiguousID,
				)},
				{ambiguousScopeID, stateGen, "azurerm_storage_account.ambiguous_b", ambiguousID, fixtureMultiCloudStatePayload(
					"azurerm_storage_account.ambiguous_b", "azurerm_storage_account", "id", ambiguousID,
				)},
			}},
		},
	}
	resolver := &stubAWSRuntimeDriftConfigResolver{
		errByBackend: map[string]error{
			"s3:missing-owner": tfstatebackend.ErrNoConfigRepoOwnsBackend,
		},
	}
	loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{
		DB:             db,
		ConfigResolver: resolver,
	}

	rows, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadMultiCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	for _, row := range rows {
		uid, _ := row.ResolveUID()
		switch uid {
		case unknownUID:
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindUnknownCloudResource {
				t.Fatalf("unknown finding = %q, want unknown", got)
			}
			if row.ManagementStatus != cloudruntime.ManagementStatusUnknown {
				t.Fatalf("unknown management status = %q", row.ManagementStatus)
			}
		case ambiguousUID:
			if got := row.EffectiveFindingKind(); got != cloudruntime.FindingKindAmbiguousCloudResource {
				t.Fatalf("ambiguous finding = %q, want ambiguous", got)
			}
			if row.Config != nil {
				t.Fatalf("ambiguous row must not carry config: %#v", row)
			}
		default:
			t.Fatalf("unexpected uid %q", uid)
		}
	}
}

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderSkipsUnresolvableIdentities
// proves a blank or malformed provider identity that cannot key into the shared
// uid keyspace is dropped at load time rather than fabricated into a finding.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderSkipsUnresolvableIdentities(t *testing.T) {
	t.Parallel()

	okName := "//compute.googleapis.com/projects/p/zones/z/instances/ok"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				// Malformed GCP identity: not a // full resource name -> ambiguous,
				// not keyable.
				{facts.GCPCloudResourceFactKind, "compute/Instance/bad", []byte(`{"full_resource_name":"compute/Instance/bad","asset_type":"compute.googleapis.com/Instance"}`)},
				{facts.GCPCloudResourceFactKind, okName, []byte(`{"full_resource_name":"` + okName + `","asset_type":"compute.googleapis.com/Instance"}`)},
			}},
			// State scan: only the ok identity is in the allowlist.
			{rows: [][]any{}},
		},
	}
	loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadMultiCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d (only keyable identities)", got, want)
	}
	if uid, _ := rows[0].ResolveUID(); uid != mustResolveUID(t, cloudinventory.ProviderGCP, okName) {
		t.Fatalf("surviving row uid = %q", uid)
	}
}

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderEmptyObservedReturnsNoRows
// proves the loader short-circuits the state/config scans when no observed
// resources resolve, so an empty generation does no extra work.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderEmptyObservedReturnsNoRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadMultiCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d (no state/config scan on empty observed)", got, want)
	}
}

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderBlankScopeAndGeneration proves
// the loader rejects blank scope or generation before issuing any query.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderBlankScopeAndGeneration(t *testing.T) {
	t.Parallel()

	loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{DB: &fakeExecQueryer{}}
	if _, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), "  ", "gen"); err == nil {
		t.Fatal("blank scope must error")
	}
	if _, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), "scope", "  "); err == nil {
		t.Fatal("blank generation must error")
	}

	nilLoader := PostgresMultiCloudRuntimeDriftEvidenceLoader{}
	if _, err := nilLoader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), "scope", "gen"); err == nil {
		t.Fatal("nil DB must error")
	}
}

// TestPostgresMultiCloudRuntimeDriftEvidenceLoaderRaceSafe proves repeated
// concurrent loads over independent fakes produce identical, stable row sets so
// the loader is safe to run under concurrent reducer workers.
func TestPostgresMultiCloudRuntimeDriftEvidenceLoaderRaceSafe(t *testing.T) {
	t.Parallel()

	gcpName := "//compute.googleapis.com/projects/p/zones/z/instances/r"
	gcpUID := mustResolveUID(t, cloudinventory.ProviderGCP, gcpName)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := &fakeExecQueryer{
				queryResponses: []queueFakeRows{
					{rows: [][]any{
						{facts.GCPCloudResourceFactKind, gcpName, []byte(`{"full_resource_name":"` + gcpName + `","asset_type":"compute.googleapis.com/Instance"}`)},
					}},
					{rows: [][]any{}},
				},
			}
			loader := PostgresMultiCloudRuntimeDriftEvidenceLoader{DB: db}
			rows, err := loader.LoadMultiCloudRuntimeDriftEvidence(context.Background(), "cloud:tenant-1", "gen-1")
			if err != nil {
				t.Errorf("LoadMultiCloudRuntimeDriftEvidence() error = %v", err)
				return
			}
			if len(rows) != 1 {
				t.Errorf("len(rows) = %d, want 1", len(rows))
				return
			}
			if uid, _ := rows[0].ResolveUID(); uid != gcpUID {
				t.Errorf("row uid = %q, want %q", uid, gcpUID)
			}
		}()
	}
	wg.Wait()
}

func mustResolveUID(t *testing.T, provider, raw string) string {
	t.Helper()
	resolution := cloudinventory.ResolveProviderIdentity(provider, raw)
	if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
		t.Fatalf("ResolveProviderIdentity(%q,%q) outcome = %q, want admitted", provider, raw, resolution.Outcome)
	}
	return resolution.CloudResourceUID
}
