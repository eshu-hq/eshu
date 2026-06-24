// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"

	cloudtrailservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudtrail"
)

// ListEventDataStores returns metadata-only CloudTrail Lake event data store
// snapshots. The adapter calls `ListEventDataStores` and `GetEventDataStore`
// only; Lake query data-plane APIs (`StartQuery`, `GetQueryResults`,
// `CancelQuery`, `DescribeQuery`) are not reached and not in the apiClient
// surface.
func (c *Client) ListEventDataStores(ctx context.Context) ([]cloudtrailservice.EventDataStore, error) {
	arns, err := c.listEventDataStoreARNs(ctx)
	if err != nil {
		return nil, err
	}
	stores := make([]cloudtrailservice.EventDataStore, 0, len(arns))
	for _, arn := range arns {
		store, err := c.eventDataStoreMetadata(ctx, arn)
		if err != nil {
			return nil, err
		}
		stores = append(stores, store)
	}
	return stores, nil
}

func (c *Client) listEventDataStoreARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awscloudtrail.ListEventDataStoresOutput
		err := c.recordAPICall(ctx, "ListEventDataStores", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListEventDataStores(callCtx, &awscloudtrail.ListEventDataStoresInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return arns, nil
		}
		for _, store := range page.EventDataStores {
			if arn := strings.TrimSpace(aws.ToString(store.EventDataStoreArn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return arns, nil
		}
	}
}

func (c *Client) eventDataStoreMetadata(ctx context.Context, arn string) (cloudtrailservice.EventDataStore, error) {
	var output *awscloudtrail.GetEventDataStoreOutput
	err := c.recordAPICall(ctx, "GetEventDataStore", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetEventDataStore(callCtx, &awscloudtrail.GetEventDataStoreInput{
			EventDataStore: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return cloudtrailservice.EventDataStore{}, err
	}
	tags, err := c.tagsFor(ctx, arn)
	if err != nil {
		return cloudtrailservice.EventDataStore{}, err
	}
	store := cloudtrailservice.EventDataStore{
		ARN:  arn,
		Tags: tags,
	}
	if output == nil {
		return store, nil
	}
	store.Name = strings.TrimSpace(aws.ToString(output.Name))
	store.Status = strings.TrimSpace(string(output.Status))
	store.RetentionPeriod = aws.ToInt32(output.RetentionPeriod)
	store.MultiRegionEnabled = aws.ToBool(output.MultiRegionEnabled)
	store.OrganizationEnabled = aws.ToBool(output.OrganizationEnabled)
	store.TerminationProtectionEnabled = aws.ToBool(output.TerminationProtectionEnabled)
	store.BillingMode = strings.TrimSpace(string(output.BillingMode))
	store.KMSKeyID = strings.TrimSpace(aws.ToString(output.KmsKeyId))
	store.CreatedTimestamp = formatTime(output.CreatedTimestamp)
	store.UpdatedTimestamp = formatTime(output.UpdatedTimestamp)
	store.AdvancedEventSelectorCount = len(output.AdvancedEventSelectors)
	return store, nil
}

// ListChannels returns CloudTrail channel metadata for the claimed boundary.
// The adapter calls `ListChannels` and `GetChannel`; it never mutates
// channels.
func (c *Client) ListChannels(ctx context.Context) ([]cloudtrailservice.Channel, error) {
	arns, err := c.listChannelARNs(ctx)
	if err != nil {
		return nil, err
	}
	channels := make([]cloudtrailservice.Channel, 0, len(arns))
	for _, arn := range arns {
		channel, err := c.channelMetadata(ctx, arn)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, nil
}

func (c *Client) listChannelARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awscloudtrail.ListChannelsOutput
		err := c.recordAPICall(ctx, "ListChannels", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListChannels(callCtx, &awscloudtrail.ListChannelsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return arns, nil
		}
		for _, channel := range page.Channels {
			if arn := strings.TrimSpace(aws.ToString(channel.ChannelArn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return arns, nil
		}
	}
}

func (c *Client) channelMetadata(ctx context.Context, arn string) (cloudtrailservice.Channel, error) {
	var output *awscloudtrail.GetChannelOutput
	err := c.recordAPICall(ctx, "GetChannel", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetChannel(callCtx, &awscloudtrail.GetChannelInput{
			Channel: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return cloudtrailservice.Channel{}, err
	}
	tags, err := c.tagsFor(ctx, arn)
	if err != nil {
		return cloudtrailservice.Channel{}, err
	}
	channel := cloudtrailservice.Channel{
		ARN:  arn,
		Tags: tags,
	}
	if output == nil {
		return channel, nil
	}
	channel.Name = strings.TrimSpace(aws.ToString(output.Name))
	channel.Source = strings.TrimSpace(aws.ToString(output.Source))
	channel.DestinationType, channel.DestinationARN = firstDestination(output.Destinations)
	return channel, nil
}

func firstDestination(destinations []cttypes.Destination) (string, string) {
	for _, dest := range destinations {
		location := strings.TrimSpace(aws.ToString(dest.Location))
		if location == "" {
			continue
		}
		return strings.TrimSpace(string(dest.Type)), location
	}
	return "", ""
}

// ListDashboards returns CloudTrail Lake dashboard metadata. Widget query
// statements (`QueryStatement`) and widget view properties are intentionally
// excluded from the persisted contract; only the widget count is retained.
func (c *Client) ListDashboards(ctx context.Context) ([]cloudtrailservice.Dashboard, error) {
	arns, err := c.listDashboardARNs(ctx)
	if err != nil {
		return nil, err
	}
	dashboards := make([]cloudtrailservice.Dashboard, 0, len(arns))
	for _, arn := range arns {
		dashboard, err := c.dashboardMetadata(ctx, arn)
		if err != nil {
			return nil, err
		}
		dashboards = append(dashboards, dashboard)
	}
	return dashboards, nil
}

func (c *Client) listDashboardARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awscloudtrail.ListDashboardsOutput
		err := c.recordAPICall(ctx, "ListDashboards", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDashboards(callCtx, &awscloudtrail.ListDashboardsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return arns, nil
		}
		for _, dashboard := range page.Dashboards {
			if arn := strings.TrimSpace(aws.ToString(dashboard.DashboardArn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return arns, nil
		}
	}
}

func (c *Client) dashboardMetadata(ctx context.Context, arn string) (cloudtrailservice.Dashboard, error) {
	var output *awscloudtrail.GetDashboardOutput
	err := c.recordAPICall(ctx, "GetDashboard", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetDashboard(callCtx, &awscloudtrail.GetDashboardInput{
			DashboardId: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return cloudtrailservice.Dashboard{}, err
	}
	tags, err := c.tagsFor(ctx, arn)
	if err != nil {
		return cloudtrailservice.Dashboard{}, err
	}
	dashboard := cloudtrailservice.Dashboard{
		ARN:  arn,
		Tags: tags,
	}
	if output == nil {
		return dashboard, nil
	}
	dashboard.Status = strings.TrimSpace(string(output.Status))
	dashboard.Type = strings.TrimSpace(string(output.Type))
	dashboard.WidgetCount = len(output.Widgets)
	dashboard.CreatedTimestamp = formatTime(output.CreatedTimestamp)
	dashboard.UpdatedTimestamp = formatTime(output.UpdatedTimestamp)
	if output.RefreshSchedule != nil && output.RefreshSchedule.Frequency != nil {
		dashboard.RefreshSchedule = strings.TrimSpace(string(output.RefreshSchedule.Frequency.Unit))
	}
	return dashboard, nil
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
