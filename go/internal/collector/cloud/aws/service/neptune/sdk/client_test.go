// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsneptune "github.com/aws/aws-sdk-go-v2/service/neptune"
	awsneptunetypes "github.com/aws/aws-sdk-go-v2/service/neptune/types"
	awsneptunegraph "github.com/aws/aws-sdk-go-v2/service/neptunegraph"
	awsneptunegraphtypes "github.com/aws/aws-sdk-go-v2/service/neptunegraph/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListsNeptuneMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:rds:us-east-1:123456789012:cluster:orders-neptune"
	instanceARN := "arn:aws:rds:us-east-1:123456789012:db:orders-neptune-1"
	paramGroupARN := "arn:aws:rds:us-east-1:123456789012:cluster-pg:orders-neptune-params"
	graphARN := "arn:aws:neptune-graph:us-east-1:123456789012:graph/g-orders"
	graphSnapshotARN := "arn:aws:neptune-graph:us-east-1:123456789012:graph-snapshot/gs-orders"

	neptuneAPI := &fakeNeptuneAPI{
		clusterPages: []*awsneptune.DescribeDBClustersOutput{{
			DBClusters: []awsneptunetypes.DBCluster{{
				DBClusterArn:                 awsv2.String(clusterARN),
				DBClusterIdentifier:          awsv2.String("orders-neptune"),
				DbClusterResourceId:          awsv2.String("cluster-ORDERSNEPTUNE"),
				Engine:                       awsv2.String("neptune"),
				EngineVersion:                awsv2.String("1.3.0.0"),
				Status:                       awsv2.String("available"),
				Endpoint:                     awsv2.String("orders.cluster.neptune.amazonaws.com"),
				ReaderEndpoint:               awsv2.String("orders.cluster-ro.neptune.amazonaws.com"),
				HostedZoneId:                 awsv2.String("Z2"),
				Port:                         awsv2.Int32(8182),
				MultiAZ:                      awsv2.Bool(true),
				StorageEncrypted:             awsv2.Bool(true),
				KmsKeyId:                     awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				DeletionProtection:           awsv2.Bool(true),
				BackupRetentionPeriod:        awsv2.Int32(7),
				DBSubnetGroup:                awsv2.String("orders-neptune-subnets"),
				DBClusterParameterGroup:      awsv2.String("orders-neptune-params"),
				EnabledCloudwatchLogsExports: []string{"audit"},
				VpcSecurityGroups:            []awsneptunetypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: awsv2.String("sg-123")}},
				DBClusterMembers:             []awsneptunetypes.DBClusterMember{{DBInstanceIdentifier: awsv2.String("orders-neptune-1"), IsClusterWriter: awsv2.Bool(true)}},
				AssociatedRoles:              []awsneptunetypes.DBClusterRole{{RoleArn: awsv2.String("arn:aws:iam::123456789012:role/neptune")}},
				// Forbidden field the mapper must drop:
				MasterUsername: awsv2.String("do-not-copy"),
			}},
		}},
		instancePages: []*awsneptune.DescribeDBInstancesOutput{{
			DBInstances: []awsneptunetypes.DBInstance{{
				DBInstanceArn:        awsv2.String(instanceARN),
				DBInstanceIdentifier: awsv2.String("orders-neptune-1"),
				DbiResourceId:        awsv2.String("db-ORDERSNEPTUNE1"),
				DBInstanceClass:      awsv2.String("db.r6g.large"),
				Engine:               awsv2.String("neptune"),
				EngineVersion:        awsv2.String("1.3.0.0"),
				DBInstanceStatus:     awsv2.String("available"),
				Endpoint:             &awsneptunetypes.Endpoint{Address: awsv2.String("orders-neptune-1.neptune.amazonaws.com"), Port: awsv2.Int32(8182), HostedZoneId: awsv2.String("Z2")},
				AvailabilityZone:     awsv2.String("us-east-1a"),
				StorageEncrypted:     awsv2.Bool(true),
				KmsKeyId:             awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				DBClusterIdentifier:  awsv2.String("orders-neptune"),
				PromotionTier:        awsv2.Int32(1),
				MasterUsername:       awsv2.String("do-not-copy"),
			}},
		}},
		parameterGroupPages: []*awsneptune.DescribeDBClusterParameterGroupsOutput{{
			DBClusterParameterGroups: []awsneptunetypes.DBClusterParameterGroup{{
				DBClusterParameterGroupArn:  awsv2.String(paramGroupARN),
				DBClusterParameterGroupName: awsv2.String("orders-neptune-params"),
				DBParameterGroupFamily:      awsv2.String("neptune1.3"),
				Description:                 awsv2.String("orders neptune cluster parameters"),
			}},
		}},
		snapshotPages: []*awsneptune.DescribeDBClusterSnapshotsOutput{{
			DBClusterSnapshots: []awsneptunetypes.DBClusterSnapshot{{
				DBClusterSnapshotArn:        awsv2.String("arn:aws:rds:us-east-1:123456789012:cluster-snapshot:orders-neptune-2026-05-01"),
				DBClusterSnapshotIdentifier: awsv2.String("orders-neptune-2026-05-01"),
				DBClusterIdentifier:         awsv2.String("orders-neptune"),
				Engine:                      awsv2.String("neptune"),
				EngineVersion:               awsv2.String("1.3.0.0"),
				Status:                      awsv2.String("available"),
				SnapshotType:                awsv2.String("manual"),
				StorageEncrypted:            awsv2.Bool(true),
				KmsKeyId:                    awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				VpcId:                       awsv2.String("vpc-123"),
				MasterUsername:              awsv2.String("do-not-copy"),
			}},
		}},
		subnetGroupPages: []*awsneptune.DescribeDBSubnetGroupsOutput{{
			DBSubnetGroups: []awsneptunetypes.DBSubnetGroup{{
				DBSubnetGroupArn:         awsv2.String("arn:aws:rds:us-east-1:123456789012:subgrp:orders-neptune-subnets"),
				DBSubnetGroupName:        awsv2.String("orders-neptune-subnets"),
				DBSubnetGroupDescription: awsv2.String("orders neptune subnets"),
				SubnetGroupStatus:        awsv2.String("Complete"),
				VpcId:                    awsv2.String("vpc-123"),
				Subnets:                  []awsneptunetypes.Subnet{{SubnetIdentifier: awsv2.String("subnet-a")}},
			}},
		}},
		globalClusterPages: []*awsneptune.DescribeGlobalClustersOutput{{
			GlobalClusters: []awsneptunetypes.GlobalCluster{{
				GlobalClusterArn:        awsv2.String("arn:aws:rds::123456789012:global-cluster:orders-global"),
				GlobalClusterIdentifier: awsv2.String("orders-global"),
				GlobalClusterResourceId: awsv2.String("global-ORDERS"),
				Engine:                  awsv2.String("neptune"),
				EngineVersion:           awsv2.String("1.3.0.0"),
				Status:                  awsv2.String("available"),
				StorageEncrypted:        awsv2.Bool(true),
				DeletionProtection:      awsv2.Bool(true),
				GlobalClusterMembers:    []awsneptunetypes.GlobalClusterMember{{DBClusterArn: awsv2.String(clusterARN), IsWriter: awsv2.Bool(true)}},
				TagList:                 []awsneptunetypes.Tag{{Key: awsv2.String("Scope"), Value: awsv2.String("global")}},
			}},
		}},
		tags: map[string]*awsneptune.ListTagsForResourceOutput{
			clusterARN: {TagList: []awsneptunetypes.Tag{{Key: awsv2.String("Tier"), Value: awsv2.String("data")}}},
		},
	}

	dimension := int32(1536)
	graphAPI := &fakeNeptuneGraphAPI{
		graphPages: []*awsneptunegraph.ListGraphsOutput{{
			Graphs: []awsneptunegraphtypes.GraphSummary{{
				Arn:    awsv2.String(graphARN),
				Id:     awsv2.String("g-orders"),
				Name:   awsv2.String("orders-graph"),
				Status: awsneptunegraphtypes.GraphStatusAvailable,
			}},
		}},
		graphDetails: map[string]*awsneptunegraph.GetGraphOutput{
			"g-orders": {
				Arn:                       awsv2.String(graphARN),
				Id:                        awsv2.String("g-orders"),
				Name:                      awsv2.String("orders-graph"),
				Status:                    awsneptunegraphtypes.GraphStatusAvailable,
				KmsKeyIdentifier:          awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				ProvisionedMemory:         awsv2.Int32(128),
				ReplicaCount:              awsv2.Int32(2),
				PublicConnectivity:        awsv2.Bool(false),
				DeletionProtection:        awsv2.Bool(true),
				Endpoint:                  awsv2.String("g-orders.us-east-1.neptune-graph.amazonaws.com"),
				VectorSearchConfiguration: &awsneptunegraphtypes.VectorSearchConfiguration{Dimension: &dimension},
			},
		},
		snapshotPages: []*awsneptunegraph.ListGraphSnapshotsOutput{{
			GraphSnapshots: []awsneptunegraphtypes.GraphSnapshotSummary{{
				Arn:              awsv2.String(graphSnapshotARN),
				Id:               awsv2.String("gs-orders"),
				Name:             awsv2.String("orders-graph-2026-05-01"),
				Status:           awsneptunegraphtypes.SnapshotStatusAvailable,
				KmsKeyIdentifier: awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				SourceGraphId:    awsv2.String("g-orders"),
			}},
		}},
		tags: map[string]*awsneptunegraph.ListTagsForResourceOutput{
			graphARN: {Tags: map[string]string{"Use": "search"}},
		},
	}
	adapter := &Client{neptune: neptuneAPI, graph: graphAPI, boundary: testBoundary()}

	clusters, err := adapter.ListDBClusters(context.Background())
	if err != nil {
		t.Fatalf("ListDBClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	cluster := clusters[0]
	if cluster.Identifier != "orders-neptune" || cluster.ReaderEndpointAddress != "orders.cluster-ro.neptune.amazonaws.com" {
		t.Fatalf("cluster = %#v, want mapped identity and reader endpoint", cluster)
	}
	if cluster.Tags["Tier"] != "data" {
		t.Fatalf("cluster tags = %#v, want Tier=data", cluster.Tags)
	}
	if len(cluster.Members) != 1 || !cluster.Members[0].IsWriter {
		t.Fatalf("cluster members = %#v, want one writer member", cluster.Members)
	}
	if len(cluster.AssociatedRoleARNs) != 1 || cluster.AssociatedRoleARNs[0] != "arn:aws:iam::123456789012:role/neptune" {
		t.Fatalf("cluster roles = %#v, want one mapped role ARN", cluster.AssociatedRoleARNs)
	}

	instances, err := adapter.ListClusterInstances(context.Background())
	if err != nil {
		t.Fatalf("ListClusterInstances() error = %v", err)
	}
	if instances[0].EndpointAddress != "orders-neptune-1.neptune.amazonaws.com" || instances[0].EndpointPort != 8182 {
		t.Fatalf("instance = %#v, want mapped endpoint", instances[0])
	}

	parameterGroups, err := adapter.ListClusterParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListClusterParameterGroups() error = %v", err)
	}
	if parameterGroups[0].Family != "neptune1.3" {
		t.Fatalf("parameterGroup family = %q, want neptune1.3", parameterGroups[0].Family)
	}

	snapshots, err := adapter.ListClusterSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListClusterSnapshots() error = %v", err)
	}
	if snapshots[0].SnapshotType != "manual" || snapshots[0].ClusterIdentifier != "orders-neptune" {
		t.Fatalf("snapshot = %#v, want mapped snapshot metadata", snapshots[0])
	}

	subnetGroups, err := adapter.ListSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListSubnetGroups() error = %v", err)
	}
	if subnetGroups[0].VPCID != "vpc-123" || subnetGroups[0].SubnetIDs[0] != "subnet-a" {
		t.Fatalf("subnetGroup = %#v, want mapped subnet group", subnetGroups[0])
	}

	globalClusters, err := adapter.ListGlobalClusters(context.Background())
	if err != nil {
		t.Fatalf("ListGlobalClusters() error = %v", err)
	}
	if globalClusters[0].Tags["Scope"] != "global" {
		t.Fatalf("globalCluster tags = %#v, want Scope=global (inline TagList)", globalClusters[0].Tags)
	}
	if len(globalClusters[0].Members) != 1 || globalClusters[0].Members[0].DBClusterARN != clusterARN {
		t.Fatalf("globalCluster members = %#v, want one cluster member", globalClusters[0].Members)
	}

	graphs, err := adapter.ListGraphs(context.Background())
	if err != nil {
		t.Fatalf("ListGraphs() error = %v", err)
	}
	if len(graphs) != 1 {
		t.Fatalf("len(graphs) = %d, want 1", len(graphs))
	}
	graph := graphs[0]
	if graph.Name != "orders-graph" || graph.Status != "AVAILABLE" {
		t.Fatalf("graph = %#v, want mapped name and status", graph)
	}
	if graph.VectorSearchDimension == nil || *graph.VectorSearchDimension != 1536 {
		t.Fatalf("graph vector dimension = %#v, want 1536 (resolved via GetGraph)", graph.VectorSearchDimension)
	}
	if graph.Tags["Use"] != "search" {
		t.Fatalf("graph tags = %#v, want Use=search", graph.Tags)
	}
	if len(graphAPI.getGraphIDs) != 1 || graphAPI.getGraphIDs[0] != "g-orders" {
		t.Fatalf("GetGraph identifiers = %#v, want [g-orders]", graphAPI.getGraphIDs)
	}

	graphSnapshots, err := adapter.ListGraphSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListGraphSnapshots() error = %v", err)
	}
	if graphSnapshots[0].SourceGraphID != "g-orders" || graphSnapshots[0].Status != "AVAILABLE" {
		t.Fatalf("graphSnapshot = %#v, want mapped snapshot metadata", graphSnapshots[0])
	}
}

