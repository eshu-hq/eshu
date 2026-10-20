// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdocdb "github.com/aws/aws-sdk-go-v2/service/docdb"
)

// fakeDocDBAPI is a metadata-only test double for the DocumentDB SDK surface
// the adapter consumes. It records cluster pagination inputs so tests can
// assert the adapter sets Marker and MaxRecords correctly.
type fakeDocDBAPI struct {
	clusterPages      []*awsdocdb.DescribeDBClustersOutput
	clusterCalls      int
	clusterMarkers    []string
	clusterMaxRecords []int32

	instancePages []*awsdocdb.DescribeDBInstancesOutput
	instanceCalls int

	parameterGroupPages []*awsdocdb.DescribeDBClusterParameterGroupsOutput
	parameterGroupCalls int

	parameterPages map[string][]*awsdocdb.DescribeDBClusterParametersOutput
	parameterCalls map[string]int

	snapshotPages []*awsdocdb.DescribeDBClusterSnapshotsOutput
	snapshotCalls int

	subnetGroupPages []*awsdocdb.DescribeDBSubnetGroupsOutput
	subnetGroupCalls int

	globalClusterPages []*awsdocdb.DescribeGlobalClustersOutput
	globalClusterCalls int

	eventSubscriptionPages []*awsdocdb.DescribeEventSubscriptionsOutput
	eventSubscriptionCalls int

	tags        map[string]*awsdocdb.ListTagsForResourceOutput
	tagRequests []string
}

func (f *fakeDocDBAPI) DescribeDBClusters(
	_ context.Context,
	input *awsdocdb.DescribeDBClustersInput,
	_ ...func(*awsdocdb.Options),
) (*awsdocdb.DescribeDBClustersOutput, error) {
	f.clusterMarkers = append(f.clusterMarkers, aws.ToString(input.Marker))
	f.clusterMaxRecords = append(f.clusterMaxRecords, aws.ToInt32(input.MaxRecords))
	if f.clusterCalls >= len(f.clusterPages) {
		return &awsdocdb.DescribeDBClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeDocDBAPI) DescribeDBInstances(
	context.Context,
	*awsdocdb.DescribeDBInstancesInput,
	...func(*awsdocdb.Options),
) (*awsdocdb.DescribeDBInstancesOutput, error) {
	if f.instanceCalls >= len(f.instancePages) {
		return &awsdocdb.DescribeDBInstancesOutput{}, nil
	}
	page := f.instancePages[f.instanceCalls]
	f.instanceCalls++
	return page, nil
}

func (f *fakeDocDBAPI) DescribeDBClusterParameterGroups(
	context.Context,
	*awsdocdb.DescribeDBClusterParameterGroupsInput,
	...func(*awsdocdb.Options),
) (*awsdocdb.DescribeDBClusterParameterGroupsOutput, error) {
	if f.parameterGroupCalls >= len(f.parameterGroupPages) {
		return &awsdocdb.DescribeDBClusterParameterGroupsOutput{}, nil
	}
	page := f.parameterGroupPages[f.parameterGroupCalls]
	f.parameterGroupCalls++
	return page, nil
}

func (f *fakeDocDBAPI) DescribeDBClusterParameters(
	_ context.Context,
	input *awsdocdb.DescribeDBClusterParametersInput,
	_ ...func(*awsdocdb.Options),
) (*awsdocdb.DescribeDBClusterParametersOutput, error) {
	name := aws.ToString(input.DBClusterParameterGroupName)
	if f.parameterCalls == nil {
		f.parameterCalls = map[string]int{}
	}
	pages := f.parameterPages[name]
	idx := f.parameterCalls[name]
	if idx >= len(pages) {
		return &awsdocdb.DescribeDBClusterParametersOutput{}, nil
	}
	f.parameterCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeDocDBAPI) DescribeDBClusterSnapshots(
	context.Context,
	*awsdocdb.DescribeDBClusterSnapshotsInput,
	...func(*awsdocdb.Options),
) (*awsdocdb.DescribeDBClusterSnapshotsOutput, error) {
	if f.snapshotCalls >= len(f.snapshotPages) {
		return &awsdocdb.DescribeDBClusterSnapshotsOutput{}, nil
	}
	page := f.snapshotPages[f.snapshotCalls]
	f.snapshotCalls++
	return page, nil
}

func (f *fakeDocDBAPI) DescribeDBSubnetGroups(
	context.Context,
	*awsdocdb.DescribeDBSubnetGroupsInput,
	...func(*awsdocdb.Options),
) (*awsdocdb.DescribeDBSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awsdocdb.DescribeDBSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeDocDBAPI) DescribeGlobalClusters(
	context.Context,
	*awsdocdb.DescribeGlobalClustersInput,
	...func(*awsdocdb.Options),
) (*awsdocdb.DescribeGlobalClustersOutput, error) {
	if f.globalClusterCalls >= len(f.globalClusterPages) {
		return &awsdocdb.DescribeGlobalClustersOutput{}, nil
	}
	page := f.globalClusterPages[f.globalClusterCalls]
	f.globalClusterCalls++
	return page, nil
}

func (f *fakeDocDBAPI) DescribeEventSubscriptions(
	context.Context,
	*awsdocdb.DescribeEventSubscriptionsInput,
	...func(*awsdocdb.Options),
) (*awsdocdb.DescribeEventSubscriptionsOutput, error) {
	if f.eventSubscriptionCalls >= len(f.eventSubscriptionPages) {
		return &awsdocdb.DescribeEventSubscriptionsOutput{}, nil
	}
	page := f.eventSubscriptionPages[f.eventSubscriptionCalls]
	f.eventSubscriptionCalls++
	return page, nil
}

func (f *fakeDocDBAPI) ListTagsForResource(
	_ context.Context,
	input *awsdocdb.ListTagsForResourceInput,
	_ ...func(*awsdocdb.Options),
) (*awsdocdb.ListTagsForResourceOutput, error) {
	resourceARN := aws.ToString(input.ResourceName)
	f.tagRequests = append(f.tagRequests, resourceARN)
	if f.tags == nil {
		return &awsdocdb.ListTagsForResourceOutput{}, nil
	}
	if output := f.tags[resourceARN]; output != nil {
		return output, nil
	}
	return &awsdocdb.ListTagsForResourceOutput{}, nil
}

var _ apiClient = (*fakeDocDBAPI)(nil)
