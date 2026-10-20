// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	awsresiliencehub "github.com/aws/aws-sdk-go-v2/service/resiliencehub"
	awsresiliencehubtypes "github.com/aws/aws-sdk-go-v2/service/resiliencehub/types"
)

// stubAPI is an in-memory apiClient used to drive the adapter's mapping and
// pagination behavior without an AWS endpoint. A single page is returned for
// each list call; that is enough to verify mapping, the ARN-only physical
// resource filter, and the missing-version warning path.
type stubAPI struct {
	apps         []awsresiliencehubtypes.AppSummary
	policies     []awsresiliencehubtypes.ResiliencyPolicy
	appPolicyARN string
	appTags      map[string]string
	inputSources []awsresiliencehubtypes.AppInputSource
	components   []awsresiliencehubtypes.AppComponent
	resources    []awsresiliencehubtypes.PhysicalResource
	assessments  []awsresiliencehubtypes.AppAssessmentSummary
	// versionNotFound makes the version-scoped reads return
	// ResourceNotFoundException, exercising the missing-version warning path.
	versionNotFound bool
}

func (s *stubAPI) ListApps(
	context.Context,
	*awsresiliencehub.ListAppsInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListAppsOutput, error) {
	return &awsresiliencehub.ListAppsOutput{AppSummaries: s.apps}, nil
}

func (s *stubAPI) DescribeApp(
	context.Context,
	*awsresiliencehub.DescribeAppInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.DescribeAppOutput, error) {
	return &awsresiliencehub.DescribeAppOutput{App: &awsresiliencehubtypes.App{
		PolicyArn: stringOrNil(s.appPolicyARN),
		Tags:      s.appTags,
	}}, nil
}

func (s *stubAPI) ListResiliencyPolicies(
	context.Context,
	*awsresiliencehub.ListResiliencyPoliciesInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListResiliencyPoliciesOutput, error) {
	return &awsresiliencehub.ListResiliencyPoliciesOutput{ResiliencyPolicies: s.policies}, nil
}

func (s *stubAPI) ListAppInputSources(
	context.Context,
	*awsresiliencehub.ListAppInputSourcesInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListAppInputSourcesOutput, error) {
	if s.versionNotFound {
		return nil, &awsresiliencehubtypes.ResourceNotFoundException{}
	}
	return &awsresiliencehub.ListAppInputSourcesOutput{AppInputSources: s.inputSources}, nil
}

func (s *stubAPI) ListAppVersionAppComponents(
	context.Context,
	*awsresiliencehub.ListAppVersionAppComponentsInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListAppVersionAppComponentsOutput, error) {
	if s.versionNotFound {
		return nil, &awsresiliencehubtypes.ResourceNotFoundException{}
	}
	return &awsresiliencehub.ListAppVersionAppComponentsOutput{AppComponents: s.components}, nil
}

func (s *stubAPI) ListAppVersionResources(
	context.Context,
	*awsresiliencehub.ListAppVersionResourcesInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListAppVersionResourcesOutput, error) {
	if s.versionNotFound {
		return nil, &awsresiliencehubtypes.ResourceNotFoundException{}
	}
	return &awsresiliencehub.ListAppVersionResourcesOutput{PhysicalResources: s.resources}, nil
}

func (s *stubAPI) ListAppAssessments(
	context.Context,
	*awsresiliencehub.ListAppAssessmentsInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListAppAssessmentsOutput, error) {
	return &awsresiliencehub.ListAppAssessmentsOutput{AssessmentSummaries: s.assessments}, nil
}

func (s *stubAPI) ListTagsForResource(
	context.Context,
	*awsresiliencehub.ListTagsForResourceInput,
	...func(*awsresiliencehub.Options),
) (*awsresiliencehub.ListTagsForResourceOutput, error) {
	return &awsresiliencehub.ListTagsForResourceOutput{}, nil
}

func stringOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