func TestClientUsesMarkersAndMaxRecords(t *testing.T) {
	neptuneAPI := &fakeNeptuneAPI{
		clusterPages: []*awsneptune.DescribeDBClustersOutput{{
			DBClusters: []awsneptunetypes.DBCluster{{DBClusterIdentifier: awsv2.String("first")}},
			Marker:     awsv2.String("next-clusters"),
		}, {
			DBClusters: []awsneptunetypes.DBCluster{{DBClusterIdentifier: awsv2.String("second")}},
		}},
	}
	adapter := &Client{neptune: neptuneAPI, graph: &fakeNeptuneGraphAPI{}, boundary: testBoundary()}

	clusters, err := adapter.ListDBClusters(context.Background())
	if err != nil {
		t.Fatalf("ListDBClusters() error = %v", err)
	}
	if got, want := len(clusters), 2; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if got, want := neptuneAPI.clusterMarkers, []string{"", "next-clusters"}; !stringSlicesEqual(got, want) {
		t.Fatalf("DescribeDBClusters Marker = %#v, want %#v", got, want)
	}
	if got, want := neptuneAPI.clusterMaxRecords, []int32{100, 100}; !int32SlicesEqual(got, want) {
		t.Fatalf("DescribeDBClusters MaxRecords = %#v, want %#v", got, want)
	}
}

