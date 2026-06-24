// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"time"

	awscodedeploy "github.com/aws/aws-sdk-go-v2/service/codedeploy"
	cdtypes "github.com/aws/aws-sdk-go-v2/service/codedeploy/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codedeploy"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// awsBatchGetDeploymentGroupsLimit mirrors the AWS contract: a single
// BatchGetDeploymentGroups call accepts at most 100 deployment group names. The
// fake rejects oversized input the same way the live API does so the adapter's
// chunking is exercised under test.
const awsBatchGetDeploymentGroupsLimit = 100

var testTime = time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

func newTestClient(api apiClient, key redact.Key) *Client {
	return &Client{
		client:       api,
		boundary:     testBoundary(),
		redactionKey: key,
	}
}

// fakeCodeDeployAPI is a metadata-only CodeDeploy SDK stub. It only implements
// the read operations the adapter consumes; the apiClient interface guard test
// ensures no mutation or data-plane method is reachable.
type fakeCodeDeployAPI struct {
	applications          []string
	applicationInfo       map[string]cdtypes.ApplicationInfo
	deploymentGroupsByApp map[string][]string
	deploymentGroupInfo   map[string]cdtypes.DeploymentGroupInfo
	deploymentConfigs     []string
	deploymentConfigInfo  map[string]cdtypes.DeploymentConfigInfo
	deploymentIDs         []string
	deploymentInfo        map[string]cdtypes.DeploymentInfo
	tags                  map[string]map[string]string

	listDeploymentsCalls int
}

func (f *fakeCodeDeployAPI) ListApplications(
	context.Context,
	*awscodedeploy.ListApplicationsInput,
	...func(*awscodedeploy.Options),
) (*awscodedeploy.ListApplicationsOutput, error) {
	return &awscodedeploy.ListApplicationsOutput{Applications: f.applications}, nil
}

func (f *fakeCodeDeployAPI) BatchGetApplications(
	_ context.Context,
	input *awscodedeploy.BatchGetApplicationsInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.BatchGetApplicationsOutput, error) {
	var infos []cdtypes.ApplicationInfo
	for _, name := range input.ApplicationNames {
		if info, ok := f.applicationInfo[name]; ok {
			infos = append(infos, info)
		}
	}
	return &awscodedeploy.BatchGetApplicationsOutput{ApplicationsInfo: infos}, nil
}

func (f *fakeCodeDeployAPI) ListDeploymentGroups(
	_ context.Context,
	input *awscodedeploy.ListDeploymentGroupsInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.ListDeploymentGroupsOutput, error) {
	app := ""
	if input.ApplicationName != nil {
		app = *input.ApplicationName
	}
	return &awscodedeploy.ListDeploymentGroupsOutput{
		ApplicationName:  input.ApplicationName,
		DeploymentGroups: f.deploymentGroupsByApp[app],
	}, nil
}

func (f *fakeCodeDeployAPI) BatchGetDeploymentGroups(
	_ context.Context,
	input *awscodedeploy.BatchGetDeploymentGroupsInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.BatchGetDeploymentGroupsOutput, error) {
	if len(input.DeploymentGroupNames) > awsBatchGetDeploymentGroupsLimit {
		return nil, fmt.Errorf(
			"BatchGetDeploymentGroups accepts at most %d names, got %d",
			awsBatchGetDeploymentGroupsLimit, len(input.DeploymentGroupNames),
		)
	}
	var infos []cdtypes.DeploymentGroupInfo
	for _, name := range input.DeploymentGroupNames {
		if info, ok := f.deploymentGroupInfo[name]; ok {
			infos = append(infos, info)
		}
	}
	return &awscodedeploy.BatchGetDeploymentGroupsOutput{DeploymentGroupsInfo: infos}, nil
}

func (f *fakeCodeDeployAPI) ListDeploymentConfigs(
	context.Context,
	*awscodedeploy.ListDeploymentConfigsInput,
	...func(*awscodedeploy.Options),
) (*awscodedeploy.ListDeploymentConfigsOutput, error) {
	return &awscodedeploy.ListDeploymentConfigsOutput{DeploymentConfigsList: f.deploymentConfigs}, nil
}

func (f *fakeCodeDeployAPI) GetDeploymentConfig(
	_ context.Context,
	input *awscodedeploy.GetDeploymentConfigInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.GetDeploymentConfigOutput, error) {
	name := ""
	if input.DeploymentConfigName != nil {
		name = *input.DeploymentConfigName
	}
	if info, ok := f.deploymentConfigInfo[name]; ok {
		return &awscodedeploy.GetDeploymentConfigOutput{DeploymentConfigInfo: &info}, nil
	}
	return &awscodedeploy.GetDeploymentConfigOutput{}, nil
}

func (f *fakeCodeDeployAPI) ListDeployments(
	_ context.Context,
	_ *awscodedeploy.ListDeploymentsInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.ListDeploymentsOutput, error) {
	f.listDeploymentsCalls++
	return &awscodedeploy.ListDeploymentsOutput{Deployments: f.deploymentIDs}, nil
}

func (f *fakeCodeDeployAPI) BatchGetDeployments(
	_ context.Context,
	input *awscodedeploy.BatchGetDeploymentsInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.BatchGetDeploymentsOutput, error) {
	var infos []cdtypes.DeploymentInfo
	for _, id := range input.DeploymentIds {
		if info, ok := f.deploymentInfo[id]; ok {
			infos = append(infos, info)
		}
	}
	return &awscodedeploy.BatchGetDeploymentsOutput{DeploymentsInfo: infos}, nil
}

func (f *fakeCodeDeployAPI) ListTagsForResource(
	_ context.Context,
	input *awscodedeploy.ListTagsForResourceInput,
	_ ...func(*awscodedeploy.Options),
) (*awscodedeploy.ListTagsForResourceOutput, error) {
	arn := ""
	if input.ResourceArn != nil {
		arn = *input.ResourceArn
	}
	tags := f.tags[arn]
	out := make([]cdtypes.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, cdtypes.Tag{Key: stringPtr(key), Value: stringPtr(value)})
	}
	return &awscodedeploy.ListTagsForResourceOutput{Tags: out}, nil
}

func stringPtr(value string) *string { return &value }

var (
	_ apiClient         = (*fakeCodeDeployAPI)(nil)
	_ codedeploy.Client = (*Client)(nil)
)
