// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestLocationS3RelationshipDerivesPartition pins the GovCloud/China graph-join
// contract for the synthesized S3 bucket ARN. The DataSync S3 location reports
// only the bucket name, so the scanner synthesizes the bucket ARN; the S3 bucket
// scanner publishes its resource_id as `arn:<partition>:s3:::<bucket>`, so a
// hardcoded commercial partition would dangle the location->S3 edge in aws-us-gov
// and aws-cn.
func TestLocationS3RelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:s3:::archive"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:s3:::archive"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:s3:::archive"},
		{name: "blank region falls back to commercial", region: "", want: "arn:aws:s3:::archive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{AccountID: "123456789012", Region: tc.region}
			location := Location{
				ARN:          "arn:aws:datasync:us-east-1:123456789012:location/loc-0s3",
				Type:         "S3",
				URI:          "s3://archive/incoming/",
				S3BucketName: "archive",
			}
			obs := locationS3Relationship(boundary, location)
			if obs == nil {
				t.Fatalf("locationS3Relationship returned nil for a valid S3 location")
			}
			if obs.TargetResourceID != tc.want {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.want)
			}
			if obs.TargetARN != tc.want {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.want)
			}
			if obs.TargetType != awscloud.ResourceTypeS3Bucket {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeS3Bucket)
			}
		})
	}
}

// TestLocationEFSRelationshipDerivesPartition pins the GovCloud/China contract
// for the synthesized EFS file system ARN. The EFS scanner publishes its
// resource_id as the file system ARN, so the synthesized ARN must inherit the
// boundary partition, region, and account.
func TestLocationEFSRelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-01"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:elasticfilesystem:us-gov-west-1:123456789012:file-system/fs-01"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:elasticfilesystem:cn-north-1:123456789012:file-system/fs-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{AccountID: "123456789012", Region: tc.region}
			location := Location{
				ARN:             "arn:aws:datasync:us-east-1:123456789012:location/loc-0efs",
				Type:            "EFS",
				URI:             "efs://" + tc.region + ".fs-01/backups/",
				EFSFileSystemID: "fs-01",
			}
			obs := locationEFSRelationship(boundary, location)
			if obs == nil {
				t.Fatalf("locationEFSRelationship returned nil for a valid EFS location")
			}
			if obs.TargetResourceID != tc.want {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.want)
			}
			if obs.TargetType != awscloud.ResourceTypeEFSFileSystem {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeEFSFileSystem)
			}
		})
	}
}

// TestLocationFSxRelationshipSynthesizesPartition pins the GovCloud/China
// contract for the synthesized FSx file system ARN used when the API reports no
// file system ARN directly (Lustre, OpenZFS, Windows).
func TestLocationFSxRelationshipSynthesizesPartition(t *testing.T) {
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{name: "commercial", region: "us-east-1", want: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-0a"},
		{name: "govcloud", region: "us-gov-west-1", want: "arn:aws-us-gov:fsx:us-gov-west-1:123456789012:file-system/fs-0a"},
		{name: "china", region: "cn-north-1", want: "arn:aws-cn:fsx:cn-north-1:123456789012:file-system/fs-0a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{AccountID: "123456789012", Region: tc.region}
			location := Location{
				ARN:             "arn:aws:datasync:us-east-1:123456789012:location/loc-0fsx",
				Type:            "FSX_LUSTRE",
				URI:             "fsxl://" + tc.region + ".fs-0a/",
				FSxFileSystemID: "fs-0a",
			}
			obs := locationFSxRelationship(boundary, location)
			if obs == nil {
				t.Fatalf("locationFSxRelationship returned nil for a valid FSx location")
			}
			if obs.TargetResourceID != tc.want {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.want)
			}
			if obs.TargetType != awscloud.ResourceTypeFSxFileSystem {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeFSxFileSystem)
			}
		})
	}
}

// TestEmittedRelationshipsSatisfyGraphJoinContract feeds every relationship the
// scanner emits through the relguard runtime layer, asserting each edge carries
// a known target_type and an ARN-shaped join key whenever target_arn is set.
func TestEmittedRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	client := fakeClient{
		tasks: []Task{{
			ARN:                    "arn:aws:datasync:us-east-1:123456789012:task/task-0",
			SourceLocationARN:      "arn:aws:datasync:us-east-1:123456789012:location/loc-0s3",
			DestinationLocationARN: "arn:aws:datasync:us-east-1:123456789012:location/loc-0efs",
			CloudWatchLogGroupARN:  "arn:aws:logs:us-east-1:123456789012:log-group:/aws/datasync:*",
		}},
		locations: []Location{
			{
				ARN:          "arn:aws:datasync:us-east-1:123456789012:location/loc-0s3",
				Type:         "S3",
				URI:          "s3://archive/",
				S3BucketName: "archive",
				IAMRoleARN:   "arn:aws:iam::123456789012:role/datasync-s3",
			},
			{
				ARN:              "arn:aws:datasync:us-east-1:123456789012:location/loc-0fsx",
				Type:             "FSX_ONTAP",
				URI:              "fsxn://us-east-1.fs-0a/vol1/",
				FSxFileSystemARN: "arn:aws:fsx:us-east-1:123456789012:file-system/fs-0a",
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relguard.AssertObservations(t, relationshipObservations(t, envelopes)...)
}

// relationshipObservations rebuilds RelationshipObservation values from the
// emitted relationship envelopes so the relguard runtime layer can re-check the
// data-dependent target_type and ARN join keys the scanner produced.
func relationshipObservations(t *testing.T, envelopes []facts.Envelope) []awscloud.RelationshipObservation {
	t.Helper()
	var observations []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relationshipType, _ := envelope.Payload["relationship_type"].(string)
		targetType, _ := envelope.Payload["target_type"].(string)
		targetID, _ := envelope.Payload["target_resource_id"].(string)
		targetARN, _ := envelope.Payload["target_arn"].(string)
		observations = append(observations, awscloud.RelationshipObservation{
			RelationshipType: relationshipType,
			TargetType:       targetType,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
		})
	}
	return observations
}
