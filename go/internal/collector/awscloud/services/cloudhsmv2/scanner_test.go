// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudhsmv2

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testClusterID = "cluster-test1234567"
	testBackupID  = "backup-test1234567"
	testBackupARN = "arn:aws:cloudhsm:us-east-1:123456789012:backup/backup-test1234567"
	testVPCID     = "vpc-0123456789abcdef0"
	testSubnetA   = "subnet-0aaa1111bbbb2222a"
	testSubnetB   = "subnet-0ccc3333dddd4444b"
	testGroupID   = "sg-0123456789abcdef0"
)

func TestScannerEmitsCloudHSMMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Clusters: []Cluster{{
			ID:                   testClusterID,
			State:                "ACTIVE",
			StateMessage:         "Cluster is active.",
			HsmType:              "hsm1.medium",
			Mode:                 "FIPS",
			NetworkType:          "IPV4",
			VPCID:                testVPCID,
			SecurityGroupID:      testGroupID,
			BackupPolicy:         "DEFAULT",
			BackupRetentionType:  "DAYS",
			BackupRetentionValue: "90",
			SubnetMappings: []SubnetMapping{
				{AvailabilityZone: "us-east-1a", SubnetID: testSubnetA},
				{AvailabilityZone: "us-east-1b", SubnetID: testSubnetB},
			},
			HSMs: []HSM{{
				ID:               "hsm-aaaa1111bbbb2222",
				State:            "ACTIVE",
				AvailabilityZone: "us-east-1a",
				SubnetID:         testSubnetA,
				ENIID:            "eni-0123456789abcdef0",
				ENIIP:            "10.0.1.10",
			}},
			CertificatePresence: CertificatePresence{
				ClusterCertificate:     true,
				HSMCertificate:         true,
				AWSHardwareCertificate: true,
			},
			CreateTimestamp: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:            map[string]string{"Environment": "prod"},
		}},
		Backups: []Backup{{
			ID:              testBackupID,
			ARN:             testBackupARN,
			State:           "READY",
			ClusterID:       testClusterID,
			HsmType:         "hsm1.medium",
			Mode:            "FIPS",
			NeverExpires:    true,
			CreateTimestamp: time.Date(2026, 5, 15, 1, 0, 0, 0, time.UTC),
			Tags:            map[string]string{"Team": "security"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Cluster resource node keyed by the bare cluster id (no API ARN).
	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeCloudHSMV2Cluster)
	if got, want := cluster.Payload["resource_id"], testClusterID; got != want {
		t.Fatalf("cluster resource_id = %#v, want %q", got, want)
	}
	if got := cluster.Payload["arn"]; got != "" {
		t.Fatalf("cluster arn = %#v, want empty (CloudHSM clusters have no API ARN)", got)
	}
	clusterAttrs := attributesOf(t, cluster)
	assertAttribute(t, clusterAttrs, "mode", "FIPS")
	assertAttribute(t, clusterAttrs, "network_type", "IPV4")
	assertAttribute(t, clusterAttrs, "backup_policy", "DEFAULT")
	assertAttribute(t, clusterAttrs, "backup_retention_value", "90")
	assertAttribute(t, clusterAttrs, "hsm_count", 1)
	assertAttribute(t, clusterAttrs, "cluster_certificate_present", true)
	assertAttribute(t, clusterAttrs, "cluster_csr_present", false)

	// Backup resource node keyed by the bare backup id, ARN carried separately.
	backup := resourceByType(t, envelopes, awscloud.ResourceTypeCloudHSMV2Backup)
	if got, want := backup.Payload["resource_id"], testBackupID; got != want {
		t.Fatalf("backup resource_id = %#v, want %q", got, want)
	}
	if got, want := backup.Payload["arn"], testBackupARN; got != want {
		t.Fatalf("backup arn = %#v, want %q", got, want)
	}
	backupAttrs := attributesOf(t, backup)
	assertAttribute(t, backupAttrs, "never_expires", true)

	// cluster -> VPC edge keyed by the bare vpc id.
	clusterVPC := relationshipByType(t, envelopes, awscloud.RelationshipCloudHSMV2ClusterInVPC)
	assertEdgeTarget(t, clusterVPC, awscloud.ResourceTypeEC2VPC, testVPCID)
	if got, want := clusterVPC.Payload["source_resource_id"], testClusterID; got != want {
		t.Fatalf("cluster->vpc source_resource_id = %#v, want %q", got, want)
	}

	// cluster -> security group edge keyed by the bare sg id.
	clusterSG := relationshipByType(t, envelopes, awscloud.RelationshipCloudHSMV2ClusterUsesSecurityGroup)
	assertEdgeTarget(t, clusterSG, awscloud.ResourceTypeEC2SecurityGroup, testGroupID)

	// cluster -> subnet edges: one per distinct subnet id.
	subnetTargets := relationshipTargets(envelopes, awscloud.RelationshipCloudHSMV2ClusterInSubnet)
	if len(subnetTargets) != 2 {
		t.Fatalf("subnet edge count = %d, want 2 (%v)", len(subnetTargets), subnetTargets)
	}
	if _, ok := subnetTargets[testSubnetA]; !ok {
		t.Fatalf("missing subnet edge for %q in %v", testSubnetA, subnetTargets)
	}
	if _, ok := subnetTargets[testSubnetB]; !ok {
		t.Fatalf("missing subnet edge for %q in %v", testSubnetB, subnetTargets)
	}

	// backup -> source cluster edge keyed by the bare cluster id this scanner publishes.
	backupCluster := relationshipByType(t, envelopes, awscloud.RelationshipCloudHSMV2BackupOfCluster)
	assertEdgeTarget(t, backupCluster, awscloud.ResourceTypeCloudHSMV2Cluster, testClusterID)
	if got, want := backupCluster.Payload["source_resource_id"], testBackupID; got != want {
		t.Fatalf("backup->cluster source_resource_id = %#v, want %q", got, want)
	}

	// No certificate body, CSR body, or PRECO password leakage anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"cluster_certificate", "cluster_csr", "hsm_certificate",
			"aws_hardware_certificate", "manufacturer_hardware_certificate",
			"certificate", "csr", "pre_co_password", "preco_password", "password",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; CloudHSM scanner must never carry certificate bodies or the PRECO password", forbidden)
			}
		}
	}
}

func TestScannerDeduplicatesSubnetEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ID: testClusterID,
		SubnetMappings: []SubnetMapping{
			{AvailabilityZone: "us-east-1a", SubnetID: testSubnetA},
			{AvailabilityZone: "us-east-1c", SubnetID: testSubnetA},
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	targets := relationshipTargets(envelopes, awscloud.RelationshipCloudHSMV2ClusterInSubnet)
	if len(targets) != 1 {
		t.Fatalf("subnet edge count = %d, want 1 deduplicated edge (%v)", len(targets), targets)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ID: testClusterID,
		// No VPC, no security group, no subnets: cluster node only.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsBackupClusterEdgeWhenClusterUnknown(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Backups: []Backup{{
		ID:    testBackupID,
		ARN:   testBackupARN,
		State: "READY",
		// ClusterId empty (source cluster deleted): no edge, node still emitted.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resourceByType(t, envelopes, awscloud.ResourceTypeCloudHSMV2Backup)
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerEmptyAccountReturnsNoEnvelopes(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("envelopes = %d, want 0 for an empty account", len(envelopes))
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	cluster := Cluster{
		ID:              testClusterID,
		VPCID:           testVPCID,
		SecurityGroupID: testGroupID,
		SubnetMappings:  []SubnetMapping{{AvailabilityZone: "us-east-1a", SubnetID: testSubnetA}},
	}
	backup := Backup{ID: testBackupID, ARN: testBackupARN, ClusterID: testClusterID}

	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		clusterVPCRelationship(boundary, cluster),
		clusterSecurityGroupRelationship(boundary, cluster),
		backupClusterRelationship(boundary, backup),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	observations = append(observations, clusterSubnetRelationships(boundary, cluster)...)
	if len(observations) == 0 {
		t.Fatal("expected relationship observations")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Clusters: []Cluster{{ID: testClusterID}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "CloudHSM DescribeBackups throttled after SDK retries; backup metadata omitted for this scan",
			SourceRecordID: "cloudhsmv2_backups_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCloudHSMV2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cloudhsmv2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
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
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func relationshipTargets(envelopes []facts.Envelope, relationshipType string) map[string]struct{} {
	targets := map[string]struct{}{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if target, ok := envelope.Payload["target_resource_id"].(string); ok {
			targets[target] = struct{}{}
		}
	}
	return targets
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
