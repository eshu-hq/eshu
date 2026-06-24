// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdax "github.com/aws/aws-sdk-go-v2/service/dax"
	awsdaxtypes "github.com/aws/aws-sdk-go-v2/service/dax/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	daxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dax"
)

// TestAPIClientExcludesMutationAndParameterReads is the exclusion gate for the
// DAX adapter. It runs before behavior tests to guarantee the SDK surface the
// adapter depends on can only describe metadata. The adapter contract must never
// gain a Create/Delete/Update/Increase/Decrease/Reboot/Tag/Untag method, must
// never reach DescribeParameters (individual parameter values), and the
// scanner-owned Cluster type must never carry a KMS key field DAX does not
// report.
func TestAPIClientExcludesMutationAndParameterReads(t *testing.T) {
	apiType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < apiType.NumMethod(); i++ {
		name := apiType.Method(i).Name
		for _, forbidden := range []string{
			"Create", "Delete", "Update", "Increase", "Decrease",
			"Reboot", "TagResource", "UntagResource", "DescribeParameters",
		} {
			if strings.HasPrefix(name, forbidden) || name == forbidden {
				t.Fatalf("apiClient exposes forbidden method %q; DAX adapter must stay metadata-only and not read parameter values", name)
			}
		}
	}

	clusterType := reflect.TypeOf(daxservice.Cluster{})
	for _, forbidden := range []string{"KMSKeyID", "KMSKeyARN", "SSEKMSKeyARN", "KmsKeyId"} {
		if _, ok := clusterType.FieldByName(forbidden); ok {
			t.Fatalf("Cluster type exposes %q; DAX does not report a server-side-encryption KMS key", forbidden)
		}
	}
}

func TestClientListsDAXMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:dax:us-east-1:123456789012:cache/orders-dax"
	roleARN := "arn:aws:iam::123456789012:role/orders-dax"

	api := &fakeDAXAPI{
		clusterPages: []*awsdax.DescribeClustersOutput{{
			Clusters: []awsdaxtypes.Cluster{{
				ClusterArn:                    aws.String(clusterARN),
				ClusterName:                   aws.String("orders-dax"),
				Description:                   aws.String("orders dax accelerator"),
				Status:                        aws.String("available"),
				NodeType:                      aws.String("dax.r5.large"),
				ActiveNodes:                   aws.Int32(3),
				TotalNodes:                    aws.Int32(3),
				NetworkType:                   awsdaxtypes.NetworkTypeIpv4,
				ClusterEndpointEncryptionType: awsdaxtypes.ClusterEndpointEncryptionTypeTls,
				IamRoleArn:                    aws.String(roleARN),
				PreferredMaintenanceWindow:    aws.String("sun:05:00-sun:06:00"),
				SubnetGroup:                   aws.String("orders-dax-subnets"),
				ParameterGroup: &awsdaxtypes.ParameterGroupStatus{
					ParameterGroupName: aws.String("default.dax1.0"),
				},
				SecurityGroups: []awsdaxtypes.SecurityGroupMembership{{
					SecurityGroupIdentifier: aws.String("sg-aaa"),
					Status:                  aws.String("active"),
				}},
				SSEDescription: &awsdaxtypes.SSEDescription{
					Status: awsdaxtypes.SSEStatusEnabled,
				},
				ClusterDiscoveryEndpoint: &awsdaxtypes.Endpoint{
					Address: aws.String("orders-dax.abc123.dax-clusters.us-east-1.amazonaws.com"),
					Port:    8111,
				},
			}},
		}},
		subnetGroupPages: []*awsdax.DescribeSubnetGroupsOutput{{
			SubnetGroups: []awsdaxtypes.SubnetGroup{{
				SubnetGroupName: aws.String("orders-dax-subnets"),
				Description:     aws.String("orders dax subnets"),
				VpcId:           aws.String("vpc-123"),
				Subnets: []awsdaxtypes.Subnet{{
					SubnetIdentifier: aws.String("subnet-a"),
				}, {
					SubnetIdentifier: aws.String("subnet-b"),
				}},
			}},
		}},
		parameterGroupPages: []*awsdax.DescribeParameterGroupsOutput{{
			ParameterGroups: []awsdaxtypes.ParameterGroup{{
				ParameterGroupName: aws.String("default.dax1.0"),
				Description:        aws.String("default dax parameter group"),
			}},
		}},
		tags: map[string][]awsdaxtypes.Tag{
			clusterARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	cluster := clusters[0]
	if cluster.Name != "orders-dax" {
		t.Fatalf("cluster.Name = %q, want orders-dax", cluster.Name)
	}
	if cluster.ActiveNodes != 3 || cluster.TotalNodes != 3 {
		t.Fatalf("cluster nodes active/total = %d/%d, want 3/3", cluster.ActiveNodes, cluster.TotalNodes)
	}
	if cluster.EndpointEncryptionType != "TLS" {
		t.Fatalf("cluster.EndpointEncryptionType = %q, want TLS", cluster.EndpointEncryptionType)
	}
	if cluster.SSEStatus != "ENABLED" {
		t.Fatalf("cluster.SSEStatus = %q, want ENABLED", cluster.SSEStatus)
	}
	if cluster.IAMRoleARN != roleARN {
		t.Fatalf("cluster.IAMRoleARN = %q, want %q", cluster.IAMRoleARN, roleARN)
	}
	if cluster.ParameterGroupName != "default.dax1.0" {
		t.Fatalf("cluster.ParameterGroupName = %q, want default.dax1.0", cluster.ParameterGroupName)
	}
	if cluster.SubnetGroupName != "orders-dax-subnets" {
		t.Fatalf("cluster.SubnetGroupName = %q, want orders-dax-subnets", cluster.SubnetGroupName)
	}
	if got, want := cluster.SecurityGroupIDs, []string{"sg-aaa"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cluster.SecurityGroupIDs = %#v, want %#v", got, want)
	}
	if cluster.DiscoveryEndpointPort != 8111 {
		t.Fatalf("cluster.DiscoveryEndpointPort = %d, want 8111", cluster.DiscoveryEndpointPort)
	}
	if cluster.Tags["Environment"] != "prod" {
		t.Fatalf("cluster.Tags = %#v, want Environment=prod", cluster.Tags)
	}

	subnetGroups, err := adapter.ListSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListSubnetGroups() error = %v", err)
	}
	if subnetGroups[0].Name != "orders-dax-subnets" {
		t.Fatalf("subnetGroups[0].Name = %q, want orders-dax-subnets", subnetGroups[0].Name)
	}
	if subnetGroups[0].VPCID != "vpc-123" {
		t.Fatalf("subnetGroups[0].VPCID = %q, want vpc-123", subnetGroups[0].VPCID)
	}
	if got, want := subnetGroups[0].SubnetIDs, []string{"subnet-a", "subnet-b"}; !stringSlicesEqual(got, want) {
		t.Fatalf("subnetGroups[0].SubnetIDs = %#v, want %#v", got, want)
	}

	parameterGroups, err := adapter.ListParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListParameterGroups() error = %v", err)
	}
	if parameterGroups[0].Name != "default.dax1.0" {
		t.Fatalf("parameterGroups[0].Name = %q, want default.dax1.0", parameterGroups[0].Name)
	}
	if parameterGroups[0].Description != "default dax parameter group" {
		t.Fatalf("parameterGroups[0].Description = %q, want the default description", parameterGroups[0].Description)
	}

	// DescribeParameters must never be called: the adapter interface excludes it,
	// so parameter values cannot be read.
	if api.describeParametersCalls != 0 {
		t.Fatalf("DescribeParameters called %d times; parameter values must never be read", api.describeParametersCalls)
	}
}

