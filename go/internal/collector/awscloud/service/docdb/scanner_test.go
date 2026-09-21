// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdb

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestScannerEmitsDocDBMetadataOnlyFactsAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb"
	instanceARN := "arn:aws:rds:us-east-1:123456789012:db:orders-docdb-1"
	subnetGroupARN := "arn:aws:rds:us-east-1:123456789012:subgrp:orders-docdb-subnets"
	paramGroupARN := "arn:aws:rds:us-east-1:123456789012:cluster-pg:orders-docdb-params"
	snapshotARN := "arn:aws:rds:us-east-1:123456789012:cluster-snapshot:orders-docdb-2026-05-01"
	globalClusterARN := "arn:aws:rds::123456789012:global-cluster:orders-global"
	eventSubARN := "arn:aws:rds:us-east-1:123456789012:es:orders-docdb-events"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders-docdb"

	client := fakeClient{
		clusters: []DBCluster{{
			ARN:                          clusterARN,
			Identifier:                   "orders-docdb",
			ResourceID:                   "cluster-ORDERSDOCDB",
			Engine:                       "docdb",
			EngineVersion:                "5.0.0",
			Status:                       "available",
			EndpointAddress:              "orders.cluster.docdb.amazonaws.com",
			ReaderEndpointAddress:        "orders.cluster-ro.docdb.amazonaws.com",
			HostedZoneID:                 "Z2",
			Port:                         27017,
			MultiAZ:                      true,
			StorageEncrypted:             true,
			KMSKeyID:                     kmsARN,
			DeletionProtection:           true,
			BackupRetentionPeriod:        7,
			DBSubnetGroupName:            "orders-docdb-subnets",
			VPCSecurityGroupIDs:          []string{"sg-123"},
			Members:                      []ClusterMember{{DBInstanceIdentifier: "orders-docdb-1", IsWriter: true}},
			ParameterGroup:               "orders-docdb-params",
			EnabledCloudwatchLogsExports: []string{"audit", "profiler"},
			Tags:                         map[string]string{"Tier": "data"},
		}},
		instances: []ClusterInstance{{
			ARN:               instanceARN,
			Identifier:        "orders-docdb-1",
			ResourceID:        "db-ORDERSDOCDB1",
			Class:             "db.r6g.large",
			Engine:            "docdb",
			EngineVersion:     "5.0.0",
			Status:            "available",
			EndpointAddress:   "orders-docdb-1.docdb.amazonaws.com",
			EndpointPort:      27017,
			HostedZoneID:      "Z2",
			AvailabilityZone:  "us-east-1a",
			StorageEncrypted:  true,
			KMSKeyID:          kmsARN,
			ClusterIdentifier: "orders-docdb",
			PromotionTier:     1,
			Tags:              map[string]string{"Role": "writer"},
		}},
		parameterGroups: []ClusterParameterGroup{{
			ARN:            paramGroupARN,
			Name:           "orders-docdb-params",
			Family:         "docdb5.0",
			Description:    "orders docdb cluster parameters",
			ParameterCount: 12,
			Tags:           map[string]string{"Managed": "true"},
		}},
		snapshots: []ClusterSnapshot{{
			ARN:               snapshotARN,
			Identifier:        "orders-docdb-2026-05-01",
			ClusterIdentifier: "orders-docdb",
			Engine:            "docdb",
			EngineVersion:     "5.0.0",
			Status:            "available",
			SnapshotType:      "manual",
			StorageEncrypted:  true,
			KMSKeyID:          kmsARN,
			VPCID:             "vpc-123",
			Tags:              map[string]string{"Retention": "long"},
		}},
		subnetGroups: []SubnetGroup{{
			ARN:         subnetGroupARN,
			Name:        "orders-docdb-subnets",
			Description: "orders docdb subnets",
			Status:      "Complete",
			VPCID:       "vpc-123",
			SubnetIDs:   []string{"subnet-a", "subnet-b"},
			Tags:        map[string]string{"Network": "private"},
		}},
		globalClusters: []GlobalCluster{{
			ARN:                globalClusterARN,
			Identifier:         "orders-global",
			ResourceID:         "global-ORDERS",
			Engine:             "docdb",
			EngineVersion:      "5.0.0",
			Status:             "available",
			StorageEncrypted:   true,
			DeletionProtection: true,
			Members:            []GlobalClusterMember{{DBClusterARN: clusterARN, IsWriter: true}},
			Tags:               map[string]string{"Scope": "global"},
		}},
		eventSubscriptions: []EventSubscription{{
			ARN:             eventSubARN,
			Name:            "orders-docdb-events",
			CustomerAWSID:   "123456789012",
			Enabled:         true,
			Status:          "active",
			SourceType:      "db-cluster",
			SNSTopicARN:     "arn:aws:sns:us-east-1:123456789012:docdb-alerts",
			SourceIDs:       []string{"orders-docdb"},
			EventCategories: []string{"failover", "maintenance"},
			Tags:            map[string]string{"Notify": "oncall"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBCluster)
	if got, want := cluster.Payload["arn"], clusterARN; got != want {
		t.Fatalf("cluster arn = %#v, want %q", got, want)
	}
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "engine", "docdb")
	assertAttribute(t, clusterAttributes, "reader_endpoint_address", "orders.cluster-ro.docdb.amazonaws.com")
	assertAttribute(t, clusterAttributes, "storage_encrypted", true)
	assertAttribute(t, clusterAttributes, "member_instance_ids", []string{"orders-docdb-1"})
	assertAttribute(t, clusterAttributes, "enabled_cloudwatch_logs_exports", []string{"audit", "profiler"})
	assertForbiddenAbsent(t, clusterAttributes, "cluster")

	instance := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBClusterInstance)
	instanceAttributes := attributesOf(t, instance)
	assertAttribute(t, instanceAttributes, "endpoint_address", "orders-docdb-1.docdb.amazonaws.com")
	assertAttribute(t, instanceAttributes, "endpoint_port", int32(27017))
	assertAttribute(t, instanceAttributes, "cluster_identifier", "orders-docdb")
	assertForbiddenAbsent(t, instanceAttributes, "instance")

	paramGroup := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBClusterParameterGroup)
	paramAttributes := attributesOf(t, paramGroup)
	assertAttribute(t, paramAttributes, "family", "docdb5.0")
	assertAttribute(t, paramAttributes, "parameter_count", 12)
	for _, forbidden := range []string{"parameters", "parameter_values", "values"} {
		if _, exists := paramAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; DocDB scanner must persist parameter count only, never values", forbidden)
		}
	}

	snapshot := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBClusterSnapshot)
	snapshotAttributes := attributesOf(t, snapshot)
	assertAttribute(t, snapshotAttributes, "snapshot_type", "manual")
	assertAttribute(t, snapshotAttributes, "cluster_identifier", "orders-docdb")
	assertForbiddenAbsent(t, snapshotAttributes, "snapshot")

	subnetGroup := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBSubnetGroup)
	subnetAttributes := attributesOf(t, subnetGroup)
	assertAttribute(t, subnetAttributes, "subnet_ids", []string{"subnet-a", "subnet-b"})
	assertAttribute(t, subnetAttributes, "vpc_id", "vpc-123")

	globalCluster := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBGlobalCluster)
	globalAttributes := attributesOf(t, globalCluster)
	assertAttribute(t, globalAttributes, "engine", "docdb")
	assertAttribute(t, globalAttributes, "member_cluster_arns", []string{clusterARN})

	eventSub := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBEventSubscription)
	eventAttributes := attributesOf(t, eventSub)
	assertAttribute(t, eventAttributes, "source_type", "db-cluster")
	assertAttribute(t, eventAttributes, "sns_topic_arn", "arn:aws:sns:us-east-1:123456789012:docdb-alerts")
	assertAttribute(t, eventAttributes, "source_ids", []string{"orders-docdb"})
	assertAttribute(t, eventAttributes, "event_categories", []string{"failover", "maintenance"})

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDocDBClusterInSubnetGroup, subnetGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDocDBClusterInVPC, "vpc-123")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipDocDBClusterUsesKMSKey, kmsARN)
	memberRel := relationshipByType(t, envelopes, awscloud.RelationshipDocDBInstanceMemberOfCluster)
	if got, want := memberRel.Payload["target_arn"], clusterARN; got != want {
		t.Fatalf("instance membership target_arn = %#v, want %q", got, want)
	}
	assertAttribute(t, attributesOf(t, memberRel), "is_writer", true)
	globalRel := relationshipByType(t, envelopes, awscloud.RelationshipDocDBGlobalClusterHasCluster)
	if got, want := globalRel.Payload["target_arn"], clusterARN; got != want {
		t.Fatalf("global cluster membership target_arn = %#v, want %q", got, want)
	}
	assertAttribute(t, attributesOf(t, globalRel), "is_writer", true)
}

