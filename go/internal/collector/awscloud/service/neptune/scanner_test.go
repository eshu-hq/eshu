// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestScannerEmitsNeptuneMetadataOnlyFactsAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune"
	instanceARN := "arn:aws:rds:us-east-1:123456789012:db:orders-neptune-1"
	subnetGroupARN := "arn:aws:rds:us-east-1:123456789012:subgrp:orders-neptune-subnets"
	paramGroupARN := "arn:aws:rds:us-east-1:123456789012:cluster-pg:orders-neptune-params"
	snapshotARN := "arn:aws:rds:us-east-1:123456789012:cluster-snapshot:orders-neptune-2026-05-01"
	globalClusterARN := "arn:aws:rds::123456789012:global-cluster:orders-global"
	graphARN := "arn:aws:neptune-graph:us-east-1:123456789012:graph/g-orders"
	graphSnapshotARN := "arn:aws:neptune-graph:us-east-1:123456789012:graph-snapshot/gs-orders"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders-neptune"
	roleARN := "arn:aws:iam::123456789012:role/neptune-s3-load"
	dimension := int32(1536)

	client := fakeClient{
		clusters: []DBCluster{{
			ARN:                          clusterARN,
			Identifier:                   "orders-neptune",
			ResourceID:                   "cluster-ORDERSNEPTUNE",
			Engine:                       "neptune",
			EngineVersion:                "1.3.0.0",
			Status:                       "available",
			EndpointAddress:              "orders.cluster.neptune.amazonaws.com",
			ReaderEndpointAddress:        "orders.cluster-ro.neptune.amazonaws.com",
			HostedZoneID:                 "Z2",
			Port:                         8182,
			MultiAZ:                      true,
			StorageEncrypted:             true,
			KMSKeyID:                     kmsARN,
			DeletionProtection:           true,
			BackupRetentionPeriod:        7,
			DBSubnetGroupName:            "orders-neptune-subnets",
			VPCSecurityGroupIDs:          []string{"sg-123"},
			Members:                      []ClusterMember{{DBInstanceIdentifier: "orders-neptune-1", IsWriter: true}},
			ParameterGroup:               "orders-neptune-params",
			EnabledCloudwatchLogsExports: []string{"audit", "slowquery"},
			AssociatedRoleARNs:           []string{roleARN},
			Tags:                         map[string]string{"Tier": "data"},
		}},
		instances: []ClusterInstance{{
			ARN:               instanceARN,
			Identifier:        "orders-neptune-1",
			ResourceID:        "db-ORDERSNEPTUNE1",
			Class:             "db.r6g.large",
			Engine:            "neptune",
			EngineVersion:     "1.3.0.0",
			Status:            "available",
			EndpointAddress:   "orders-neptune-1.neptune.amazonaws.com",
			EndpointPort:      8182,
			HostedZoneID:      "Z2",
			AvailabilityZone:  "us-east-1a",
			StorageEncrypted:  true,
			KMSKeyID:          kmsARN,
			ClusterIdentifier: "orders-neptune",
			PromotionTier:     1,
			Tags:              map[string]string{"Role": "writer"},
		}},
		parameterGroups: []ClusterParameterGroup{{
			ARN:         paramGroupARN,
			Name:        "orders-neptune-params",
			Family:      "neptune1.3",
			Description: "orders neptune cluster parameters",
			Tags:        map[string]string{"Managed": "true"},
		}},
		snapshots: []ClusterSnapshot{{
			ARN:               snapshotARN,
			Identifier:        "orders-neptune-2026-05-01",
			ClusterIdentifier: "orders-neptune",
			Engine:            "neptune",
			EngineVersion:     "1.3.0.0",
			Status:            "available",
			SnapshotType:      "manual",
			StorageEncrypted:  true,
			KMSKeyID:          kmsARN,
			VPCID:             "vpc-123",
			Tags:              map[string]string{"Retention": "long"},
		}},
		subnetGroups: []SubnetGroup{{
			ARN:         subnetGroupARN,
			Name:        "orders-neptune-subnets",
			Description: "orders neptune subnets",
			Status:      "Complete",
			VPCID:       "vpc-123",
			SubnetIDs:   []string{"subnet-a", "subnet-b"},
			Tags:        map[string]string{"Network": "private"},
		}},
		globalClusters: []GlobalCluster{{
			ARN:                globalClusterARN,
			Identifier:         "orders-global",
			ResourceID:         "global-ORDERS",
			Engine:             "neptune",
			EngineVersion:      "1.3.0.0",
			Status:             "available",
			StorageEncrypted:   true,
			DeletionProtection: true,
			Members:            []GlobalClusterMember{{DBClusterARN: clusterARN, IsWriter: true}},
			Tags:               map[string]string{"Scope": "global"},
		}},
		graphs: []Graph{{
			ARN:                   graphARN,
			ID:                    "g-orders",
			Name:                  "orders-graph",
			Status:                "AVAILABLE",
			KMSKeyID:              kmsARN,
			VectorSearchDimension: &dimension,
			ProvisionedMemory:     128,
			ReplicaCount:          2,
			PublicConnectivity:    false,
			DeletionProtection:    true,
			EndpointAddress:       "g-orders.us-east-1.neptune-graph.amazonaws.com",
			Tags:                  map[string]string{"Use": "search"},
		}},
		graphSnapshots: []GraphSnapshot{{
			ARN:           graphSnapshotARN,
			ID:            "gs-orders",
			Name:          "orders-graph-2026-05-01",
			Status:        "AVAILABLE",
			KMSKeyID:      kmsARN,
			SourceGraphID: "g-orders",
			Tags:          map[string]string{"Retention": "short"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneCluster)
	if got, want := cluster.Payload["arn"], clusterARN; got != want {
		t.Fatalf("cluster arn = %#v, want %q", got, want)
	}
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "engine", "neptune")
	assertAttribute(t, clusterAttributes, "reader_endpoint_address", "orders.cluster-ro.neptune.amazonaws.com")
	assertAttribute(t, clusterAttributes, "storage_encrypted", true)
	assertAttribute(t, clusterAttributes, "member_instance_ids", []string{"orders-neptune-1"})
	assertAttribute(t, clusterAttributes, "enabled_cloudwatch_logs_exports", []string{"audit", "slowquery"})
	assertAttribute(t, clusterAttributes, "associated_role_arns", []string{roleARN})
	assertForbiddenAbsent(t, clusterAttributes, "cluster")

	instance := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneClusterInstance)
	instanceAttributes := attributesOf(t, instance)
	assertAttribute(t, instanceAttributes, "endpoint_address", "orders-neptune-1.neptune.amazonaws.com")
	assertAttribute(t, instanceAttributes, "endpoint_port", int32(8182))
	assertAttribute(t, instanceAttributes, "cluster_identifier", "orders-neptune")
	assertForbiddenAbsent(t, instanceAttributes, "instance")

	paramGroup := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneClusterParameterGroup)
	paramAttributes := attributesOf(t, paramGroup)
	assertAttribute(t, paramAttributes, "family", "neptune1.3")
	for _, forbidden := range []string{"parameters", "parameter_values", "values", "parameter_count"} {
		if _, exists := paramAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; Neptune scanner must persist name and family only, never values", forbidden)
		}
	}

	snapshot := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneClusterSnapshot)
	snapshotAttributes := attributesOf(t, snapshot)
	assertAttribute(t, snapshotAttributes, "snapshot_type", "manual")
	assertAttribute(t, snapshotAttributes, "cluster_identifier", "orders-neptune")
	assertForbiddenAbsent(t, snapshotAttributes, "snapshot")

	subnetGroup := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneSubnetGroup)
	subnetAttributes := attributesOf(t, subnetGroup)
	assertAttribute(t, subnetAttributes, "subnet_ids", []string{"subnet-a", "subnet-b"})
	assertAttribute(t, subnetAttributes, "vpc_id", "vpc-123")

	globalCluster := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneGlobalCluster)
	globalAttributes := attributesOf(t, globalCluster)
	assertAttribute(t, globalAttributes, "engine", "neptune")
	assertAttribute(t, globalAttributes, "member_cluster_arns", []string{clusterARN})

	graph := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneGraph)
	if got, want := graph.Payload["name"], "orders-graph"; got != want {
		t.Fatalf("graph name = %#v, want %q", got, want)
	}
	if got, want := graph.Payload["state"], "AVAILABLE"; got != want {
		t.Fatalf("graph state = %#v, want %q", got, want)
	}
	graphAttributes := attributesOf(t, graph)
	assertAttribute(t, graphAttributes, "vector_search", true)
	assertAttribute(t, graphAttributes, "vector_search_dimension", int32(1536))
	assertAttribute(t, graphAttributes, "replica_count", int32(2))
	assertForbiddenAbsent(t, graphAttributes, "graph")

	graphSnapshot := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneGraphSnapshot)
	graphSnapshotAttributes := attributesOf(t, graphSnapshot)
	assertAttribute(t, graphSnapshotAttributes, "source_graph_id", "g-orders")
	assertForbiddenAbsent(t, graphSnapshotAttributes, "graph_snapshot")

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipNeptuneClusterInSubnetGroup, subnetGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipNeptuneClusterInVPC, "vpc-123")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipNeptuneClusterUsesKMSKey, kmsARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipNeptuneClusterUsesIAMRole, roleARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipNeptuneGraphUsesKMSKey, kmsARN)

	roleRel := relationshipByType(t, envelopes, awscloud.RelationshipNeptuneClusterUsesIAMRole)
	if got, want := roleRel.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("cluster-uses-iam-role target_type = %#v, want %q", got, want)
	}

	memberRel := relationshipByType(t, envelopes, awscloud.RelationshipNeptuneInstanceMemberOfCluster)
	if got, want := memberRel.Payload["target_arn"], clusterARN; got != want {
		t.Fatalf("instance membership target_arn = %#v, want %q", got, want)
	}
	assertAttribute(t, attributesOf(t, memberRel), "is_writer", true)

	globalRel := relationshipByType(t, envelopes, awscloud.RelationshipNeptuneGlobalClusterHasCluster)
	if got, want := globalRel.Payload["target_arn"], clusterARN; got != want {
		t.Fatalf("global cluster membership target_arn = %#v, want %q", got, want)
	}
	assertAttribute(t, attributesOf(t, globalRel), "is_writer", true)

	graphKMSRel := relationshipByType(t, envelopes, awscloud.RelationshipNeptuneGraphUsesKMSKey)
	if got, want := graphKMSRel.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("graph-uses-kms target_type = %#v, want %q", got, want)
	}
}

