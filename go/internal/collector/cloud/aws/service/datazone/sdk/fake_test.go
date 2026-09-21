// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatazone "github.com/aws/aws-sdk-go-v2/service/datazone"
)

// fakeDatazoneAPI is a single-page fake DataZone control-plane client for the
// adapter tests. List operations return the first registered page; pagination
// terminates on the empty NextToken the constructed pages carry.
type fakeDatazoneAPI struct {
	domainPages      []*awsdatazone.ListDomainsOutput
	domainCall       int
	getDomain        map[string]*awsdatazone.GetDomainOutput
	projectPages     map[string][]*awsdatazone.ListProjectsOutput
	projectCalls     map[string]int
	environmentPages map[string][]*awsdatazone.ListEnvironmentsOutput
	environmentCalls map[string]int
	dataSourcePages  map[string][]*awsdatazone.ListDataSourcesOutput
	dataSourceCalls  map[string]int
	getDataSource    map[string]*awsdatazone.GetDataSourceOutput
	tags             map[string]map[string]string
}

func (f *fakeDatazoneAPI) ListDomains(
	_ context.Context,
	_ *awsdatazone.ListDomainsInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.ListDomainsOutput, error) {
	if f.domainCall >= len(f.domainPages) {
		return &awsdatazone.ListDomainsOutput{}, nil
	}
	page := f.domainPages[f.domainCall]
	f.domainCall++
	return page, nil
}

func (f *fakeDatazoneAPI) GetDomain(
	_ context.Context,
	input *awsdatazone.GetDomainInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.GetDomainOutput, error) {
	if output, ok := f.getDomain[aws.ToString(input.Identifier)]; ok {
		return output, nil
	}
	return &awsdatazone.GetDomainOutput{Id: input.Identifier}, nil
}

func (f *fakeDatazoneAPI) ListProjects(
	_ context.Context,
	input *awsdatazone.ListProjectsInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.ListProjectsOutput, error) {
	if f.projectCalls == nil {
		f.projectCalls = map[string]int{}
	}
	key := aws.ToString(input.DomainIdentifier)
	pages := f.projectPages[key]
	idx := f.projectCalls[key]
	if idx >= len(pages) {
		return &awsdatazone.ListProjectsOutput{}, nil
	}
	f.projectCalls[key] = idx + 1
	return pages[idx], nil
}

func (f *fakeDatazoneAPI) ListEnvironments(
	_ context.Context,
	input *awsdatazone.ListEnvironmentsInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.ListEnvironmentsOutput, error) {
	if f.environmentCalls == nil {
		f.environmentCalls = map[string]int{}
	}
	key := aws.ToString(input.DomainIdentifier) + "/" + aws.ToString(input.ProjectIdentifier)
	pages := f.environmentPages[key]
	idx := f.environmentCalls[key]
	if idx >= len(pages) {
		return &awsdatazone.ListEnvironmentsOutput{}, nil
	}
	f.environmentCalls[key] = idx + 1
	return pages[idx], nil
}

func (f *fakeDatazoneAPI) ListDataSources(
	_ context.Context,
	input *awsdatazone.ListDataSourcesInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.ListDataSourcesOutput, error) {
	if f.dataSourceCalls == nil {
		f.dataSourceCalls = map[string]int{}
	}
	key := aws.ToString(input.DomainIdentifier) + "/" + aws.ToString(input.ProjectIdentifier)
	pages := f.dataSourcePages[key]
	idx := f.dataSourceCalls[key]
	if idx >= len(pages) {
		return &awsdatazone.ListDataSourcesOutput{}, nil
	}
	f.dataSourceCalls[key] = idx + 1
	return pages[idx], nil
}

func (f *fakeDatazoneAPI) GetDataSource(
	_ context.Context,
	input *awsdatazone.GetDataSourceInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.GetDataSourceOutput, error) {
	if output, ok := f.getDataSource[aws.ToString(input.Identifier)]; ok {
		return output, nil
	}
	return &awsdatazone.GetDataSourceOutput{Id: input.Identifier}, nil
}

func (f *fakeDatazoneAPI) ListTagsForResource(
	_ context.Context,
	input *awsdatazone.ListTagsForResourceInput,
	_ ...func(*awsdatazone.Options),
) (*awsdatazone.ListTagsForResourceOutput, error) {
	return &awsdatazone.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}
