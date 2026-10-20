// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsneptune "github.com/aws/aws-sdk-go-v2/service/neptune"
	awsneptunegraph "github.com/aws/aws-sdk-go-v2/service/neptunegraph"
)

// fakeNeptuneAPI is a metadata-only test double for the Amazon Neptune
// (provisioned) SDK surface the adapter consumes. It records cluster
// pagination inputs so tests can assert the adapter sets Marker and MaxRecords
// correctly.
type fakeNeptuneAPI struct {
	clusterPages      []*awsneptune.DescribeDBClustersOutput
	clusterCalls      int
	clusterMarkers    []string
	clusterMaxRecords []int32

	instancePages []*awsneptune.DescribeDBInstancesOutput
	instanceCalls int

	parameterGroupPages []*awsneptune.DescribeDBClusterParameterGroupsOutput
	parameterGroupCalls int

	snapshotPages []*awsneptune.DescribeDBClusterSnapshotsOutput
	snapshotCalls int

	subnetGroupPages []*awsneptune.DescribeDBSubnetGroupsOutput
	subnetGroupCalls int

	globalClusterPages []*awsneptune.DescribeGlobalClustersOutput
	globalClusterCalls int

	tags        map[string]*awsneptune.ListTagsForResourceOutput
	tagRequests []string
}