func TestClientPaginatesGraphsWithNextToken(t *testing.T) {
	graphAPI := &fakeNeptuneGraphAPI{
		graphPages: []*awsneptunegraph.ListGraphsOutput{{
			Graphs:    []awsneptunegraphtypes.GraphSummary{{Id: awsv2.String("g-1"), Arn: awsv2.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-1"), Name: awsv2.String("first")}},
			NextToken: awsv2.String("next-graphs"),
		}, {
			Graphs: []awsneptunegraphtypes.GraphSummary{{Id: awsv2.String("g-2"), Arn: awsv2.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-2"), Name: awsv2.String("second")}},
		}},
		graphDetails: map[string]*awsneptunegraph.GetGraphOutput{
			"g-1": {Id: awsv2.String("g-1"), Arn: awsv2.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-1"), Name: awsv2.String("first"), Status: awsneptunegraphtypes.GraphStatusAvailable},
			"g-2": {Id: awsv2.String("g-2"), Arn: awsv2.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-2"), Name: awsv2.String("second"), Status: awsneptunegraphtypes.GraphStatusAvailable},
		},
	}
	adapter := &Client{neptune: &fakeNeptuneAPI{}, graph: graphAPI, boundary: testBoundary()}

	graphs, err := adapter.ListGraphs(context.Background())
	if err != nil {
		t.Fatalf("ListGraphs() error = %v", err)
	}
	if got, want := len(graphs), 2; got != want {
		t.Fatalf("len(graphs) = %d, want %d", got, want)
	}
	if got, want := graphAPI.graphTokens, []string{"", "next-graphs"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListGraphs NextToken = %#v, want %#v", got, want)
	}
	if got, want := graphAPI.graphMaxRows, []int32{100, 100}; !int32SlicesEqual(got, want) {
		t.Fatalf("ListGraphs MaxResults = %#v, want %#v", got, want)
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceNeptune,
	}
}

func int32SlicesEqual(got, want []int32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
