// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsdocdb "github.com/aws/aws-sdk-go-v2/service/docdb"
	awsdocdbtypes "github.com/aws/aws-sdk-go-v2/service/docdb/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListsDocDBMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb"
	instanceARN := "arn:aws:rds:us-east-1:123456789012:db:orders-docdb-1"
	paramGroupARN := "arn:aws:rds:us-east-1:123456789012:cluster-pg:orders-docdb-params"
	api := &fakeDocDBAPI{
		clusterPages: []*awsdocdb.DescribeDBClustersOutput{{
			DBClusters: []awsdocdbtypes.DBCluster{{
				DBClusterArn:                 awsv2.String(clusterARN),
				DBClusterIdentifier:          awsv2.String("orders-docdb"),
				DbClusterResourceId:          awsv2.String("cluster-ORDERSDOCDB"),
				Engine:                       awsv2.String("docdb"),
				EngineVersion:                awsv2.String("5.0.0"),
				Status:                       awsv2.String("available"),
				Endpoint:                     awsv2.String("orders.cluster.docdb.amazonaws.com"),
				ReaderEndpoint:               awsv2.String("orders.cluster-ro.docdb.amazonaws.com"),
				HostedZoneId:                 awsv2.String("Z2"),
				Port:                         awsv2.Int32(27017),
				MultiAZ:                      awsv2.Bool(true),
				StorageEncrypted:             awsv2.Bool(true),
				KmsKeyId:                     awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-docdb"),
				DeletionProtection:           awsv2.Bool(true),
				BackupRetentionPeriod:        awsv2.Int32(7),
				DBSubnetGroup:                awsv2.String("orders-docdb-subnets"),
				DBClusterParameterGroup:      awsv2.String("orders-docdb-params"),
				EnabledCloudwatchLogsExports: []string{"audit"},
				VpcSecurityGroups:            []awsdocdbtypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: awsv2.String("sg-123")}},
				DBClusterMembers:             []awsdocdbtypes.DBClusterMember{{DBInstanceIdentifier: awsv2.String("orders-docdb-1"), IsClusterWriter: awsv2.Bool(true)}},
				AssociatedRoles:              []awsdocdbtypes.DBClusterRole{{RoleArn: awsv2.String("arn:aws:iam::123456789012:role/docdb")}},
				// Forbidden fields the mapper must drop:
				MasterUsername:   awsv2.String("do-not-copy"),
				MasterUserSecret: &awsdocdbtypes.ClusterMasterUserSecret{SecretArn: awsv2.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:do-not-copy")},
			}},
		}},
		instancePages: []*awsdocdb.DescribeDBInstancesOutput{{
			DBInstances: []awsdocdbtypes.DBInstance{{
				DBInstanceArn:        awsv2.String(instanceARN),
				DBInstanceIdentifier: awsv2.String("orders-docdb-1"),
				DbiResourceId:        awsv2.String("db-ORDERSDOCDB1"),
				DBInstanceClass:      awsv2.String("db.r6g.large"),
				Engine:               awsv2.String("docdb"),
				EngineVersion:        awsv2.String("5.0.0"),
				DBInstanceStatus:     awsv2.String("available"),
				Endpoint:             &awsdocdbtypes.Endpoint{Address: awsv2.String("orders-docdb-1.docdb.amazonaws.com"), Port: awsv2.Int32(27017), HostedZoneId: awsv2.String("Z2")},
				AvailabilityZone:     awsv2.String("us-east-1a"),
				StorageEncrypted:     awsv2.Bool(true),
				KmsKeyId:             awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-docdb"),
				DBClusterIdentifier:  awsv2.String("orders-docdb"),
				PromotionTier:        awsv2.Int32(1),
			}},
		}},
		parameterGroupPages: []*awsdocdb.DescribeDBClusterParameterGroupsOutput{{
			DBClusterParameterGroups: []awsdocdbtypes.DBClusterParameterGroup{{
				DBClusterParameterGroupArn:  awsv2.String(paramGroupARN),
				DBClusterParameterGroupName: awsv2.String("orders-docdb-params"),
				DBParameterGroupFamily:      awsv2.String("docdb5.0"),
				Description:                 awsv2.String("orders docdb cluster parameters"),
			}},
		}},
		parameterPages: map[string][]*awsdocdb.DescribeDBClusterParametersOutput{
			"orders-docdb-params": {{
				Parameters: []awsdocdbtypes.Parameter{
					{ParameterName: awsv2.String("tls"), ParameterValue: awsv2.String("do-not-copy")},
					{ParameterName: awsv2.String("audit_logs"), ParameterValue: awsv2.String("do-not-copy")},
				},
			}},
		},
		snapshotPages: []*awsdocdb.DescribeDBClusterSnapshotsOutput{{
			DBClusterSnapshots: []awsdocdbtypes.DBClusterSnapshot{{
				DBClusterSnapshotArn:        awsv2.String("arn:aws:rds:us-east-1:123456789012:cluster-snapshot:orders-docdb-2026-05-01"),
				DBClusterSnapshotIdentifier: awsv2.String("orders-docdb-2026-05-01"),
				DBClusterIdentifier:         awsv2.String("orders-docdb"),
				Engine:                      awsv2.String("docdb"),
				EngineVersion:               awsv2.String("5.0.0"),
				Status:                      awsv2.String("available"),
				SnapshotType:                awsv2.String("manual"),
				StorageEncrypted:            awsv2.Bool(true),
				KmsKeyId:                    awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders-docdb"),
				VpcId:                       awsv2.String("vpc-123"),
				MasterUsername:              awsv2.String("do-not-copy"),
			}},
		}},
		subnetGroupPages: []*awsdocdb.DescribeDBSubnetGroupsOutput{{
			DBSubnetGroups: []awsdocdbtypes.DBSubnetGroup{{
				DBSubnetGroupArn:         awsv2.String("arn:aws:rds:us-east-1:123456789012:subgrp:orders-docdb-subnets"),
				DBSubnetGroupName:        awsv2.String("orders-docdb-subnets"),
				DBSubnetGroupDescription: awsv2.String("orders docdb subnets"),
				SubnetGroupStatus:        awsv2.String("Complete"),
				VpcId:                    awsv2.String("vpc-123"),
				Subnets:                  []awsdocdbtypes.Subnet{{SubnetIdentifier: awsv2.String("subnet-a")}},
			}},
		}},
		globalClusterPages: []*awsdocdb.DescribeGlobalClustersOutput{{
			GlobalClusters: []awsdocdbtypes.GlobalCluster{{
				GlobalClusterArn:        awsv2.String("arn:aws:rds::123456789012:global-cluster:orders-global"),
				GlobalClusterIdentifier: awsv2.String("orders-global"),
				GlobalClusterResourceId: awsv2.String("global-ORDERS"),
				Engine:                  awsv2.String("docdb"),
				EngineVersion:           awsv2.String("5.0.0"),
				Status:                  awsv2.String("available"),
				StorageEncrypted:        awsv2.Bool(true),
				DeletionProtection:      awsv2.Bool(true),
				GlobalClusterMembers:    []awsdocdbtypes.GlobalClusterMember{{DBClusterArn: awsv2.String(clusterARN), IsWriter: awsv2.Bool(true)}},
				TagList:                 []awsdocdbtypes.Tag{{Key: awsv2.String("Scope"), Value: awsv2.String("global")}},
			}},
		}},
		eventSubscriptionPages: []*awsdocdb.DescribeEventSubscriptionsOutput{{
			EventSubscriptionsList: []awsdocdbtypes.EventSubscription{{
				EventSubscriptionArn: awsv2.String("arn:aws:rds:us-east-1:123456789012:es:orders-docdb-events"),
				CustSubscriptionId:   awsv2.String("orders-docdb-events"),
				CustomerAwsId:        awsv2.String("123456789012"),
				Enabled:              awsv2.Bool(true),
				Status:               awsv2.String("active"),
				SourceType:           awsv2.String("db-cluster"),
				SnsTopicArn:          awsv2.String("arn:aws:sns:us-east-1:123456789012:docdb-alerts"),
				SourceIdsList:        []string{"orders-docdb"},
				EventCategoriesList:  []string{"failover"},
			}},
		}},
		tags: map[string]*awsdocdb.ListTagsForResourceOutput{
			clusterARN: {TagList: []awsdocdbtypes.Tag{{Key: awsv2.String("Tier"), Value: awsv2.String("data")}}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListDBClusters(context.Background())
	if err != nil {
		t.Fatalf("ListDBClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	cluster := clusters[0]
	if cluster.Identifier != "orders-docdb" || cluster.ReaderEndpointAddress != "orders.cluster-ro.docdb.amazonaws.com" {
		t.Fatalf("cluster = %#v, want mapped identity and reader endpoint", cluster)
	}
	if cluster.Tags["Tier"] != "data" {
		t.Fatalf("cluster tags = %#v, want Tier=data", cluster.Tags)
	}
	if len(cluster.Members) != 1 || !cluster.Members[0].IsWriter {
		t.Fatalf("cluster members = %#v, want one writer member", cluster.Members)
	}

	instances, err := adapter.ListClusterInstances(context.Background())
	if err != nil {
		t.Fatalf("ListClusterInstances() error = %v", err)
	}
	if instances[0].EndpointAddress != "orders-docdb-1.docdb.amazonaws.com" || instances[0].EndpointPort != 27017 {
		t.Fatalf("instance = %#v, want mapped endpoint", instances[0])
	}

	parameterGroups, err := adapter.ListClusterParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListClusterParameterGroups() error = %v", err)
	}
	if parameterGroups[0].Family != "docdb5.0" {
		t.Fatalf("parameterGroup family = %q, want docdb5.0", parameterGroups[0].Family)
	}
	if got, want := parameterGroups[0].ParameterCount, 2; got != want {
		t.Fatalf("ParameterCount = %d, want %d (count only, never values)", got, want)
	}

	snapshots, err := adapter.ListClusterSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListClusterSnapshots() error = %v", err)
	}
	if snapshots[0].SnapshotType != "manual" || snapshots[0].ClusterIdentifier != "orders-docdb" {
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

	eventSubscriptions, err := adapter.ListEventSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListEventSubscriptions() error = %v", err)
	}
	if eventSubscriptions[0].SourceType != "db-cluster" || !eventSubscriptions[0].Enabled {
		t.Fatalf("eventSubscription = %#v, want mapped subscription", eventSubscriptions[0])
	}
}

func TestClientUsesMarkersAndMaxRecords(t *testing.T) {
	api := &fakeDocDBAPI{
		clusterPages: []*awsdocdb.DescribeDBClustersOutput{{
			DBClusters: []awsdocdbtypes.DBCluster{{DBClusterIdentifier: awsv2.String("first")}},
			Marker:     awsv2.String("next-clusters"),
		}, {
			DBClusters: []awsdocdbtypes.DBCluster{{DBClusterIdentifier: awsv2.String("second")}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListDBClusters(context.Background())
	if err != nil {
		t.Fatalf("ListDBClusters() error = %v", err)
	}
	if got, want := len(clusters), 2; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if got, want := api.clusterMarkers, []string{"", "next-clusters"}; !stringSlicesEqual(got, want) {
		t.Fatalf("DescribeDBClusters Marker = %#v, want %#v", got, want)
	}
	if got, want := api.clusterMaxRecords, []int32{100, 100}; !int32SlicesEqual(got, want) {
		t.Fatalf("DescribeDBClusters MaxRecords = %#v, want %#v", got, want)
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceDocDB,
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
