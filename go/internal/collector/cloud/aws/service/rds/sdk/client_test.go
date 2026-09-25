// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	awsrdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListsRDSMetadataOnly(t *testing.T) {
	api := &fakeRDSAPI{
		instancePages: []*awsrds.DescribeDBInstancesOutput{{
			DBInstances: []awsrdstypes.DBInstance{{
				DBInstanceArn:                      awsv2.String("arn:aws:rds:us-east-1:123456789012:db:orders-writer"),
				DBInstanceIdentifier:               awsv2.String("orders-writer"),
				DbiResourceId:                      awsv2.String("db-ORDERSWRITER"),
				DBInstanceClass:                    awsv2.String("db.r7g.large"),
				Engine:                             awsv2.String("postgres"),
				EngineVersion:                      awsv2.String("16.3"),
				DBInstanceStatus:                   awsv2.String("available"),
				Endpoint:                           &awsrdstypes.Endpoint{Address: awsv2.String("orders.example"), Port: awsv2.Int32(5432), HostedZoneId: awsv2.String("Z2")},
				AvailabilityZone:                   awsv2.String("us-east-1a"),
				SecondaryAvailabilityZone:          awsv2.String("us-east-1b"),
				MultiAZ:                            awsv2.Bool(true),
				PubliclyAccessible:                 awsv2.Bool(false),
				StorageEncrypted:                   awsv2.Bool(true),
				KmsKeyId:                           awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				IAMDatabaseAuthenticationEnabled:   awsv2.Bool(true),
				DeletionProtection:                 awsv2.Bool(true),
				BackupRetentionPeriod:              awsv2.Int32(7),
				DBSubnetGroup:                      &awsrdstypes.DBSubnetGroup{DBSubnetGroupName: awsv2.String("orders-db"), VpcId: awsv2.String("vpc-123")},
				VpcSecurityGroups:                  []awsrdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: awsv2.String("sg-123")}},
				DBClusterIdentifier:                awsv2.String("orders"),
				DBParameterGroups:                  []awsrdstypes.DBParameterGroupStatus{{DBParameterGroupName: awsv2.String("orders-postgres16"), ParameterApplyStatus: awsv2.String("in-sync")}},
				OptionGroupMemberships:             []awsrdstypes.OptionGroupMembership{{OptionGroupName: awsv2.String("orders-options"), Status: awsv2.String("in-sync")}},
				MonitoringRoleArn:                  awsv2.String("arn:aws:iam::123456789012:role/rds-monitoring"),
				PerformanceInsightsEnabled:         awsv2.Bool(true),
				PerformanceInsightsRetentionPeriod: awsv2.Int32(731),
				PerformanceInsightsKMSKeyId:        awsv2.String("arn:aws:kms:us-east-1:123456789012:key/pi"),
				CACertificateIdentifier:            awsv2.String("rds-ca-rsa2048-g1"),
				DBName:                             awsv2.String("do-not-copy"),
				MasterUsername:                     awsv2.String("do-not-copy"),
			}},
		}},
		clusterPages: []*awsrds.DescribeDBClustersOutput{{
			DBClusters: []awsrdstypes.DBCluster{{
				DBClusterArn:                       awsv2.String("arn:aws:rds:us-east-1:123456789012:cluster:orders"),
				DBClusterIdentifier:                awsv2.String("orders"),
				DbClusterResourceId:                awsv2.String("cluster-ORDERS"),
				Engine:                             awsv2.String("aurora-postgresql"),
				EngineVersion:                      awsv2.String("16.3"),
				Status:                             awsv2.String("available"),
				Endpoint:                           awsv2.String("orders.cluster.example"),
				ReaderEndpoint:                     awsv2.String("orders.cluster-ro.example"),
				HostedZoneId:                       awsv2.String("Z2"),
				Port:                               awsv2.Int32(5432),
				MultiAZ:                            awsv2.Bool(true),
				StorageEncrypted:                   awsv2.Bool(true),
				KmsKeyId:                           awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				IAMDatabaseAuthenticationEnabled:   awsv2.Bool(true),
				DeletionProtection:                 awsv2.Bool(true),
				BackupRetentionPeriod:              awsv2.Int32(7),
				DBSubnetGroup:                      awsv2.String("orders-db"),
				VpcSecurityGroups:                  []awsrdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: awsv2.String("sg-123")}},
				DBClusterMembers:                   []awsrdstypes.DBClusterMember{{DBInstanceIdentifier: awsv2.String("orders-writer"), IsClusterWriter: awsv2.Bool(true)}},
				DBClusterParameterGroup:            awsv2.String("orders-cluster-params"),
				AssociatedRoles:                    []awsrdstypes.DBClusterRole{{RoleArn: awsv2.String("arn:aws:iam::123456789012:role/rds-s3-import")}},
				PubliclyAccessible:                 awsv2.Bool(false),
				PerformanceInsightsEnabled:         awsv2.Bool(true),
				PerformanceInsightsRetentionPeriod: awsv2.Int32(7),
				PerformanceInsightsKMSKeyId:        awsv2.String("arn:aws:kms:us-east-1:123456789012:key/pi"),
				DatabaseName:                       awsv2.String("do-not-copy"),
				MasterUsername:                     awsv2.String("do-not-copy"),
			}},
		}},
		subnetGroupPages: []*awsrds.DescribeDBSubnetGroupsOutput{{
			DBSubnetGroups: []awsrdstypes.DBSubnetGroup{{
				DBSubnetGroupArn:         awsv2.String("arn:aws:rds:us-east-1:123456789012:subgrp:orders-db"),
				DBSubnetGroupName:        awsv2.String("orders-db"),
				DBSubnetGroupDescription: awsv2.String("orders database subnets"),
				SubnetGroupStatus:        awsv2.String("Complete"),
				VpcId:                    awsv2.String("vpc-123"),
				Subnets:                  []awsrdstypes.Subnet{{SubnetIdentifier: awsv2.String("subnet-a")}},
			}},
		}},
		tags: map[string]*awsrds.ListTagsForResourceOutput{
			"arn:aws:rds:us-east-1:123456789012:db:orders-writer": {
				TagList: []awsrdstypes.Tag{{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
			},
			"arn:aws:rds:us-east-1:123456789012:cluster:orders": {
				TagList: []awsrdstypes.Tag{{Key: awsv2.String("Tier"), Value: awsv2.String("data")}},
			},
			"arn:aws:rds:us-east-1:123456789012:subgrp:orders-db": {
				TagList: []awsrdstypes.Tag{{Key: awsv2.String("Network"), Value: awsv2.String("private")}},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	instances, err := adapter.ListDBInstances(context.Background())
	if err != nil {
		t.Fatalf("ListDBInstances() error = %v, want nil", err)
	}
	clusters, err := adapter.ListDBClusters(context.Background())
	if err != nil {
		t.Fatalf("ListDBClusters() error = %v, want nil", err)
	}
	subnetGroups, err := adapter.ListDBSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListDBSubnetGroups() error = %v, want nil", err)
	}

	if got, want := api.instanceMaxRecords, []int32{100}; !int32SlicesEqual(got, want) {
		t.Fatalf("DescribeDBInstances MaxRecords = %#v, want %#v", got, want)
	}
	instance := instances[0]
	if instance.Identifier != "orders-writer" || instance.EndpointAddress != "orders.example" {
		t.Fatalf("instance = %#v, want mapped identity and endpoint", instance)
	}
	if instance.Tags["Environment"] != "prod" {
		t.Fatalf("instance tags = %#v, want Environment=prod", instance.Tags)
	}
	if len(instance.ParameterGroups) != 1 || instance.ParameterGroups[0].Name != "orders-postgres16" {
		t.Fatalf("ParameterGroups = %#v, want orders-postgres16", instance.ParameterGroups)
	}
	if instance.PerformanceInsightsRetentionDays != 731 {
		t.Fatalf("instance PerformanceInsightsRetentionDays = %d, want 731", instance.PerformanceInsightsRetentionDays)
	}
	if instance.CACertificateIdentifier != "rds-ca-rsa2048-g1" {
		t.Fatalf("instance CACertificateIdentifier = %q, want rds-ca-rsa2048-g1", instance.CACertificateIdentifier)
	}
	cluster := clusters[0]
	if cluster.Identifier != "orders" || cluster.ReaderEndpointAddress != "orders.cluster-ro.example" {
		t.Fatalf("cluster = %#v, want mapped identity and reader endpoint", cluster)
	}
	if len(cluster.Members) != 1 || !cluster.Members[0].IsWriter {
		t.Fatalf("Members = %#v, want writer member", cluster.Members)
	}
	if cluster.Tags["Tier"] != "data" {
		t.Fatalf("cluster tags = %#v, want Tier=data", cluster.Tags)
	}
	if cluster.PerformanceInsightsRetentionDays != 7 || !cluster.PerformanceInsightsEnabled {
		t.Fatalf("cluster PI = enabled %v retention %d, want enabled true retention 7", cluster.PerformanceInsightsEnabled, cluster.PerformanceInsightsRetentionDays)
	}
	if cluster.PerformanceInsightsKMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/pi" {
		t.Fatalf("cluster PI KMS = %q, want key/pi", cluster.PerformanceInsightsKMSKeyID)
	}
	subnetGroup := subnetGroups[0]
	if subnetGroup.Name != "orders-db" || subnetGroup.SubnetIDs[0] != "subnet-a" {
		t.Fatalf("subnetGroup = %#v, want mapped subnet group", subnetGroup)
	}
	if subnetGroup.Tags["Network"] != "private" {
		t.Fatalf("subnet group tags = %#v, want Network=private", subnetGroup.Tags)
	}
}

func TestClientUsesMarkersAndMaxRecords(t *testing.T) {
	api := &fakeRDSAPI{
		instancePages: []*awsrds.DescribeDBInstancesOutput{{
			DBInstances: []awsrdstypes.DBInstance{{DBInstanceIdentifier: awsv2.String("first")}},
			Marker:      awsv2.String("next-instances"),
		}, {
			DBInstances: []awsrdstypes.DBInstance{{DBInstanceIdentifier: awsv2.String("second")}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	instances, err := adapter.ListDBInstances(context.Background())
	if err != nil {
		t.Fatalf("ListDBInstances() error = %v, want nil", err)
	}
	if got, want := len(instances), 2; got != want {
		t.Fatalf("len(instances) = %d, want %d", got, want)
	}
	if got, want := api.instanceMarkers, []string{"", "next-instances"}; !stringSlicesEqual(got, want) {
		t.Fatalf("DescribeDBInstances Marker = %#v, want %#v", got, want)
	}
	if got, want := api.instanceMaxRecords, []int32{100, 100}; !int32SlicesEqual(got, want) {
		t.Fatalf("DescribeDBInstances MaxRecords = %#v, want %#v", got, want)
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceRDS,
	}
}

type fakeRDSAPI struct {
	instancePages      []*awsrds.DescribeDBInstancesOutput
	instanceCalls      int
	instanceMarkers    []string
	instanceMaxRecords []int32
	clusterPages       []*awsrds.DescribeDBClustersOutput
	clusterCalls       int
	subnetGroupPages   []*awsrds.DescribeDBSubnetGroupsOutput
	subnetGroupCalls   int
	tags               map[string]*awsrds.ListTagsForResourceOutput
	tagRequests        []string
}

func (f *fakeRDSAPI) DescribeDBInstances(
	_ context.Context,
	input *awsrds.DescribeDBInstancesInput,
	_ ...func(*awsrds.Options),
) (*awsrds.DescribeDBInstancesOutput, error) {
	f.instanceMarkers = append(f.instanceMarkers, awsv2.ToString(input.Marker))
	f.instanceMaxRecords = append(f.instanceMaxRecords, awsv2.ToInt32(input.MaxRecords))
	if f.instanceCalls >= len(f.instancePages) {
		return &awsrds.DescribeDBInstancesOutput{}, nil
	}
	page := f.instancePages[f.instanceCalls]
	f.instanceCalls++
	return page, nil
}

func (f *fakeRDSAPI) DescribeDBClusters(
	context.Context,
	*awsrds.DescribeDBClustersInput,
	...func(*awsrds.Options),
) (*awsrds.DescribeDBClustersOutput, error) {
	if f.clusterCalls >= len(f.clusterPages) {
		return &awsrds.DescribeDBClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeRDSAPI) DescribeDBSubnetGroups(
	context.Context,
	*awsrds.DescribeDBSubnetGroupsInput,
	...func(*awsrds.Options),
) (*awsrds.DescribeDBSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awsrds.DescribeDBSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeRDSAPI) ListTagsForResource(
	_ context.Context,
	input *awsrds.ListTagsForResourceInput,
	_ ...func(*awsrds.Options),
) (*awsrds.ListTagsForResourceOutput, error) {
	resourceARN := awsv2.ToString(input.ResourceName)
	f.tagRequests = append(f.tagRequests, resourceARN)
	if f.tags == nil {
		return &awsrds.ListTagsForResourceOutput{}, nil
	}
	if output := f.tags[resourceARN]; output != nil {
		return output, nil
	}
	return &awsrds.ListTagsForResourceOutput{}, nil
}

var _ apiClient = (*fakeRDSAPI)(nil)

func int32SlicesEqual(got []int32, want []int32) bool {
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

func stringSlicesEqual(got []string, want []string) bool {
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
