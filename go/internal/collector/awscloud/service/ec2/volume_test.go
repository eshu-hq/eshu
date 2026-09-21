// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsEBSVolumeMetadataAndKMSRelationship(t *testing.T) {
	createTime := time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)
	attachTime := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	encrypted := true
	fastRestored := false
	multiAttach := true
	sizeGiB := int32(100)
	iops := int32(3000)
	throughput := int32(125)
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd"
	client := fakeClient{
		volumes: []Volume{{
			ID:                 "vol-0abc",
			ARN:                "arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc",
			State:              "in-use",
			AvailabilityZone:   "us-east-1a",
			AvailabilityZoneID: "use1-az1",
			CreateTime:         createTime,
			Encrypted:          &encrypted,
			FastRestored:       &fastRestored,
			IOPS:               &iops,
			KMSKeyID:           kmsARN,
			MultiAttachEnabled: &multiAttach,
			SizeGiB:            &sizeGiB,
			SnapshotID:         "snap-123",
			ThroughputMiBps:    &throughput,
			VolumeType:         "gp3",
			Attachments: []VolumeAttachment{{
				AttachTime:          attachTime,
				DeleteOnTermination: true,
				Device:              "/dev/xvda",
				InstanceID:          "i-1234567890abcdef0",
				State:               "attached",
				VolumeID:            "vol-0abc",
			}},
			Tags: map[string]string{"env": "prod"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 1 {
		t.Fatalf("aws_resource count = %d, want 1", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 1 {
		t.Fatalf("aws_relationship count = %d, want 1", counts[facts.AWSRelationshipFactKind])
	}
	assertNoResourceType(t, envelopes, awscloud.ResourceTypeEC2Instance)

	volume := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2Volume)
	if got := volume.Payload["resource_id"]; got != "vol-0abc" {
		t.Fatalf("volume resource_id = %#v, want vol-0abc", got)
	}
	if got := volume.Payload["arn"]; got != "arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc" {
		t.Fatalf("volume arn = %#v", got)
	}
	assertAttribute(t, volume, "encrypted", true)
	assertAttribute(t, volume, "kms_key_id", kmsARN)
	assertAttribute(t, volume, "attachment_count", 1)
	assertAttribute(t, volume, "volume_type", "gp3")

	attrs := attributesOf(t, volume)
	attachments, ok := attrs["attachments"].([]map[string]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want one attachment map", attrs["attachments"])
	}
	if got := attachments[0]["instance_id"]; got != "i-1234567890abcdef0" {
		t.Fatalf("attachment instance_id = %#v, want instance id", got)
	}

	edge := assertRelationship(t, envelopes, awscloud.RelationshipEC2VolumeUsesKMSKey)
	if got := edge.Payload["source_resource_id"]; got != "vol-0abc" {
		t.Fatalf("kms edge source_resource_id = %#v, want vol-0abc", got)
	}
	if got := edge.Payload["source_arn"]; got != "arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc" {
		t.Fatalf("kms edge source_arn = %#v", got)
	}
	if got := edge.Payload["target_resource_id"]; got != kmsARN {
		t.Fatalf("kms edge target_resource_id = %#v, want KMS ARN", got)
	}
	if got := edge.Payload["target_arn"]; got != kmsARN {
		t.Fatalf("kms edge target_arn = %#v, want KMS ARN", got)
	}
	if got := edge.Payload["target_type"]; got != awscloud.ResourceTypeKMSKey {
		t.Fatalf("kms edge target_type = %#v, want aws_kms_key", got)
	}
}

func TestScannerKeepsVolumesWithoutKMSRelationshipWhenKeyMissing(t *testing.T) {
	encrypted := true
	unencrypted := false
	client := fakeClient{
		volumes: []Volume{
			{ID: "vol-encrypted-missing-key", State: "available", Encrypted: &encrypted},
			{ID: "vol-unencrypted", State: "available", Encrypted: &unencrypted},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 2 {
		t.Fatalf("aws_resource count = %d, want 2", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 0 {
		t.Fatalf("aws_relationship count = %d, want 0 without KMS key evidence", counts[facts.AWSRelationshipFactKind])
	}

	encryptedVolume := assertResourceID(t, envelopes, "vol-encrypted-missing-key")
	assertAttribute(t, encryptedVolume, "encrypted", true)
	assertAttribute(t, encryptedVolume, "kms_key_id", "")
	unencryptedVolume := assertResourceID(t, envelopes, "vol-unencrypted")
	assertAttribute(t, unencryptedVolume, "encrypted", false)
}

func TestScannerSkipsVolumeWithoutIdentity(t *testing.T) {
	client := fakeClient{
		volumes: []Volume{{State: "available"}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("envelopes = %#v, want none for missing volume identity", envelopes)
	}
}
