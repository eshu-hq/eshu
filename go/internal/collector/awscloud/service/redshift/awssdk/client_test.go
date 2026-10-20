// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsredshift "github.com/aws/aws-sdk-go-v2/service/redshift"
	awsredshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	awsserverless "github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	awsserverlesstypes "github.com/aws/aws-sdk-go-v2/service/redshiftserverless/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientMapsProvisionedRedshiftMetadataOnly(t *testing.T) {
	provisioned := &fakeProvisionedAPI{
		clusterPages: []*awsredshift.DescribeClustersOutput{{
			Clusters: []awsredshifttypes.Cluster{{
				ClusterIdentifier:                aws.String("analytics"),
				NodeType:                         aws.String("ra3.xlplus"),
				ClusterStatus:                    aws.String("available"),
				ClusterAvailabilityStatus:        aws.String("Available"),
				DBName:                           aws.String("analytics"),
				Endpoint:                         &awsredshifttypes.Endpoint{Address: aws.String("analytics.example"), Port: aws.Int32(5439)},
				ClusterCreateTime:                aws.Time(time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)),
				AutomatedSnapshotRetentionPeriod: aws.Int32(7),
				ManualSnapshotRetentionPeriod:    aws.Int32(-1),
				VpcSecurityGroups:                []awsredshifttypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-redshift-1")}},
				ClusterParameterGroups:           []awsredshifttypes.ClusterParameterGroupStatus{{ParameterGroupName: aws.String("analytics-params")}},
				ClusterSubnetGroupName:           aws.String("analytics-subnets"),
				VpcId:                            aws.String("vpc-redshift"),
				AvailabilityZone:                 aws.String("us-east-1a"),
				ClusterVersion:                   aws.String("1.0"),
				AllowVersionUpgrade:              aws.Bool(true),
				NumberOfNodes:                    aws.Int32(4),
				PubliclyAccessible:               aws.Bool(false),
				Encrypted:                        aws.Bool(true),
				KmsKeyId:                         aws.String("arn:aws:kms:us-east-1:123456789012:key/analytics"),
				EnhancedVpcRouting:               aws.Bool(true),
				IamRoles:                         []awsredshifttypes.ClusterIamRole{{IamRoleArn: aws.String("arn:aws:iam::123456789012:role/redshift-analytics")}},
				MultiAZ:                          aws.String("Enabled"),
				MasterUsername:                   aws.String("do-not-copy"),
				MasterPasswordSecretArn:          aws.String("do-not-copy"),
				MasterPasswordSecretKmsKeyId:     aws.String("do-not-copy"),
				Tags:                             []awsredshifttypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
			}},
		}},
		parameterGroupPages: []*awsredshift.DescribeClusterParameterGroupsOutput{{
			ParameterGroups: []awsredshifttypes.ClusterParameterGroup{{
				ParameterGroupName:   aws.String("analytics-params"),
				ParameterGroupFamily: aws.String("redshift-1.0"),
				Description:          aws.String("analytics"),
				Tags:                 []awsredshifttypes.Tag{{Key: aws.String("Tier"), Value: aws.String("data")}},
			}},
		}},
		subnetGroupPages: []*awsredshift.DescribeClusterSubnetGroupsOutput{{
			ClusterSubnetGroups: []awsredshifttypes.ClusterSubnetGroup{{
				ClusterSubnetGroupName: aws.String("analytics-subnets"),
				VpcId:                  aws.String("vpc-redshift"),
				Description:            aws.String("subnets"),
				SubnetGroupStatus:      aws.String("Complete"),
				Subnets:                []awsredshifttypes.Subnet{{SubnetIdentifier: aws.String("subnet-a")}},
			}},
		}},
		snapshotPages: []*awsredshift.DescribeClusterSnapshotsOutput{{
			Snapshots: []awsredshifttypes.Snapshot{{
				SnapshotIdentifier:           aws.String("rs:analytics-2026-05-20-00"),
				ClusterIdentifier:            aws.String("analytics"),
				SnapshotType:                 aws.String("automated"),
				Status:                       aws.String("available"),
				NodeType:                     aws.String("ra3.xlplus"),
				NumberOfNodes:                aws.Int32(4),
				DBName:                       aws.String("analytics"),
				VpcId:                        aws.String("vpc-redshift"),
				Encrypted:                    aws.Bool(true),
				KmsKeyId:                     aws.String("arn:aws:kms:us-east-1:123456789012:key/analytics"),
				SnapshotCreateTime:           aws.Time(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)),
				MasterUsername:               aws.String("do-not-copy"),
				MasterPasswordSecretArn:      aws.String("do-not-copy"),
				MasterPasswordSecretKmsKeyId: aws.String("do-not-copy"),
				Tags:                         []awsredshifttypes.Tag{{Key: aws.String("Backup"), Value: aws.String("true")}},
			}},
		}},
		scheduledActionPages: []*awsredshift.DescribeScheduledActionsOutput{{
			ScheduledActions: []awsredshifttypes.ScheduledAction{{
				ScheduledActionName: aws.String("pause-analytics-overnight"),
				Schedule:            aws.String("cron(0 23 * * ? *)"),
				IamRole:             aws.String("arn:aws:iam::123456789012:role/redshift-pauser"),
				State:               awsredshifttypes.ScheduledActionStateActive,
				StartTime:           aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
				NextInvocations:     []time.Time{time.Date(2026, 5, 27, 23, 0, 0, 0, time.UTC)},
				TargetAction: &awsredshifttypes.ScheduledActionType{
					PauseCluster: &awsredshifttypes.PauseClusterMessage{ClusterIdentifier: aws.String("analytics")},
				},
			}},
		}},
	}
	adapter := &Client{provisioned: provisioned, serverless: &fakeServerlessAPI{}, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("len(clusters) = %d, want 1", len(clusters))
	}
	cluster := clusters[0]
	if got, want := cluster.ARN, "arn:aws:redshift:us-east-1:123456789012:cluster:analytics"; got != want {
		t.Fatalf("cluster.ARN = %q, want %q", got, want)
	}
	if cluster.Endpoint != "analytics.example" || cluster.EndpointPort != 5439 {
		t.Fatalf("cluster endpoint = %q:%d, want analytics.example:5439", cluster.Endpoint, cluster.EndpointPort)
	}
	if !cluster.MultiAZ {
		t.Fatalf("cluster.MultiAZ = false, want true (mapped from MultiAZ=\"Enabled\")")
	}
	if cluster.ClusterParameterGroup != "analytics-params" {
		t.Fatalf("cluster.ClusterParameterGroup = %q, want analytics-params", cluster.ClusterParameterGroup)
	}
	if want := []string{"arn:aws:iam::123456789012:role/redshift-analytics"}; !reflect.DeepEqual(cluster.IAMRoleARNs, want) {
		t.Fatalf("cluster.IAMRoleARNs = %#v, want %#v", cluster.IAMRoleARNs, want)
	}
	if got := strings.Join(allClusterFields(cluster), " "); strings.Contains(strings.ToLower(got), "password") {
		t.Fatalf("scanner-owned cluster contains a password field: %q", got)
	}

	groups, err := adapter.ListClusterParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListClusterParameterGroups() error = %v, want nil", err)
	}
	if len(groups) != 1 || groups[0].Family != "redshift-1.0" {
		t.Fatalf("ListClusterParameterGroups() = %#v, want redshift-1.0", groups)
	}
	subnetGroups, err := adapter.ListClusterSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListClusterSubnetGroups() error = %v, want nil", err)
	}
	if len(subnetGroups) != 1 || subnetGroups[0].VPCID != "vpc-redshift" {
		t.Fatalf("ListClusterSubnetGroups() = %#v, want vpc-redshift", subnetGroups)
	}
	snapshots, err := adapter.ListClusterSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListClusterSnapshots() error = %v, want nil", err)
	}
	if len(snapshots) != 1 || snapshots[0].ClusterIdentifier != "analytics" {
		t.Fatalf("ListClusterSnapshots() = %#v, want cluster=analytics", snapshots)
	}
	// The scanner-owned snapshot model must not surface master password fields.
	if got := strings.Join(allSnapshotFields(snapshots[0]), " "); strings.Contains(strings.ToLower(got), "password") {
		t.Fatalf("scanner-owned snapshot contains a password field: %q", got)
	}
	actions, err := adapter.ListScheduledActions(context.Background())
	if err != nil {
		t.Fatalf("ListScheduledActions() error = %v, want nil", err)
	}
	if len(actions) != 1 {
		t.Fatalf("len(actions) = %d, want 1", len(actions))
	}
	if actions[0].TargetActionName != "PauseCluster" || actions[0].TargetClusterIdentifier != "analytics" {
		t.Fatalf("scheduled action target = %s/%s, want PauseCluster/analytics", actions[0].TargetActionName, actions[0].TargetClusterIdentifier)
	}

	if got, want := provisioned.clusterMaxRecords, []int32{100}; !int32SlicesEqual(got, want) {
		t.Fatalf("DescribeClusters MaxRecords = %#v, want %#v", got, want)
	}
}