func TestClientPaginatesClusters(t *testing.T) {
	api := &fakeDAXAPI{
		clusterPages: []*awsdax.DescribeClustersOutput{{
			Clusters:  []awsdaxtypes.Cluster{{ClusterName: aws.String("first")}},
			NextToken: aws.String("next"),
		}, {
			Clusters: []awsdaxtypes.Cluster{{ClusterName: aws.String("second")}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if got, want := len(clusters), 2; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if got, want := api.clusterTokens, []string{"", "next"}; !stringSlicesEqual(got, want) {
		t.Fatalf("DescribeClusters tokens = %#v, want %#v", got, want)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDAX,
	}
}

type fakeDAXAPI struct {
	clusterPages  []*awsdax.DescribeClustersOutput
	clusterCalls  int
	clusterTokens []string

	subnetGroupPages []*awsdax.DescribeSubnetGroupsOutput
	subnetGroupCalls int

	parameterGroupPages []*awsdax.DescribeParameterGroupsOutput
	parameterGroupCalls int

	describeParametersCalls int

	tags map[string][]awsdaxtypes.Tag
}

func (f *fakeDAXAPI) DescribeClusters(
	_ context.Context,
	input *awsdax.DescribeClustersInput,
	_ ...func(*awsdax.Options),
) (*awsdax.DescribeClustersOutput, error) {
	f.clusterTokens = append(f.clusterTokens, aws.ToString(input.NextToken))
	if f.clusterCalls >= len(f.clusterPages) {
		return &awsdax.DescribeClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeDAXAPI) DescribeSubnetGroups(
	_ context.Context,
	_ *awsdax.DescribeSubnetGroupsInput,
	_ ...func(*awsdax.Options),
) (*awsdax.DescribeSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awsdax.DescribeSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeDAXAPI) DescribeParameterGroups(
	_ context.Context,
	_ *awsdax.DescribeParameterGroupsInput,
	_ ...func(*awsdax.Options),
) (*awsdax.DescribeParameterGroupsOutput, error) {
	if f.parameterGroupCalls >= len(f.parameterGroupPages) {
		return &awsdax.DescribeParameterGroupsOutput{}, nil
	}
	page := f.parameterGroupPages[f.parameterGroupCalls]
	f.parameterGroupCalls++
	return page, nil
}

func (f *fakeDAXAPI) ListTags(
	_ context.Context,
	input *awsdax.ListTagsInput,
	_ ...func(*awsdax.Options),
) (*awsdax.ListTagsOutput, error) {
	if f.tags == nil {
		return &awsdax.ListTagsOutput{}, nil
	}
	tags := f.tags[aws.ToString(input.ResourceName)]
	return &awsdax.ListTagsOutput{Tags: tags}, nil
}

var _ apiClient = (*fakeDAXAPI)(nil)

var _ daxservice.Client = (*Client)(nil)

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
