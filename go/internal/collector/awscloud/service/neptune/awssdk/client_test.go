// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsneptune "github.com/aws/aws-sdk-go-v2/service/neptune"
	awsneptunetypes "github.com/aws/aws-sdk-go-v2/service/neptune/types"
	awsneptunegraph "github.com/aws/aws-sdk-go-v2/service/neptunegraph"
	awsneptunegraphtypes "github.com/aws/aws-sdk-go-v2/service/neptunegraph/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
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
				DBClusterArn:                 aws.String(clusterARN),
				DBClusterIdentifier:          aws.String("orders-neptune"),
				DbClusterResourceId:          aws.String("cluster-ORDERSNEPTUNE"),
				Engine:                       aws.String("neptune"),
				EngineVersion:                aws.String("1.3.0.0"),
				Status:                       aws.String("available"),
				Endpoint:                     aws.String("orders.cluster.neptune.amazonaws.com"),
				ReaderEndpoint:               aws.String("orders.cluster-ro.neptune.amazonaws.com"),
				HostedZoneId:                 aws.String("Z2"),
				Port:                         aws.Int32(8182),
				MultiAZ:                      aws.Bool(true),
				StorageEncrypted:             aws.Bool(true),
				KmsKeyId:                     aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				DeletionProtection:           aws.Bool(true),
				BackupRetentionPeriod:        aws.Int32(7),
				DBSubnetGroup:                aws.String("orders-neptune-subnets"),
				DBClusterParameterGroup:      aws.String("orders-neptune-params"),
				EnabledCloudwatchLogsExports: []string{"audit"},
				VpcSecurityGroups:            []awsneptunetypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-123")}},
				DBClusterMembers:             []awsneptunetypes.DBClusterMember{{DBInstanceIdentifier: aws.String("orders-neptune-1"), IsClusterWriter: aws.Bool(true)}},
				AssociatedRoles:              []awsneptunetypes.DBClusterRole{{RoleArn: aws.String("arn:aws:iam::123456789012:role/neptune")}},
				// Forbidden field the mapper must drop:
				MasterUsername: aws.String("do-not-copy"),
			}},
		}},
		instancePages: []*awsneptune.DescribeDBInstancesOutput{{
			DBInstances: []awsneptunetypes.DBInstance{{
				DBInstanceArn:        aws.String(instanceARN),
				DBInstanceIdentifier: aws.String("orders-neptune-1"),
				DbiResourceId:        aws.String("db-ORDERSNEPTUNE1"),
				DBInstanceClass:      aws.String("db.r6g.large"),
				Engine:               aws.String("neptune"),
				EngineVersion:        aws.String("1.3.0.0"),
				DBInstanceStatus:     aws.String("available"),
				Endpoint:             &awsneptunetypes.Endpoint{Address: aws.String("orders-neptune-1.neptune.amazonaws.com"), Port: aws.Int32(8182), HostedZoneId: aws.String("Z2")},
				AvailabilityZone:     aws.String("us-east-1a"),
				StorageEncrypted:     aws.Bool(true),
				KmsKeyId:             aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				DBClusterIdentifier:  aws.String("orders-neptune"),
				PromotionTier:        aws.Int32(1),
				MasterUsername:       aws.String("do-not-copy"),
			}},
		}},
		parameterGroupPages: []*awsneptune.DescribeDBClusterParameterGroupsOutput{{
			DBClusterParameterGroups: []awsneptunetypes.DBClusterParameterGroup{{
				DBClusterParameterGroupArn:  aws.String(paramGroupARN),
				DBClusterParameterGroupName: aws.String("orders-neptune-params"),
				DBParameterGroupFamily:      aws.String("neptune1.3"),
				Description:                 aws.String("orders neptune cluster parameters"),
			}},
		}},
		snapshotPages: []*awsneptune.DescribeDBClusterSnapshotsOutput{{
			DBClusterSnapshots: []awsneptunetypes.DBClusterSnapshot{{
				DBClusterSnapshotArn:        aws.String("arn:aws:rds:us-east-1:123456789012:cluster-snapshot:orders-neptune-2026-05-01"),
				DBClusterSnapshotIdentifier: aws.String("orders-neptune-2026-05-01"),
				DBClusterIdentifier:         aws.String("orders-neptune"),
				Engine:                      aws.String("neptune"),
				EngineVersion:               aws.String("1.3.0.0"),
				Status:                      aws.String("available"),
				SnapshotType:                aws.String("manual"),
				StorageEncrypted:            aws.Bool(true),
				KmsKeyId:                    aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				VpcId:                       aws.String("vpc-123"),
				MasterUsername:              aws.String("do-not-copy"),
			}},
		}},
		subnetGroupPages: []*awsneptune.DescribeDBSubnetGroupsOutput{{
			DBSubnetGroups: []awsneptunetypes.DBSubnetGroup{{
				DBSubnetGroupArn:         aws.String("arn:aws:rds:us-east-1:123456789012:subgrp:orders-neptune-subnets"),
				DBSubnetGroupName:        aws.String("orders-neptune-subnets"),
				DBSubnetGroupDescription: aws.String("orders neptune subnets"),
				SubnetGroupStatus:        aws.String("Complete"),
				VpcId:                    aws.String("vpc-123"),
				Subnets:                  []awsneptunetypes.Subnet{{SubnetIdentifier: aws.String("subnet-a")}},
			}},
		}},
		globalClusterPages: []*awsneptune.DescribeGlobalClustersOutput{{
			GlobalClusters: []awsneptunetypes.GlobalCluster{{
				GlobalClusterArn:        aws.String("arn:aws:rds::123456789012:global-cluster:orders-global"),
				GlobalClusterIdentifier: aws.String("orders-global"),
				GlobalClusterResourceId: aws.String("global-ORDERS"),
				Engine:                  aws.String("neptune"),
				EngineVersion:           aws.String("1.3.0.0"),
				Status:                  aws.String("available"),
				StorageEncrypted:        aws.Bool(true),
				DeletionProtection:      aws.Bool(true),
				GlobalClusterMembers:    []awsneptunetypes.GlobalClusterMember{{DBClusterArn: aws.String(clusterARN), IsWriter: aws.Bool(true)}},
				TagList:                 []awsneptunetypes.Tag{{Key: aws.String("Scope"), Value: aws.String("global")}},
			}},
		}},
		tags: map[string]*awsneptune.ListTagsForResourceOutput{
			clusterARN: {TagList: []awsneptunetypes.Tag{{Key: aws.String("Tier"), Value: aws.String("data")}}},
		},
	}

	dimension := int32(1536)
	graphAPI := &fakeNeptuneGraphAPI{
		graphPages: []*awsneptunegraph.ListGraphsOutput{{
			Graphs: []awsneptunegraphtypes.GraphSummary{{
				Arn:    aws.String(graphARN),
				Id:     aws.String("g-orders"),
				Name:   aws.String("orders-graph"),
				Status: awsneptunegraphtypes.GraphStatusAvailable,
			}},
		}},
		graphDetails: map[string]*awsneptunegraph.GetGraphOutput{
			"g-orders": {
				Arn:                       aws.String(graphARN),
				Id:                        aws.String("g-orders"),
				Name:                      aws.String("orders-graph"),
				Status:                    awsneptunegraphtypes.GraphStatusAvailable,
				KmsKeyIdentifier:          aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				ProvisionedMemory:         aws.Int32(128),
				ReplicaCount:              aws.Int32(2),
				PublicConnectivity:        aws.Bool(false),
				DeletionProtection:        aws.Bool(true),
				Endpoint:                  aws.String("g-orders.us-east-1.neptune-graph.amazonaws.com"),
				VectorSearchConfiguration: &awsneptunegraphtypes.VectorSearchConfiguration{Dimension: &dimension},
			},
		},
		snapshotPages: []*awsneptunegraph.ListGraphSnapshotsOutput{{
			GraphSnapshots: []awsneptunegraphtypes.GraphSnapshotSummary{{
				Arn:              aws.String(graphSnapshotARN),
				Id:               aws.String("gs-orders"),
				Name:             aws.String("orders-graph-2026-05-01"),
				Status:           awsneptunegraphtypes.SnapshotStatusAvailable,
				KmsKeyIdentifier: aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-neptune"),
				SourceGraphId:    aws.String("g-orders"),
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
			DBClusters: []awsneptunetypes.DBCluster{{DBClusterIdentifier: aws.String("first")}},
			Marker:     aws.String("next-clusters"),
		}, {
			DBClusters: []awsneptunetypes.DBCluster{{DBClusterIdentifier: aws.String("second")}},
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
			Graphs:    []awsneptunegraphtypes.GraphSummary{{Id: aws.String("g-1"), Arn: aws.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-1"), Name: aws.String("first")}},
			NextToken: aws.String("next-graphs"),
		}, {
			Graphs: []awsneptunegraphtypes.GraphSummary{{Id: aws.String("g-2"), Arn: aws.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-2"), Name: aws.String("second")}},
		}},
		graphDetails: map[string]*awsneptunegraph.GetGraphOutput{
			"g-1": {Id: aws.String("g-1"), Arn: aws.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-1"), Name: aws.String("first"), Status: awsneptunegraphtypes.GraphStatusAvailable},
			"g-2": {Id: aws.String("g-2"), Arn: aws.String("arn:aws:neptune-graph:us-east-1:123456789012:graph/g-2"), Name: aws.String("second"), Status: awsneptunegraphtypes.GraphStatusAvailable},
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceNeptune,
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