func TestClientPaginatesClustersAcrossMarkers(t *testing.T) {
	provisioned := &fakeProvisionedAPI{
		clusterPages: []*awsredshift.DescribeClustersOutput{{
			Clusters: []awsredshifttypes.Cluster{{ClusterIdentifier: aws.String("first")}},
			Marker:   aws.String("next-clusters"),
		}, {
			Clusters: []awsredshifttypes.Cluster{{ClusterIdentifier: aws.String("second")}},
		}},
	}
	adapter := &Client{provisioned: provisioned, serverless: &fakeServerlessAPI{}, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("len(clusters) = %d, want 2", len(clusters))
	}
	if got, want := provisioned.clusterMarkers, []string{"", "next-clusters"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cluster markers = %#v, want %#v", got, want)
	}
}

func TestClientMapsServerlessMetadataAndTags(t *testing.T) {
	namespaceARN := "arn:aws:redshift-serverless:us-east-1:123456789012:namespace/analytics-ns"
	workgroupARN := "arn:aws:redshift-serverless:us-east-1:123456789012:workgroup/analytics-wg"
	serverless := &fakeServerlessAPI{
		namespacePages: []*awsserverless.ListNamespacesOutput{{
			Namespaces: []awsserverlesstypes.Namespace{{
				NamespaceArn:                aws.String(namespaceARN),
				NamespaceName:               aws.String("analytics-ns"),
				NamespaceId:                 aws.String("ns-abc"),
				Status:                      awsserverlesstypes.NamespaceStatusAvailable,
				DbName:                      aws.String("analytics"),
				DefaultIamRoleArn:           aws.String("arn:aws:iam::123456789012:role/redshift-serverless"),
				IamRoles:                    []string{"arn:aws:iam::123456789012:role/redshift-serverless"},
				KmsKeyId:                    aws.String("arn:aws:kms:us-east-1:123456789012:key/analytics-ns"),
				LogExports:                  []awsserverlesstypes.LogExport{awsserverlesstypes.LogExportConnectionLog, awsserverlesstypes.LogExportUserLog},
				CreationDate:                aws.Time(time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)),
				AdminUsername:               aws.String("do-not-copy"),
				AdminPasswordSecretArn:      aws.String("do-not-copy"),
				AdminPasswordSecretKmsKeyId: aws.String("do-not-copy"),
			}},
		}},
		workgroupPages: []*awsserverless.ListWorkgroupsOutput{{
			Workgroups: []awsserverlesstypes.Workgroup{{
				WorkgroupArn:       aws.String(workgroupARN),
				WorkgroupName:      aws.String("analytics-wg"),
				WorkgroupId:        aws.String("wg-abc"),
				NamespaceName:      aws.String("analytics-ns"),
				Status:             awsserverlesstypes.WorkgroupStatusAvailable,
				BaseCapacity:       aws.Int32(64),
				MaxCapacity:        aws.Int32(512),
				EnhancedVpcRouting: aws.Bool(true),
				PubliclyAccessible: aws.Bool(false),
				ConfigParameters: []awsserverlesstypes.ConfigParameter{{
					ParameterKey:   aws.String("datestyle"),
					ParameterValue: aws.String("ISO, MDY"),
				}},
				SubnetIds:        []string{"subnet-a", "subnet-b"},
				SecurityGroupIds: []string{"sg-redshift-wg"},
				Endpoint:         &awsserverlesstypes.Endpoint{Address: aws.String("analytics-wg.example"), Port: aws.Int32(5439)},
				CreationDate:     aws.Time(time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)),
			}},
		}},
		tags: map[string][]awsserverlesstypes.Tag{
			namespaceARN: {{Key: aws.String("Owner"), Value: aws.String("analytics")}},
			workgroupARN: {{Key: aws.String("Owner"), Value: aws.String("analytics")}},
		},
	}
	adapter := &Client{provisioned: &fakeProvisionedAPI{}, serverless: serverless, boundary: testBoundary()}

	namespaces, err := adapter.ListServerlessNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListServerlessNamespaces() error = %v, want nil", err)
	}
	if len(namespaces) != 1 {
		t.Fatalf("len(namespaces) = %d, want 1", len(namespaces))
	}
	namespace := namespaces[0]
	if namespace.ARN != namespaceARN || namespace.NamespaceID != "ns-abc" {
		t.Fatalf("namespace identity = %s/%s", namespace.ARN, namespace.NamespaceID)
	}
	if namespace.Tags["Owner"] != "analytics" {
		t.Fatalf("namespace tags = %#v, want Owner=analytics", namespace.Tags)
	}
	if want := []string{"connectionlog", "userlog"}; !reflect.DeepEqual(namespace.LogExports, want) {
		t.Fatalf("namespace.LogExports = %#v, want %#v", namespace.LogExports, want)
	}
	if got := strings.Join(allNamespaceFields(namespace), " "); strings.Contains(strings.ToLower(got), "password") {
		t.Fatalf("scanner-owned namespace contains a password field: %q", got)
	}

	workgroups, err := adapter.ListServerlessWorkgroups(context.Background())
	if err != nil {
		t.Fatalf("ListServerlessWorkgroups() error = %v, want nil", err)
	}
	workgroup := workgroups[0]
	if workgroup.NamespaceName != "analytics-ns" || workgroup.BaseCapacity != 64 {
		t.Fatalf("workgroup = %#v", workgroup)
	}
	if workgroup.Tags["Owner"] != "analytics" {
		t.Fatalf("workgroup tags = %#v, want Owner=analytics", workgroup.Tags)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceRedshift,
	}
}