// TestScannerClusterInVPCEdgeTargetsEC2VPCType proves the cluster-to-VPC
// relationship labels its target with the canonical EC2 VPC resource type
// (aws_ec2_vpc), which is the type the EC2/VPC scanner emits for the VPC node.
// A bare "aws_vpc" label points the edge at a resource type that does not
// exist in the graph, so the placement edge does not agree with the VPC node
// it describes.
func TestScannerClusterInVPCEdgeTargetsEC2VPCType(t *testing.T) {
	client := fakeClient{
		clusters: []DBCluster{{
			ARN:               "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb",
			Identifier:        "orders-docdb",
			DBSubnetGroupName: "orders-docdb-subnets",
		}},
		subnetGroups: []SubnetGroup{{
			ARN:   "arn:aws:rds:us-east-1:123456789012:subgrp:orders-docdb-subnets",
			Name:  "orders-docdb-subnets",
			VPCID: "vpc-123",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := relationshipByType(t, envelopes, awscloud.RelationshipDocDBClusterInVPC)
	if got, want := rel.Payload["target_resource_id"], "vpc-123"; got != want {
		t.Fatalf("cluster-in-vpc target_resource_id = %#v, want %q", got, want)
	}
	if got, want := rel.Payload["target_type"], awscloud.ResourceTypeEC2VPC; got != want {
		t.Fatalf("cluster-in-vpc target_type = %#v, want %q", got, want)
	}
}

// TestScannerInstanceMembershipReflectsClusterWriterRole proves the
// instance-to-cluster relationship carries each instance's true writer role,
// derived from the cluster's reported member list, instead of a hardcoded
// value. A DocumentDB cluster has exactly one writer and N readers, so reader
// instances must emit is_writer=false; mislabeling readers as writers is wrong
// graph truth.
func TestScannerInstanceMembershipReflectsClusterWriterRole(t *testing.T) {
	const clusterARN = "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb"
	const writerARN = "arn:aws:rds:us-east-1:123456789012:db:orders-docdb-1"
	const readerARN = "arn:aws:rds:us-east-1:123456789012:db:orders-docdb-2"

	client := fakeClient{
		clusters: []DBCluster{{
			ARN:        clusterARN,
			Identifier: "orders-docdb",
			Members: []ClusterMember{
				{DBInstanceIdentifier: "orders-docdb-1", IsWriter: true},
				{DBInstanceIdentifier: "orders-docdb-2", IsWriter: false},
			},
		}},
		instances: []ClusterInstance{
			{
				ARN:               writerARN,
				Identifier:        "orders-docdb-1",
				ClusterIdentifier: "orders-docdb",
			},
			{
				ARN:               readerARN,
				Identifier:        "orders-docdb-2",
				ClusterIdentifier: "orders-docdb",
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	writers := map[string]bool{}
	for _, envelope := range envelopes {
		if got, _ := envelope.Payload["relationship_type"].(string); got != awscloud.RelationshipDocDBInstanceMemberOfCluster {
			continue
		}
		sourceID, _ := envelope.Payload["source_resource_id"].(string)
		attributes, _ := envelope.Payload["attributes"].(map[string]any)
		isWriter, _ := attributes["is_writer"].(bool)
		writers[sourceID] = isWriter
	}
	if got, want := writers[writerARN], true; got != want {
		t.Fatalf("writer instance is_writer = %v, want %v", got, want)
	}
	if got, want := writers[readerARN], false; got != want {
		t.Fatalf("reader instance is_writer = %v, want %v", got, want)
	}
}

// TestScannerNeverPersistsMasterUserPasswordAnchors guards the issue #736
// security requirement: even when the SDK shape carries master usernames, the
// scanner must not surface password, secret, or document-content fields in any
// emitted resource attribute set or correlation anchor.
func TestScannerNeverPersistsMasterUserPasswordAnchors(t *testing.T) {
	client := fakeClient{clusters: []DBCluster{{
		ARN:        "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb",
		Identifier: "orders-docdb",
		Engine:     "docdb",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBCluster)
	assertForbiddenAbsent(t, attributesOf(t, cluster), "cluster")
	anchors, _ := cluster.Payload["correlation_anchors"].([]string)
	for _, anchor := range anchors {
		for _, forbidden := range forbiddenSubstrings() {
			if contains(anchor, forbidden) {
				t.Fatalf("correlation anchor %q contains forbidden substring %q", anchor, forbidden)
			}
		}
	}
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{clusters: []DBCluster{{
		ARN:               "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb",
		Identifier:        "orders-docdb",
		DBSubnetGroupName: "missing-subnet-group",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes); got != 0 {
		t.Fatalf("relationship count = %d, want 0 without resolvable target identity", got)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{clusters: []DBCluster{{
		ARN:        "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb",
		Identifier: "orders-docdb",
		KMSKeyID:   "alias/orders-docdb",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipDocDBClusterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders-docdb"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}
