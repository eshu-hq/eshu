// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsemr "github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	awsemrserverless "github.com/aws/aws-sdk-go-v2/service/emrserverless"
	emrserverlesstypes "github.com/aws/aws-sdk-go-v2/service/emrserverless/types"
)

// fakeEMRAPI is a metadata-only EMR test double. It implements exactly the
// emrAPIClient read surface so a test cannot accidentally exercise a mutation
// API the adapter does not depend on.
type fakeEMRAPI struct {
	clusters          []emrtypes.ClusterSummary
	clusterDetail     map[string]emrtypes.Cluster
	instanceGroups    map[string][]emrtypes.InstanceGroup
	instanceFleets    map[string][]emrtypes.InstanceFleet
	securityConfigs   []emrtypes.SecurityConfigurationSummary
	studios           []emrtypes.StudioSummary
	studioDetail      map[string]emrtypes.Studio
	sessionMappings   map[string][]emrtypes.SessionMappingSummary
	listClustersInput *awsemr.ListClustersInput
}

func (f *fakeEMRAPI) ListClusters(
	_ context.Context,
	input *awsemr.ListClustersInput,
	_ ...func(*awsemr.Options),
) (*awsemr.ListClustersOutput, error) {
	f.listClustersInput = input
	return &awsemr.ListClustersOutput{Clusters: f.clusters}, nil
}

func (f *fakeEMRAPI) DescribeCluster(
	_ context.Context,
	input *awsemr.DescribeClusterInput,
	_ ...func(*awsemr.Options),
) (*awsemr.DescribeClusterOutput, error) {
	cluster, ok := f.clusterDetail[aws.ToString(input.ClusterId)]
	if !ok {
		return &awsemr.DescribeClusterOutput{}, nil
	}
	return &awsemr.DescribeClusterOutput{Cluster: &cluster}, nil
}

func (f *fakeEMRAPI) ListInstanceGroups(
	_ context.Context,
	input *awsemr.ListInstanceGroupsInput,
	_ ...func(*awsemr.Options),
) (*awsemr.ListInstanceGroupsOutput, error) {
	return &awsemr.ListInstanceGroupsOutput{InstanceGroups: f.instanceGroups[aws.ToString(input.ClusterId)]}, nil
}

func (f *fakeEMRAPI) ListInstanceFleets(
	_ context.Context,
	input *awsemr.ListInstanceFleetsInput,
	_ ...func(*awsemr.Options),
) (*awsemr.ListInstanceFleetsOutput, error) {
	return &awsemr.ListInstanceFleetsOutput{InstanceFleets: f.instanceFleets[aws.ToString(input.ClusterId)]}, nil
}

func (f *fakeEMRAPI) ListSecurityConfigurations(
	_ context.Context,
	_ *awsemr.ListSecurityConfigurationsInput,
	_ ...func(*awsemr.Options),
) (*awsemr.ListSecurityConfigurationsOutput, error) {
	return &awsemr.ListSecurityConfigurationsOutput{SecurityConfigurations: f.securityConfigs}, nil
}

func (f *fakeEMRAPI) ListStudios(
	_ context.Context,
	_ *awsemr.ListStudiosInput,
	_ ...func(*awsemr.Options),
) (*awsemr.ListStudiosOutput, error) {
	return &awsemr.ListStudiosOutput{Studios: f.studios}, nil
}

func (f *fakeEMRAPI) DescribeStudio(
	_ context.Context,
	input *awsemr.DescribeStudioInput,
	_ ...func(*awsemr.Options),
) (*awsemr.DescribeStudioOutput, error) {
	studio, ok := f.studioDetail[aws.ToString(input.StudioId)]
	if !ok {
		return &awsemr.DescribeStudioOutput{}, nil
	}
	return &awsemr.DescribeStudioOutput{Studio: &studio}, nil
}

func (f *fakeEMRAPI) ListStudioSessionMappings(
	_ context.Context,
	input *awsemr.ListStudioSessionMappingsInput,
	_ ...func(*awsemr.Options),
) (*awsemr.ListStudioSessionMappingsOutput, error) {
	return &awsemr.ListStudioSessionMappingsOutput{SessionMappings: f.sessionMappings[aws.ToString(input.StudioId)]}, nil
}

// fakeServerlessAPI is a metadata-only EMR Serverless test double implementing
// exactly the emrServerlessAPIClient read surface.
type fakeServerlessAPI struct {
	applications      []emrserverlesstypes.ApplicationSummary
	applicationDetail map[string]emrserverlesstypes.Application
}

func (f *fakeServerlessAPI) ListApplications(
	_ context.Context,
	_ *awsemrserverless.ListApplicationsInput,
	_ ...func(*awsemrserverless.Options),
) (*awsemrserverless.ListApplicationsOutput, error) {
	return &awsemrserverless.ListApplicationsOutput{Applications: f.applications}, nil
}

func (f *fakeServerlessAPI) GetApplication(
	_ context.Context,
	input *awsemrserverless.GetApplicationInput,
	_ ...func(*awsemrserverless.Options),
) (*awsemrserverless.GetApplicationOutput, error) {
	application, ok := f.applicationDetail[aws.ToString(input.ApplicationId)]
	if !ok {
		return &awsemrserverless.GetApplicationOutput{}, nil
	}
	return &awsemrserverless.GetApplicationOutput{Application: &application}, nil
}

var (
	_ emrAPIClient           = (*fakeEMRAPI)(nil)
	_ emrServerlessAPIClient = (*fakeServerlessAPI)(nil)
)