type fakeProvisionedAPI struct {
	clusterPages         []*awsredshift.DescribeClustersOutput
	clusterCalls         int
	clusterMarkers       []string
	clusterMaxRecords    []int32
	parameterGroupPages  []*awsredshift.DescribeClusterParameterGroupsOutput
	parameterGroupCalls  int
	subnetGroupPages     []*awsredshift.DescribeClusterSubnetGroupsOutput
	subnetGroupCalls     int
	snapshotPages        []*awsredshift.DescribeClusterSnapshotsOutput
	snapshotCalls        int
	scheduledActionPages []*awsredshift.DescribeScheduledActionsOutput
	scheduledActionCalls int
}

func (f *fakeProvisionedAPI) DescribeClusters(
	_ context.Context,
	input *awsredshift.DescribeClustersInput,
	_ ...func(*awsredshift.Options),
) (*awsredshift.DescribeClustersOutput, error) {
	f.clusterMarkers = append(f.clusterMarkers, aws.ToString(input.Marker))
	f.clusterMaxRecords = append(f.clusterMaxRecords, aws.ToInt32(input.MaxRecords))
	if f.clusterCalls >= len(f.clusterPages) {
		return &awsredshift.DescribeClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeProvisionedAPI) DescribeClusterParameterGroups(
	context.Context,
	*awsredshift.DescribeClusterParameterGroupsInput,
	...func(*awsredshift.Options),
) (*awsredshift.DescribeClusterParameterGroupsOutput, error) {
	if f.parameterGroupCalls >= len(f.parameterGroupPages) {
		return &awsredshift.DescribeClusterParameterGroupsOutput{}, nil
	}
	page := f.parameterGroupPages[f.parameterGroupCalls]
	f.parameterGroupCalls++
	return page, nil
}

func (f *fakeProvisionedAPI) DescribeClusterSubnetGroups(
	context.Context,
	*awsredshift.DescribeClusterSubnetGroupsInput,
	...func(*awsredshift.Options),
) (*awsredshift.DescribeClusterSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awsredshift.DescribeClusterSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeProvisionedAPI) DescribeClusterSnapshots(
	context.Context,
	*awsredshift.DescribeClusterSnapshotsInput,
	...func(*awsredshift.Options),
) (*awsredshift.DescribeClusterSnapshotsOutput, error) {
	if f.snapshotCalls >= len(f.snapshotPages) {
		return &awsredshift.DescribeClusterSnapshotsOutput{}, nil
	}
	page := f.snapshotPages[f.snapshotCalls]
	f.snapshotCalls++
	return page, nil
}

func (f *fakeProvisionedAPI) DescribeScheduledActions(
	context.Context,
	*awsredshift.DescribeScheduledActionsInput,
	...func(*awsredshift.Options),
) (*awsredshift.DescribeScheduledActionsOutput, error) {
	if f.scheduledActionCalls >= len(f.scheduledActionPages) {
		return &awsredshift.DescribeScheduledActionsOutput{}, nil
	}
	page := f.scheduledActionPages[f.scheduledActionCalls]
	f.scheduledActionCalls++
	return page, nil
}

type fakeServerlessAPI struct {
	namespacePages []*awsserverless.ListNamespacesOutput
	namespaceCalls int
	workgroupPages []*awsserverless.ListWorkgroupsOutput
	workgroupCalls int
	tags           map[string][]awsserverlesstypes.Tag
	tagRequests    []string
}

func (f *fakeServerlessAPI) ListNamespaces(
	context.Context,
	*awsserverless.ListNamespacesInput,
	...func(*awsserverless.Options),
) (*awsserverless.ListNamespacesOutput, error) {
	if f.namespaceCalls >= len(f.namespacePages) {
		return &awsserverless.ListNamespacesOutput{}, nil
	}
	page := f.namespacePages[f.namespaceCalls]
	f.namespaceCalls++
	return page, nil
}

func (f *fakeServerlessAPI) ListWorkgroups(
	context.Context,
	*awsserverless.ListWorkgroupsInput,
	...func(*awsserverless.Options),
) (*awsserverless.ListWorkgroupsOutput, error) {
	if f.workgroupCalls >= len(f.workgroupPages) {
		return &awsserverless.ListWorkgroupsOutput{}, nil
	}
	page := f.workgroupPages[f.workgroupCalls]
	f.workgroupCalls++
	return page, nil
}

func (f *fakeServerlessAPI) ListTagsForResource(
	_ context.Context,
	input *awsserverless.ListTagsForResourceInput,
	_ ...func(*awsserverless.Options),
) (*awsserverless.ListTagsForResourceOutput, error) {
	resourceARN := aws.ToString(input.ResourceArn)
	f.tagRequests = append(f.tagRequests, resourceARN)
	if f.tags == nil {
		return &awsserverless.ListTagsForResourceOutput{}, nil
	}
	return &awsserverless.ListTagsForResourceOutput{Tags: f.tags[resourceARN]}, nil
}

var (
	_ provisionedAPI = (*fakeProvisionedAPI)(nil)
	_ serverlessAPI  = (*fakeServerlessAPI)(nil)
)

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

func allClusterFields(value any) []string {
	return reflectFieldNames(value)
}

func allSnapshotFields(value any) []string {
	return reflectFieldNames(value)
}

func allNamespaceFields(value any) []string {
	return reflectFieldNames(value)
}

func reflectFieldNames(value any) []string {
	rt := reflect.TypeOf(value)
	if rt.Kind() != reflect.Struct {
		return nil
	}
	names := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		names = append(names, rt.Field(i).Name)
	}
	return names
}
