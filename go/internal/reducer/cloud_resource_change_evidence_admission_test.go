// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

type stubCloudResourceChangeEvidenceLoader struct {
	records []CloudResourceChangeEvidenceRecord
	err     error
	calls   int
}

func (s *stubCloudResourceChangeEvidenceLoader) LoadCloudResourceChangeEvidence(
	context.Context,
	string,
	string,
) ([]CloudResourceChangeEvidenceRecord, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]CloudResourceChangeEvidenceRecord(nil), s.records...), nil
}

const azureChangeSubject = "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1"

func TestCloudInventoryAdmissionAttachesResourceChangeEvidenceToAdmittedResource(t *testing.T) {
	t.Parallel()

	changeTime := time.Date(2026, time.June, 16, 10, 30, 0, 0, time.UTC)
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  azureChangeSubject,
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	changeLoader := &stubCloudResourceChangeEvidenceLoader{records: []CloudResourceChangeEvidenceRecord{
		{
			Provider:                 cloudinventory.ProviderAzure,
			RawIdentity:              azureChangeSubject,
			EvidenceKey:              "change-stable-1",
			ChangeType:               "deleted",
			ChangeTime:               changeTime,
			Operation:                "Microsoft.Compute/virtualMachines/delete",
			ClientType:               "AzurePortal",
			ActorClass:               "user",
			ActorFingerprint:         "actor-marker",
			ChangedPropertyPaths:     []string{"properties.provisioningState"},
			ChangedPropertyTruncated: true,
			TombstoneCandidate:       true,
		},
		{
			Provider:         cloudinventory.ProviderAzure,
			RawIdentity:      azureChangeSubject,
			EvidenceKey:      "change-stable-1",
			ChangeType:       "deleted",
			ChangeTime:       changeTime,
			ActorFingerprint: "actor-marker",
		},
		{
			Provider:         cloudinventory.ProviderAzure,
			RawIdentity:      "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/ghost",
			EvidenceKey:      "change-stable-ghost",
			ChangeType:       "updated",
			ChangeTime:       changeTime,
			ActorFingerprint: "ghost-marker",
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:               loader,
		Writer:                       writer,
		ResourceChangeEvidenceLoader: changeLoader,
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if changeLoader.calls != 1 {
		t.Fatalf("change loader calls = %d, want 1", changeLoader.calls)
	}
	resources := writer.writes[0].Resources
	if len(resources) != 1 {
		t.Fatalf("admitted resources = %d, want 1 (change facts must not admit resources)", len(resources))
	}
	evidence := resources[0].ResourceChangeEvidence
	if len(evidence) != 1 {
		t.Fatalf("resource change evidence = %#v, want one attached row", evidence)
	}
	if evidence[0].ChangeType != "deleted" || !evidence[0].TombstoneCandidate {
		t.Fatalf("change evidence = %#v, want delete tombstone candidate only", evidence[0])
	}
}

func TestCloudInventoryAdmissionCapsResourceChangeEvidence(t *testing.T) {
	t.Parallel()

	changeTime := time.Date(2026, time.June, 16, 10, 30, 0, 0, time.UTC)
	records := make([]CloudResourceChangeEvidenceRecord, 0, maxCloudResourceChangeEvidencePerResource+1)
	for i := 0; i < maxCloudResourceChangeEvidencePerResource+1; i++ {
		records = append(records, CloudResourceChangeEvidenceRecord{
			Provider:         cloudinventory.ProviderAzure,
			RawIdentity:      azureChangeSubject,
			EvidenceKey:      fmt.Sprintf("change-stable-%02d", i),
			ChangeType:       "updated",
			ChangeTime:       changeTime.Add(time.Duration(i) * time.Minute),
			ActorFingerprint: "actor-marker",
		})
	}
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  azureChangeSubject,
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:               loader,
		Writer:                       writer,
		ResourceChangeEvidenceLoader: &stubCloudResourceChangeEvidenceLoader{records: records},
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	resource := writer.writes[0].Resources[0]
	if got, want := len(resource.ResourceChangeEvidence), maxCloudResourceChangeEvidencePerResource; got != want {
		t.Fatalf("resource change evidence count = %d, want capped %d", got, want)
	}
	if !resource.ResourceChangeEvidenceTruncated {
		t.Fatal("ResourceChangeEvidenceTruncated = false, want true")
	}
}