// TestScannerClusterInVPCEdgeTargetsEC2VPCType proves the cluster-to-VPC
// relationship labels its target with the canonical EC2 VPC resource type
// (aws_ec2_vpc), which is the type the EC2/VPC scanner emits for the VPC node.
func TestScannerClusterInVPCEdgeTargetsEC2VPCType(t *testing.T) {
	client := fakeClient{
		clusters: []DBCluster{{
			ARN:               "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune",
			Identifier:        "orders-neptune",
			DBSubnetGroupName: "orders-neptune-subnets",
		}},
		subnetGroups: []SubnetGroup{{
			ARN:   "arn:aws:rds:us-east-1:123456789012:subgrp:orders-neptune-subnets",
			Name:  "orders-neptune-subnets",
			VPCID: "vpc-123",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := relationshipByType(t, envelopes, awscloud.RelationshipNeptuneClusterInVPC)
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
// value. A Neptune cluster has exactly one writer and N readers.
func TestScannerInstanceMembershipReflectsClusterWriterRole(t *testing.T) {
	const clusterARN = "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune"
	const writerARN = "arn:aws:rds:us-east-1:123456789012:db:orders-neptune-1"
	const readerARN = "arn:aws:rds:us-east-1:123456789012:db:orders-neptune-2"

	client := fakeClient{
		clusters: []DBCluster{{
			ARN:        clusterARN,
			Identifier: "orders-neptune",
			Members: []ClusterMember{
				{DBInstanceIdentifier: "orders-neptune-1", IsWriter: true},
				{DBInstanceIdentifier: "orders-neptune-2", IsWriter: false},
			},
		}},
		instances: []ClusterInstance{
			{
				ARN:               writerARN,
				Identifier:        "orders-neptune-1",
				ClusterIdentifier: "orders-neptune",
			},
			{
				ARN:               readerARN,
				Identifier:        "orders-neptune-2",
				ClusterIdentifier: "orders-neptune",
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	writers := map[string]bool{}
	for _, envelope := range envelopes {
		if got, _ := envelope.Payload["relationship_type"].(string); got != awscloud.RelationshipNeptuneInstanceMemberOfCluster {
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

// TestScannerNeverPersistsMasterUserPasswordAnchors guards the issue #737
// security requirement: even when the SDK shape carries master usernames, the
// scanner must not surface password, secret, vertex, or edge fields in any
// emitted resource attribute set or correlation anchor.
func TestScannerNeverPersistsMasterUserPasswordAnchors(t *testing.T) {
	client := fakeClient{clusters: []DBCluster{{
		ARN:        "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune",
		Identifier: "orders-neptune",
		Engine:     "neptune",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneCluster)
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

// TestScannerGraphWithoutKMSEmitsNoKMSEdge proves a Neptune Analytics graph
// with no reported KMS key (AWS-owned encryption) emits the graph resource but
// no dangling KMS edge.
func TestScannerGraphWithoutKMSEmitsNoKMSEdge(t *testing.T) {
	client := fakeClient{graphs: []Graph{{
		ARN:    "arn:aws:neptune-graph:us-east-1:123456789012:graph/g-orders",
		ID:     "g-orders",
		Name:   "orders-graph",
		Status: "AVAILABLE",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resourceByType(t, envelopes, awscloud.ResourceTypeNeptuneGraph)
	if got := countRelationships(envelopes); got != 0 {
		t.Fatalf("relationship count = %d, want 0 for graph without KMS key", got)
	}
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{clusters: []DBCluster{{
		ARN:               "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune",
		Identifier:        "orders-neptune",
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
		ARN:        "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune",
		Identifier: "orders-neptune",
		KMSKeyID:   "alias/orders-neptune",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipNeptuneClusterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders-neptune"; got != want {
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
