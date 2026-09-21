// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdocdb "github.com/aws/aws-sdk-go-v2/service/docdb"
	awsdocdbtypes "github.com/aws/aws-sdk-go-v2/service/docdb/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsDocDBMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:rds:us-east-1:123456789012:cluster:orders-docdb"
	instanceARN := "arn:aws:rds:us-east-1:123456789012:db:orders-docdb-1"
	paramGroupARN := "arn:aws:rds:us-east-1:123456789012:cluster-pg:orders-docdb-params"
	api := &fakeDocDBAPI{
		clusterPages: []*awsdocdb.DescribeDBClustersOutput{{
			DBClusters: []awsdocdbtypes.DBCluster{{
				DBClusterArn:                 aws.String(clusterARN),
				DBClusterIdentifier:          aws.String("orders-docdb"),
				DbClusterResourceId:          aws.String("cluster-ORDERSDOCDB"),
				Engine:                       aws.String("docdb"),
				EngineVersion:                aws.String("5.0.0"),
				Status:                       aws.String("available"),
				Endpoint:                     aws.String("orders.cluster.docdb.amazonaws.com"),
				ReaderEndpoint:               aws.String("orders.cluster-ro.docdb.amazonaws.com"),
				HostedZoneId:                 aws.String("Z2"),
				Port:                         aws.Int32(27017),
				MultiAZ:                      aws.Bool(true),
				StorageEncrypted:             aws.Bool(true),
				KmsKeyId:                     aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-docdb"),
				DeletionProtection:           aws.Bool(true),
				BackupRetentionPeriod:        aws.Int32(7),
				DBSubnetGroup:                aws.String("orders-docdb-subnets"),
				DBClusterParameterGroup:      aws.String("orders-docdb-params"),
				EnabledCloudwatchLogsExports: []string{"audit"},
				VpcSecurityGroups:            []awsdocdbtypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-123")}},
				DBClusterMembers:             []awsdocdbtypes.DBClusterMember{{DBInstanceIdentifier: aws.String("orders-docdb-1"), IsClusterWriter: aws.Bool(true)}},
				AssociatedRoles:              []awsdocdbtypes.DBClusterRole{{RoleArn: aws.String("arn:aws:iam::123456789012:role/docdb")}},
				// Forbidden fields the mapper must drop:
				MasterUsername:   aws.String("do-not-copy"),
				MasterUserSecret: &awsdocdbtypes.ClusterMasterUserSecret{SecretArn: aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:do-not-copy")},
			}},
		}},
		instancePages: []*awsdocdb.DescribeDBInstancesOutput{{
			DBInstances: []awsdocdbtypes.DBInstance{{
				DBInstanceArn:        aws.String(instanceARN),
				DBInstanceIdentifier: aws.String("orders-docdb-1"),
				DbiResourceId:        aws.String("db-ORDERSDOCDB1"),
				DBInstanceClass:      aws.String("db.r6g.large"),
				Engine:               aws.String("docdb"),
				EngineVersion:        aws.String("5.0.0"),
				DBInstanceStatus:     aws.String("available"),
				Endpoint:             &awsdocdbtypes.Endpoint{Address: aws.String("orders-docdb-1.docdb.amazonaws.com"), Port: aws.Int32(27017), HostedZoneId: aws.String("Z2")},
				AvailabilityZone:     aws.String("us-east-1a"),
				StorageEncrypted:     aws.Bool(true),
				KmsKeyId:             aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-docdb"),
				DBClusterIdentifier:  aws.String("orders-docdb"),
				PromotionTier:        aws.Int32(1),
			}},
		}},
		parameterGroupPages: []*awsdocdb.DescribeDBClusterParameterGroupsOutput{{
			DBClusterParameterGroups: []awsdocdbtypes.DBClusterParameterGroup{{
				DBClusterParameterGroupArn:  aws.String(paramGroupARN),
				DBClusterParameterGroupName: aws.String("orders-docdb-params"),
				DBParameterGroupFamily:      aws.String("docdb5.0"),
				Description:                 aws.String("orders docdb cluster parameters"),
			}},
		}},
		parameterPages: map[string][]*awsdocdb.DescribeDBClusterParametersOutput{
			"orders-docdb-params": {{
				Parameters: []awsdocdbtypes.Parameter{
					{ParameterName: aws.String("tls"), ParameterValue: aws.String("do-not-copy")},
					{ParameterName: aws.String("audit_logs"), ParameterValue: aws.String("do-not-copy")},
				},
			}},
		},
		snapshotPages: []*awsdocdb.DescribeDBClusterSnapshotsOutput{{
			DBClusterSnapshots: []awsdocdbtypes.DBClusterSnapshot{{
				DBClusterSnapshotArn:        aws.String("arn:aws:rds:us-east-1:123456789012:cluster-snapshot:orders-docdb-2026-05-01"),
				DBClusterSnapshotIdentifier: aws.String("orders-docdb-2026-05-01"),
				DBClusterIdentifier:         aws.String("orders-docdb"),
				Engine:                      aws.String("docdb"),
				EngineVersion:               aws.String("5.0.0"),
				Status:                      aws.String("available"),
				SnapshotType:                aws.String("manual"),
				StorageEncrypted:            aws.Bool(true),
				KmsKeyId:                    aws.String("arn:aws:kms:us-east-1:123456789012:key/orders-docdb"),
				VpcId:                       aws.String("vpc-123"),
				MasterUsername:              aws.String("do-not-copy"),
			}},
		}},
		subnetGroupPages: []*awsdocdb.DescribeDBSubnetGroupsOutput{{
			DBSubnetGroups: []awsdocdbtypes.DBSubnetGroup{{
				DBSubnetGroupArn:         aws.String("arn:aws:rds:us-east-1:123456789012:subgrp:orders-docdb-subnets"),
				DBSubnetGroupName:        aws.String("orders-docdb-subnets"),
				DBSubnetGroupDescription: aws.String("orders docdb subnets"),
				SubnetGroupStatus:        aws.String("Complete"),
				VpcId:                    aws.String("vpc-123"),
				Subnets:                  []awsdocdbtypes.Subnet{{SubnetIdentifier: aws.String("subnet-a")}},
			}},
		}},
		globalClusterPages: []*awsdocdb.DescribeGlobalClustersOutput{{
			GlobalClusters: []awsdocdbtypes.GlobalCluster{{
				GlobalClusterArn:        aws.String("arn:aws:rds::123456789012:global-cluster:orders-global"),
				GlobalClusterIdentifier: aws.String("orders-global"),
				GlobalClusterResourceId: aws.String("global-ORDERS"),
				Engine:                  aws.String("docdb"),
				EngineVersion:           aws.String("5.0.0"),
				Status:                  aws.String("available"),
				StorageEncrypted:        aws.Bool(true),
				DeletionProtection:      aws.Bool(true),
				GlobalClusterMembers:    []awsdocdbtypes.GlobalClusterMember{{DBClusterArn: aws.String(clusterARN), IsWriter: aws.Bool(true)}},
				TagList:                 []awsdocdbtypes.Tag{{Key: aws.String("Scope"), Value: aws.String("global")}},
			}},
		}},
		eventSubscriptionPages: []*awsdocdb.DescribeEventSubscriptionsOutput{{
			EventSubscriptionsList: []awsdocdbtypes.EventSubscription{{
				EventSubscriptionArn: aws.String("arn:aws:rds:us-east-1:123456789012:es:orders-docdb-events"),
				CustSubscriptionId:   aws.String("orders-docdb-events"),
				CustomerAwsId:        aws.String("123456789012"),
				Enabled:              aws.Bool(true),
				Status:               aws.String("active"),
				SourceType:           aws.String("db-cluster"),
				SnsTopicArn:          aws.String("arn:aws:sns:us-east-1:123456789012:docdb-alerts"),
				SourceIdsList:        []string{"orders-docdb"},
				EventCategoriesList:  []string{"failover"},
			}},
		}},
		tags: map[string]*awsdocdb.ListTagsForResourceOutput{
			clusterARN: {TagList: []awsdocdbtypes.Tag{{Key: aws.String("Tier"), Value: aws.String("data")}}},
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
			DBClusters: []awsdocdbtypes.DBCluster{{DBClusterIdentifier: aws.String("first")}},
			Marker:     aws.String("next-clusters"),
		}, {
			DBClusters: []awsdocdbtypes.DBCluster{{DBClusterIdentifier: aws.String("second")}},
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDocDB,
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
