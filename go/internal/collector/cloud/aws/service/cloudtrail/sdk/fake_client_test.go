// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
)

// fakeCloudTrailAPI implements apiClient with map-driven fixtures. It records
// every call into the `calls` slice so security tests can assert that
// forbidden CloudTrail methods are never reached.
type fakeCloudTrailAPI struct {
	trailsPages           []*awscloudtrail.ListTrailsOutput
	trailsPageIdx         int
	trail                 map[string]*awscloudtrail.GetTrailOutput
	trailStatus           map[string]*awscloudtrail.GetTrailStatusOutput
	eventSelectors        map[string]*awscloudtrail.GetEventSelectorsOutput
	insightSelectors      map[string]*awscloudtrail.GetInsightSelectorsOutput
	tags                  map[string]*awscloudtrail.ListTagsOutput
	eventDataStoresPages  []*awscloudtrail.ListEventDataStoresOutput
	eventDataStoresIdx    int
	eventDataStoreDetails map[string]*awscloudtrail.GetEventDataStoreOutput
	channelsPages         []*awscloudtrail.ListChannelsOutput
	channelsIdx           int
	channelDetails        map[string]*awscloudtrail.GetChannelOutput
	dashboardsPages       []*awscloudtrail.ListDashboardsOutput
	dashboardsIdx         int
	dashboardDetails      map[string]*awscloudtrail.GetDashboardOutput
	calls                 []string
}

func (f *fakeCloudTrailAPI) ListTrails(
	_ context.Context,
	_ *awscloudtrail.ListTrailsInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.ListTrailsOutput, error) {
	f.calls = append(f.calls, "ListTrails")
	if f.trailsPageIdx >= len(f.trailsPages) {
		return &awscloudtrail.ListTrailsOutput{}, nil
	}
	page := f.trailsPages[f.trailsPageIdx]
	f.trailsPageIdx++
	return page, nil
}

func (f *fakeCloudTrailAPI) GetTrail(
	_ context.Context,
	input *awscloudtrail.GetTrailInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetTrailOutput, error) {
	f.calls = append(f.calls, "GetTrail")
	return f.trail[aws.ToString(input.Name)], nil
}

func (f *fakeCloudTrailAPI) GetTrailStatus(
	_ context.Context,
	input *awscloudtrail.GetTrailStatusInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetTrailStatusOutput, error) {
	f.calls = append(f.calls, "GetTrailStatus")
	return f.trailStatus[aws.ToString(input.Name)], nil
}

func (f *fakeCloudTrailAPI) GetEventSelectors(
	_ context.Context,
	input *awscloudtrail.GetEventSelectorsInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetEventSelectorsOutput, error) {
	f.calls = append(f.calls, "GetEventSelectors")
	return f.eventSelectors[aws.ToString(input.TrailName)], nil
}

func (f *fakeCloudTrailAPI) GetInsightSelectors(
	_ context.Context,
	input *awscloudtrail.GetInsightSelectorsInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetInsightSelectorsOutput, error) {
	f.calls = append(f.calls, "GetInsightSelectors")
	return f.insightSelectors[aws.ToString(input.TrailName)], nil
}

func (f *fakeCloudTrailAPI) ListEventDataStores(
	_ context.Context,
	_ *awscloudtrail.ListEventDataStoresInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.ListEventDataStoresOutput, error) {
	f.calls = append(f.calls, "ListEventDataStores")
	if f.eventDataStoresIdx >= len(f.eventDataStoresPages) {
		return &awscloudtrail.ListEventDataStoresOutput{}, nil
	}
	page := f.eventDataStoresPages[f.eventDataStoresIdx]
	f.eventDataStoresIdx++
	return page, nil
}

func (f *fakeCloudTrailAPI) GetEventDataStore(
	_ context.Context,
	input *awscloudtrail.GetEventDataStoreInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetEventDataStoreOutput, error) {
	f.calls = append(f.calls, "GetEventDataStore")
	return f.eventDataStoreDetails[aws.ToString(input.EventDataStore)], nil
}

func (f *fakeCloudTrailAPI) ListChannels(
	_ context.Context,
	_ *awscloudtrail.ListChannelsInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.ListChannelsOutput, error) {
	f.calls = append(f.calls, "ListChannels")
	if f.channelsIdx >= len(f.channelsPages) {
		return &awscloudtrail.ListChannelsOutput{}, nil
	}
	page := f.channelsPages[f.channelsIdx]
	f.channelsIdx++
	return page, nil
}

func (f *fakeCloudTrailAPI) GetChannel(
	_ context.Context,
	input *awscloudtrail.GetChannelInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetChannelOutput, error) {
	f.calls = append(f.calls, "GetChannel")
	return f.channelDetails[aws.ToString(input.Channel)], nil
}

func (f *fakeCloudTrailAPI) ListDashboards(
	_ context.Context,
	_ *awscloudtrail.ListDashboardsInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.ListDashboardsOutput, error) {
	f.calls = append(f.calls, "ListDashboards")
	if f.dashboardsIdx >= len(f.dashboardsPages) {
		return &awscloudtrail.ListDashboardsOutput{}, nil
	}
	page := f.dashboardsPages[f.dashboardsIdx]
	f.dashboardsIdx++
	return page, nil
}

func (f *fakeCloudTrailAPI) GetDashboard(
	_ context.Context,
	input *awscloudtrail.GetDashboardInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.GetDashboardOutput, error) {
	f.calls = append(f.calls, "GetDashboard")
	return f.dashboardDetails[aws.ToString(input.DashboardId)], nil
}

func (f *fakeCloudTrailAPI) ListTags(
	_ context.Context,
	input *awscloudtrail.ListTagsInput,
	_ ...func(*awscloudtrail.Options),
) (*awscloudtrail.ListTagsOutput, error) {
	f.calls = append(f.calls, "ListTags")
	if len(input.ResourceIdList) == 0 {
		return &awscloudtrail.ListTagsOutput{}, nil
	}
	if output := f.tags[input.ResourceIdList[0]]; output != nil {
		return output, nil
	}
	return &awscloudtrail.ListTagsOutput{}, nil
}
