// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfn "github.com/aws/aws-sdk-go-v2/service/cloudformation"
)

// fakeCFNAPI implements apiClient with map- and slice-driven fixtures. It
// records every operation name so tests can assert that no template-body,
// change-set-body, or mutation API was reached.
type fakeCFNAPI struct {
	calls []string

	describeStacksPages []*awscfn.DescribeStacksOutput
	describeStacksIndex int

	listStacksPages []*awscfn.ListStacksOutput
	listStacksIndex int

	listStackResourcesPages []*awscfn.ListStackResourcesOutput
	listStackResourcesIndex int

	listStackSetsPages []*awscfn.ListStackSetsOutput
	listStackSetsIndex int

	describeStackSet map[string]*awscfn.DescribeStackSetOutput

	listChangeSetsPages []*awscfn.ListChangeSetsOutput
	listChangeSetsIndex int

	driftsPages []*awscfn.DescribeStackResourceDriftsOutput
	driftsIndex int

	listStackInstancesPages []*awscfn.ListStackInstancesOutput
	listStackInstancesIndex int

	listTypesPages []*awscfn.ListTypesOutput
	listTypesIndex int
}

func (f *fakeCFNAPI) record(op string) { f.calls = append(f.calls, op) }

func (f *fakeCFNAPI) DescribeStacks(
	context.Context, *awscfn.DescribeStacksInput, ...func(*awscfn.Options),
) (*awscfn.DescribeStacksOutput, error) {
	f.record("DescribeStacks")
	if f.describeStacksIndex >= len(f.describeStacksPages) {
		return &awscfn.DescribeStacksOutput{}, nil
	}
	page := f.describeStacksPages[f.describeStacksIndex]
	f.describeStacksIndex++
	return page, nil
}

func (f *fakeCFNAPI) ListStacks(
	context.Context, *awscfn.ListStacksInput, ...func(*awscfn.Options),
) (*awscfn.ListStacksOutput, error) {
	f.record("ListStacks")
	if f.listStacksIndex >= len(f.listStacksPages) {
		return &awscfn.ListStacksOutput{}, nil
	}
	page := f.listStacksPages[f.listStacksIndex]
	f.listStacksIndex++
	return page, nil
}

func (f *fakeCFNAPI) ListStackResources(
	context.Context, *awscfn.ListStackResourcesInput, ...func(*awscfn.Options),
) (*awscfn.ListStackResourcesOutput, error) {
	f.record("ListStackResources")
	if f.listStackResourcesIndex >= len(f.listStackResourcesPages) {
		return &awscfn.ListStackResourcesOutput{}, nil
	}
	page := f.listStackResourcesPages[f.listStackResourcesIndex]
	f.listStackResourcesIndex++
	return page, nil
}

func (f *fakeCFNAPI) ListStackSets(
	context.Context, *awscfn.ListStackSetsInput, ...func(*awscfn.Options),
) (*awscfn.ListStackSetsOutput, error) {
	f.record("ListStackSets")
	if f.listStackSetsIndex >= len(f.listStackSetsPages) {
		return &awscfn.ListStackSetsOutput{}, nil
	}
	page := f.listStackSetsPages[f.listStackSetsIndex]
	f.listStackSetsIndex++
	return page, nil
}

func (f *fakeCFNAPI) DescribeStackSet(
	_ context.Context, input *awscfn.DescribeStackSetInput, _ ...func(*awscfn.Options),
) (*awscfn.DescribeStackSetOutput, error) {
	f.record("DescribeStackSet")
	if f.describeStackSet == nil {
		return &awscfn.DescribeStackSetOutput{}, nil
	}
	if output, ok := f.describeStackSet[aws.ToString(input.StackSetName)]; ok {
		return output, nil
	}
	return &awscfn.DescribeStackSetOutput{}, nil
}

func (f *fakeCFNAPI) ListChangeSets(
	context.Context, *awscfn.ListChangeSetsInput, ...func(*awscfn.Options),
) (*awscfn.ListChangeSetsOutput, error) {
	f.record("ListChangeSets")
	if f.listChangeSetsIndex >= len(f.listChangeSetsPages) {
		return &awscfn.ListChangeSetsOutput{}, nil
	}
	page := f.listChangeSetsPages[f.listChangeSetsIndex]
	f.listChangeSetsIndex++
	return page, nil
}

func (f *fakeCFNAPI) DescribeStackResourceDrifts(
	context.Context, *awscfn.DescribeStackResourceDriftsInput, ...func(*awscfn.Options),
) (*awscfn.DescribeStackResourceDriftsOutput, error) {
	f.record("DescribeStackResourceDrifts")
	if f.driftsIndex >= len(f.driftsPages) {
		return &awscfn.DescribeStackResourceDriftsOutput{}, nil
	}
	page := f.driftsPages[f.driftsIndex]
	f.driftsIndex++
	return page, nil
}

func (f *fakeCFNAPI) ListStackInstances(
	context.Context, *awscfn.ListStackInstancesInput, ...func(*awscfn.Options),
) (*awscfn.ListStackInstancesOutput, error) {
	f.record("ListStackInstances")
	if f.listStackInstancesIndex >= len(f.listStackInstancesPages) {
		return &awscfn.ListStackInstancesOutput{}, nil
	}
	page := f.listStackInstancesPages[f.listStackInstancesIndex]
	f.listStackInstancesIndex++
	return page, nil
}

func (f *fakeCFNAPI) ListTypes(
	context.Context, *awscfn.ListTypesInput, ...func(*awscfn.Options),
) (*awscfn.ListTypesOutput, error) {
	f.record("ListTypes")
	if f.listTypesIndex >= len(f.listTypesPages) {
		return &awscfn.ListTypesOutput{}, nil
	}
	page := f.listTypesPages[f.listTypesIndex]
	f.listTypesIndex++
	return page, nil
}

var _ apiClient = (*fakeCFNAPI)(nil)
