// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package efs

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsFileSystemTopologyAndRelationships(t *testing.T) {
	fsID := "fs-01234567"
	fsARN := "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-01234567"
	apID := "fsap-0001"
	apARN := "arn:aws:elasticfilesystem:us-east-1:123456789012:access-point/fsap-0001"
	mtID := "fsmt-0001"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd"

	client := fakeClient{
		fileSystems: []FileSystem{{
			ID:              fsID,
			ARN:             fsARN,
			Name:            "prod-data",
			OwnerID:         "123456789012",
			LifeCycleState:  "available",
			PerformanceMode: "generalPurpose",
			ThroughputMode:  "bursting",
			Encrypted:       true,
			KMSKeyID:        kmsARN,
			LifecyclePolicy: LifecyclePolicySummary{TransitionToIA: "AFTER_30_DAYS"},
			Tags:            map[string]string{"Environment": "prod"},
			AccessPoints: []AccessPoint{{
				ID:            apID,
				ARN:           apARN,
				Name:          "app-ap",
				FileSystemID:  fsID,
				RootDirectory: "/app",
			}},
			MountTargets: []MountTarget{{
				ID:               mtID,
				FileSystemID:     fsID,
				SubnetID:         "subnet-aaa",
				VPCID:            "vpc-bbb",
				IPAddress:        "10.0.0.5",
				SecurityGroupIDs: []string{"sg-111", "sg-222"},
			}},
		}},
		replications: []ReplicationConfiguration{{
			SourceFileSystemID:  fsID,
			SourceFileSystemARN: fsARN,
			Destinations: []ReplicationDestination{{
				FileSystemID: "fs-dest",
				Region:       "us-west-2",
				Status:       "ENABLED",
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	fs := resourceByType(t, envelopes, awscloud.ResourceTypeEFSFileSystem)
	attributes := attributesOf(t, fs)
	if got, want := attributes["performance_mode"], "generalPurpose"; got != want {
		t.Fatalf("performance_mode = %#v, want %q", got, want)
	}
	if got, want := attributes["throughput_mode"], "bursting"; got != want {
		t.Fatalf("throughput_mode = %#v, want %q", got, want)
	}
	if got, want := attributes["encrypted"], true; got != want {
		t.Fatalf("encrypted = %#v, want %v", got, want)
	}
	if got, want := attributes["lifecycle_transition_to_ia"], "AFTER_30_DAYS"; got != want {
		t.Fatalf("lifecycle_transition_to_ia = %#v, want %q", got, want)
	}
	// The NFS file system policy body must never be persisted (issue #734).
	for _, forbidden := range []string{"policy", "file_system_policy", "policy_document", "nfs_policy"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("attribute %q persisted; EFS scanner must not store NFS file system policy bodies", forbidden)
		}
	}
	if got, want := fs.Payload["arn"], fsARN; got != want {
		t.Fatalf("file system ARN = %#v, want %q", got, want)
	}

	accessPoint := resourceByType(t, envelopes, awscloud.ResourceTypeEFSAccessPoint)
	if got, want := accessPoint.Payload["arn"], apARN; got != want {
		t.Fatalf("access point ARN = %#v, want %q", got, want)
	}
	if got, want := attributesOf(t, accessPoint)["root_directory"], "/app"; got != want {
		t.Fatalf("root_directory = %#v, want %q", got, want)
	}

	resourceByType(t, envelopes, awscloud.ResourceTypeEFSMountTarget)
	resourceByType(t, envelopes, awscloud.ResourceTypeEFSReplicationConfiguration)

	assertRelationship(t, envelopes, awscloud.RelationshipEFSMountTargetInSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipEFSMountTargetUsesSecurityGroup)
	assertRelationship(t, envelopes, awscloud.RelationshipEFSFileSystemUsesKMSKey)
	assertRelationship(t, envelopes, awscloud.RelationshipEFSAccessPointTargetsFileSystem)
	assertRelationship(t, envelopes, awscloud.RelationshipEFSReplicationTargetsFileSystem)

	// Two security groups produce two distinct uses-security-group relationships.
	if got, want := countRelationships(envelopes, awscloud.RelationshipEFSMountTargetUsesSecurityGroup), 2; got != want {
		t.Fatalf("uses-security-group relationships = %d, want %d", got, want)
	}
}

func TestScannerOmitsKMSRelationshipWhenUnencrypted(t *testing.T) {
	client := fakeClient{
		fileSystems: []FileSystem{{
			ID:             "fs-plain",
			ARN:            "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-plain",
			LifeCycleState: "available",
			Encrypted:      false,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipEFSFileSystemUsesKMSKey); got != 0 {
		t.Fatalf("file-system-uses-kms-key relationships = %d, want 0 for unencrypted file system", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSQS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceEFS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:efs:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	fileSystems  []FileSystem
	replications []ReplicationConfiguration
}

func (c fakeClient) ListFileSystems(context.Context) ([]FileSystem, error) {
	return c.fileSystems, nil
}

func (c fakeClient) ListReplicationConfigurations(context.Context) ([]ReplicationConfiguration, error) {
	return c.replications, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if countRelationships(envelopes, relationshipType) == 0 {
		t.Fatalf("missing relationship_type %q", relationshipType)
	}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
