// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

// TestAdmitCloudInventoryRecordsCarriesAccountIDPerProvider is the #5238
// per-provider regression: admitCloudInventoryRecords must fold each record's
// AccountID (the raw provider account/project/subscription identifier the
// loader reads straight from the source fact -- see
// go/internal/storage/postgres/cloud_inventory_evidence.go) onto the admitted
// canonical resource for AWS, GCP, and Azure alike, not just AWS.
func TestAdmitCloudInventoryRecordsCarriesAccountIDPerProvider(t *testing.T) {
	t.Parallel()

	records := []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAWS,
			FactKind:     "aws_resource",
			RawIdentity:  "arn:aws:s3:::managed-bucket",
			ResourceType: "aws_s3_bucket",
			AccountID:    "111111111111",
			SourceLayer:  SourceLayerObserved,
		},
		{
			Provider:     cloudinventory.ProviderGCP,
			FactKind:     "gcp_cloud_resource",
			RawIdentity:  "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
			ResourceType: "compute.googleapis.com/Instance",
			AccountID:    "eshu-prod",
			SourceLayer:  SourceLayerObserved,
		},
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1",
			ResourceType: "Microsoft.Compute/virtualMachines",
			AccountID:    "0000",
			SourceLayer:  SourceLayerObserved,
		},
	}

	resources, summary := admitCloudInventoryRecords(records)
	if got, want := summary.Admitted, 3; got != want {
		t.Fatalf("summary.Admitted = %d, want %d", got, want)
	}
	if got, want := len(resources), 3; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}

	byProvider := make(map[string]AdmittedCloudResource, len(resources))
	for _, resource := range resources {
		byProvider[resource.Provider] = resource
	}
	if got, want := byProvider[cloudinventory.ProviderAWS].AccountID, "111111111111"; got != want {
		t.Fatalf("aws AccountID = %q, want %q", got, want)
	}
	if got, want := byProvider[cloudinventory.ProviderGCP].AccountID, "eshu-prod"; got != want {
		t.Fatalf("gcp AccountID = %q, want %q", got, want)
	}
	if got, want := byProvider[cloudinventory.ProviderAzure].AccountID, "0000"; got != want {
		t.Fatalf("azure AccountID = %q, want %q", got, want)
	}
}

// TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider proves
// the persisted canonical payload (what cloud_inventory_admission_writer.go
// writes and what go/internal/query/cloud_inventory_read_model.go's
// account_id/project_id/subscription_id selectors read back) carries the
// resolved account identifier under one uniform "account_id" key regardless
// of which provider admitted the resource.
func TestCloudInventoryAdmissionPayloadIncludesAccountIDForEveryProvider(t *testing.T) {
	t.Parallel()

	write := CloudInventoryAdmissionWrite{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "multi-cloud",
	}
	cases := []struct {
		provider  string
		accountID string
	}{
		{cloudinventory.ProviderAWS, "111111111111"},
		{cloudinventory.ProviderGCP, "eshu-prod"},
		{cloudinventory.ProviderAzure, "0000"},
		// #5238 follow-up (account-alias rollout-gap warning signal, review
		// finding): a GCP org- or folder-level asset genuinely has no
		// derivable project segment, so its resolved AccountID is blank --
		// but cloudInventoryRolloutGapWarningFlags' pre-rollout probe
		// distinguishes "no account_id key at all" (a pre-fix payload) from
		// "account_id key present but empty" (this genuine, current,
		// org-level shape) via Postgres jsonb `?` key-EXISTENCE, never a
		// value comparison. That distinction is only correct because
		// cloudInventoryAdmissionBasePayload writes "account_id" into the
		// payload map UNCONDITIONALLY -- encoding/json always emits a map
		// key regardless of an empty value; there is no implicit omitempty
		// for maps. This case pins that invariant directly: the key-presence
		// assertion below must fail loudly if a future refactor (a typed
		// struct with `json:"account_id,omitempty"`, or an
		// `if resource.AccountID != ""` guard) ever makes the key vanish for
		// a blank value -- which would make every GCP org-level asset in
		// every deployment misreported as a pre-rollout gap, permanently,
		// with no test catching it otherwise.
		{cloudinventory.ProviderGCP, ""},
	}
	for _, tc := range cases {
		resource := AdmittedCloudResource{
			CloudResourceUID: "cloud_resource:" + tc.provider + "-1",
			Provider:         tc.provider,
			RawIdentity:      "raw-identity",
			ResourceType:     "resource-type",
			AccountID:        tc.accountID,
			ManagementOrigin: ManagementOriginObserved,
		}
		payload := cloudInventoryAdmissionPayload(write, resource)
		if got, want := payload["account_id"], tc.accountID; got != want {
			t.Fatalf("%s payload[account_id] = %#v, want %#v", tc.provider, got, want)
		}
		// Round-trip through JSON to prove the field survives the exact
		// marshal path the writer uses (json.Marshal(cloudInventoryAdmissionPayload(...))
		// in WriteCloudInventoryAdmission), not just the in-memory map.
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("%s marshal payload: %v", tc.provider, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("%s unmarshal payload: %v", tc.provider, err)
		}
		if got, want := decoded["account_id"], tc.accountID; got != want {
			t.Fatalf("%s round-tripped payload[account_id] = %#v, want %#v", tc.provider, got, want)
		}
		// The key-presence assertion the rollout-gap signal's correctness
		// depends on: a blank value must still round-trip as a PRESENT key,
		// not an absent one (json.Marshal on a map never applies omitempty).
		if _, ok := decoded["account_id"]; !ok {
			t.Fatalf(
				"%s round-tripped payload has NO account_id key at all for accountID=%q; "+
					"the key must always be present, even when blank, or cloudInventoryRolloutGapWarningFlags "+
					"cannot distinguish a genuine org-level asset from a pre-fix row",
				tc.provider, tc.accountID,
			)
		}
	}
}

// TestCloudInventoryAdmissionEndToEndCarriesAccountIDToWriter is the
// end-to-end fold-through-handler proof: a GCP record's AccountID reaches the
// writer's persisted admission write unchanged, exercising the same handler
// path production reducer workers run (context is unused by the fixture
// writer/loader but required by the Handle signature).
func TestCloudInventoryAdmissionEndToEndCarriesAccountIDToWriter(t *testing.T) {
	t.Parallel()

	inst, _ := newCloudInventoryInstruments(t)
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderGCP,
			FactKind:     "gcp_cloud_resource",
			RawIdentity:  "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
			ResourceType: "compute.googleapis.com/Instance",
			AccountID:    "eshu-prod",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader: loader,
		Writer:         writer,
		Instruments:    inst,
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.writes) != 1 || len(writer.writes[0].Resources) != 1 {
		t.Fatalf("writer.writes = %#v, want exactly one write with one resource", writer.writes)
	}
	if got, want := writer.writes[0].Resources[0].AccountID, "eshu-prod"; got != want {
		t.Fatalf("written resource AccountID = %q, want %q", got, want)
	}
}