func (f *fakeNeptuneAPI) DescribeDBClusters(
	_ context.Context,
	input *awsneptune.DescribeDBClustersInput,
	_ ...func(*awsneptune.Options),
) (*awsneptune.DescribeDBClustersOutput, error) {
	f.clusterMarkers = append(f.clusterMarkers, aws.ToString(input.Marker))
	f.clusterMaxRecords = append(f.clusterMaxRecords, aws.ToInt32(input.MaxRecords))
	if f.clusterCalls >= len(f.clusterPages) {
		return &awsneptune.DescribeDBClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeNeptuneAPI) DescribeDBInstances(
	context.Context,
	*awsneptune.DescribeDBInstancesInput,
	...func(*awsneptune.Options),
) (*awsneptune.DescribeDBInstancesOutput, error) {
	if f.instanceCalls >= len(f.instancePages) {
		return &awsneptune.DescribeDBInstancesOutput{}, nil
	}
	page := f.instancePages[f.instanceCalls]
	f.instanceCalls++
	return page, nil
}

func (f *fakeNeptuneAPI) DescribeDBClusterParameterGroups(
	context.Context,
	*awsneptune.DescribeDBClusterParameterGroupsInput,
	...func(*awsneptune.Options),
) (*awsneptune.DescribeDBClusterParameterGroupsOutput, error) {
	if f.parameterGroupCalls >= len(f.parameterGroupPages) {
		return &awsneptune.DescribeDBClusterParameterGroupsOutput{}, nil
	}
	page := f.parameterGroupPages[f.parameterGroupCalls]
	f.parameterGroupCalls++
	return page, nil
}

func (f *fakeNeptuneAPI) DescribeDBClusterSnapshots(
	context.Context,
	*awsneptune.DescribeDBClusterSnapshotsInput,
	...func(*awsneptune.Options),
) (*awsneptune.DescribeDBClusterSnapshotsOutput, error) {
	if f.snapshotCalls >= len(f.snapshotPages) {
		return &awsneptune.DescribeDBClusterSnapshotsOutput{}, nil
	}
	page := f.snapshotPages[f.snapshotCalls]
	f.snapshotCalls++
	return page, nil
}

func (f *fakeNeptuneAPI) DescribeDBSubnetGroups(
	context.Context,
	*awsneptune.DescribeDBSubnetGroupsInput,
	...func(*awsneptune.Options),
) (*awsneptune.DescribeDBSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awsneptune.DescribeDBSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeNeptuneAPI) DescribeGlobalClusters(
	context.Context,
	*awsneptune.DescribeGlobalClustersInput,
	...func(*awsneptune.Options),
) (*awsneptune.DescribeGlobalClustersOutput, error) {
	if f.globalClusterCalls >= len(f.globalClusterPages) {
		return &awsneptune.DescribeGlobalClustersOutput{}, nil
	}
	page := f.globalClusterPages[f.globalClusterCalls]
	f.globalClusterCalls++
	return page, nil
}

func (f *fakeNeptuneAPI) ListTagsForResource(
	_ context.Context,
	input *awsneptune.ListTagsForResourceInput,
	_ ...func(*awsneptune.Options),
) (*awsneptune.ListTagsForResourceOutput, error) {
	resourceARN := aws.ToString(input.ResourceName)
	f.tagRequests = append(f.tagRequests, resourceARN)
	if f.tags == nil {
		return &awsneptune.ListTagsForResourceOutput{}, nil
	}
	if output := f.tags[resourceARN]; output != nil {
		return output, nil
	}
	return &awsneptune.ListTagsForResourceOutput{}, nil
}

// fakeNeptuneGraphAPI is a metadata-only test double for the Neptune Analytics
// SDK surface the adapter consumes. It records ListGraphs pagination inputs and
// GetGraph identifiers so tests can assert pagination and detail resolution.
type fakeNeptuneGraphAPI struct {
	graphPages   []*awsneptunegraph.ListGraphsOutput
	graphCalls   int
	graphTokens  []string
	graphMaxRows []int32

	graphDetails map[string]*awsneptunegraph.GetGraphOutput
	getGraphIDs  []string

	snapshotPages []*awsneptunegraph.ListGraphSnapshotsOutput
	snapshotCalls int

	tags        map[string]*awsneptunegraph.ListTagsForResourceOutput
	tagRequests []string
}

func (f *fakeNeptuneGraphAPI) ListGraphs(
	_ context.Context,
	input *awsneptunegraph.ListGraphsInput,
	_ ...func(*awsneptunegraph.Options),
) (*awsneptunegraph.ListGraphsOutput, error) {
	f.graphTokens = append(f.graphTokens, aws.ToString(input.NextToken))
	f.graphMaxRows = append(f.graphMaxRows, aws.ToInt32(input.MaxResults))
	if f.graphCalls >= len(f.graphPages) {
		return &awsneptunegraph.ListGraphsOutput{}, nil
	}
	page := f.graphPages[f.graphCalls]
	f.graphCalls++
	return page, nil
}

func (f *fakeNeptuneGraphAPI) GetGraph(
	_ context.Context,
	input *awsneptunegraph.GetGraphInput,
	_ ...func(*awsneptunegraph.Options),
) (*awsneptunegraph.GetGraphOutput, error) {
	id := aws.ToString(input.GraphIdentifier)
	f.getGraphIDs = append(f.getGraphIDs, id)
	if f.graphDetails == nil {
		return &awsneptunegraph.GetGraphOutput{}, nil
	}
	if output := f.graphDetails[id]; output != nil {
		return output, nil
	}
	return &awsneptunegraph.GetGraphOutput{}, nil
}

func (f *fakeNeptuneGraphAPI) ListGraphSnapshots(
	context.Context,
	*awsneptunegraph.ListGraphSnapshotsInput,
	...func(*awsneptunegraph.Options),
) (*awsneptunegraph.ListGraphSnapshotsOutput, error) {
	if f.snapshotCalls >= len(f.snapshotPages) {
		return &awsneptunegraph.ListGraphSnapshotsOutput{}, nil
	}
	page := f.snapshotPages[f.snapshotCalls]
	f.snapshotCalls++
	return page, nil
}

func (f *fakeNeptuneGraphAPI) ListTagsForResource(
	_ context.Context,
	input *awsneptunegraph.ListTagsForResourceInput,
	_ ...func(*awsneptunegraph.Options),
) (*awsneptunegraph.ListTagsForResourceOutput, error) {
	resourceARN := aws.ToString(input.ResourceArn)
	f.tagRequests = append(f.tagRequests, resourceARN)
	if f.tags == nil {
		return &awsneptunegraph.ListTagsForResourceOutput{}, nil
	}
	if output := f.tags[resourceARN]; output != nil {
		return output, nil
	}
	return &awsneptunegraph.ListTagsForResourceOutput{}, nil
}

var (
	_ neptuneAPI      = (*fakeNeptuneAPI)(nil)
	_ neptuneGraphAPI = (*fakeNeptuneGraphAPI)(nil)
)
