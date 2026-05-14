package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	awsrdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsRDSMetadataOnly(t *testing.T) {
	api := &fakeRDSAPI{
		instancePages: []*awsrds.DescribeDBInstancesOutput{{
			DBInstances: []awsrdstypes.DBInstance{{
				DBInstanceArn:                    aws.String("arn:aws:rds:us-east-1:123456789012:db:orders-writer"),
				DBInstanceIdentifier:             aws.String("orders-writer"),
				DbiResourceId:                    aws.String("db-ORDERSWRITER"),
				DBInstanceClass:                  aws.String("db.r7g.large"),
				Engine:                           aws.String("postgres"),
				EngineVersion:                    aws.String("16.3"),
				DBInstanceStatus:                 aws.String("available"),
				Endpoint:                         &awsrdstypes.Endpoint{Address: aws.String("orders.example"), Port: aws.Int32(5432), HostedZoneId: aws.String("Z2")},
				AvailabilityZone:                 aws.String("us-east-1a"),
				SecondaryAvailabilityZone:        aws.String("us-east-1b"),
				MultiAZ:                          aws.Bool(true),
				PubliclyAccessible:               aws.Bool(false),
				StorageEncrypted:                 aws.Bool(true),
				KmsKeyId:                         aws.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				IAMDatabaseAuthenticationEnabled: aws.Bool(true),
				DeletionProtection:               aws.Bool(true),
				BackupRetentionPeriod:            aws.Int32(7),
				DBSubnetGroup:                    &awsrdstypes.DBSubnetGroup{DBSubnetGroupName: aws.String("orders-db"), VpcId: aws.String("vpc-123")},
				VpcSecurityGroups:                []awsrdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-123")}},
				DBClusterIdentifier:              aws.String("orders"),
				DBParameterGroups:                []awsrdstypes.DBParameterGroupStatus{{DBParameterGroupName: aws.String("orders-postgres16"), ParameterApplyStatus: aws.String("in-sync")}},
				OptionGroupMemberships:           []awsrdstypes.OptionGroupMembership{{OptionGroupName: aws.String("orders-options"), Status: aws.String("in-sync")}},
				MonitoringRoleArn:                aws.String("arn:aws:iam::123456789012:role/rds-monitoring"),
				PerformanceInsightsEnabled:       aws.Bool(true),
				PerformanceInsightsKMSKeyId:      aws.String("arn:aws:kms:us-east-1:123456789012:key/pi"),
				DBName:                           aws.String("do-not-copy"),
				MasterUsername:                   aws.String("do-not-copy"),
			}},
		}},
		clusterPages: []*awsrds.DescribeDBClustersOutput{{
			DBClusters: []awsrdstypes.DBCluster{{
				DBClusterArn:                     aws.String("arn:aws:rds:us-east-1:123456789012:cluster:orders"),
				DBClusterIdentifier:              aws.String("orders"),
				DbClusterResourceId:              aws.String("cluster-ORDERS"),
				Engine:                           aws.String("aurora-postgresql"),
				EngineVersion:                    aws.String("16.3"),
				Status:                           aws.String("available"),
				Endpoint:                         aws.String("orders.cluster.example"),
				ReaderEndpoint:                   aws.String("orders.cluster-ro.example"),
				HostedZoneId:                     aws.String("Z2"),
				Port:                             aws.Int32(5432),
				MultiAZ:                          aws.Bool(true),
				StorageEncrypted:                 aws.Bool(true),
				KmsKeyId:                         aws.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				IAMDatabaseAuthenticationEnabled: aws.Bool(true),
				DeletionProtection:               aws.Bool(true),
				BackupRetentionPeriod:            aws.Int32(7),
				DBSubnetGroup:                    aws.String("orders-db"),
				VpcSecurityGroups:                []awsrdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-123")}},
				DBClusterMembers:                 []awsrdstypes.DBClusterMember{{DBInstanceIdentifier: aws.String("orders-writer"), IsClusterWriter: aws.Bool(true)}},
				DBClusterParameterGroup:          aws.String("orders-cluster-params"),
				AssociatedRoles:                  []awsrdstypes.DBClusterRole{{RoleArn: aws.String("arn:aws:iam::123456789012:role/rds-s3-import")}},
				DatabaseName:                     aws.String("do-not-copy"),
				MasterUsername:                   aws.String("do-not-copy"),
			}},
		}},
		subnetGroupPages: []*awsrds.DescribeDBSubnetGroupsOutput{{
			DBSubnetGroups: []awsrdstypes.DBSubnetGroup{{
				DBSubnetGroupArn:         aws.String("arn:aws:rds:us-east-1:123456789012:subgrp:orders-db"),
				DBSubnetGroupName:        aws.String("orders-db"),
				DBSubnetGroupDescription: aws.String("orders database subnets"),
				SubnetGroupStatus:        aws.String("Complete"),
				VpcId:                    aws.String("vpc-123"),
				Subnets:                  []awsrdstypes.Subnet{{SubnetIdentifier: aws.String("subnet-a")}},
			}},
		}},
		tags: map[string]*awsrds.ListTagsForResourceOutput{
			"arn:aws:rds:us-east-1:123456789012:db:orders-writer": {
				TagList: []awsrdstypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
			},
			"arn:aws:rds:us-east-1:123456789012:cluster:orders": {
				TagList: []awsrdstypes.Tag{{Key: aws.String("Tier"), Value: aws.String("data")}},
			},
			"arn:aws:rds:us-east-1:123456789012:subgrp:orders-db": {
				TagList: []awsrdstypes.Tag{{Key: aws.String("Network"), Value: aws.String("private")}},
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
			DBInstances: []awsrdstypes.DBInstance{{DBInstanceIdentifier: aws.String("first")}},
			Marker:      aws.String("next-instances"),
		}, {
			DBInstances: []awsrdstypes.DBInstance{{DBInstanceIdentifier: aws.String("second")}},
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceRDS,
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
	f.instanceMarkers = append(f.instanceMarkers, aws.ToString(input.Marker))
	f.instanceMaxRecords = append(f.instanceMaxRecords, aws.ToInt32(input.MaxRecords))
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
	resourceARN := aws.ToString(input.ResourceName)
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
