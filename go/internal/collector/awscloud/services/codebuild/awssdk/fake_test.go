// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"time"

	awscodebuild "github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codebuild"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// awsBatchGetProjectsLimit mirrors the AWS contract: a single BatchGetProjects
// call accepts at most 100 project names. The fake rejects oversized input the
// same way the live API does so the adapter's chunking is exercised.
const awsBatchGetProjectsLimit = 100

var testTime = time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceCodeBuild,
	}
}

func newTestClient(api apiClient, key redact.Key) *Client {
	return &Client{
		client:       api,
		boundary:     testBoundary(),
		redactionKey: key,
	}
}

// fakeCodeBuildAPI is a metadata-only CodeBuild SDK stub. It only implements the
// read operations the adapter consumes; the apiClient interface guard test
// ensures no mutation, source-credential, or log-content method is reachable.
type fakeCodeBuildAPI struct {
	projectNames     []string
	projectInfo      map[string]cbtypes.Project
	projectsNotFound []string

	reportGroupARNs      []string
	reportGroupInfo      map[string]cbtypes.ReportGroup
	reportGroupsNotFound []string

	buildIDs       []string
	buildInfo      map[string]cbtypes.Build
	buildsNotFound []string
}

func (f *fakeCodeBuildAPI) ListProjects(
	context.Context,
	*awscodebuild.ListProjectsInput,
	...func(*awscodebuild.Options),
) (*awscodebuild.ListProjectsOutput, error) {
	return &awscodebuild.ListProjectsOutput{Projects: f.projectNames}, nil
}

func (f *fakeCodeBuildAPI) BatchGetProjects(
	_ context.Context,
	input *awscodebuild.BatchGetProjectsInput,
	_ ...func(*awscodebuild.Options),
) (*awscodebuild.BatchGetProjectsOutput, error) {
	if len(input.Names) > awsBatchGetProjectsLimit {
		return nil, fmt.Errorf("BatchGetProjects accepts at most %d names, got %d",
			awsBatchGetProjectsLimit, len(input.Names))
	}
	var projects []cbtypes.Project
	for _, name := range input.Names {
		if info, ok := f.projectInfo[name]; ok {
			projects = append(projects, info)
		}
	}
	return &awscodebuild.BatchGetProjectsOutput{
		Projects:         projects,
		ProjectsNotFound: f.projectsNotFound,
	}, nil
}

func (f *fakeCodeBuildAPI) ListReportGroups(
	context.Context,
	*awscodebuild.ListReportGroupsInput,
	...func(*awscodebuild.Options),
) (*awscodebuild.ListReportGroupsOutput, error) {
	return &awscodebuild.ListReportGroupsOutput{ReportGroups: f.reportGroupARNs}, nil
}

func (f *fakeCodeBuildAPI) BatchGetReportGroups(
	_ context.Context,
	input *awscodebuild.BatchGetReportGroupsInput,
	_ ...func(*awscodebuild.Options),
) (*awscodebuild.BatchGetReportGroupsOutput, error) {
	var groups []cbtypes.ReportGroup
	for _, arn := range input.ReportGroupArns {
		if info, ok := f.reportGroupInfo[arn]; ok {
			groups = append(groups, info)
		}
	}
	return &awscodebuild.BatchGetReportGroupsOutput{
		ReportGroups:         groups,
		ReportGroupsNotFound: f.reportGroupsNotFound,
	}, nil
}

func (f *fakeCodeBuildAPI) ListBuilds(
	context.Context,
	*awscodebuild.ListBuildsInput,
	...func(*awscodebuild.Options),
) (*awscodebuild.ListBuildsOutput, error) {
	return &awscodebuild.ListBuildsOutput{Ids: f.buildIDs}, nil
}

func (f *fakeCodeBuildAPI) BatchGetBuilds(
	_ context.Context,
	input *awscodebuild.BatchGetBuildsInput,
	_ ...func(*awscodebuild.Options),
) (*awscodebuild.BatchGetBuildsOutput, error) {
	var builds []cbtypes.Build
	for _, id := range input.Ids {
		if info, ok := f.buildInfo[id]; ok {
			builds = append(builds, info)
		}
	}
	return &awscodebuild.BatchGetBuildsOutput{
		Builds:         builds,
		BuildsNotFound: f.buildsNotFound,
	}, nil
}

var (
	_ apiClient        = (*fakeCodeBuildAPI)(nil)
	_ codebuild.Client = (*Client)(nil)
)
