// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

type stubCloudIdentityPolicyEvidenceLoader struct {
	records []CloudIdentityPolicyEvidenceRecord
	err     error
	calls   int
}

func (s *stubCloudIdentityPolicyEvidenceLoader) LoadCloudIdentityPolicyEvidence(
	context.Context,
	string,
	string,
) ([]CloudIdentityPolicyEvidenceRecord, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]CloudIdentityPolicyEvidenceRecord(nil), s.records...), nil
}

const azureIdentitySubject = "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1"

// TestCloudInventoryAdmissionAttachesIdentityPolicyEvidenceToAdmittedResource
// proves Azure identity observations attach only to an already admitted
// CloudResource. Orphan identity facts remain policy evidence and must not
// admit a resource by themselves.
func TestCloudInventoryAdmissionAttachesIdentityPolicyEvidenceToAdmittedResource(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  azureIdentitySubject,
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	identityLoader := &stubCloudIdentityPolicyEvidenceLoader{records: []CloudIdentityPolicyEvidenceRecord{
		{
			Provider:             cloudinventory.ProviderAzure,
			RawIdentity:          azureIdentitySubject,
			EvidenceKey:          "identity-stable-1",
			IdentityType:         "system_assigned",
			RoleClass:            "contributor",
			PrincipalFingerprint: "principal-marker",
			TenantFingerprint:    "tenant-marker",
		},
		{
			Provider:             cloudinventory.ProviderAzure,
			RawIdentity:          azureIdentitySubject,
			EvidenceKey:          "identity-stable-1",
			IdentityType:         "system_assigned",
			RoleClass:            "contributor",
			PrincipalFingerprint: "principal-marker",
			TenantFingerprint:    "tenant-marker",
		},
		{
			Provider:             cloudinventory.ProviderAzure,
			RawIdentity:          "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/ghost",
			EvidenceKey:          "identity-stable-ghost",
			IdentityType:         "user_assigned",
			PrincipalFingerprint: "ghost-marker",
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:               loader,
		Writer:                       writer,
		IdentityPolicyEvidenceLoader: identityLoader,
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if identityLoader.calls != 1 {
		t.Fatalf("identity loader calls = %d, want 1", identityLoader.calls)
	}
	resources := writer.writes[0].Resources
	if len(resources) != 1 {
		t.Fatalf("admitted resources = %d, want 1 (identity facts must not admit resources)", len(resources))
	}
	evidence := resources[0].IdentityPolicyEvidence
	if len(evidence) != 1 {
		t.Fatalf("identity policy evidence = %#v, want one attached row", evidence)
	}
	if evidence[0].PrincipalFingerprint != "principal-marker" || evidence[0].TenantFingerprint != "tenant-marker" {
		t.Fatalf("fingerprints = %#v", evidence[0])
	}
}

// TestCloudInventoryAdmissionCapsIdentityPolicyEvidence proves per-resource
// identity evidence is deterministic, capped, and marks truncation so readback
// payloads cannot grow without bound.
func TestCloudInventoryAdmissionCapsIdentityPolicyEvidence(t *testing.T) {
	t.Parallel()

	records := make([]CloudIdentityPolicyEvidenceRecord, 0, maxCloudIdentityPolicyEvidencePerResource+1)
	for i := 0; i < maxCloudIdentityPolicyEvidencePerResource+1; i++ {
		records = append(records, CloudIdentityPolicyEvidenceRecord{
			Provider:             cloudinventory.ProviderAzure,
			RawIdentity:          azureIdentitySubject,
			EvidenceKey:          fmt.Sprintf("identity-stable-%02d", i),
			IdentityType:         "user_assigned",
			PrincipalFingerprint: "principal-marker",
			ClientFingerprint:    "client-marker",
		})
	}
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  azureIdentitySubject,
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:               loader,
		Writer:                       writer,
		IdentityPolicyEvidenceLoader: &stubCloudIdentityPolicyEvidenceLoader{records: records},
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	resource := writer.writes[0].Resources[0]
	if got, want := len(resource.IdentityPolicyEvidence), maxCloudIdentityPolicyEvidencePerResource; got != want {
		t.Fatalf("identity evidence count = %d, want capped %d", got, want)
	}
	if !resource.IdentityPolicyEvidenceTruncated {
		t.Fatal("IdentityPolicyEvidenceTruncated = false, want true")
	}
}
