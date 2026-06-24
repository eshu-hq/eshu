// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

type stubCloudTagEvidenceLoader struct {
	records []CloudTagEvidenceRecord
	err     error
	calls   int
}

func (s *stubCloudTagEvidenceLoader) LoadCloudTagEvidence(
	context.Context,
	string,
	string,
) ([]CloudTagEvidenceRecord, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]CloudTagEvidenceRecord(nil), s.records...), nil
}

const azureTagSubject = "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1"

// TestCloudInventoryAdmissionAttachesTagEvidenceToAdmittedResource proves tag
// evidence fingerprints attach to the canonical resource that shares the tag
// subject's cloud_resource_uid, while tag evidence for an identity that no
// resource fact admitted is ignored (tags are not identity and must not admit a
// resource on their own).
func TestCloudInventoryAdmissionAttachesTagEvidenceToAdmittedResource(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  azureTagSubject,
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	tagLoader := &stubCloudTagEvidenceLoader{records: []CloudTagEvidenceRecord{
		{
			Provider:             cloudinventory.ProviderAzure,
			RawIdentity:          azureTagSubject,
			TagValueFingerprints: map[string]string{"env": "aztag-env-marker", "owner": "aztag-owner-marker"},
		},
		{
			// No resource fact admitted this identity; its tags must be dropped.
			Provider:             cloudinventory.ProviderAzure,
			RawIdentity:          "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/ghost",
			TagValueFingerprints: map[string]string{"env": "ghost-marker"},
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:    loader,
		Writer:            writer,
		TagEvidenceLoader: tagLoader,
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if tagLoader.calls != 1 {
		t.Fatalf("tag loader calls = %d, want 1", tagLoader.calls)
	}
	resources := writer.writes[0].Resources
	if len(resources) != 1 {
		t.Fatalf("admitted resources = %d, want 1 (tags must not admit identity)", len(resources))
	}
	if resources[0].RawIdentity != azureTagSubject {
		t.Fatalf("admitted raw identity = %q, want %q", resources[0].RawIdentity, azureTagSubject)
	}
	got := resources[0].TagValueFingerprints
	if len(got) != 2 || got["env"] != "aztag-env-marker" || got["owner"] != "aztag-owner-marker" {
		t.Fatalf("TagValueFingerprints = %#v, want env/owner markers", got)
	}
}

// TestCloudInventoryAdmissionWithoutTagLoaderCarriesNoTags proves the admission
// path is unchanged when no tag evidence loader is configured: the canonical
// resource carries no tag fingerprints (zero regression to the AWS/GCP path).
func TestCloudInventoryAdmissionWithoutTagLoaderCarriesNoTags(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAWS,
			FactKind:     "aws_resource",
			RawIdentity:  "arn:aws:s3:::eshu-prod-bucket",
			ResourceType: "AWS::S3::Bucket",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader: loader,
		Writer:         writer,
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := writer.writes[0].Resources[0].TagValueFingerprints; len(got) != 0 {
		t.Fatalf("TagValueFingerprints = %#v, want empty without a tag loader", got)
	}
}

// TestCloudInventoryAdmissionTagLoaderErrorAborts proves a tag-evidence load
// failure aborts the admission rather than publishing a canonical row with
// silently missing tag evidence.
func TestCloudInventoryAdmissionTagLoaderErrorAborts(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  azureTagSubject,
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	tagLoader := &stubCloudTagEvidenceLoader{err: context.DeadlineExceeded}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:    loader,
		Writer:            writer,
		TagEvidenceLoader: tagLoader,
	}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err == nil {
		t.Fatal("Handle() error = nil, want tag-evidence load failure to abort")
	}
	if len(writer.writes) != 0 {
		t.Fatalf("writer writes = %d, want 0 on tag-load failure", len(writer.writes))
	}
}
